// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The answer of a call over MCP does not name the record the call itself
// left as what an earlier run left: it says how the call ended, and the
// CLI's answer has no such note. evo stands where its state is — the
// engine's directory is the working one, as in the product. The record is
// left out by its id, in that one answer only: the next answer of the
// server names it, and the same call again, with the consent of the first,
// is asked about and not sent («Риск», conclusion 172, У7–У9).
func TestIntent_ACallsOwnRecordIsNotInItsOwnAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	r := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{Config: config.Config{ProjectID: "p", KeyID: "k", Secret: "s"}, StateDir: dir}, nil
	})
	r.SetStateDir(dir)
	reboot, _ := r.Lookup("vm_reboot")
	args := func() map[string]any { return map[string]any{"vm_id": resMine, "confirmed": true} }
	sent := 0
	call := r.budgeted(r.intended(reboot, func(_ context.Context, args map[string]any) (string, bool) {
		// The layer asks the handler for the preview of its question
		// without the consent: only a consented call sends.
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return `{"status":"error","reason":"USAGE"}`, true
		}
		sent++
		return `{"status":"error","error":"reboot: API_ERROR: 502","reason":"API_ERROR"}`, true
	}))

	text, _ := call(context.Background(), args())
	var cut map[string]any
	_ = json.Unmarshal([]byte(text), &cut)
	if _, named := cut["pending_operations"]; named || cut["reason"] != "API_ERROR" || sent != 1 {
		t.Fatalf("the answer of the cut call names its own record, or lost its reason: sent %d\n%s", sent, text)
	}
	if n := len(intent.Pending(intent.Dir(dir))); n != 1 {
		t.Fatalf("%d records after the cut call, want 1", n)
	}

	// Another call of the same server: its answer names the record.
	skill, _ := r.Lookup("agent_skill")
	other, _ := skill.Handler(context.Background(), map[string]any{"group": "vm"})
	if !strings.Contains(other, "pending_operations") || !strings.Contains(other, "vm_reboot") {
		t.Fatalf("the next answer does not name the record the cut call left:\n%s", other)
	}

	// The same call again, with the consent of the first: asked, not sent.
	again, _ := call(context.Background(), args())
	if !strings.Contains(again, `"confirmation_required"`) || sent != 1 {
		t.Fatalf("the same call after its cut one: sent %d\n%s", sent, again)
	}
}

// The layers of one call read the directory of the records once, and again
// only after the call itself wrote there: a record another process adds in
// between is the next call's to see (release plan 1.9, «Каталог записей
// намерений»: several reads of the directory on every consented call were
// the cost of the layer on Windows).
func TestIntent_ACallReadsTheRecordsOnceUntilItWritesThere(t *testing.T) {
	dir := t.TempDir()
	ctx := context.WithValue(context.Background(), callRecordsKey{}, &callRecords{})
	if n := len(pendingIn(ctx, dir)); n != 0 {
		t.Fatalf("%d records in an empty directory", n)
	}
	if _, err := intent.Open(intent.Dir(dir), intent.Intent{Tool: "vm_reboot", Target: "vm_id=" + resMine}); err != nil {
		t.Fatal(err)
	}
	if n := len(pendingIn(ctx, dir)); n != 0 {
		t.Errorf("the call read the directory again: %d records, want the first reading", n)
	}
	forgetPending(ctx)
	if n := len(pendingIn(ctx, dir)); n != 1 {
		t.Errorf("%d records after the call wrote there, want 1", n)
	}
	// A layer called on its own, with no call around it, reads the directory.
	if n := len(pendingIn(context.Background(), dir)); n != 1 {
		t.Errorf("%d records read with no call, want 1", n)
	}
}

// How many times a call of a tool that takes consent reads the directory
// of the records — the whole cost of the layer for a call, since a reading
// reads every record there: once for a call without consent (the question,
// a refusal), twice for a consented call (before its record is opened —
// the question after a cut call, the operation that goes on already — and
// after its own record is settled). No layer reads it for itself.
func TestIntent_ReadsOfTheRecordsByACall(t *testing.T) {
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{Config: config.Config{ProjectID: "p", KeyID: "k", Secret: "s"}, StateDir: dir}, nil
	})
	r.SetStateDir(dir)
	reads := 0
	old := readPending
	readPending = func(d string) []intent.Intent { reads++; return old(d) }
	t.Cleanup(func() { readPending = old })
	for _, c := range []struct {
		tool  string
		args  map[string]any
		reads int
	}{
		{"disk_delete", map[string]any{"disk_id": resMine}, 1},
		// Before the record is opened (is the operation of the call
		// going on already: goesOn) and after the call ended.
		{"disk_delete", map[string]any{"disk_id": resMine, "confirmed": true}, 2},
		{"vm_reboot", map[string]any{"vm_id": resMine}, 1},
		{"vm_reboot", map[string]any{"vm_id": resMine, "confirmed": true}, 2},
	} {
		tl, _ := r.Lookup(c.tool)
		call := r.budgeted(r.intended(tl, func(_ context.Context, args map[string]any) (string, bool) {
			if confirmed, _ := args["confirmed"].(bool); !confirmed {
				return `{"status":"confirmation_required"}`, true
			}
			return `{"status":"ok"}`, false
		}))
		reads = 0
		call(context.Background(), c.args)
		if reads != c.reads {
			t.Errorf("%s %v read the records %d times, want %d", c.tool, c.args, reads, c.reads)
		}
	}
}

// readPending is the seam the test above counts by. Nothing of the product
// assigns it: a build that put another function there would switch the
// reading of the records off («Риск», a word on 428d400). The files of the
// package that are not tests name it twice — its declaration and its two
// calls in pendingIn — and assign it nowhere.
func TestIntent_OnlyTestsReplaceTheReadingOfTheRecords(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	assigned := regexp.MustCompile(`(^|[^.\w])readPending\s*(=[^=]|:=)`)
	declared := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !assigned.MatchString(line) {
				continue
			}
			if strings.HasPrefix(line, "var readPending = intent.Pending") {
				declared++
				continue
			}
			t.Errorf("%s:%d assigns readPending: only a test replaces the reading of the records\n%s", f, i+1, line)
		}
	}
	if declared != 1 {
		t.Errorf("readPending is declared %d times as intent.Pending, want once", declared)
	}
}
