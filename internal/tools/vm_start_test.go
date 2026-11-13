// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// vm_start is the reference operation of the first wave (D99,
// docs/internal/briefs/ops-wave-1.md §3). Its table «state of the VM → the
// answer» (§3.1) has a world per row here: what the planner reads before
// the question decides whether there is a question and whether there is a
// write. The writes are counted by the world (powerCalls).

// powerWorld is the fake cloud whose VM ends its power transition on the
// stepAfter-th read of the VM after the command was taken — the cloud
// finishing the start while evo waits — with no wall clock. stepAfter 0
// never ends it.
type powerWorld struct {
	*faultCloud
	id        string
	stepAfter int
	stepTo    []string
	// settleTo, when set, is the state the VM is put in at that read
	// instead: the end of a change that is not a power transition of the
	// fake (creating, updating), or the VM deleted ("gone").
	settleTo string
	// onPower, when set, runs in place of the fake's power command.
	onPower func() error
	// flavorBack, when set, is the flavor the VM is left with at that read:
	// a change of flavor the platform did not make (vm_resize).
	flavorBack string

	mu    sync.Mutex
	reads int
}

func (w *powerWorld) read() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stepAfter == 0 {
		return
	}
	w.reads++
	if w.reads < w.stepAfter {
		return
	}
	if w.flavorBack != "" {
		w.setVMFlavor(w.id, w.flavorBack)
	}
	switch w.settleTo {
	case "":
		w.stepVMPower(w.id, w.stepTo...)
	case "gone":
		w.removeVM()
	default:
		w.setVMState(w.id, w.settleTo)
	}
}

// removeVM deletes the VM of the world, as another caller would.
func (w *powerWorld) removeVM() {
	w.faultCloud.mu.Lock()
	defer w.faultCloud.mu.Unlock()
	for i := range w.vms {
		if w.vms[i].ID == w.id {
			w.vms[i].gone = true
		}
	}
}

func (w *powerWorld) SetVMPower(ctx context.Context, id, command string) error {
	// The command goes out, as the Compute client counts it
	// (httpretry.UserAgent): a write of the call.
	if err := httpretry.NoteWrite(ctx); err != nil {
		return err
	}
	if w.onPower != nil {
		return w.onPower()
	}
	return w.faultCloud.SetVMPower(ctx, id, command)
}

func (w *powerWorld) GetVM(ctx context.Context, id string) (*provision.VM, error) {
	w.read()
	return w.faultCloud.GetVM(ctx, id)
}

func (w *powerWorld) GetVMDetail(ctx context.Context, id string) (*provision.VMDetail, error) {
	w.read()
	return w.faultCloud.GetVMDetail(ctx, id)
}

func (w *powerWorld) ListVMsInProject(ctx context.Context, project string) ([]*provision.VM, error) {
	w.read()
	return w.faultCloud.ListVMsInProject(ctx, project)
}

// vmStartWorld is a registry over a world with one VM named web in state;
// its own waits end in 50 ms.
func vmStartWorld(t *testing.T, state string) (*Registry, *powerWorld) {
	t.Helper()
	return vmStartWorldWaiting(t, state, 50*time.Millisecond)
}

// vmStartWorldWaiting is vmStartWorld whose own waits end in ownLimit; 0
// leaves the limits of production, so only the budget of a call ends a
// wait.
func vmStartWorldWaiting(t *testing.T, state string, ownLimit time.Duration) (*Registry, *powerWorld) {
	t.Helper()
	c := &faultCloud{}
	vm, err := c.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	c.setVMState(vm.ID, state)
	w := &powerWorld{faultCloud: c, id: vm.ID}
	// One directory for the world, not one for each engine: the next step
	// reads the record the first one left.
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
	return r, w
}

// A stopped VM: the preview names what will be done and to what, and
// nothing is written before the yes (axes 1 and 4).
func TestVMStart_StoppedAsksWithAPreviewThatNamesTheVM(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	m, text, _ := callNamed(t, r, "vm_start", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("vm_start of a stopped VM: %s", text)
	}
	for _, want := range []string{w.id, "web", "power_on", "stopped"} {
		if !strings.Contains(text, want) {
			t.Errorf("the preview does not name %q: %s", want, text)
		}
	}
	if n := w.powerCalls(); n != 0 {
		t.Errorf("written before consent: %d power calls", n)
	}
}

