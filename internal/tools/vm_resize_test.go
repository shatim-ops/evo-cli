// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

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

// vm_resize changes the flavor of a VM (docs/internal/briefs/ops-wave-1.md
// §6). Its table «what is read → the answer» (§6.2) has a world per row
// here; the writes are counted by the world (resizeCalls).

// The flavors of the worlds of vm_resize: the VM's own (faultFlavor,
// low-1-1), one step up, an interruptible one, and one of another zone.
const (
	resizeUp    = "00000000-0000-4000-8000-0000000000c2"
	resizeSpot  = "00000000-0000-4000-8000-0000000000c3"
	resizeOther = "00000000-0000-4000-8000-0000000000c4"
)

func resizeCatalogue() []*provision.Flavor {
	zone := []provision.FlavorZone{{ID: faultZone, Name: "ru.AZ-1", Enabled: true}}
	gpu := 0
	return []*provision.Flavor{
		{ID: faultFlavor, Name: "low-1-1", VCPUs: 1, RAMGB: 1, Share: "1:1", GPU: &gpu, Type: &provision.FlavorType{}, Zones: zone},
		{ID: resizeUp, Name: "low-2-4", VCPUs: 2, RAMGB: 4, Share: "1:1", GPU: &gpu, Type: &provision.FlavorType{}, Zones: zone},
		{ID: resizeSpot, Name: "spot-2-4", VCPUs: 2, RAMGB: 4, Share: "1:10", GPU: &gpu, Type: &provision.FlavorType{IsSpot: true}, Zones: zone},
		{ID: resizeOther, Name: "far-2-4", VCPUs: 2, RAMGB: 4, Share: "1:1", GPU: &gpu, Type: &provision.FlavorType{},
			Zones: []provision.FlavorZone{{ID: "00000000-0000-4000-8000-0000000000a2", Name: "ru.AZ-2", Enabled: true}}},
	}
}

// vmResizeWorld is a registry over a world with one VM named web on
// low-1-1 in state, the catalogue of resizeCatalogue, its own waits ending
// in 50 ms; dir is the folder's state directory.
func vmResizeWorld(t *testing.T, state string) (*Registry, *powerWorld, string) {
	t.Helper()
	return vmResizeWorldWaiting(t, state, 50*time.Millisecond)
}

func vmResizeWorldWaiting(t *testing.T, state string, ownLimit time.Duration) (*Registry, *powerWorld, string) {
	t.Helper()
	c := &faultCloud{flavors: resizeCatalogue()}
	vm, err := c.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	c.setVMState(vm.ID, state)
	w := &powerWorld{faultCloud: c, id: vm.ID}
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			StateDir: dir,
			Config:   config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    w,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			Timings:  engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: ownLimit},
		}, nil
	})
	r.SetStateDir(dir)
	return r, w, dir
}

// The flavor the VM of the world has now, by its card.
func (w *powerWorld) flavorNow(t *testing.T) string {
	t.Helper()
	d, err := w.faultCloud.GetVMDetail(context.Background(), w.id)
	if err != nil || d.Flavor == nil {
		t.Fatalf("read of the VM: %v", err)
	}
	return d.Flavor.ID
}

