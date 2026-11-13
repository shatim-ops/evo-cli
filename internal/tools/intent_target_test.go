// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
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
	if !isErr || w.powerCalls() != sent || len(intent.Pending(dir)) != 0 {
		t.Errorf("the VM could not be read: power calls %d (were %d), records %d, want a refusal with nothing sent and no record\n%s",
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
