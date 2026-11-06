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
		reason        string // a CLIError reason; UPLOAD_FAILED and COMPOSE_FAILED come with D87
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
		// D87, R-51 (Risk, conclusion 30): a failed upload and a failed
		// docker compose up end the deploy with a status of their own,
		// written by the same decision as the answer (writeDecided).
		{"upload", "upload_failed", "UPLOAD_FAILED", func() *mockDeployer {
			dep := newMockDeployer()
			dep.uploadErr = stderrors.New("sftp: connection lost")
			return dep
		}, nil},
		{"compose", "compose_failed", "COMPOSE_FAILED", func() *mockDeployer {
			dep := newMockDeployer()
			dep.streamErrs = map[string][]error{"cd /opt/app && docker compose -f docker-compose.yml up -d": {stderrors.New("pull access denied")}}
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

// D87, R-51: the bound of the call or a cancel in upload or in docker
// compose up is not a failure of either — the deploy goes on, and its
// record stays deploying (D76), so that the next deploy finishes it. Only
// a failure before the bound writes upload_failed or compose_failed.
func TestDeploy_UploadAndComposeAtTheBoundOrCancelStayDeploying(t *testing.T) {
	upCmd := "cd /opt/app && docker compose -f docker-compose.yml up -d"
	for _, tc := range []struct {
		name, phase string
		cancel      bool
		dep         func(cancel context.CancelFunc) *mockDeployer
	}{
		{"upload at the bound", "upload", false, func(context.CancelFunc) *mockDeployer {
			dep := newMockDeployer()
			dep.uploadErr = stderrors.New("sftp: connection lost")
			return dep
		}},
		{"compose up at the bound", "compose-up", false, func(context.CancelFunc) *mockDeployer {
			dep := newMockDeployer()
			dep.streamErrs = map[string][]error{upCmd: {stderrors.New("pull access denied")}}
			return dep
		}},
		{"upload cancelled", "upload", true, func(cancel context.CancelFunc) *mockDeployer {
			dep := newMockDeployer()
			dep.hold = func(ctx context.Context, op string) error {
				if op == "upload" {
					cancel()
					return ctx.Err()
				}
				return nil
			}
			return dep
		}},
		{"compose up cancelled", "compose-up", true, func(cancel context.CancelFunc) *mockDeployer {
			dep := newMockDeployer()
			dep.streamHook = func(ctx context.Context, cmd string) error {
				if cmd == upCmd {
					cancel()
					return ctx.Err()
				}
				return nil
			}
			return dep
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			eng, dir := newTestEngine(t, &mockCloudAPI{}, tc.dep(cancel))
			eng.Timings.VMPollInterval = time.Millisecond
			eng.Timings.SSHRetryInterval = time.Millisecond
			opts := newDeployOpts(t, dir)
			opts.Until = time.Now().Add(time.Hour)
			// The bound comes with the phase, not by the wall clock.
			eng.now = func() time.Time {
				if !tc.cancel && eng.phase == tc.phase {
					return opts.Until.Add(time.Second)
				}
				return opts.Until.Add(-time.Hour)
			}
			err := eng.Deploy(ctx, opts)
			a := readActive(t, dir)
			if a == nil || a.Status != "deploying" {
				t.Fatalf("record %+v after %s: want deploying — the deploy goes on (D76), the next one finishes it", a, tc.name)
			}
			var going *InProgress
			switch {
			case tc.cancel && err == nil:
				t.Errorf("%s: no error", tc.name)
			case !tc.cancel && !stderrors.As(err, &going):
				t.Errorf("%s: answer %v (%s), want deploy in progress (phase %s)", tc.name, err, clerrors.Reason(err), tc.phase)
			case clerrors.Reason(err) == "UPLOAD_FAILED" || clerrors.Reason(err) == "COMPOSE_FAILED":
				t.Errorf("%s: answer %s — a failure, where the record says the deploy goes on", tc.name, clerrors.Reason(err))
			}
		})
	}
}