// With the yes the command is sent once, and the answer is the end of the
// transition the cloud made, not the acceptance of the command (axis 2).
func TestVMStart_ConfirmedStartsAndAnswersRunning(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.stepAfter = 3
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "started" || m["state"] != "running" || m["vm_id"] != w.id {
		t.Fatalf("vm_start with consent: %s", text)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d, want 1", n)
	}
}

// A repeat (П3; D26, D81): the VM runs already — success with no question
// and no write, whatever confirmed says.
func TestVMStart_RunningIsAlreadyRunningWithoutAQuestion(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w := vmStartWorld(t, "running")
		m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"vm_id": w.id, "confirmed": confirmed})
		if isErr || m["status"] != "already_running" {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
		if n := w.powerCalls(); n != 0 {
			t.Errorf("confirmed=%v: a repeat wrote: %d power calls", confirmed, n)
		}
	}
}

// A start in flight — the call cut before its end, or another caller's —
// is waited for with no question and no second command (П3).
func TestVMStart_StartingIsWaitedForWithoutAQuestionOrAWrite(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w := vmStartWorld(t, "stopped")
		if err := w.SetVMPower(context.Background(), w.id, "power_on"); err != nil {
			t.Fatal(err)
		}
		w.stepAfter = 3
		m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": confirmed})
		if isErr || m["status"] != "started" || m["state"] != "running" {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
		if n := w.powerCalls(); n != 1 {
			t.Errorf("confirmed=%v: power calls = %d, want the 1 of the world's own setup", confirmed, n)
		}
	}
}

// A VM that is being deleted or was never made cannot be started: refused
// before the question with its state, and the hint is a read — it does
// not name a delete as the next step («Риск», conclusion 111, У3).
func TestVMStart_NothingToStartIsRefusedBeforeTheQuestion(t *testing.T) {
	for _, state := range []string{"deleting", "error_deleting", "error_creating"} {
		for _, confirmed := range []bool{false, true} {
			r, w := vmStartWorld(t, state)
			m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": confirmed})
			if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, state) {
				t.Errorf("%s, confirmed=%v: %s", state, confirmed, text)
			}
			if strings.Contains(text, "destroy") || strings.Contains(text, "delete") {
				t.Errorf("%s: the refusal names a delete as a step: %s", state, text)
			}
			if n := w.powerCalls(); n != 0 {
				t.Errorf("%s, confirmed=%v: written: %d power calls", state, confirmed, n)
			}
		}
	}
}

// A typo in the id is not a success and not a question (axis 3).
func TestVMStart_NoSuchVMIsNotFoundBeforeTheQuestion(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w := vmStartWorld(t, "stopped")
		m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"vm_id": faultID(0x7f), "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeVMNotFound {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
		if n := w.powerCalls(); n != 0 {
			t.Errorf("confirmed=%v: written: %d power calls", confirmed, n)
		}
	}
}

// The transient state of another change settles before the question (П4,
// as a disk's, D53): a VM that was stopping and is stopped by the time
// the plan decides is asked about as a stopped one.
func TestVMStart_StoppingThatSettlesIsAskedAboutAsStopped(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	if err := w.SetVMPower(context.Background(), w.id, "power_off"); err != nil {
		t.Fatal(err)
	}
	w.stepAfter = 2
	m, text, _ := callNamed(t, r, "vm_start", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" || !strings.Contains(text, "stopped") {
		t.Fatalf("vm_start of a VM that stopped while the plan waited: %s", text)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d, want the 1 of the world's own setup", n)
	}
}

// A VM in error (conclusion 111, variant А): whether the platform starts
// it is not known, so the call is not refused — the preview says so.
func TestVMStart_ErrorStateIsAskedAboutNotRefused(t *testing.T) {
	r, w := vmStartWorld(t, "error")
	m, text, _ := callNamed(t, r, "vm_start", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" || !strings.Contains(text, "error") {
		t.Fatalf("vm_start of a VM in error: %s", text)
	}
	if n := w.powerCalls(); n != 0 {
		t.Errorf("written before consent: %d power calls", n)
	}
}

// The start that ends in error is not a success (П5).
func TestVMStart_TransitionThatEndsInErrorIsAnError(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.stepAfter, w.stepTo = 3, []string{"error"}
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || !strings.Contains(text, "error") {
		t.Fatalf("vm_start whose VM ended in error: %s", text)
	}
}

