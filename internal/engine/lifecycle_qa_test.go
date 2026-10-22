// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Ctrl-C while the VM is created, "y" to delete it: the deleted VM must
// not stay in state as the active deployment — the next deploy would
// "reuse" it and fail on SSH forever.
func TestProvisionVM_InterruptedAndDeleted_ForgetsTheVM(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), true)
	cloud, e, _, _ := provisionInterrupted(t, false)
	if len(cloud.deleted) != 1 {
		t.Fatalf("deleted = %v", cloud.deleted)
	}
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if a := sf.ActiveDeployment(); a != nil {
		t.Fatalf("deleted VM is still the active deployment: %+v", a)
	}
}

type failingDeleteCloud struct{ interruptCloud }

func (c *failingDeleteCloud) DeleteVM(context.Context, string) error {
	return stderrors.New("boom")
}

func TestProvisionVM_InterruptedDeleteFailed_KeepsTheVM(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), true)
	dir := t.TempDir()
	e := &Engine{
		Config:   config.Config{ProjectID: "p", ImageID: "img"},
		Cloud:    &failingDeleteCloud{},
		Printer:  output.NewWithWriters(&bytes.Buffer{}, &bytes.Buffer{}, output.ModePlain, false),
		StateDir: dir,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = e.provisionVM(ctx, DeployOptions{Flavor: "flavor", Yes: true}, &state.File{Version: 1}, state.DefaultPath(dir),
		&compose.Project{Hash: "h"}, testZoneID)
	assertVMRecorded(t, e)
}

// CreateVM succeeded, the state write did not: the error must name the VM,
// or it is billed with no trace anywhere.
func TestProvisionVM_StateWriteFailsAfterCreate_NamesTheVM(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Engine{
		Config:   config.Config{ProjectID: "p", ImageID: "img"},
		Cloud:    &interruptCloud{},
		Printer:  output.NewWithWriters(&bytes.Buffer{}, &bytes.Buffer{}, output.ModePlain, false),
		StateDir: dir,
	}
	rec, err := e.provisionVM(context.Background(), DeployOptions{Flavor: "flavor", Yes: true}, &state.File{Version: 1},
		filepath.Join(blocker, "state.json"), &compose.Project{Hash: "h"}, testZoneID)
	if err == nil {
		t.Fatal("state write into a file path succeeded")
	}
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) || !strings.Contains(ce.Message+ce.Hint, "vm-new") {
		t.Fatalf("error does not name the created VM: %v", err)
	}
	if rec.VMID != "vm-new" {
		t.Errorf("returned record has no VM ID: %+v", rec)
	}
}

// docker compose up failed on a new VM: the record said "running" with the
// new hash, so a plain re-run answered "No changes" (BACKLOG [05]).
func TestDeploy_ComposeUpFailsOnNewVM_RerunRetries(t *testing.T) {
	dep := newMockDeployer()
	upCmd := "cd /opt/app && docker compose -f docker-compose.yml up -d"
	dep.streamErrs = map[string][]error{upCmd: {stderrors.New("pull access denied")}}
	cloud := &mockCloudAPI{}
	eng, dir := newTestEngine(t, cloud, dep)
	opts := newDeployOpts(t, dir)

	if err := eng.Deploy(context.Background(), opts); err == nil {
		t.Fatal("deploy with failing compose up succeeded")
	}
	a := readActive(t, dir)
	if a == nil || a.Status == "running" {
		t.Fatalf("state after failed compose up = %+v, want not running", a)
	}
	if a.SSHHostKeyFingerprint == "" {
		t.Error("host key seen on first connect was not kept (TOFU would repeat)")
	}

	dep.mu.Lock()
	dep.execCalls = nil
	dep.mu.Unlock()
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	dep.mu.Lock()
	calls := strings.Join(dep.execCalls, "\n")
	dep.mu.Unlock()
	if !strings.Contains(calls, upCmd) {
		t.Fatalf("re-run did not run compose up: %s", calls)
	}
	if a := readActive(t, dir); a.Status != "running" {
		t.Errorf("status after re-run = %q", a.Status)
	}
}

// The floating IP failed on the first run: a re-run must try again rather
// than fail forever on "VM has no public IP".
func TestDeploy_RerunAfterFIPFailure_AssignsFIP(t *testing.T) {
	cloud := &mockCloudAPI{}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", Region: "az-1", SSHUser: "user1", Status: "provisioned", ComposeHash: "old"})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	a := readActive(t, dir)
	if a.VMIP != "127.0.0.1" || a.FloatingIPID != "fip-1" || a.Status != "running" {
		t.Fatalf("record = %+v, want FIP fip-1 127.0.0.1 and running", a)
	}
}