// recordOn writes a record of this folder's deployment on the VM.
func recordOn(t *testing.T, dir, vmID string) {
	t.Helper()
	if err := state.Update(state.DefaultPath(dir), stateLockWait, func(sf *state.File) error {
		sf.UpsertDeployment(state.DeploymentRecord{ID: "d-1", VMID: vmID, VMName: "web", Status: "running",
			VMFlavor: faultFlavor, FlavorName: "low-1-1"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// recordFlavor is the flavor the folder's record of the VM names.
func recordFlavor(t *testing.T, dir, vmID string) (id, name string) {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range sf.ActiveDeployments() {
		if d.VMID == vmID {
			return d.VMFlavor, d.FlavorName
		}
	}
	t.Fatal("no record of the VM")
	return "", ""
}

// §6.2, row 1: a target the project does not have is USAGE with the
// names it has; nothing is sent, with consent or without (axis 1).
func TestVMResize_UnknownFlavorIsUsageBeforeTheQuestion(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w, _ := vmResizeWorld(t, "running")
		m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "huge-64", "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "low-2-4") {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
		if n := w.resizeCalls(); n != 0 {
			t.Errorf("confirmed=%v: sent %d", confirmed, n)
		}
	}
	r, _, _ := vmResizeWorld(t, "running")
	if m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "  "}); m["reason"] != clerrors.CodeUsage {
		t.Errorf("a flavor of spaces: %s", text)
	}
}

// §6.2, row 2: the VM is on the target already — already_resized with no
// question and no write, by the target's name or id (a repeat, axis 3).
func TestVMResize_SameFlavorIsAlreadyResizedWithoutAQuestion(t *testing.T) {
	for _, flavor := range []string{"low-1-1", faultFlavor} {
		for _, confirmed := range []bool{false, true} {
			r, w, _ := vmResizeWorld(t, "running")
			m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"vm_id": w.id, "flavor": flavor, "confirmed": confirmed})
			if isErr || m["status"] != "already_resized" || m["flavor_name"] != "low-1-1" || m["state"] != "running" {
				t.Errorf("%s confirmed=%v: %s", flavor, confirmed, text)
			}
			if n := w.resizeCalls(); n != 0 {
				t.Errorf("%s confirmed=%v: a repeat wrote %d", flavor, confirmed, n)
			}
		}
	}
}

// §6.2, rows «running» and «stopped»: the preview names the VM, its state,
// both flavors with what tells them apart, what goes to the request, and
// the price as a class with the rates — no number without a probe of the
// price (axis 4); nothing is sent before the yes. The platform takes the
// change of a running VM (Г-10, 4 of 4): no stop is asked for.
func TestVMResize_PreviewNamesBothFlavorsAndThePrice(t *testing.T) {
	for _, st := range []string{"running", "stopped"} {
		r, w, _ := vmResizeWorld(t, st)
		m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4"})
		if m["status"] != "confirmation_required" {
			t.Fatalf("%s: %s", st, text)
		}
		p, _ := m["preview"].(map[string]any)
		if p["vm_id"] != w.id || p["vm_state"] != st || p["flavor_id"] != resizeUp {
			t.Errorf("%s: preview %v", st, p)
		}
		from, _ := p["flavor_from"].(map[string]any)
		to, _ := p["flavor_to"].(map[string]any)
		if from["name"] != "low-1-1" || to["name"] != "low-2-4" || to["cpu"] != float64(2) || to["ram_gb"] != float64(4) ||
			to["cpu_share"] != "1:1" || to["spot"] != false || to["gpu"] != float64(0) {
			t.Errorf("%s: flavors %v → %v", st, from, to)
		}
		classes, _ := p["price_classes"].([]any)
		if len(classes) != 1 || p["tariff_url"] == nil || strings.Contains(text, "cost_to") {
			t.Errorf("%s: price %v", st, p)
		}
		// A bigger target is said to cost more in words (conclusion 150, В2).
		if c, _ := classes[0].(map[string]any); !strings.Contains(c["note"].(string), "more") {
			t.Errorf("%s: a bigger flavor is not said to cost more: %v", st, c)
		}
		if n := w.resizeCalls(); n != 0 {
			t.Errorf("%s: written before consent: %d", st, n)
		}
	}
}

// With the yes the change is sent once, and the answer is the end the
// cloud made — the flavor the VM has and the state it is in — not the
// acceptance of the request (axis 2). A stopped VM stays stopped.
func TestVMResize_ConfirmedResizesAndAnswersTheFlavorAndState(t *testing.T) {
	for _, st := range []string{"running", "stopped"} {
		r, w, _ := vmResizeWorld(t, st)
		w.stepAfter = 3
		m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true})
		if isErr || m["status"] != "resized" || m["state"] != st || m["flavor_name"] != "low-2-4" || m["flavor_id"] != resizeUp || m["vm_id"] != w.id {
			t.Errorf("%s: %s", st, text)
		}
		if n := w.resizeCalls(); n != 1 {
			t.Errorf("%s: sent %d, want 1", st, n)
		}
		if w.flavorNow(t) != resizeUp {
			t.Errorf("%s: the world's VM is not on the target", st)
		}
	}
}