// vm_id and name together, or neither, is a mistake of the call (as
// vm_get), found before the cloud.
func TestVMStart_OneReferenceToTheVM(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	for _, args := range []map[string]any{{}, {"vm_id": w.id, "name": "web"}} {
		m, text, isErr := callNamed(t, r, "vm_start", args)
		if !isErr || m["reason"] != clerrors.CodeUsage {
			t.Errorf("%v: %s", args, text)
		}
	}
	if log := w.callLog(); len(log) != 1 || log[0] != "CreateVM" {
		t.Errorf("a call refused by its arguments reached the cloud: %v", log)
	}
}

// The limit of an MCP call (D69) while the VM is still starting is not an
// error: the answer is the transient state with the same call as the next
// step, and that step finds the VM on its way and sends no second command
// (П5). Its own start goes on with the same call until the limit of the
// operation; a read is the step of a start that is not its own (the test
// below).
func TestVMStart_AtTheLimitOfAnMCPCallAnswersStartingAndTheNextStepDoesNotWrite(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "stopped", 0)
	budgeted(r)
	args := map[string]any{"name": "web", "confirmed": true}
	m, text, isErr := callNamed(t, r, "vm_start", args)
	if isErr || m["status"] != "starting" || m["state"] != "starting" || m["next_tool"] != "vm_start" {
		t.Fatalf("vm_start whose VM stays starting past the limit: %s", text)
	}
	if _, ok := m["elapsed_sec"]; !ok {
		t.Errorf("the answer at the limit has no elapsed_sec: %s", text)
	}
	next, _ := m["next_args"].(map[string]any)
	if next["vm_id"] != w.id || next["name"] != nil {
		t.Fatalf("next_args do not name the VM the call read by its id: %s", text)
	}
	if _, has := next["confirmed"]; has {
		t.Fatalf("next_args carry the consent of the call into a step that only waits (R-85): %s", text)
	}
	m, text, isErr = callNamed(t, r, "vm_start", next)
	if isErr || m["status"] != "starting" || m["next_tool"] != "vm_start" {
		t.Fatalf("the next step on a VM still starting: its own start goes on with the same call: %s", text)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d after the next step, want 1: the repeat sent a command", n)
	}
	w.stepVMPower(w.id)
	m, text, isErr = callNamed(t, r, "vm_start", next)
	if isErr || m["status"] != "already_running" {
		t.Fatalf("the next step once the VM runs: %s", text)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d at the end, want 1", n)
	}
}

// A start somebody else began, when the call's limit ends the wait, names
// the read of the VM as the next step, not the same call: evo sent
// nothing, so it has no change of its own to go on with, and the chain of
// repeats ends (П5; conclusion 111, У1).
func TestVMStart_ForeignStartAtTheLimitNamesTheReadAsTheNextStep(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "stopped", 0)
	budgeted(r)
	if err := w.SetVMPower(context.Background(), w.id, "power_on"); err != nil {
		t.Fatal(err)
	}
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web"})
	if isErr || m["status"] != "starting" {
		t.Fatalf("vm_start of a VM somebody else is starting: %s", text)
	}
	if next, _ := m["next_args"].(map[string]any); m["next_tool"] != "vm_get" || next["vm_id"] != w.id {
		t.Errorf("a start evo did not send names %v as the next step, want the read vm_get of the VM: %s", m["next_tool"], text)
	}
}

// A lost answer may be a command taken (П6): the VM is read again, found
// starting, and the call goes on to its end — one command, a success.
func TestVMStart_LostAnswerOfACommandTakenGoesOn(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.failNth("SetVMPower", 1, faultLost)
	w.stepAfter = 4
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "started" {
		t.Fatalf("vm_start whose answer was lost: %s", text)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d, want 1", n)
	}
}

// A fault before the command was taken is the fault's error, and the VM
// stays stopped: a repeat asks again (nothing is assumed done).
func TestVMStart_FaultBeforeTheCommandIsTheError(t *testing.T) {
	for _, kind := range []string{faultTransport, faultHTTP500, faultAuth} {
		r, w := vmStartWorld(t, "stopped")
		w.failNth("SetVMPower", 1, kind)
		m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
		if !isErr || m["status"] == "started" {
			t.Errorf("%s: %s", kind, text)
		}
		m, text, _ = callNamed(t, r, "vm_start", map[string]any{"name": "web"})
		if m["status"] != "confirmation_required" {
			t.Errorf("%s: the repeat after the fault does not ask again: %s", kind, text)
		}
	}
}

