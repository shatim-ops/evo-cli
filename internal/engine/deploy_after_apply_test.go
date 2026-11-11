// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// deploy_apply records the VM running with no compose hash; the first
// deploy, its wait for SSH cut by the bound, wrote the hash and left the
// record running, and the second deploy answered "unchanged" with nothing
// uploaded: docker and compose up never ran (cloud-e2e 194, live (mcp),
// 5 of 5 since 186; Fable analysis, evo-internal #258). A deploy answers
// unchanged only when compose up for that hash has finished.
func TestDeploy_AfterApplyCutInSSHIsNotUnchanged(t *testing.T) {
	var eng *Engine
	var dir string
	var dep *mockDeployer
	err := boundedUntilHeld(t, func(bound time.Duration, held *atomic.Bool) error {
		dep = newMockDeployer()
		dep.hold = func(ctx context.Context, op string) error {
			if op == "connect" {
				held.Store(true)
				return untilCtx(ctx)
			}
			return nil
		}
		eng, dir = newTestEngine(t, &mockCloudAPI{}, dep)
		eng.Timings.SSHRetryInterval = time.Hour
		opts := newDeployOpts(t, dir)
		// The record deploy_apply leaves (tools.recordDeployment).
		sf := &state.File{}
		now := time.Now()
		sf.UpsertDeployment(state.DeploymentRecord{
			ID: "vm-1", VMID: "vm-1", VMIP: "192.0.2.10", FloatingIPID: "fip-1",
			ProjectID: eng.Config.ProjectID, SSHUser: "user1", Status: "running",
			Source: state.SourceDeployApply, PlanID: "plan-1", CreatedAt: now, UpdatedAt: now,
		})
		if err := state.Write(state.DefaultPath(dir), sf); err != nil {
			t.Fatal(err)
		}
		opts.Until = time.Now().Add(bound)
		return deployBounded(t, eng, opts)
	})
	var going *InProgress
	if !stderrors.As(err, &going) {
		t.Fatalf("first call: err = %v, want InProgress", err)
	}
	dep.hold = nil
	opts := newDeployOpts(t, dir)
	opts.Until = time.Now().Add(30 * time.Second)
	err = deployBounded(t, eng, opts)
	dep.mu.Lock()
	uploads := len(dep.uploadedFiles)
	dep.mu.Unlock()
	if uploads == 0 {
		t.Fatalf("deploy answered %v and the record is %q, but nothing was uploaded to the VM: compose up never ran",
			err, readActive(t, dir).Status)
	}
}
