// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// D69, PR 2: provision under the budget of an MCP call answers the VM it
// made and its state so far, not an error; the repeat of the call adopts
// that VM and finishes it, and makes no second one.

// provisionWorld is the slow cloud with a budgeted registry over dir.
func provisionWorld(t *testing.T) (*faultCloud, string, *Registry) {
	t.Helper()
	faultToolsIsolate(t)
	cloud := &faultCloud{slow: true, imagePassword: true}
	dir := t.TempDir()
	return cloud, dir, budgeted(budgetComputeRegistry(t, cloud, dir, provisionTimings()))
}

// provisionTimings polls at once and waits for the VM as long as production
// does: in the slow cloud only the budget ends the call.
func provisionTimings() engine.Timings {
	t := faultToolsTimings()
	t.VMPollTimeout = time.Hour
	return t
}

func TestD69_ProvisionAnswersCreatingWithTheNextStep(t *testing.T) {
	cloud, dir, r := provisionWorld(t)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "ports": []any{"80"}, "confirmed": true})
	m := decode(t, text)
	if isErr || m["status"] != "creating" || m["vm_id"] == nil || m["vm_id"] == "" {
		t.Fatalf("want creating with vm_id: %s", text)
	}
	if m["vm_status"] != "creating" {
		t.Errorf("vm_status %v, want the state read last (creating): %s", m["vm_status"], text)
	}
	if p, _ := m["password"].(string); p == "" {
		t.Errorf("the password made for this VM is lost: %s", text)
	}
	if m["next_tool"] != "provision" {
		t.Errorf("next_tool %v, want provision: %s", m["next_tool"], text)
	}
	args, _ := m["next_args"].(map[string]any)
	if args["name"] != "web" || args["confirmed"] != true || args["ports"] == nil {
		t.Errorf("next_args do not repeat the call: %v", args)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil || len(sf.Deployments) != 1 || sf.Deployments[0].Status != "creating" || sf.Deployments[0].VMName != "web" {
		t.Errorf("the VM is not in the state as creating, with its name: %+v %v", sf, err)
	}
	if n := len(cloud.liveVMs()); n != 1 {
		t.Errorf("%d VMs, want 1", n)
	}
}

// The repeat adopts the VM the first call made: no second VM, the VM
// finished (public IP, the ports, the record running), and no password —
// the first answer had it.
func TestD69_ProvisionRepeatAdoptsItsVM(t *testing.T) {
	cloud, dir, r := provisionWorld(t)
	first := decode(t, mustCall(t, r, "provision", map[string]any{"name": "web", "ports": []any{"80"}, "confirmed": true}))
	if first["status"] != "creating" {
		t.Fatalf("first call: %v", first)
	}
	next, _ := first["next_args"].(map[string]any)

	// The preview of the repeat says it continues that VM.
	preview := decode(t, mustCallErr(t, r, "provision", map[string]any{"name": "web", "ports": []any{"80"}}))
	if adopts, _ := preview["adopts"].(map[string]any); adopts["vm_id"] != first["vm_id"] {
		t.Errorf("the preview of the repeat does not name the VM it continues: %v", preview)
	}

	cloud.mu.Lock()
	cloud.slow = false
	for i := range cloud.vms {
		cloud.vms[i].Status = "active"
	}
	cloud.mu.Unlock()
	text, isErr := r.handlers["provision"](context.Background(), next)
	m := decode(t, text)
	if isErr || m["status"] != "ok" || m["vm_id"] != first["vm_id"] {
		t.Fatalf("the repeat does not finish the same VM: %s", text)
	}
	if _, ok := m["password"]; ok {
		t.Errorf("the repeat names a password it does not know: %s", text)
	}
	if n := len(cloud.liveVMs()); n != 1 {
		t.Errorf("%d VMs after the repeat, want 1", n)
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	if len(sf.Deployments) != 1 || sf.Deployments[0].Status != "running" || len(sf.Deployments[0].OpenedRules) == 0 {
		t.Errorf("the record is not the finished VM with its port: %+v", sf.Deployments)
	}
}

// wait=false answers as soon as the cloud took the create, on any surface.
func TestD69_ProvisionWaitFalseDoesNotWait(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &faultCloud{slow: true}
	r := budgetComputeRegistry(t, cloud, t.TempDir(), provisionTimings())
	done := make(chan string, 1)
	go func() {
		text, _ := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true, "wait": false})
		done <- text
	}()
	select {
	case text := <-done:
		if m := decode(t, text); m["status"] != "creating" || m["next_tool"] != "provision" {
			t.Errorf("wait=false: want creating with the next step: %s", text)
		}
	// wall-clock: a share (1/45) of D69's 45 s budget; wait=false waits
	// for nothing.
	case <-time.After(budgetWatch):
		t.Fatal("wait=false waited for the VM")
	}
}

