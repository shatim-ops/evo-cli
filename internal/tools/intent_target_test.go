// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// listCounting counts the lists of VMs of a power world, and fails them
// when told to.
type listCounting struct {
	*powerWorld
	lists int
	fail  error
}

func (c *listCounting) ListVMsInProject(ctx context.Context, project string) ([]*provision.VM, error) {
	c.lists++
	if c.fail != nil {
		return nil, c.fail
	}
	return c.powerWorld.ListVMsInProject(ctx, project)
}

// The question after a cut call is found by the resource, not by the way
// the call spells it: a reboot cut when the VM was named by its name, and
// the repeat that names it by id — or the other way — is the same repeat,
// and went out with no question, the record of the cut call beside it
// (the probe of «Риск» on #1602; conclusion 177, У11; release plan 1.9,
// «Запись оборванного вызова сравнивается по разрешённому id цели»). The
// layer resolves the target as the tool declares it (changeRule.target),
// before it looks for the cut calls and before it opens the record.
func TestIntent_ACutCallIsFoundHoweverTheResourceIsSpelled(t *testing.T) {
	for _, c := range []struct {
		name       string
		cut, again func(w *powerWorld) map[string]any
	}{
		{"cut by name, repeated by id",
			func(*powerWorld) map[string]any { return map[string]any{"name": "web"} },
			func(w *powerWorld) map[string]any { return map[string]any{"vm_id": w.id} }},
		{"cut by id, repeated by name",
			func(w *powerWorld) map[string]any { return map[string]any{"vm_id": w.id} },
			func(*powerWorld) map[string]any { return map[string]any{"name": "web"} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, w, dir := rebootWorldInADir(t)
			tl, _ := r.Lookup("vm_reboot")
			ctx, cancel := context.WithCancel(context.Background())
			w.onPower = func() error {
				_ = w.faultCloud.SetVMPower(context.Background(), w.id, "reboot")
				cancel()
				return ctx.Err()
			}
			first := c.cut(w)
			first["confirmed"] = true
			tl.Handler(ctx, first)
			w.onPower = nil
			w.stepVMPower(w.id)
			left := intent.Pending(dir)
			if len(left) != 1 || left[0].Target != "vm_id="+w.id || left[0].Name != "" {
				t.Fatalf("the record of the cut call names the VM as the call spelled it: %+v", left)
			}
			again := c.again(w)
			again["confirmed"] = true
			text, isErr := tl.Handler(context.Background(), again)
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			if !isErr || m["status"] != "confirmation_required" || m["interrupted"] == nil {
				t.Errorf("the repeat that spells the VM another way: %s, want the question about the cut call", text)
			}
			if n := w.powerCalls(); n != 1 {
				t.Errorf("power calls = %d, want 1: the repeat rebooted the VM again", n)
			}
		})
	}
}

