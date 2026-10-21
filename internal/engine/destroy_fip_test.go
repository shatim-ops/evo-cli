// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// destroyFixture returns an engine whose state holds rec and whose VM is
// gone right after DeleteVM.
func destroyFixture(t *testing.T, rec state.DeploymentRecord) (*Engine, *mockCloudAPI, *bytes.Buffer) {
	t.Helper()
	cloud := &mockCloudAPI{getVMFn: func(context.Context, string) (*provision.VM, error) {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(rec)
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	return eng, cloud, &out
}

func runningRecord() state.DeploymentRecord {
	return state.DeploymentRecord{
		ID: "vm-1", VMID: "vm-1", VMIP: "198.51.100.10", FloatingIPID: "fip-9",
		SSHUser: "user1", Status: "running", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

// destroy releases the floating IP recorded at deploy, as the MCP tool does;
// otherwise the address stays billed after the VM is gone.
func TestDestroy_ReleasesFloatingIPFromState(t *testing.T) {
	eng, cloud, out := destroyFixture(t, runningRecord())

	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-9" {
		t.Errorf("released FIPs = %v, want [fip-9]", cloud.deletedFIPs)
	}
	if !strings.Contains(out.String(), "Floating IP 198.51.100.10 (fip-9) released") {
		t.Errorf("output:\n%s", out.String())
	}
	if a := readActive(t, eng.StateDir); a != nil {
		t.Errorf("deployment left in state: %+v", a)
	}
}

// State written before floating_ip_id existed: the FIP is found by address.
func TestDestroy_OldStateFindsFloatingIPByAddress(t *testing.T) {
	rec := runningRecord()
	rec.FloatingIPID = ""
	eng, cloud, _ := destroyFixture(t, rec)
	cloud.listFloatingIPsFn = func(context.Context, string) ([]*provision.FloatingIP, error) {
		return []*provision.FloatingIP{
			{ID: "fip-other", IPAddress: "198.51.100.99"},
			{ID: "fip-old", IPAddress: "198.51.100.10"},
		}, nil
	}

	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-old" {
		t.Errorf("released FIPs = %v, want [fip-old]", cloud.deletedFIPs)
	}
}

func TestDestroy_NoFloatingIPNothingToRelease(t *testing.T) {
	rec := runningRecord()
	rec.FloatingIPID, rec.VMIP = "", ""
	eng, cloud, _ := destroyFixture(t, rec)

	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(cloud.deletedFIPs) != 0 {
		t.Errorf("released FIPs = %v, want none", cloud.deletedFIPs)
	}
}

// A FIP the platform already removed is not an error.
func TestDestroy_FloatingIPAlreadyGone(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, runningRecord())
	cloud.deleteFloatingIPFn = func(context.Context, string, string) error {
		return clerrors.New(clerrors.CodeNotFound, "resource not found", "")
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if a := readActive(t, eng.StateDir); a != nil {
		t.Errorf("deployment left in state: %+v", a)
	}
}

// A release that keeps failing is an API error (exit 5), not success: the
// record stays, and re-running destroy finishes the job.
func TestDestroy_FloatingIPReleaseFailureKeepsRecordForRetry(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, runningRecord())
	cloud.deleteFloatingIPFn = func(context.Context, string, string) error {
		return clerrors.New(clerrors.CodeAPIError, "API returned status 409: floating ip is in use", "")
	}

	err := eng.Destroy(context.Background(), DestroyOptions{Force: true})
	if got := clerrors.ExitCode(err); got != clerrors.ExitAPI {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitAPI)
	}
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || !strings.Contains(cliErr.Message, "fip-9") || !strings.Contains(cliErr.Hint, "destroy --yes") {
		t.Errorf("error must name the FIP and how to retry: %v", err)
	}
	if len(cloud.deletedFIPs) != fipReleaseAttempts {
		t.Errorf("release attempts = %d, want %d", len(cloud.deletedFIPs), fipReleaseAttempts)
	}
	a := readActive(t, eng.StateDir)
	if a == nil || a.Status != "destroying" || a.FloatingIPID != "fip-9" {
		t.Fatalf("record must stay for a retry: %+v", a)
	}

	// Re-run: the VM is already gone, the release now succeeds.
	cloud.deleteVMFn = func(context.Context, string) error {
		return clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	cloud.deleteFloatingIPFn = nil
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if a := readActive(t, eng.StateDir); a != nil {
		t.Errorf("deployment left in state after re-run: %+v", a)
	}
}

// The deletion wait uses Engine.Timings: a VM that never disappears costs
// DestroyPollTimeout, not minutes of real time. Unconfirmed, the destroy is
// partial: an API_ERROR (exit 5), the FIP not released while the VM may
// still hold it, the record kept as destroying (release audit, A-005).
func TestDestroy_DeletionWaitUsesTimings(t *testing.T) {
	eng, cloud, out := destroyFixture(t, runningRecord())
	cloud.getVMFn = func(_ context.Context, id string) (*provision.VM, error) {
		return &provision.VM{ID: id, Status: "deleting"}, nil
	}
	eng.Timings.DestroyPollTimeout = 20 * time.Millisecond

	// The bound is the production default, not a fixed number of seconds: a
	// loaded CI runner (Windows, -race, packages in parallel) can stretch
	// 20 ms well past any small bound, while a wait that ignored Timings
	// takes the whole default.
	start := time.Now()
	err := eng.Destroy(context.Background(), DestroyOptions{Force: true})
	if d := time.Since(start); d >= destroyPollTimeout/2 {
		t.Errorf("destroy took %s: the deletion wait ignored Engine.Timings (default %s)", d, destroyPollTimeout)
	}
	if !stderrors.Is(err, ErrDeletionUnconfirmed) || clerrors.ExitCode(err) != clerrors.ExitAPI ||
		!strings.Contains(hintOf(err), "re-run 'evo destroy'") {
		t.Fatalf("Destroy = %v (hint %q), want the unconfirmed deletion, exit 5", err, hintOf(err))
	}
	if !strings.Contains(out.String(), "Timeout waiting for VM deletion confirmation") {
		t.Errorf("output:\n%s", out.String())
	}
	if len(cloud.deletedFIPs) != 0 {
		t.Errorf("FIP released while the VM may still hold it: %v", cloud.deletedFIPs)
	}
	if a := readActive(t, eng.StateDir); a == nil || a.Status != "destroying" || a.FloatingIPID != "fip-9" {
		t.Errorf("record = %+v, want it kept as destroying with its FIP", a)
	}
}

// A record of another project whose VM the active key does not see: the
// 404 of DELETE /vms/{id} (no project in it) does not prove the VM is
// gone, and "already deleted" would drop the record of a billed VM.
// destroy refuses and keeps the record (ревью Fable, A2). A key that sees
// the record's project still deletes there (TestDestroy_UsesRecordedProject).
func TestDestroy_RecordOfAnotherProject404Refused(t *testing.T) {
	rec := runningRecord()
	rec.ProjectID = "p-other"
	eng, cloud, _ := destroyFixture(t, rec)
	eng.Config.ProjectID = "p-active"
	cloud.deleteVMFn = func(context.Context, string) error {
		return clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")
	}
	err := eng.Destroy(context.Background(), DestroyOptions{Force: true})
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeConfigInvalid || !strings.Contains(ce.Message, "p-other") ||
		!strings.Contains(ce.Hint, "evo project use p-other") {
		t.Fatalf("Destroy = %v (hint %q)", err, hintOf(err))
	}
	if a := readActive(t, eng.StateDir); a == nil || a.Status != "running" || a.ProjectID != "p-other" || a.FloatingIPID != "fip-9" {
		t.Errorf("record after refusal = %+v, want unchanged", a)
	}
	if len(cloud.deletedFIPs) != 0 {
		t.Errorf("FIPs released: %v", cloud.deletedFIPs)
	}

	// In the record's project the same 404 means the VM is gone.
	eng.Config.ProjectID = "p-other"
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy in the record's project: %v", err)
	}
	if a := readActive(t, eng.StateDir); a != nil {
		t.Errorf("deployment left in state: %+v", a)
	}
}
