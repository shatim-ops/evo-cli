// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// ownRefusal reads the refusal of D73: USAGE, the VM's id in existing_id,
// and the way out — another name, or the person's own delete.
func ownRefusal(t *testing.T, tool, text string, isErr bool, id string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || !isErr {
		t.Fatalf("%s: want a refusal (isError=%v): %s", tool, isErr, text)
	}
	if m["reason"] != "USAGE" || m["existing_id"] != id {
		t.Errorf("%s: want USAGE with existing_id %s: %s", tool, id, text)
	}
	if hint, _ := m["hint"].(string); !strings.Contains(hint, "name") {
		t.Errorf("%s: the hint names no way out (another name): %s", tool, text)
	}
}

// A VM recorded in the state of folder A is not folder B's: deploy_apply
// from B with a plan of the same name refuses it with its id, it does not
// adopt it (D73; «1.7 — релиз», condition 3 — the MCP server works from its
// own folder, as the CLI does).
func TestDeployApply_AVMOfAnotherFolderIsNotAdopted(t *testing.T) {
	vm := &provision.VM{ID: "0000a1a1-0000-4000-8000-000000000001", Name: "wiki-1", Status: "running",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	cloud := &sgCloud{vm: vm}
	_, dirA := sgRegistry(t, cloud)
	writeRecords(t, dirA, state.DeploymentRecord{ID: vm.ID, VMID: vm.ID, VMName: vm.Name, Status: "running", CreatedAt: vm.CreatedAt})
	rB, dirB := sgRegistry(t, cloud)
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Product: "wiki",
		VM: deployplan.VMSpec{Name: "wiki-1"}, Network: deployplan.NetworkSpec{SecurityGroupID: "sg-1"}, Ports: []int{80}}
	if err := deployplan.Save(dirB, plan); err != nil {
		t.Fatal(err)
	}
	text, isErr := rB.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": plan.PlanID, "confirmed": true})
	ownRefusal(t, "deploy_apply", text, isErr, vm.ID)
	if sf, err := state.Read(state.DefaultPath(dirB)); err == nil && len(sf.Deployments) != 0 {
		t.Errorf("folder B recorded someone else's VM: %+v", sf.Deployments)
	}
}

// vmPlanCloud is planCloud with VMs in the project.
type vmPlanCloud struct {
	planCloud
	vms []*provision.VM
}

func (c vmPlanCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return c.vms, nil
}

func ownPlanRegistry(t *testing.T, cloud provision.CloudAPI, dir string) *Registry {
	t.Helper()
	cfg := config.Config{Region: planZoneAZ2, ProjectID: faultProject, KeyID: "k", Secret: "s"}
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{Config: cfg, Auth: staticToken{}, Cloud: cloud, StateDir: dir,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
			SSHKeyLoader: func() ([]byte, string, error) {
				return []byte("key"), "ssh-ed25519 AAAA test", nil
			}}, nil
	})
	r.SetStateDir(dir)
	return r
}

// deploy_plan with the name of a VM the folder did not make refuses it
// before any plan, with its id (D73, the mechanism of D49); the folder's
// own VM — recorded in its state — is planned again (§1, idempotence).
func TestDeployPlan_ANameTakenBySomeoneElseIsRefused(t *testing.T) {
	vm := &provision.VM{ID: "00000000-0000-4000-8000-0000000000e1", Name: "web", Status: "running"}
	dir := t.TempDir()
	r := ownPlanRegistry(t, vmPlanCloud{vms: []*provision.VM{vm}}, dir)
	text, isErr := r.handlers["deploy_plan"](context.Background(), map[string]any{"name": "web"})
	ownRefusal(t, "deploy_plan", text, isErr, vm.ID)

	writeRecords(t, dir, state.DeploymentRecord{ID: vm.ID, VMID: vm.ID, VMName: vm.Name, ProjectID: faultProject, Status: "running"})
	if text, isErr := r.handlers["deploy_plan"](context.Background(), map[string]any{"name": "web"}); isErr {
		t.Errorf("the folder's own VM is refused: %s", text)
	}
}

// provision with the name of a VM it did not leave creating refuses it
// before the question, with its id (D73): before, the create went out after
// consent and Cloud.ru answered 409.
func TestProvision_ANameTakenBySomeoneElseIsRefusedBeforeTheQuestion(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &faultCloud{}
	vm, err := cloud.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web", ProjectID: faultProject})
	if err != nil {
		t.Fatal(err)
	}
	r := budgetComputeRegistry(t, cloud, t.TempDir(), faultToolsTimings())
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web"})
	ownRefusal(t, "provision", text, isErr, vm.ID)
}

// D73, the second part: destroy by vm_id of a VM this folder records, but
// one an evo before 1.7 adopted (made long before its record), refuses
// before the question — the record held by an adoption is not a VM evo
// made; the person deletes it if it is theirs. A VM made with its record
// goes as before.
func TestDestroyTargeted_ARecordOfAnAdoptedVMIsNotDeleted(t *testing.T) {
	recorded := time.Now().Add(-time.Hour)
	vm := &provision.VM{ID: "0000a1a1-0000-4000-8000-000000000001", Name: "wiki-1", Status: "running",
		CreatedAt: recorded.Add(-24 * time.Hour)}
	cloud := &sgCloud{vm: vm}
	r, dir := sgRegistry(t, cloud)
	writeRecords(t, dir, state.DeploymentRecord{ID: vm.ID, VMID: vm.ID, VMName: vm.Name, Status: "running", CreatedAt: recorded})
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": vm.ID})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	if !isErr || m["reason"] != "USAGE" || m["status"] == "confirmation_required" {
		t.Fatalf("an adopted VM's destroy: want USAGE before the question: %s", text)
	}
	if hint, _ := m["hint"].(string); !strings.Contains(hint, "vm_delete") {
		t.Errorf("the hint names no way out for the person (vm_delete): %s", text)
	}
	if sf, err := state.Read(state.DefaultPath(dir)); err != nil || len(sf.Deployments) != 1 {
		t.Errorf("the record is not kept: %+v", sf)
	}
}

// Agent run of 1.7.0 (m0, F15; findings 2.1, №10): provision with the name
// of a VM this folder's state records, and not creating, is refused (D73),
// but the refusal said «this folder's evo did not create it (its state does
// not record it)» — false: the state records it. The refusal says the VM
// is this folder's and names it, with USAGE and existing_id as before.
func TestProvision_RecordedOwnVMRefusalDoesNotSayUnrecorded(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &faultCloud{}
	vm, err := cloud.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web", ProjectID: faultProject})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeRecords(t, dir, state.DeploymentRecord{ID: vm.ID, VMID: vm.ID, VMName: "web", Status: "running", Source: "provision", CreatedAt: time.Now()})
	r := budgetComputeRegistry(t, cloud, dir, faultToolsTimings())
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web"})
	ownRefusal(t, "provision", text, isErr, vm.ID)
	if strings.Contains(text, "did not create it") || strings.Contains(text, "does not record it") {
		t.Errorf("the refusal says the folder's state does not record a VM it records: %s", text)
	}
	if !strings.Contains(text, "this folder") {
		t.Errorf("the refusal does not say the VM is this folder's: %s", text)
	}
}
