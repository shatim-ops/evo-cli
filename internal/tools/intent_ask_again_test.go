// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A tool every call of which is one more change (everySendIsAChange): a
// client that timed out sends the same call again with the consent of the
// first one — and the VM would be rebooted twice on one consent («Риск»,
// conclusion 138). A consented call that finds the record of its own cut
// call is not sent: it answers the question again, saying what is known,
// and closes the record; the next consented call is sent as any other.
func TestIntent_ConsentedCallAsksAgainAfterItsCutCall(t *testing.T) {
	const tool = "cert_renew" // stands for a tool of the list: any tool that takes consent
	everySendIsAChange[tool] = changeRule{read: "cert_get"}
	t.Cleanup(func() { delete(everySendIsAChange, tool) })
	args := func() map[string]any { return map[string]any{"certificate_id": resMine, "confirmed": true} }
	this := &state.Maker{KeyID: config.MaskKeyID("k")}
	for _, c := range []struct {
		name   string
		record *intent.Intent
		asks   bool
	}{
		{"its own cut call", &intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this}, true},
		{"no record", nil, false},
		{"a cut call on another resource", &intent.Intent{Tool: tool, Target: "certificate_id=" + resForeign, Key: this}, false},
		{"a cut call of another key", &intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: &state.Maker{ServiceAccountID: "sa-other", KeyID: "ot…er"}}, false},
		{"a record with no key", &intent.Intent{Tool: tool, Target: "certificate_id=" + resMine}, false},
		{"an operation going on", &intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this, Held: true}, false},
		{"a cut call of another tool", &intent.Intent{Tool: "cert_delete", Target: "certificate_id=" + resMine, Key: this}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ownerRegistry(t, &ownerFake{}, false, ownProject)
			eng, _ := r.newEngine(nil)
			t.Chdir(eng.StateDir) // evo runs in the directory of its state
			dir := intent.Dir(eng.StateDir)
			if c.record != nil {
				if _, err := intent.Open(dir, *c.record); err != nil {
					t.Fatal(err)
				}
			}
			sent := 0
			tl, _ := r.Lookup(tool)
			call := r.intended(tl, func(ctx context.Context, args map[string]any) (string, bool) {
				// Without the consent the handler plans and sends nothing
				// (the layer asks it for the preview of its question).
				if confirmed, _ := args["confirmed"].(bool); !confirmed {
					return `{"status":"error","reason":"USAGE"}`, true
				}
				_ = httpretry.NoteWrite(ctx)
				sent++
				return `{"status":"ok"}`, false
			})
			text, isErr := call(context.Background(), args())
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			if asked := m["status"] == "confirmation_required"; asked != c.asks || (asked && (!isErr || sent != 0)) || (!asked && sent != 1) {
				t.Fatalf("asks %v, want %v; sent %d\n%s", asked, c.asks, sent, text)
			}
			if !c.asks {
				return
			}
			msg, _ := m["message"].(string)
			cut, _ := m["interrupted"].(map[string]any)
			// The words send to the person, not to «confirmed again»: the
			// consent given was to one change («Риск», conclusion 145, У1).
			if !strings.Contains(msg, "ask the person whether to send one more") || strings.Contains(msg, "confirmed=true again") {
				t.Errorf("the question tells the model how to go round the person: %s", msg)
			}
			if !strings.Contains(msg, "its outcome is unknown") || !strings.Contains(msg, "cert_get") || cut["calls"] != float64(1) {
				t.Errorf("the question does not say what is known: %s", text)
			}
			if left := intent.Pending(dir); len(left) != 0 {
				t.Errorf("the record of the cut call is not closed: %+v", left)
			}
			// The person was asked: the next consented call is sent.
			if text, _ := call(context.Background(), args()); sent != 1 || strings.Contains(text, "confirmation_required") {
				t.Errorf("the call after the question: sent %d\n%s", sent, text)
			}
		})
	}
	// A tool not of the list is sent as before, its cut record settled by
	// the call that answered.
	t.Run("a tool not of the list", func(t *testing.T) {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		t.Chdir(eng.StateDir)
		if _, err := intent.Open(intent.Dir(eng.StateDir), intent.Intent{Tool: "pg_delete", Target: "id=" + resMine, Key: this}); err != nil {
			t.Fatal(err)
		}
		sent := 0
		tl, _ := r.Lookup("pg_delete")
		r.intended(tl, func(ctx context.Context, _ map[string]any) (string, bool) {
			_ = httpretry.NoteWrite(ctx)
			sent++
			return `{"status":"deleted"}`, false
		})(context.Background(), map[string]any{"id": resMine, "confirmed": true})
		if sent != 1 || len(intent.Pending(intent.Dir(eng.StateDir))) != 0 {
			t.Errorf("sent %d, records %+v", sent, intent.Pending(intent.Dir(eng.StateDir)))
		}
	})
	// The words of the record send to the read, not to the same call.
	t.Run("the words of the record", func(t *testing.T) {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		if _, err := intent.Open(intent.Dir(eng.StateDir), intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this}); err != nil {
			t.Fatal(err)
		}
		notes := r.PendingNotes(eng.StateDir)
		if len(notes) != 1 || !strings.Contains(notes[0], "one more change, not a repeat: whether to send it is the person's decision") || strings.Contains(notes[0], "settles it") || strings.Contains(notes[0], "doctor checks it") {
			t.Errorf("the note: %v", notes)
		}
	})
}

