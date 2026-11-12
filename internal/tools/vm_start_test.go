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
	if w.reads >= w.stepAfter {
		w.stepVMPower(w.id, w.stepTo...)
	}
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
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   w,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
			Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: ownLimit},
		}, nil
	}), w
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
// (П5).
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
	if next["name"] != "web" {
		t.Fatalf("next_args do not name the VM: %s", text)
	}
	m, text, isErr = callNamed(t, r, "vm_start", next)
	if isErr || m["status"] != "starting" {
		t.Fatalf("the next step on a VM still starting: %s", text)
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

// A start somebody else began names no next step when the call's limit
// ends the wait: evo sent nothing, so it has nothing of its own to go on
// with (П5; conclusion 111, У1).
func TestVMStart_ForeignStartAtTheLimitNamesNoNextStep(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "stopped", 0)
	budgeted(r)
	if err := w.SetVMPower(context.Background(), w.id, "power_on"); err != nil {
		t.Fatal(err)
	}
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web"})
	if isErr || m["status"] != "starting" {
		t.Fatalf("vm_start of a VM somebody else is starting: %s", text)
	}
	if _, has := m["next_tool"]; has {
		t.Errorf("a start evo did not send names a next step: %s", text)
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
