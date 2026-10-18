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

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// interruptCloud creates a VM and records deletions. Any other CloudAPI call
// hits the nil embedded interface and panics.
type interruptCloud struct {
	provision.CloudAPI
	deleted     []string
	deletedFIPs []string
	fipErr      error // DeleteFloatingIP fails with it, if set
}

// The deleted VM is gone at once.
func (c *interruptCloud) GetVM(context.Context, string) (*provision.VM, error) {
	return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
}

// The public IP is reserved before the VM is created.
func (c *interruptCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.7", State: "available"}, nil
}

func (c *interruptCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	if c.fipErr != nil {
		return c.fipErr
	}
	c.deletedFIPs = append(c.deletedFIPs, id)
	return nil
}

func (c *interruptCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{MetadataFields: []provision.ImageMetadataField{
		{Name: "name", DefaultValue: "user1"},
	}}, nil
}

func (c *interruptCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: "subnet-1", Name: "Default"}}, nil
}

func (c *interruptCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "sg-1", Name: "default"}}, nil
}

func (c *interruptCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}

func (c *interruptCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return []*provision.DiskType{{ID: "dt-ssd", Name: "SSD"}}, nil
}

// The preview looks names up best effort; failing lookups must not stop it.
func (c *interruptCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: "flavor", Name: "flavor"}}, nil
}

func (c *interruptCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return nil, stderrors.New("not in this test")
}

func (c *interruptCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	return &provision.VM{ID: "vm-new", Status: "creating"}, nil
}

func (c *interruptCloud) DeleteVM(_ context.Context, id string) error {
	c.deleted = append(c.deleted, id)
	return nil
}

// provisionInterrupted runs provisionVM with a context that is already
// cancelled, as after Ctrl-C in the CLI. The cancelled context is the only
// ready case of the poll select (the ticker is seconds away), so the path
// is deterministic and needs no sleeps.
func provisionInterrupted(t *testing.T, nonInteractive bool) (*interruptCloud, *Engine, *bytes.Buffer, error) {
	t.Helper()
	return provisionInterruptedWith(t, nonInteractive, &interruptCloud{})
}

func provisionInterruptedWith(t *testing.T, nonInteractive bool, cloud *interruptCloud) (*interruptCloud, *Engine, *bytes.Buffer, error) {
	t.Helper()
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	e := &Engine{
		Config:   config.Config{ProjectID: "p", ImageID: "img"},
		Cloud:    cloud,
		Printer:  output.NewWithWriters(&stdout, &stderr, output.ModePlain, false),
		StateDir: dir,
		Timings:  fastTimings,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	statePath := state.DefaultPath(dir)
	sf := &state.File{Version: 1}
	_, err := e.provisionVM(ctx, DeployOptions{Flavor: "flavor", NonInteractive: nonInteractive, Yes: true}, sf, statePath,
		&compose.Project{Hash: "h"}, "az")
	return cloud, e, &stderr, err
}

func assertVMRecorded(t *testing.T, e *Engine) {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if a := sf.ActiveDeployment(); a == nil || a.VMID != "vm-new" {
		t.Fatalf("VM not recorded in state: %+v", a)
	}
}

func TestProvisionVM_CancelledNoTTY_LeavesVMAndSaysSo(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), false)

	cloud, e, stderr, err := provisionInterrupted(t, false)

	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
	if len(cloud.deleted) != 0 {
		t.Errorf("VM deleted without a question: %v", cloud.deleted)
	}
	if strings.Contains(stderr.String(), "Delete the VM") {
		t.Errorf("asked without a terminal: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "VM vm-new left running") {
		t.Errorf("no warning that the VM is still running: %q", stderr.String())
	}
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || !strings.Contains(cliErr.Hint, "vm-new") || !strings.Contains(cliErr.Hint, "evo destroy") {
		t.Errorf("hint must name the VM and how to delete it: %v", err)
	}
	assertVMRecorded(t, e)
}

func TestProvisionVM_CancelledNonInteractiveFlag_DoesNotAsk(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), true)

	cloud, e, stderr, err := provisionInterrupted(t, true)

	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
	if len(cloud.deleted) != 0 || strings.Contains(stderr.String(), "Delete the VM") {
		t.Errorf("--non-interactive must not ask or delete: deleted=%v stderr=%q", cloud.deleted, stderr.String())
	}
	assertVMRecorded(t, e)
}