// CreateFloatingIP failed on the transport after the platform made the
// FIP: it is attached to the VM and billed, so it must land in state.
func TestAssignFloatingIP_LostResponseRecoversFIPByName(t *testing.T) {
	cloud := &mockCloudAPI{
		createFloatingIPFn: func(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
			return nil, clerrors.New(clerrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", "")
		},
		listFloatingIPsFn: func(context.Context, string) ([]*provision.FloatingIP, error) {
			return []*provision.FloatingIP{
				{ID: "fip-other", Name: "fip-vm-0", IPAddress: "198.51.100.1"},
				{ID: "fip-made", Name: "fip-vm-1", IPAddress: "198.51.100.2", State: "in_use"},
			}, nil
		},
	}
	ip, id, err := assignFloatingIP(context.Background(), cloud, fastTimings.withDefaults(), nil, "vm-1", "p", "az")
	if id != "fip-made" || ip != "198.51.100.2" {
		t.Fatalf("assign = %q %q %v, want the FIP named for the VM", ip, id, err)
	}
}

// A VM that cannot be deleted in its state (422 while it is still being
// created) gets a message that says so, and the record keeps its status.
func TestDestroy_VMBusy422_ClearMessageRecordUnchanged(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, runningRecord())
	cloud.deleteVMFn = func(context.Context, string) error {
		return clerrors.Wrap(clerrors.CodeAPIError, `API returned status 422: {"code":"vm_can_not_be_deleted"}`,
			"Cloud.ru rejected the request; the message above says why.",
			&provision.StatusError{Status: 422, Body: `{"code":"vm_can_not_be_deleted"}`})
	}
	err := eng.Destroy(context.Background(), DestroyOptions{Force: true})
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) || !strings.Contains(ce.Message, "vm-1") || !strings.Contains(ce.Hint, "re-run") {
		t.Fatalf("Destroy = %v (hint %q), want a message naming the VM and when to retry", err, hintOf(err))
	}
	if a := readActive(t, eng.StateDir); a == nil || a.Status != "running" {
		t.Fatalf("record after refused delete = %+v, want unchanged running", a)
	}
	if len(cloud.deletedFIPs) != 0 {
		t.Error("FIP released while the VM still exists")
	}
}

// A VM still being created is deleted once Cloud.ru accepts the delete:
// destroy waits instead of giving up and leaving the VM and its FIP billed
// (cloud-e2e, 15.10.2026).
func TestDestroy_WaitsWhileVMNotDeletable(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, runningRecord())
	calls := 0
	cloud.deleteVMFn = func(context.Context, string) error {
		calls++
		if calls < 3 {
			return clerrors.Wrap(clerrors.CodeAPIError, "API returned status 409", "",
				&provision.StatusError{Status: 409, Body: `{"code":"vm_is_creating"}`})
		}
		return nil
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy = %v, want success after the VM became deletable", err)
	}
	if calls != 3 {
		t.Errorf("DeleteVM calls = %d, want 3", calls)
	}
	if len(cloud.deletedFIPs) == 0 {
		t.Error("FIP not released after the VM was deleted")
	}
}

func hintOf(err error) string {
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) {
		return ce.Hint
	}
	return ""
}

// A record without vm_id (hand-edited, or from an interrupted older run):
// no DELETE /vms/ with an empty ID, the FIP is still released.
func TestDestroy_RecordWithoutVMID(t *testing.T) {
	rec := runningRecord()
	rec.VMID = ""
	eng, cloud, _ := destroyFixture(t, rec)
	var deletedIDs []string
	cloud.deleteVMFn = func(_ context.Context, id string) error {
		deletedIDs = append(deletedIDs, id)
		return nil
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(deletedIDs) != 0 {
		t.Errorf("DeleteVM called with %q", deletedIDs)
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-9" {
		t.Errorf("released = %v, want fip-9", cloud.deletedFIPs)
	}
	if a := readActive(t, eng.StateDir); a != nil {
		t.Errorf("record left: %+v", a)
	}
}

// State without FIP ID and without an address (the FIP response was lost
// on deploy): destroy finds the FIP by the name deploy gives it.
func TestDestroy_NoFIPIDNoAddress_FindsFIPByName(t *testing.T) {
	rec := runningRecord()
	rec.FloatingIPID, rec.VMIP = "", ""
	eng, cloud, _ := destroyFixture(t, rec)
	cloud.listFloatingIPsFn = func(context.Context, string) ([]*provision.FloatingIP, error) {
		return []*provision.FloatingIP{{ID: "fip-x", Name: "fip-vm-1", IPAddress: "198.51.100.7"}}, nil
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-x" {
		t.Errorf("released = %v, want fip-x", cloud.deletedFIPs)
	}
}

// The wait for deletion timed out: do not claim "VM deleted".
func TestDestroy_PollTimeoutDoesNotClaimDeleted(t *testing.T) {
	eng, cloud, out := destroyFixture(t, runningRecord())
	cloud.getVMFn = func(_ context.Context, id string) (*provision.VM, error) {
		return &provision.VM{ID: id, Status: "deleting"}, nil
	}
	eng.Timings.DestroyPollTimeout = 5 * fastTimings.DestroyPollInterval
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); !stderrors.Is(err, ErrDeletionUnconfirmed) {
		t.Fatalf("Destroy = %v, want the unconfirmed deletion", err)
	}
	if strings.Contains(out.String(), "VM deleted") {
		t.Errorf("claims the VM is deleted after a timeout:\n%s", out.String())
	}
}

// 405 «the VM has related entities» (documented for DELETE /api/v1/vms/{id},
// api-contract §4.8): a message that names the cause, the record and the
// FIP kept for the retry.
func TestDestroy_VM405_RelatedEntities(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, runningRecord())
	cloud.deleteVMFn = func(context.Context, string) error {
		return clerrors.Wrap(clerrors.CodeAPIError, "API returned status 405", "",
			&provision.StatusError{Status: 405, Body: `{"code":"vm_has_related_entities"}`})
	}
	err := eng.Destroy(context.Background(), DestroyOptions{Force: true})
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) || !strings.Contains(ce.Message, "405") || !strings.Contains(ce.Hint, "fip-release") {
		t.Fatalf("Destroy = %v (hint %q), want the 405 explained", err, hintOf(err))
	}
	if a := readActive(t, eng.StateDir); a == nil {
		t.Fatal("record dropped after a refused delete")
	}
	if len(cloud.deletedFIPs) != 0 {
		t.Error("FIP released while the VM still exists")
	}
}