// The platform refuses the command from a state the preview warned of
// (error): the answer names the state and the cause, and no next call —
// a repeat will not help (conclusion 111, the conditions of variant А).
func TestVMStart_RefusalFromErrorNamesNoNextStep(t *testing.T) {
	r, _ := vmStartWorld(t, "error")
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["state"] != "error" {
		t.Fatalf("vm_start refused from error: %s", text)
	}
	if d, _ := m["details"].(map[string]any); d["cause"] != "state" || d["platform_code"] != "wrong_vm_state" {
		t.Errorf("the refusal has no details.cause and the platform's code: %s", text)
	}
	if _, has := m["next_tool"]; has {
		t.Errorf("a refusal a repeat does not get past names a next step: %s", text)
	}
	if !strings.Contains(text, "support") && !strings.Contains(text, "поддержк") {
		t.Errorf("the refusal does not name the way (console or support): %s", text)
	}
}

// A name two VMs of the project have is not resolved to the first: the
// call is refused before the question with both ids («Риск», conclusion
// 111, У2). The platform refuses a second VM of a name; this is the guard.
func TestVMStart_NameOfTwoVMsIsRefusedWithBothIDs(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.mu.Lock()
	w.faultCloud.mu.Lock()
	w.vms = append(w.vms, faultVM{ID: faultID(0x66), Name: "web", Status: "stopped"})
	w.faultCloud.mu.Unlock()
	w.mu.Unlock()
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, w.id) || !strings.Contains(text, faultID(0x66)) {
		t.Fatalf("vm_start by a name two VMs have: %s", text)
	}
	if n := w.powerCalls(); n != 0 {
		t.Errorf("written: %d power calls", n)
	}
}

// A start that began and fell back — the VM was starting and is stopped
// again — did not take place: an error with the state and no next step,
// not «still waiting». A wait with a next step would send the command
// again on the consent already given («Риск», conclusion 123, К2).
func TestVMStart_StartThatFallsBackIsAnErrorWithNoNextStep(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.stepAfter, w.stepTo = 3, []string{"stopped"}
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["state"] != "stopped" {
		t.Fatalf("vm_start whose VM fell back to stopped: %s", text)
	}
	if _, has := m["next_tool"]; has {
		t.Errorf("a start that did not take place names a next step: %s", text)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d, want 1", n)
	}
}

// The platform took the command and the VM never left stopped: by the
// limit of the call that is an error with no next step either — nothing
// is going on that a repeat would go on with (К2).
func TestVMStart_CommandAcceptedAndIgnoredIsAnErrorWithNoNextStep(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "stopped", 0)
	budgeted(r)
	w.powerIgnored = true
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["state"] != "stopped" {
		t.Fatalf("vm_start whose command the platform took and ignored: %s", text)
	}
	if _, has := m["next_tool"]; has {
		t.Errorf("a command that changed nothing names a next step: %s", text)
	}
}

// A 422 for a VM in the very state the command is accepted in is not
// about the state (changes prohibited in the zone): the cause is not
// "state", the hint does not say «repeat when it is stopped» — it is
// stopped — and no next step is named (conclusion 123, К3).
func TestVMStart_RefusalFromStoppedIsNotAboutTheState(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.powerRefused = "changes_prohibited"
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["state"] != "stopped" {
		t.Fatalf("vm_start refused from stopped: %s", text)
	}
	d, _ := m["details"].(map[string]any)
	if d["cause"] == "state" || d["platform_code"] != "changes_prohibited" {
		t.Errorf("details = %v: the cause of a refusal from stopped is not the state, and the platform's code goes beside it", d)
	}
	if strings.Contains(text, "repeat the call when") || strings.Contains(text, "повторите вызов, когда") {
		t.Errorf("the hint says to repeat when the VM is stopped, and it is: %s", text)
	}
	if _, has := m["next_tool"]; has {
		t.Errorf("a refusal a repeat does not get past names a next step: %s", text)
	}
}

// The states of another change (П4, the table of §3.1): each is waited
// for before the question, and a VM that does not settle is refused with
// its state and no write — not asked about, not sent a command the
// platform would refuse after the yes.
func TestVMStart_UnsettledStateIsRefusedBeforeTheQuestion(t *testing.T) {
	for _, state := range []string{"stopping", "rebooting", "creating", "updating"} {
		for _, confirmed := range []bool{false, true} {
			r, w := vmStartWorld(t, state)
			m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": confirmed})
			if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, state) {
				t.Errorf("%s, confirmed=%v: %s", state, confirmed, text)
			}
			if n := w.powerCalls(); n != 0 {
				t.Errorf("%s, confirmed=%v: written: %d power calls", state, confirmed, n)
			}
		}
	}
}