func TestProvisionVM_CancelledInteractive_AsksAndDeletesOnYes(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), true)

	cloud, _, stderr, err := provisionInterrupted(t, false)

	if !strings.Contains(stderr.String(), "Delete the VM vm-new? [y/N]") {
		t.Errorf("question not asked: %q", stderr.String())
	}
	if len(cloud.deleted) != 1 || cloud.deleted[0] != "vm-new" {
		t.Errorf("deleted = %v, want [vm-new]", cloud.deleted)
	}
	// The public IP reserved for the VM goes with it.
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-boot" {
		t.Errorf("released FIPs = %v, want [fip-boot]", cloud.deletedFIPs)
	}
	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Errorf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
}

func TestProvisionVM_CancelledInteractive_KeepsVMOnNo(t *testing.T) {
	withPromptStdin(t, strings.NewReader("\n"), true)

	cloud, e, stderr, _ := provisionInterrupted(t, false)

	if !strings.Contains(stderr.String(), "Delete the VM vm-new?") {
		t.Errorf("question not asked: %q", stderr.String())
	}
	if len(cloud.deleted) != 0 {
		t.Errorf("VM deleted on 'no': %v", cloud.deleted)
	}
	assertVMRecorded(t, e)
}

// Ctrl-C, "y": the VM is deleted, but its FIP cannot be released (still
// attached, API error). The record stays, marked destroying and still
// naming the FIP, so 'evo destroy' releases it; it used to be dropped with
// the FIP left billed and unknown to evo (QA H2).
func TestProvisionVM_CancelledInteractive_FIPNotReleasedKeepsRecord(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), true)

	cloud, e, stderr, _ := provisionInterruptedWith(t, false, &interruptCloud{fipErr: stderrors.New("floating IP is attached")})

	if len(cloud.deleted) != 1 {
		t.Fatalf("VM not deleted: %v", cloud.deleted)
	}
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	a := sf.ActiveDeployment()
	if a == nil || a.Status != "destroying" || a.FloatingIPID != "fip-boot" {
		t.Fatalf("record = %+v, want destroying with floating_ip_id fip-boot", a)
	}
	if !strings.Contains(stderr.String(), "not released") {
		t.Errorf("no warning about the FIP: %q", stderr.String())
	}
}

// stuckCloud accepts the deletion, but the VM stays listed as deleting.
type stuckCloud struct{ interruptCloud }

func (c *stuckCloud) GetVM(context.Context, string) (*provision.VM, error) {
	return &provision.VM{ID: "vm-new", Status: "deleting"}, nil
}

// The deletion is accepted but never confirmed: the record stays
// (destroying, with its FIP) so 'evo destroy' can finish — a VM stuck in
// deleting must not be billed without a trace.
func TestProvisionVM_CancelledInteractive_DeletionUnconfirmedKeepsRecord(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), true)
	cloud := &stuckCloud{}
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	timings := fastTimings
	timings.DestroyPollTimeout = 20 * time.Millisecond
	e := &Engine{
		Config:   config.Config{ProjectID: "p", ImageID: "img"},
		Cloud:    cloud,
		Printer:  output.NewWithWriters(&stdout, &stderr, output.ModePlain, false),
		StateDir: dir,
		Timings:  timings,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = e.provisionVM(ctx, DeployOptions{Flavor: "flavor", Yes: true}, &state.File{Version: 1}, state.DefaultPath(dir),
		&compose.Project{Hash: "h"}, "az")

	if len(cloud.deleted) != 1 {
		t.Fatalf("VM not deleted: %v", cloud.deleted)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if a := sf.ActiveDeployment(); a == nil || a.Status != "destroying" || a.FloatingIPID != "fip-boot" {
		t.Fatalf("record = %+v, want destroying with floating_ip_id fip-boot", a)
	}
	if !strings.Contains(stderr.String(), "not confirmed") {
		t.Errorf("no warning about the unconfirmed deletion: %q", stderr.String())
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (*interruptCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}

// errNoPriceAPI — ответ подделок на PriceCalculation: расчёт цены не
// получен, превью и план показывают оценку по карте, как раньше.
var errNoPriceAPI = stderrors.New("price-calculation: not in this fake")

// ListFloatingIPs: deploy first looks for a boot address left by an
// earlier run; there is none.
func (c *interruptCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}
