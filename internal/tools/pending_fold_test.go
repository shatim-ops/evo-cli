// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// Many records of cut calls: the first answer over MCP and the stderr of
// the CLI name the newest few in full and one line for the rest, which
// sends to evo doctor; doctor names every one, and none is closed by the
// fold or by its age («Риск», conclusion 187, У1 and У3; the owner's
// decision of 13.11).
func TestPendingNotes_ManyRecordsFoldAndDoctorShowsEvery(t *testing.T) {
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, _ := r.newEngine(nil)
	root := eng.StateDir
	dir := intent.Dir(root)
	start := time.Now().Add(-400 * 24 * time.Hour)
	const total = foldPendingAfter + 3
	for i := 0; i < total; i++ {
		in := intent.Intent{Tool: "vm_reboot", Target: fmt.Sprintf("vm_id=vm-%02d", i), Surface: "mcp", PID: os.Getpid(),
			StartedAt: start.Add(time.Duration(i) * time.Hour), Ended: true, Key: eng.Maker()}
		if _, err := intent.Open(dir, in); err != nil {
			t.Fatal(err)
		}
	}
	notes := r.PendingNotes(root)
	if len(notes) != foldPendingAfter+1 {
		t.Fatalf("%d lines, want %d in full and one for the rest:\n%s", len(notes), foldPendingAfter, strings.Join(notes, "\n"))
	}
	for i, n := range notes[:foldPendingAfter] {
		// The newest, oldest of them first.
		if want := fmt.Sprintf("vm-%02d", total-foldPendingAfter+i); !strings.Contains(n, want) {
			t.Errorf("line %d does not name %s: %s", i, want, n)
		}
	}
	last := notes[foldPendingAfter]
	if !strings.Contains(last, "3") || !strings.Contains(last, "evo doctor") {
		t.Errorf("the fold line: %s", last)
	}

	// Over MCP: the same lines in pending_operations of the first answer.
	tl, _ := r.Lookup("whoami")
	var a struct {
		Pending []string `json:"pending_operations"`
	}
	text, _ := tl.Handler(context.Background(), map[string]any{})
	_ = json.Unmarshal([]byte(text), &a)
	if strings.Join(a.Pending, "\n") != strings.Join(notes, "\n") {
		t.Errorf("pending_operations differ from the CLI's lines:\n%v\n%v", a.Pending, notes)
	}

	// Doctor names each of them, and nothing was closed.
	if left := intent.Pending(dir); len(left) != total {
		t.Fatalf("%d records left of %d", len(left), total)
	}
	doc, _ := r.Lookup("doctor")
	text, _ = doc.Handler(context.Background(), map[string]any{})
	for i := 0; i < total; i++ {
		if want := fmt.Sprintf("vm-%02d", i); !strings.Contains(text, want) {
			t.Errorf("doctor does not name %s", want)
		}
	}

	// As many as the fold shows: no fold line.
	for _, in := range intent.Pending(dir)[:3] {
		intent.Close(in)
	}
	if notes := r.PendingNotes(root); len(notes) != foldPendingAfter || strings.Contains(strings.Join(notes, "\n"), "evo doctor shows") {
		t.Errorf("%d records: %d lines\n%s", foldPendingAfter, len(notes), strings.Join(notes, "\n"))
	}
}