// ...and one that settles is decided by the state it settled in: a
// rebooted VM runs (a repeat), a created or updated one that is stopped is
// asked about.
func TestVMStart_SettledStateDecides(t *testing.T) {
	for _, tc := range []struct{ from, to, status string }{
		{"rebooting", "running", "already_running"},
		{"creating", "running", "already_running"},
		{"updating", "stopped", "confirmation_required"},
		{"creating", "stopped", "confirmation_required"},
	} {
		r, w := vmStartWorld(t, tc.from)
		w.stepAfter, w.settleTo = 2, tc.to
		m, text, _ := callNamed(t, r, "vm_start", map[string]any{"name": "web"})
		if m["status"] != tc.status {
			t.Errorf("%s that settled in %s: %s", tc.from, tc.to, text)
		}
		if n := w.powerCalls(); n != 0 {
			t.Errorf("%s → %s: written before consent: %d power calls", tc.from, tc.to, n)
		}
	}
}

// At the limit of an MCP call a VM still in another change's state is
// neither asked about nor refused: the answer says it waits, with the same
// call as the next step — and that step writes nothing either (the audit
// of the reference: a test of «the step does not write» for every answer
// that names a next step).
func TestVMStart_UnsettledAtTheLimitOfAnMCPCallWaitsAndTheNextStepDoesNotWrite(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "stopping", 0)
	budgeted(r)
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "waiting" || m["state"] != "stopping" || m["next_tool"] != "vm_start" {
		t.Fatalf("vm_start of a VM that is stopping past the limit: %s", text)
	}
	next, _ := m["next_args"].(map[string]any)
	if _, text, _ = callNamed(t, r, "vm_start", next); w.powerCalls() != 0 {
		t.Errorf("the next step wrote on a VM still stopping: %s", text)
	}
}

// A state evo does not know is the platform's to decide (as a disk's
// before D53): asked about, with the state named and the warning, not
// refused and not taken for stopped.
func TestVMStart_UnknownStateIsAskedAboutWithAWarning(t *testing.T) {
	for _, state := range []string{"error", "paused"} {
		r, w := vmStartWorld(t, state)
		m, text, _ := callNamed(t, r, "vm_start", map[string]any{"name": "web"})
		warning, _ := m["warning"].(string)
		if m["status"] != "confirmation_required" || !strings.Contains(warning, state) {
			t.Errorf("%s: the preview does not warn of the state: %s", state, text)
		}
		if n := w.powerCalls(); n != 0 {
			t.Errorf("%s: written before consent: %d power calls", state, n)
		}
	}
	r, _ := vmStartWorld(t, "stopped")
	if m, text, _ := callNamed(t, r, "vm_start", map[string]any{"name": "web"}); m["warning"] != nil {
		t.Errorf("the preview of a stopped VM warns of nothing: %s", text)
	}
}

// The answer of a start says how long it took (axis 2).
func TestVMStart_AnswerSaysHowLongItTook(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.stepAfter = 3
	m, text, _ := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if _, ok := m["elapsed_sec"]; m["status"] != "started" || !ok {
		t.Fatalf("the answer of a start has no elapsed_sec: %s", text)
	}
}

// The VM was deleted while the call waited for it to run: not found, not
// «still starting» to the end of the wait (П6; the audit of the reference,
// Н2).
func TestVMStart_VMDeletedWhileStartingIsNotFound(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "stopped", 0)
	budgeted(r)
	w.stepAfter, w.settleTo = 4, "gone"
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeVMNotFound {
		t.Fatalf("vm_start whose VM was deleted while it started: %s", text)
	}
	if _, has := m["next_tool"]; has {
		t.Errorf("a VM that is gone names a next step: %s", text)
	}
}

// The platform's refusal of a VM that is gone by then is not found (П6):
// the specification gives the power request no 404, the 422 says it.
func TestVMStart_RefusalOfAVMGoneIsNotFound(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.onPower = func() error {
		w.removeVM()
		return w.faultCloud.SetVMPower(context.Background(), w.id, "power_on")
	}
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeVMNotFound {
		t.Fatalf("vm_start refused for a VM deleted after the plan: %s", text)
	}
}