// The target is resolved by one read, which the plan of the call uses
// too; and when it cannot be read, the call with consent is refused before
// anything is sent, with the reason of the read — a record by the spelling
// would bring the same way round back whenever the read fails («Риск»,
// conclusion 177, В1).
func TestIntent_TheTargetIsReadOnceAndAFailedReadRefusesTheCall(t *testing.T) {
	r, w, dir := rebootWorldInADir(t)
	counted := &listCounting{powerWorld: w}
	eng, _ := r.newEngine(nil)
	r.newEngine = func(map[string]any) (*engine.Engine, error) { e := *eng; e.Cloud = counted; return &e, nil }
	tl, _ := r.Lookup("vm_reboot")
	w.stepAfter = 6
	text, isErr := tl.Handler(context.Background(), map[string]any{"name": "web", "confirmed": true})
	if isErr || counted.lists != 1 {
		t.Errorf("a consented call by name listed the VMs %d times, want 1 (the layer and the plan share the read)\n%s", counted.lists, text)
	}
	for _, in := range intent.Pending(dir) {
		intent.Close(in)
	}
	counted.lists, counted.fail = 0, errors.New("list VMs: status 503")
	sent := w.powerCalls()
	text, isErr = tl.Handler(context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr || w.powerCalls() != sent || len(intent.Pending(dir)) != 0 || !strings.Contains(text, "status 503") {
		t.Errorf("the VM could not be read: power calls %d (were %d), records %d, want a refusal that names the read that failed, with nothing sent and no record\n%s",
			w.powerCalls(), sent, len(intent.Pending(dir)), text)
	}
}

// The words of a record promise what the layer does: it is settled by the
// same call — the same arguments — not by any call of the tool on the
// resource («Риск», conclusion 177, В3).
func TestIntent_TheNoteSaysTheSameArgumentsSettleTheRecord(t *testing.T) {
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, _ := r.newEngine(nil)
	for _, in := range []intent.Intent{
		{Tool: "disk_create", Name: "data", Key: eng.Maker()},
		{Tool: "vpc_delete", Target: "vpc_id=" + resMine, Key: eng.Maker()},
	} {
		rec, err := intent.Open(intent.Dir(eng.StateDir), in)
		if err != nil {
			t.Fatal(err)
		}
		note := r.pendingNote(rec, eng.Maker())
		if strings.Contains(note, "settles") && !strings.Contains(note, "with the same arguments settles") {
			t.Errorf("the note of %s promises that any call of the tool settles the record: %s", in.Tool, note)
		}
		intent.Close(rec)
	}
}

// What «Риск» probes of the resolved target (conclusion on the second
// part of 177): a record of the spelling of evo before the target was
// resolved is found by a repeat that spells the VM another way; a name
// that is two VMs, or a VM that is gone, refuses the call with consent
// before anything is sent and writes no record; a call without consent
// reads the target once and writes nothing.
func TestIntent_TheResolvedTargetAtItsEdges(t *testing.T) {
	t.Run("a record of the earlier spelling", func(t *testing.T) {
		r, w, dir := rebootWorldInADir(t)
		eng, _ := r.newEngine(nil)
		if _, err := intent.Open(dir, intent.Intent{Tool: "vm_reboot", Name: "web", Key: eng.Maker()}); err != nil {
			t.Fatal(err)
		}
		tl, _ := r.Lookup("vm_reboot")
		text, isErr := tl.Handler(context.Background(), map[string]any{"vm_id": w.id, "confirmed": true})
		if !isErr || !strings.Contains(text, `"interrupted"`) || w.powerCalls() != 0 {
			t.Errorf("a repeat by id after a cut call recorded by name: power calls %d\n%s", w.powerCalls(), text)
		}
	})
	t.Run("a name of two VMs", func(t *testing.T) {
		r, w, dir := rebootWorldInADir(t)
		if _, err := w.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web-2"}); err != nil {
			t.Fatal(err)
		}
		// The fake refuses a second VM of a name at its create, as the
		// platform does: two of a name are what the resolver guards against.
		w.faultCloud.mu.Lock()
		for i := range w.vms {
			w.vms[i].Name = "web"
		}
		w.faultCloud.mu.Unlock()
		tl, _ := r.Lookup("vm_reboot")
		text, isErr := tl.Handler(context.Background(), map[string]any{"name": "web", "confirmed": true})
		if !isErr || w.powerCalls() != 0 || len(intent.Pending(dir)) != 0 || !strings.Contains(text, "USAGE") {
			t.Errorf("a name of two VMs: power calls %d, records %d\n%s", w.powerCalls(), len(intent.Pending(dir)), text)
		}
	})
	t.Run("the VM is gone after the cut call", func(t *testing.T) {
		r, w, dir, tl := cutReboot(t)
		_ = r
		w.removeVM()
		text, isErr := tl.Handler(context.Background(), map[string]any{"name": "web", "confirmed": true})
		if !isErr || w.powerCalls() != 1 || !strings.Contains(text, "VM_NOT_FOUND") {
			t.Errorf("a repeat by the name of a VM that is gone: power calls %d\n%s", w.powerCalls(), text)
		}
		// The record of the cut call stays: nothing read tells its outcome.
		if left := intent.Pending(dir); len(left) != 1 {
			t.Errorf("records after the refusal: %+v, want the one of the cut call", left)
		}
	})
	t.Run("a call without consent", func(t *testing.T) {
		r, w, dir := rebootWorldInADir(t)
		counted := &listCounting{powerWorld: w}
		eng, _ := r.newEngine(nil)
		r.newEngine = func(map[string]any) (*engine.Engine, error) { e := *eng; e.Cloud = counted; return &e, nil }
		tl, _ := r.Lookup("vm_reboot")
		text, _ := tl.Handler(context.Background(), map[string]any{"name": "web"})
		if counted.lists != 1 || len(intent.Pending(dir)) != 0 || !strings.Contains(text, "confirmation_required") {
			t.Errorf("a call without consent: %d lists of the VMs, %d records\n%s", counted.lists, len(intent.Pending(dir)), text)
		}
	})
}

// A call that would be refused for its own reason — no engine is built
// for it — is told that reason, not that its record could not be written
// («Риск», the conclusion on #1620).
func TestIntent_ACallWithNoEngineIsToldItsOwnReasonFirst(t *testing.T) {
	dir := t.TempDir()
	// A file where the directory of the records would be: no record can
	// be written.
	if err := os.WriteFile(filepath.Join(dir, ".evo"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	noKey := clierrors.New(clierrors.CodeAuthRequired, "no key", "evo login")
	r := New(func(map[string]any) (*engine.Engine, error) { return nil, noKey })
	r.SetStateDir(dir)
	if _, err := r.beginIntent(context.Background(), "disk_create", map[string]any{"name": "data", "confirmed": true}); !errors.Is(err, noKey) {
		t.Errorf("the refusal: %v, want the reason no engine was built for", err)
	}
}
