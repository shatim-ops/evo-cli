package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// targetCloud has one VM whose list entry carries no floating IP (as the
// API's VM list does); its FIP is attached until the VM is gone.
type targetCloud struct {
	provision.CloudAPI
	deleteErr   error
	vmGone      bool
	fipTries    int
	releasedFIP string
}

func (*targetCloud) SetToken(string) {}
func (*targetCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "vm-1", Name: "web"}}, nil
}
func (c *targetCloud) DeleteVMInProject(context.Context, string, string) error {
	if c.deleteErr != nil {
		return c.deleteErr
	}
	c.vmGone = true
	return nil
}
func (c *targetCloud) GetVM(context.Context, string) (*provision.VM, error) {
	if c.vmGone {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	return &provision.VM{ID: "vm-1", Status: "creating"}, nil
}
func (c *targetCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.fipTries++
	if c.fipTries < 2 {
		return clerrors.New(clerrors.CodeAPIError, "floating ip is attached", "")
	}
	c.releasedFIP = id
	return nil
}
func (*targetCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}

func targetRegistry(t *testing.T, cloud provision.CloudAPI) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "203.0.113.5", FloatingIPID: "fip-1", Status: "running"})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: "p", KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
			Timings:  engine.Timings{FIPRetryInterval: time.Millisecond, DestroyPollInterval: time.Millisecond, DestroyPollTimeout: time.Second},
		}, nil
	})
	return r, dir
}

// destroy vm_id=… releases the VM's FIP named in the state, retrying while
// it is still attached; it used to read the list's empty flat field and
// release nothing (QA H4).
func TestDestroyTargeted_ReleasesFIPFromState(t *testing.T) {
	cloud := &targetCloud{}
	r, _ := targetRegistry(t, cloud)
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-1", "confirmed": true})
	if isErr {
		t.Fatalf("destroy: %s", text)
	}
	if cloud.releasedFIP != "fip-1" || cloud.fipTries < 2 {
		t.Fatalf("released %q after %d tries, want fip-1 after a retry", cloud.releasedFIP, cloud.fipTries)
	}
	if !strings.Contains(text, "released floating IP 203.0.113.5") {
		t.Errorf("answer does not name the released FIP: %s", text)
	}
}

// A VM still being created: a clear "come back later", nothing deleted.
func TestDestroyTargeted_NotDeletableYet(t *testing.T) {
	cloud := &targetCloud{deleteErr: clerrors.Wrap(clerrors.CodeAPIError, "API returned status 409", "",
		&provision.StatusError{Status: 409, Body: "creating"})}
	r, _ := targetRegistry(t, cloud)
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-1", "confirmed": true})
	if !isErr || !strings.Contains(text, "not_deletable_yet") || !strings.Contains(text, "again") {
		t.Fatalf("answer = %s", text)
	}
	if cloud.fipTries != 0 {
		t.Errorf("FIP touched while the VM still exists")
	}
}

// goneCloud: the VM was deleted outside evo — not in the list, 404 on GET;
// its FIP is free to release.
type goneCloud struct {
	targetCloud
	getCalls int
}

func (*goneCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, nil
}
func (c *goneCloud) GetVM(context.Context, string) (*provision.VM, error) {
	c.getCalls++
	return nil, clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")
}
func (c *goneCloud) DeleteVMInProject(context.Context, string, string) error {
	panic("the VM is gone: nothing to delete")
}
func (c *goneCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.releasedFIP = id
	return nil
}

// destroy vm_id=… for a VM deleted outside evo: the record goes (and its
// FIP is released), the answer says "already deleted". It used to answer
// "no VM matching" and keep the record, so status showed it forever.
func TestDestroyTargeted_AlreadyGoneCleansState(t *testing.T) {
	cloud := &goneCloud{}
	r, dir := targetRegistry(t, cloud)
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-1", "confirmed": true})
	if isErr || !strings.Contains(text, `"status": "already_deleted"`) || !strings.Contains(text, "already deleted") {
		t.Fatalf("answer = %s", text)
	}
	if cloud.releasedFIP != "fip-1" {
		t.Errorf("released FIP %q, want fip-1", cloud.releasedFIP)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range sf.Deployments {
		if d.VMID == "vm-1" && d.Status != "destroyed" {
			t.Errorf("record kept: %+v", d)
		}
	}
	if a := sf.ActiveDeployment(); a != nil {
		t.Errorf("active deployment left: %+v", a)
	}

	// A VM this directory never recorded: VM_NOT_FOUND (exit 4, as vm_get —
	// release audit, A-004), and the cloud is not asked.
	cloud = &goneCloud{}
	r, _ = targetRegistry(t, cloud)
	text, isErr = r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-other", "confirmed": true})
	if !isErr || !strings.Contains(text, `"reason": "VM_NOT_FOUND"`) || !strings.Contains(text, "vm-other") || cloud.getCalls != 0 {
		t.Errorf("unrecorded VM: answer %s, GET calls %d", text, cloud.getCalls)
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (*targetCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}

// failingGetCloud: the VM is not in the list and GET /vms/{id} fails with
// something other than 404 — the cloud did not say the VM is gone.
type failingGetCloud struct{ goneCloud }

func (*failingGetCloud) GetVM(context.Context, string) (*provision.VM, error) {
	return nil, clerrors.New(clerrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", "")
}

// destroy vm_id for a recorded VM missing from the list whose GET fails:
// the answer says the check failed (not "no VM matching") and the record
// stays, nothing released.
func TestDestroyTargeted_GetFailsKeepsRecord(t *testing.T) {
	cloud := &failingGetCloud{}
	r, dir := targetRegistry(t, cloud)
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-1", "confirmed": true})
	if !isErr || strings.Contains(text, "no VM matching") || !strings.Contains(text, "record is kept") {
		t.Fatalf("answer = %s", text)
	}
	if cloud.releasedFIP != "" {
		t.Errorf("FIP released: %q", cloud.releasedFIP)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if a := sf.ActiveDeployment(); a == nil || a.VMID != "vm-1" || a.Status != "running" {
		t.Errorf("record changed: %+v", a)
	}
}

// Запись ВМ другого проекта: 404 на GET /vms/{id} не доказывает, что ВМ нет
// — ничего не удаляется, запись остаётся (ревью Fable на #81).
func TestDestroyTargeted_RecordOfAnotherProject(t *testing.T) {
	cloud := &goneCloud{}
	r, dir := targetRegistry(t, cloud)
	path := state.DefaultPath(dir)
	sf, err := state.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range sf.Deployments {
		sf.Deployments[i].ProjectID = "p-other"
	}
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-1", "confirmed": true})
	if !isErr || !strings.Contains(text, "another project") || !strings.Contains(text, `"reason": "CONFIG_INVALID"`) ||
		cloud.getCalls != 0 || cloud.releasedFIP != "" {
		t.Fatalf("answer %s, GET calls %d, released %q", text, cloud.getCalls, cloud.releasedFIP)
	}
	sf, _ = state.Read(path)
	if a := sf.ActiveDeployment(); a == nil || a.VMID != "vm-1" {
		t.Errorf("record dropped: %+v", sf.Deployments)
	}
}
