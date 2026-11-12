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

// vmStartWorld is a registry over a world with one VM named web in state.
func vmStartWorld(t *testing.T, state string) (*Registry, *powerWorld) {
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
			Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: 50 * time.Millisecond},
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