// §6.2, row 3: the VM is updating and its card names the target already —
// this change, cut before its end, or another caller's: it is waited for
// with no question and no second request.
func TestVMResize_UpdatingToTheTargetIsWaitedForWithoutAQuestion(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w, _ := vmResizeWorld(t, "running")
		if err := w.SetVMFlavor(context.Background(), w.id, resizeUp); err != nil {
			t.Fatal(err)
		}
		w.stepAfter = 3
		m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": confirmed})
		if isErr || m["status"] != "resized" || m["state"] != "running" {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
		if n := w.resizeCalls(); n != 1 {
			t.Errorf("confirmed=%v: sent %d, want the 1 of the world's own setup", confirmed, n)
		}
	}
}

// §6.2, the transient states of another change: settled within the wait,
// the call goes on from the state reached; not settled, USAGE with the
// state — no question, no write.
func TestVMResize_AnotherChangeSettlesFirst(t *testing.T) {
	r, w, _ := vmResizeWorld(t, "starting")
	w.stepAfter, w.settleTo = 2, "running"
	m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4"})
	if m["status"] != "confirmation_required" || w.resizeCalls() != 0 {
		t.Errorf("a start that settles: %s", text)
	}
	for _, st := range []string{"starting", "stopping", "rebooting", "creating", "updating"} {
		r, w, _ := vmResizeWorld(t, st)
		m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, st) || w.resizeCalls() != 0 {
			t.Errorf("%s that does not settle: %s", st, text)
		}
	}
}

// §6.2, the rest of the table: nothing to change, no VM, an error state.
func TestVMResize_TheRestOfTheTable(t *testing.T) {
	for _, st := range []string{"deleting", "error_deleting", "error_creating"} {
		r, w, _ := vmResizeWorld(t, st)
		m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true})
		if !isErr || m["reason"] != clerrors.CodeUsage || w.resizeCalls() != 0 {
			t.Errorf("%s: %s", st, text)
		}
	}
	r, w, _ := vmResizeWorld(t, "running")
	if m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"vm_id": faultID(0x7f), "flavor": "low-2-4", "confirmed": true}); !isErr || m["reason"] != clerrors.CodeVMNotFound || w.resizeCalls() != 0 {
		t.Errorf("no such VM: %s", text)
	}
	r, w, _ = vmResizeWorld(t, "error")
	m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4"})
	if m["status"] != "confirmation_required" || m["warning"] == nil || w.resizeCalls() != 0 {
		t.Errorf("a VM in error: the preview with a warning, %s", text)
	}
}

// A known limit of the pair: a target not given in the zone of the VM is
// refused before the question — evo does not ask about a change the
// platform will not make (axis 1; conclusion 150, У1).
func TestVMResize_FlavorOfAnotherZoneIsRefusedBeforeTheQuestion(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w, _ := vmResizeWorld(t, "stopped")
		m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "far-2-4", "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "ru.AZ-1") || w.resizeCalls() != 0 {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
	}
}

// An interruptible target is a line of its own: the platform may stop
// such a VM (conclusion 150, У3).
func TestVMResize_SpotTargetIsWarnedOf(t *testing.T) {
	r, _, _ := vmResizeWorld(t, "running")
	m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "spot-2-4"})
	p, _ := m["preview"].(map[string]any)
	to, _ := p["flavor_to"].(map[string]any)
	if to["spot"] != true || to["cpu_share"] != "1:10" || !strings.Contains(text, "interruptible") {
		t.Errorf("a spot target: %s", text)
	}
}