// A call without consent that finds the caller's own cut call asks one
// question that says everything: the preview and the cut call, with the
// record closed — so the consented call that follows is sent, and the
// person is not asked twice for one action («Риск», conclusion 145, У2).
// An answer that is no question leaves the record.
func TestIntent_QuestionWithoutConsentNamesTheCutCall(t *testing.T) {
	const tool = "cert_renew"
	everySendIsAChange[tool] = changeRule{read: "cert_get"}
	t.Cleanup(func() { delete(everySendIsAChange, tool) })
	this := &state.Maker{KeyID: config.MaskKeyID("k")}
	for _, c := range []struct {
		name, preview string
		named         bool
	}{
		{"a question", `{"status":"confirmation_required","message":"cert_renew renews the certificate."}`, true},
		{"a refusal", `{"status":"error","reason":"USAGE"}`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ownerRegistry(t, &ownerFake{}, false, ownProject)
			eng, _ := r.newEngine(nil)
			t.Chdir(eng.StateDir)
			dir := intent.Dir(eng.StateDir)
			if _, err := intent.Open(dir, intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this}); err != nil {
				t.Fatal(err)
			}
			sent := 0
			tl, _ := r.Lookup(tool)
			call := r.intended(tl, func(ctx context.Context, args map[string]any) (string, bool) {
				if confirmed, _ := args["confirmed"].(bool); !confirmed {
					return c.preview, true
				}
				_ = httpretry.NoteWrite(ctx)
				sent++
				return `{"status":"ok"}`, false
			})
			text, _ := call(context.Background(), map[string]any{"certificate_id": resMine})
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			msg, _ := m["message"].(string)
			named := m["interrupted"] != nil && strings.Contains(msg, "its outcome is unknown") && strings.Contains(msg, "cert_renew renews the certificate.")
			if named != c.named || (len(intent.Pending(dir)) == 0) != c.named {
				t.Fatalf("named %v, want %v; records %+v\n%s", named, c.named, intent.Pending(dir), text)
			}
			if !c.named {
				return
			}
			// One question: the consented call that follows is sent.
			if text, _ := call(context.Background(), map[string]any{"certificate_id": resMine, "confirmed": true}); sent != 1 || strings.Contains(text, "confirmation_required") {
				t.Errorf("the consented call after the question: sent %d\n%s", sent, text)
			}
		})
	}
}

// unnamedBy are the ways to call tl — each argument of its one-of group,
// else each required argument — by which its intent names no resource: a
// record of such a call is found by no later call, and the tool's line of
// everySendIsAChange does nothing, with no one to notice («Риск»,
// conclusion 145, У3).
func unnamedBy(tl Tool) []string {
	// Each way carries the required arguments too: a call without them
	// is refused before the layer (tools.CheckArgs).
	required, _ := tl.InputSchema["required"].([]string)
	ways := [][]string{}
	for _, group := range tl.Decl.RequiredOneOf {
		for _, arg := range group {
			ways = append(ways, append([]string{arg}, required...))
		}
	}
	if len(ways) == 0 {
		ways = append(ways, required)
	}
	var bad []string
	for _, way := range ways {
		args := map[string]any{}
		for _, arg := range way {
			args[arg] = "x"
		}
		// As the layer names it (resourceOf); a resolver that went to
		// read the resource names it.
		noCloud := New(func(map[string]any) (*engine.Engine, error) { return nil, stderrors.New("no cloud in this test") })
		if name, target, err := noCloud.resourceOf(context.Background(), tl.Name, args); err == nil && name == "" && target == "" {
			bad = append(bad, strings.Join(way, "+"))
		}
	}
	return bad
}

// A tool of everySendIsAChange names its resource in its intent however
// it is called, and its read is a tool of the registry: else the line is
// a declaration that does nothing.
func TestEverySendIsAChange_ToolNamesItsResource(t *testing.T) {
	r := New(nil)
	for tool, rule := range everySendIsAChange {
		read := rule.read
		tl, ok := r.Lookup(tool)
		if !ok {
			t.Errorf("everySendIsAChange names %s, which is no tool", tool)
			continue
		}
		if _, ok := r.Lookup(read); !ok {
			t.Errorf("everySendIsAChange: the read of %s is %q, which is no tool", tool, read)
		}
		if bad := unnamedBy(tl); len(bad) > 0 {
			t.Errorf("%s is in everySendIsAChange, and its intent names no resource when it is called by %v: a cut call of it is found by no later call — name the resource (Decl.IDArgs, or the argument name)", tool, bad)
		}
	}
	// The rule tells the two apart: a tool addressed by an id or a name,
	// in each of its spellings, and one whose intent names nothing.
	for tool, wantBad := range map[string]bool{"vm_start": false, "cert_renew": false, "secret_add_version": true} {
		tl, _ := r.Lookup(tool)
		if got := len(unnamedBy(tl)) > 0; got != wantBad {
			t.Errorf("%s: unnamed %v (%v), want %v", tool, got, unnamedBy(tl), wantBad)
		}
	}
}
