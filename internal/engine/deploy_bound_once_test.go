// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// FB171-4: the record and the answer of deploy at the bound of an MCP call
// come from one decision. A step that failed before the bound writes its
// record as failed (ssh_timeout, docker_failed); if the bound comes while
// that record is written — a slow disk, Windows of release.yml run
// 41983811886 — the answer still says the step failed. It said deploying
// with next_tool deploy, and the record said the deploy was over.
//
// The bound here comes at a point, not by the wall clock: the clock of the
// bound (Engine.now) reads «past» once the record with that status is on
// disk, between the write and the end of Deploy.
func TestDeploy_RecordAndAnswerAtTheBoundAreOneDecision(t *testing.T) {
	for _, tc := range []struct {
		phase, status string
		reason        string
		dep           func() *mockDeployer
		cloud         *mockCloudAPI
	}{
		{"vm", "error", clerrors.CodeVMProvisionFailed, newMockDeployer, &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
			return &provision.VM{ID: id, Status: "error_creating"}, nil
		}}},
		{"ssh", "ssh_timeout", clerrors.CodeSSHTimeout, func() *mockDeployer {
			dep := newMockDeployer()
			dep.connectErr = stderrors.New("ssh: connection timed out")
			return dep
		}, nil},
		{"docker", "docker_failed", clerrors.CodeSSHTimeout, func() *mockDeployer {
			dep := newMockDeployer()
			dep.streamHook = func(_ context.Context, cmd string) error {
				if strings.Contains(cmd, "base64 -d") {
					return stderrors.New("exit status 6")
				}
				return nil
			}
			return dep
		}, nil},
	} {
		t.Run(tc.phase, func(t *testing.T) {
			cloud := tc.cloud
			if cloud == nil {
				cloud = &mockCloudAPI{}
			}
			eng, dir := newTestEngine(t, cloud, tc.dep())
			eng.Timings.VMPollInterval = time.Millisecond
			eng.Timings.SSHRetryInterval = time.Millisecond
			eng.Timings.DockerWaitTimeout = 20 * time.Millisecond
			opts := newDeployOpts(t, dir)
			// The bound of the call, far on the wall clock: only Engine.now
			// brings it.
			opts.Until = time.Now().Add(time.Hour)
			eng.now = func() time.Time {
				if sf, err := state.Read(state.DefaultPath(dir)); err == nil {
					if a := sf.ActiveDeployment(); a != nil && a.Status == tc.status {
						return opts.Until.Add(time.Second)
					}
				}
				return opts.Until.Add(-time.Hour)
			}

			err := eng.Deploy(context.Background(), opts)
			a := readActive(t, dir)
			if a == nil || a.Status != tc.status {
				t.Fatalf("record %+v, want %s: the step failed before the bound", a, tc.status)
			}
			var going *InProgress
			if stderrors.As(err, &going) {
				t.Fatalf("the record says %s, the answer says deploy goes on (phase %s): one decision for both", a.Status, going.Phase)
			}
			if clerrors.Reason(err) != tc.reason {
				t.Errorf("answer %v (%s), want %s as the record says", err, clerrors.Reason(err), tc.reason)
			}
		})
	}
}