// The platform refuses the command because the VM runs by then — another
// caller started it between the plan and the command: the goal is
// reached, the answer is the success, not the 422 (П6).
func TestVMStart_RefusalWithTheGoalReachedIsTheSuccess(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	w.onPower = func() error {
		w.setVMState(w.id, "running")
		return w.faultCloud.SetVMPower(context.Background(), w.id, "power_on")
	}
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "started" || m["state"] != "running" {
		t.Fatalf("vm_start refused while the VM runs already: %s", text)
	}
}

// A call cut while the command was out (Ctrl-C, the client gone) answers
// the cut and reads nothing more: the context is done, a read would only
// fail in its place.
func TestVMStart_CutCallReadsNothingMore(t *testing.T) {
	r, w := vmStartWorld(t, "stopped")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.onPower = func() error {
		cancel()
		return context.Canceled
	}
	tl, _ := r.Lookup("vm_start")
	before := len(w.callLog())
	if _, isErr := tl.Handler(ctx, map[string]any{"name": "web", "confirmed": true}); !isErr {
		t.Fatal("a cut call answered a success")
	}
	if after := w.callLog()[before:]; len(after) != 2 {
		// The plan's two reads: the list and the VM. Nothing after the cut.
		t.Errorf("calls of the cloud in a cut call: %v, want the plan's two reads only", after)
	}
}

// The key may not start VMs (403), and the platform's rate (429): the
// platform's refusal as it is, an error with no next step — the rights of
// the key are not evo's to widen, and nothing was changed.
func TestVMStart_ForbiddenAndRateLimitedAreTheErrors(t *testing.T) {
	for name, fault := range map[string]error{
		"403": clerrors.Wrap(clerrors.CodeAPIError, "forbidden", "", &provision.StatusError{Status: 403, Body: `[{"code":"forbidden","message":"no"}]`}),
		"429": clerrors.New(clerrors.CodeAPIRateLimited, "too many requests", ""),
	} {
		r, w := vmStartWorld(t, "stopped")
		w.onPower = func() error { return fault }
		m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
		if !isErr || m["state"] != "stopped" {
			t.Errorf("%s: %s", name, text)
		}
		if _, has := m["next_tool"]; has {
			t.Errorf("%s: names a next step: %s", name, text)
		}
		if d, _ := m["details"].(map[string]any); d["cause"] == "state" {
			t.Errorf("%s: the cause of the refusal is not the state of the VM: %s", name, text)
		}
	}
}

// The start falls back between two calls (R-85; «Риск», conclusion 123,
// К1; the audit of the reference): the first call ends at the limit of an
// MCP call with the VM starting and names the same call as the next step;
// the VM goes back to stopped; an agent that follows next_args to the
// letter, again and again, sends no second command — the step carries no
// consent, so the call that finds the VM stopped asks.
func TestVMStart_FallBackBetweenCallsAsksAgainAndSendsOneCommand(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "stopped", 0)
	budgeted(r)
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "starting" || m["next_tool"] != "vm_start" {
		t.Fatalf("the first call: %s", text)
	}
	next, _ := m["next_args"].(map[string]any)
	for i := 2; i <= 4; i++ {
		w.stepVMPower(w.id, "stopped")
		m, text, _ = callNamed(t, r, "vm_start", next)
		if m["status"] != "confirmation_required" {
			t.Fatalf("call %d by next_args on a VM that fell back to stopped does not ask: %s", i, text)
		}
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d after three calls by next_args, want the 1 the person consented to", n)
	}
}

// The state moved between the plan and the command: planned from error
// (asked about with the warning), the VM is stopped by the time the
// platform refuses. That is not «a repeat will not help» — a repeat reads
// the VM again and finds it stopped (the audit of the reference).
func TestVMStart_RefusalAfterTheStateMovedSaysToRepeat(t *testing.T) {
	r, w := vmStartWorld(t, "error")
	w.onPower = func() error {
		w.setVMState(w.id, "stopped")
		return faultPowerRefused("wrong_vm_state")
	}
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["state"] != "stopped" {
		t.Fatalf("vm_start refused after the VM moved from error to stopped: %s", text)
	}
	if d, _ := m["details"].(map[string]any); d["cause"] != "state" {
		t.Errorf("details = %v: the refusal was of the state the VM was in", d)
	}
	hint, _ := m["hint"].(string)
	if !strings.Contains(hint, "repeat the call") && !strings.Contains(hint, "повторите вызов") {
		t.Errorf("the hint does not say to repeat, though the state moved: %s", text)
	}
}
