// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/history"
)

// journalLines are the lines of the journal of changes beside the intents
// of dir, in the order written.
func journalLines(t *testing.T, dir string) []history.Line {
	t.Helper()
	entries, _ := os.ReadDir(history.DirOf(dir))
	var out []history.Line
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		f, err := os.Open(filepath.Join(history.DirOf(dir), e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var l history.Line
			if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
				t.Fatalf("not a line: %q", sc.Text())
			}
			out = append(out, l)
		}
		f.Close()
	}
	return out
}

func stopClock(t *testing.T) time.Time {
	t.Helper()
	at := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	old := clock.Now
	clock.Now = func() time.Time { return at }
	t.Cleanup(func() { clock.Now = old })
	return at
}

// Each point that ends or keeps a record writes its line (D43, the ТЗ of
// the journal, §0, 1): Close with the outcome it is told, End and Hold
// unknown — with what the answer said — HoldCut unknown, Settle found for
// each record it removes, and the end of Begin removes the record or, on
// an unknown outcome, keeps it. The line carries the record's names and
// the answer's ids, the time of internal/clock, the folder of state.
func TestJournal_EveryEndOfARecordIsALine(t *testing.T) {
	stopClock(t)
	root := t.TempDir()
	dir := Dir(root)
	open := func(name string) Intent {
		in, err := Open(dir, Intent{Tool: "pg_create", Name: name, ProjectID: "p", Zone: "ru-central-1", Surface: "cli", StartedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	closed, ended, held, cut := open("a"), open("b"), open("c"), open("d")
	Close(closed, history.Outcome{Result: history.Created, ResourceID: "c-1", OperationID: "op-1"})
	End(ended, history.Outcome{Result: history.Error, Reason: "API_ERROR"})
	Hold(held, history.Outcome{Result: history.Created, ResourceID: "c-3"})
	HoldCut(cut, time.Now())
	repeat := open("b")
	Settle(dir, repeat, history.Outcome{Result: history.Created, ResourceID: "c-2"})

	want := []struct{ id, outcome, resource, reason string }{
		{closed.ID, history.Created, "c-1", ""},
		{ended.ID, history.Unknown, "", "API_ERROR"},
		{held.ID, history.Unknown, "c-3", ""},
		{cut.ID, history.Unknown, "", ""},
		{ended.ID, history.Found, "c-2", ""},
	}
	got := journalLines(t, dir)
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		l := got[i]
		if l.IntentID != w.id || l.Outcome != w.outcome || l.ResourceID != w.resource || l.Reason != w.reason {
			t.Errorf("line %d: %+v; want intent %s, %s, resource %q, reason %q", i, l, w.id, w.outcome, w.resource, w.reason)
		}
		if l.Time != "2026-11-17T12:00:00Z" || l.Tool != "pg_create" || l.ProjectID != "p" || l.Zone != "ru-central-1" ||
			l.Surface != "cli" || filepath.Clean(l.StateDir) != filepath.Clean(root) {
			t.Errorf("line %d: %+v; want the time of internal/clock and the record's names, the folder %s", i, l, root)
		}
	}
	if got[0].OperationID != "op-1" {
		t.Errorf("the operation of the answer is not in the line: %+v", got[0])
	}

	// The end of Begin: kept, the record stays (End); else it goes — with
	// its line either way, unknown too (a login that left what its undo
	// could not remove).
	for _, c := range []struct {
		result string
		kept   bool
	}{{history.Created, false}, {history.Unknown, true}, {history.Unknown, false}} {
		done, err := Begin(dir, Intent{Tool: "login", Name: "evo", StartedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		before := len(Pending(dir))
		done(history.Outcome{Result: c.result}, c.kept)
		if after := len(Pending(dir)); (after == before) != c.kept {
			t.Errorf("Begin's end with %s: %d records before, %d after; kept %v", c.result, before, after, c.kept)
		}
		lines := journalLines(t, dir)
		if last := lines[len(lines)-1]; last.Tool != "login" || last.Outcome != c.result {
			t.Errorf("Begin's end with %s: last line %+v", c.result, last)
		}
	}
}

// The line is written before the record changes: a process killed between
// the two leaves the line and the record, never neither («Риск», conclusion
// 296, У4). The record is still there when its line is written.
func TestJournal_TheLineComesBeforeTheRecordChanges(t *testing.T) {
	dir := Dir(t.TempDir())
	was := appendLine
	t.Cleanup(func() { appendLine = was })
	var seen []bool
	appendLine = func(hdir string, l history.Line) error {
		_, err := os.Stat(recordPath(dir, l.IntentID))
		seen = append(seen, err == nil)
		return was(hdir, l)
	}
	in, err := Open(dir, Intent{Tool: "vm_create", Name: "web", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	Close(in, history.Outcome{Result: history.Created, ResourceID: "vm-1"})
	if len(seen) != 1 || !seen[0] {
		t.Errorf("the line was written when the record was gone: %v", seen)
	}
	if len(Pending(dir)) != 0 {
		t.Error("the record stayed after Close")
	}
}

// A journal that cannot be written leaves no intent hanging: the record
// ends as it would, and the failure is said once («Риск», conclusion 296).
func TestJournal_AFailedLineLeavesNoIntentHanging(t *testing.T) {
	dir := Dir(t.TempDir())
	if err := config.MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	// A file where the directory of the journal goes.
	if err := os.WriteFile(history.DirOf(dir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var warned []string
	config.SetWarnFunc(func(msg string) { warned = append(warned, msg) })
	t.Cleanup(func() { config.SetWarnFunc(func(msg string) { os.Stderr.WriteString(msg + "\n") }) })
	in, err := Open(dir, Intent{Tool: "disk_create", Name: "unwritable-journal", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	Close(in, history.Outcome{Result: history.Created, ResourceID: "d-1"})
	if got := Pending(dir); len(got) != 0 {
		t.Errorf("the record hangs after a line that failed: %+v", got)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "disk_create") {
		t.Errorf("warnings %q; want one that names the call", warned)
	}
}