// A refusal of the platform names its cause in one machine field; one at
// a stopped VM says the VM stays stopped with the old flavor and names
// vm_start as the step, without consent (conclusion 150, У1).
func TestVMResize_RefusalNamesTheCauseAndAStoppedVMIsTold(t *testing.T) {
	cases := []struct{ state, code, cause string }{
		{"stopped", "organization_quota_exceeded", "quota"},
		{"stopped", "flavor_not_allowed", "refused"},
		{"running", "flavor_not_allowed", "refused"},
	}
	for _, c := range cases {
		r, w, _ := vmResizeWorld(t, c.state)
		w.resizeRefused = c.code
		m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true})
		d, _ := m["details"].(map[string]any)
		if !isErr || m["reason"] != clerrors.CodeAPIError || d["cause"] != c.cause || d["platform_code"] != c.code || m["flavor_name"] != "low-1-1" {
			t.Errorf("%s/%s: %s", c.state, c.code, text)
		}
		next, _ := m["next_args"].(map[string]any)
		if c.state == "stopped" {
			if m["next_tool"] != "vm_start" || next["vm_id"] != w.id || next["confirmed"] != nil || !strings.Contains(text, "stopped") {
				t.Errorf("%s/%s: no vm_start as the step: %s", c.state, c.code, text)
			}
		} else if m["next_tool"] != nil {
			t.Errorf("%s/%s: a step after a refusal of a running VM: %s", c.state, c.code, text)
		}
	}
}

// A lost answer may be a change taken: the VM is read again, and updating
// or the target means it was — the call goes on to the end, with no second
// request (§6.4).
func TestVMResize_LostAnswerReadsTheVMAgain(t *testing.T) {
	r, w, _ := vmResizeWorld(t, "running")
	w.stepAfter = 4
	w.failNth("SetVMFlavor", 1, faultLost)
	m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true})
	if isErr || m["status"] != "resized" || w.resizeCalls() != 1 {
		t.Errorf("a lost answer of a taken change: %s (sent %d)", text, w.resizeCalls())
	}
}

// At the limit of an MCP call the answer is resizing with the same call,
// without the consent, as the next step; the step writes nothing and ends
// with the end the cloud made (axis 2; R-85).
func TestVMResize_AtTheLimitTheStepIsTheSameCallWithoutConsent(t *testing.T) {
	r, w, _ := vmResizeWorldWaiting(t, "running", 0)
	budgeted(r)
	m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true})
	if isErr || m["status"] != "resizing" || m["next_tool"] != "vm_resize" {
		t.Fatalf("vm_resize past the limit: %s", text)
	}
	next, _ := m["next_args"].(map[string]any)
	if next["vm_id"] != w.id || next["confirmed"] != nil || next["flavor"] != "low-2-4" {
		t.Fatalf("next_args: %v", next)
	}
	if _, text, _ = callNamed(t, r, "vm_resize", next); w.resizeCalls() != 1 {
		t.Errorf("the step on a VM still updating wrote: %s", text)
	}
	w.stepVMPower(w.id)
	m, text, _ = callNamed(t, r, "vm_resize", next)
	if m["status"] != "already_resized" || w.resizeCalls() != 1 {
		t.Errorf("the step after the end: %s", text)
	}
}

// A VM that left updating with the flavor it had, or for an error, is an
// error with its state and flavor, and no step with consent (§6.4).
func TestVMResize_ChangeThatDidNotTakePlaceIsAnError(t *testing.T) {
	r, w, _ := vmResizeWorld(t, "running")
	w.stepAfter, w.stepTo = 3, []string{"error"}
	m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["state"] != "error" || m["next_tool"] != nil {
		t.Errorf("a VM gone to error: %s", text)
	}
	r, w, _ = vmResizeWorld(t, "running")
	w.stepAfter, w.settleTo = 3, "running"
	w.flavorBack = faultFlavor
	m, text, isErr = callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true})
	if !isErr || m["state"] != "running" || m["flavor_name"] != "low-1-1" || m["next_tool"] != nil {
		t.Errorf("a VM back on its flavor: %s", text)
	}
}