// A name of no VM evo recorded is a new VM, as before: the state names the
// VMs a repeat may adopt, and nothing else does (D73 keeps others' VMs).
func TestD69_ProvisionAdoptsOnlyARecordOfItsName(t *testing.T) {
	cloud, _, r := provisionWorld(t)
	mustCall(t, r, "provision", map[string]any{"name": "web", "confirmed": true})
	text := mustCall(t, r, "provision", map[string]any{"name": "api", "confirmed": true})
	if m := decode(t, text); m["status"] != "creating" {
		t.Fatalf("another name: %s", text)
	}
	if n := len(cloud.liveVMs()); n != 2 {
		t.Errorf("%d VMs, want 2: another name is another VM", n)
	}
}

func mustCall(t *testing.T, r *Registry, tool string, args map[string]any) string {
	t.Helper()
	text, isErr := r.handlers[tool](context.Background(), args)
	if isErr {
		t.Fatalf("%s: %s", tool, text)
	}
	return text
}

// mustCallErr is a call that answers isError: a preview
// (confirmation_required) is one.
func mustCallErr(t *testing.T, r *Registry, tool string, args map[string]any) string {
	t.Helper()
	text, isErr := r.handlers[tool](context.Background(), args)
	if !isErr {
		t.Fatalf("%s: want a preview: %s", tool, text)
	}
	return text
}

// The repeat adopts the VM by the id its record holds, not by the name: a
// VM of the name made between the calls by someone else is not adopted
// (D73: state ↔ cloud). Here the recorded VM is gone and another VM has
// its name — the taken name is refused before the question (D73; before,
// the create met the 409), the other VM is left as it is and never
// recorded. The gone VM's record stays until a call that writes: the plan
// before the question writes nothing (creatingVM).
func TestD69_ProvisionAdoptsByTheRecordedIDNotTheName(t *testing.T) {
	cloud, dir, r := provisionWorld(t)
	first := decode(t, mustCall(t, r, "provision", map[string]any{"name": "web", "confirmed": true}))
	cloud.mu.Lock()
	for i := range cloud.vms {
		if cloud.vms[i].ID == first["vm_id"] {
			cloud.vms[i].gone = true
		}
	}
	foreign := faultVM{ID: faultID(0x77), Name: "web", Status: "active", CreatedAt: time.Now()}
	cloud.vms = append(cloud.vms, foreign)
	cloud.mu.Unlock()

	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	m := decode(t, text)
	if !isErr || m["reason"] != "USAGE" || m["vm_id"] == foreign.ID {
		t.Fatalf("a VM of the name with another id is adopted, or the taken name is not refused: %s", text)
	}
	for _, vm := range cloud.liveVMs() {
		if vm.ID == foreign.ID && vm.Status != "active" {
			t.Errorf("the other VM was touched: %+v", vm)
		}
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	for _, d := range sf.Deployments {
		if d.VMID == foreign.ID {
			t.Errorf("the state holds %s: the other VM is not this folder's", d.VMID)
		}
	}
}
