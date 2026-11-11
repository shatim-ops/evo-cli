// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
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
	if previewUnchanged(t, eng, newDeployOpts(t, dir).ComposeFile) {
		t.Error("the preview says unchanged (confirmed=true changes nothing) of a deployment compose up never ran on")
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

// The same class without deploy_apply: a running deployment whose compose
// file changed, the wait for SSH cut by the bound, left the new hash on the
// running record, and the retry answered "unchanged" while the VM ran the
// old services.
func TestDeploy_ChangedComposeCutInSSHIsNotUnchanged(t *testing.T) {
	dep := newMockDeployer()
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("first deploy: %v", err)
	}
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(testComposeContent+"# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dep.mu.Lock()
	before := len(dep.uploadedFiles)
	dep.mu.Unlock()
	err := boundedUntilHeld(t, func(bound time.Duration, held *atomic.Bool) error {
		dep.hold = func(ctx context.Context, op string) error {
			if op == "connect" {
				held.Store(true)
				return untilCtx(ctx)
			}
			return nil
		}
		eng.Timings.SSHRetryInterval = time.Hour
		opts := DeployOptions{ComposeFile: path, Flavor: "small", NonInteractive: true, Yes: true}
		opts.Until = time.Now().Add(bound)
		return deployBounded(t, eng, opts)
	})
	var going *InProgress
	if !stderrors.As(err, &going) {
		t.Fatalf("changed compose, cut in SSH: err = %v, want InProgress", err)
	}
	if previewUnchanged(t, eng, path) {
		t.Error("the preview says unchanged of a changed compose file the VM never got")
	}
	dep.hold = nil
	opts := DeployOptions{ComposeFile: path, Flavor: "small", NonInteractive: true, Yes: true}
	opts.Until = time.Now().Add(30 * time.Second)
	err = deployBounded(t, eng, opts)
	dep.mu.Lock()
	after := len(dep.uploadedFiles)
	dep.mu.Unlock()
	if after == before {
		t.Fatalf("deploy answered %v and the record is %q, but the changed compose file was never uploaded: the VM runs the old services",
			err, readActive(t, dir).Status)
	}
}

// A record an earlier deploy brought to running (not deploy_apply's) with
// the hash of the compose file stays "unchanged": the fix must not redeploy
// every working deployment after an upgrade.
func TestDeploy_RunningRecordOfADeployStaysUnchanged(t *testing.T) {
	dep := newMockDeployer()
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	opts := newDeployOpts(t, dir)
	sf := &state.File{}
	now := time.Now()
	sf.UpsertDeployment(state.DeploymentRecord{
		ID: "vm-1", VMID: "vm-1", VMIP: "192.0.2.10", FloatingIPID: "fip-1",
		ProjectID: eng.Config.ProjectID, SSHUser: "user1", Status: "running",
		ComposeHash: composeHash(), ComposePath: opts.ComposeFile, CreatedAt: now, UpdatedAt: now,
	})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	if !previewUnchanged(t, eng, opts.ComposeFile) {
		t.Error("the preview of a finished deploy with the same hash must say unchanged, as deploy does")
	}
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	dep.mu.Lock()
	uploads := len(dep.uploadedFiles)
	dep.mu.Unlock()
	if uploads != 0 {
		t.Errorf("a running record of a finished deploy with the same hash: %d files uploaded, want 0 (unchanged)", uploads)
	}
}

// previewUnchanged is what the preview of deploy (DeployUpdateOf, the
// answer without confirmed=true) says of the state now: one decision with
// the skip of Deploy — the preview never promises "nothing to do" where
// deploy would do something, nor the other way round.
func previewUnchanged(t *testing.T, eng *Engine, compose string) bool {
	t.Helper()
	eng.Config.ComposeFile = compose
	u := eng.DeployUpdateOf(false)
	return u != nil && u.Unchanged
}