// The folder's record of the VM names the flavor the VM has: after the end
// of this call, after already_resized, and after the step at the limit —
// whichever call saw the target (conclusion 150, У2).
func TestVMResize_FoldersRecordFollowsTheFlavorTheVMHas(t *testing.T) {
	r, w, dir := vmResizeWorld(t, "running")
	recordOn(t, dir, w.id)
	w.stepAfter = 3
	if m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true}); m["status"] != "resized" {
		t.Fatal(text)
	}
	if id, name := recordFlavor(t, dir, w.id); id != resizeUp || name != "low-2-4" {
		t.Errorf("after resized the record names %s %s", id, name)
	}

	r, w, dir = vmResizeWorld(t, "running")
	recordOn(t, dir, w.id)
	w.setVMFlavor(w.id, resizeUp)
	if m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4"}); m["status"] != "already_resized" {
		t.Fatal(text)
	}
	if id, name := recordFlavor(t, dir, w.id); id != resizeUp || name != "low-2-4" {
		t.Errorf("after already_resized the record names %s %s", id, name)
	}

	r, w, dir = vmResizeWorldWaiting(t, "running", 0)
	recordOn(t, dir, w.id)
	budgeted(r)
	if m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": true}); m["status"] != "resizing" {
		t.Fatal(text)
	}
	if id, name := recordFlavor(t, dir, w.id); id != resizeUp || name != "low-2-4" {
		t.Errorf("after the step at the limit the record names %s %s", id, name)
	}
}

// A smaller flavor under this folder's deployment is a warning of the
// preview, not a refusal (conclusion 150, В4); a VM of no record gets none.
func TestVMResize_SmallerUnderTheFoldersDeploymentIsWarnedOf(t *testing.T) {
	r, w, dir := vmResizeWorld(t, "running")
	w.setVMFlavor(w.id, resizeUp)
	m, text, _ := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-1-1"})
	if p, _ := m["preview"].(map[string]any); p["folder_deployment"] != nil {
		t.Errorf("no record, a deployment named: %s", text)
	}
	recordOn(t, dir, w.id)
	m, text, _ = callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-1-1"})
	if p, _ := m["preview"].(map[string]any); m["status"] != "confirmation_required" || p["folder_deployment"] == nil {
		t.Errorf("a smaller flavor under the deployment: %s", text)
	}
	// Smaller is not said to be cheaper without a price (conclusion 150).
	if strings.Contains(strings.ToLower(text), "saving") || strings.Contains(strings.ToLower(text), "cheaper") {
		t.Errorf("a smaller flavor is promised a saving: %s", text)
	}
}

// vmResizeStateRows is the table «state → answer» of vm_resize for a
// target the VM is not on (§6.2), a world per row. «Waits» is a state of
// another change that does not settle within the wait: USAGE with the
// state; the row of updating to the target itself is
// TestVMResize_UpdatingToTheTargetIsWaitedForWithoutAQuestion.
var vmResizeStateRows = map[string]map[string]vmStateRow{
	"vm_resize": {
		"running":        rowAsks,
		"stopped":        rowAsks,
		"starting":       rowRefused,
		"stopping":       rowRefused,
		"rebooting":      rowRefused,
		"creating":       rowRefused,
		"updating":       rowRefused,
		"deleting":       rowRefused,
		"error_deleting": rowRefused,
		"error_creating": rowRefused,
		"error":          rowAsksWarned,
		vmStateUnknown:   rowAsksWarned,
	},
}

func init() { addStateRows(stateRowsOf, vmResizeStateRows) }

func TestVMResize_EveryStateOfTheTableHasAWorld(t *testing.T) {
	for state, row := range vmResizeStateRows["vm_resize"] {
		for _, confirmed := range []bool{false, true} {
			r, w, _ := vmResizeWorld(t, state)
			w.stepAfter, w.settleTo = 0, ""
			m, text, isErr := callNamed(t, r, "vm_resize", map[string]any{"name": "web", "flavor": "low-2-4", "confirmed": confirmed})
			warning, _ := m["warning"].(string)
			asked := m["status"] == "confirmation_required"
			want := 0
			var problem string
			switch row {
			case rowAsks, rowAsksWarned:
				if confirmed {
					want = 1
				} else if !asked {
					problem = "no question"
				} else if (row == rowAsksWarned) != strings.Contains(warning, state) {
					problem = "the warning of the preview: " + warning
				}
			case rowRefused:
				if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, state) {
					problem = "not a USAGE that names the state"
				}
			}
			if problem != "" {
				t.Errorf("a VM in %s (%s), confirmed=%v: %s: %s", state, row, confirmed, problem, text)
			}
			if n := w.resizeCalls(); n != want {
				t.Errorf("a VM in %s (%s), confirmed=%v: sent %d, want %d: %s", state, row, confirmed, n, want, text)
			}
		}
	}
}
