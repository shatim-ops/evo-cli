// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// historyAnswer is the answer of history, read back.
type historyAnswer struct {
	JournalOf string        `json:"journal_of"`
	Since     string        `json:"since"`
	Lines     []historyLine `json:"lines"`
	Live      []historyLine `json:"live"`
	Skipped   int           `json:"skipped"`
	Hidden    int           `json:"hidden_without_project"`
	Truncated bool          `json:"truncated"`
	Untrusted []string      `json:"_untrusted"`
}

// historyRegistry is a registry over the folder of state dir, whose
// factory admits the project p only when bound.
func historyRegistry(t *testing.T, bound bool) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	r := New(func(over map[string]any) (*engine.Engine, error) {
		if p, _ := over["ProjectID"].(string); bound && p != "" && p != "p" {
			return nil, ErrProjectNotAllowed
		}
		var buf bytes.Buffer
		return &engine.Engine{Config: config.Config{ProjectID: "p"}, Printer: output.NewWithWriter(&buf, output.ModeText, false), StateDir: dir}, nil
	})
	r.SetStateDir(dir)
	if bound {
		r.SetProjectBound()
	}
	return r, dir
}

func callHistory(t *testing.T, r *Registry, args map[string]any) (historyAnswer, string) {
	t.Helper()
	tl, _ := r.Lookup("history")
	text, isErr := tl.Handler(context.Background(), args)
	if isErr {
		t.Fatalf("history: %s", text)
	}
	var a historyAnswer
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		t.Fatalf("not JSON: %s", text)
	}
	return a, text
}

func journalLine(dir string, l history.Line) error {
	return history.Append(history.DirOf(intent.Dir(dir)), l)
}

// history answers the lines of its window and what is live: a resource
// made and not removed, a call whose outcome is not known — with the read
// that tells it — and a record of an intent with no line. A line not of
// evo's is counted, not answered. The folder is named by its base name:
// no local path goes out (У6), and so neither in the JSON of the CLI.
func TestHistory_LinesAndWhatIsLive(t *testing.T) {
	at := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	old := clock.Now
	clock.Now = func() time.Time { return at }
	t.Cleanup(func() { clock.Now = old })
	r, dir := historyRegistry(t, false)
	ln := func(when, tool, id, outcome, resource string) history.Line {
		return history.Line{Time: when, Tool: tool, ProjectID: "p", Name: "n-" + id[:2], Outcome: outcome, ResourceID: resource, IntentID: id, StateDir: dir}
	}
	for _, l := range []history.Line{
		ln("2026-09-01T10:00:00Z", "disk_create", "aa00000000000001", history.Created, "d-old"), // out of the window, live
		ln("2026-11-17T10:00:00Z", "disk_create", "bb00000000000002", history.Created, "d-1"),
		ln("2026-11-17T10:05:00Z", "disk_delete", "cc00000000000003", history.Deleted, "d-1"),
		ln("2026-11-17T10:10:00Z", "pg_create", "dd00000000000004", history.Unknown, ""),
	} {
		if err := journalLine(dir, l); err != nil {
			t.Fatal(err)
		}
	}
	// A line not of evo's in a file of a day.
	f, _ := os.OpenFile(history.FileOf(history.DirOf(intent.Dir(dir)), at), os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"format":1,"time":"2026-11-17T11:00:00Z","tool":"rm_everything","outcome":"created","intent_id":"ee00000000000005"}` + "\n")
	f.Close()
	// A record of an intent with no line: a call killed before its end.
	if _, err := intent.Open(intent.Dir(dir), intent.Intent{Tool: "vm_create", Name: "web", ProjectID: "p", StartedAt: at}); err != nil {
		t.Fatal(err)
	}

	a, text := callHistory(t, r, nil)
	if a.JournalOf != filepath.Base(dir) || strings.Contains(text, dir) || strings.Contains(text, `"state_dir"`) {
		t.Errorf("journal_of %q; a local path in the answer: %s", a.JournalOf, text)
	}
	if a.Since != "2026-10-18T12:00:00Z" || len(a.Lines) != 3 || a.Skipped != 1 {
		t.Errorf("since %s, %d lines, skipped %d; want the last 30 days, 3 lines, 1 skipped", a.Since, len(a.Lines), a.Skipped)
	}
	var live []string
	for _, l := range a.Live {
		live = append(live, l.Tool+":"+l.Outcome+":"+l.ResourceID+":"+l.Check)
	}
	want := []string{"disk_create:created:d-old:", "pg_create:unknown::pg_list", "vm_create:unknown::vm_list"}
	if strings.Join(live, " ") != strings.Join(want, " ") {
		t.Errorf("live %v, want %v", live, want)
	}
	if !slices.Contains(a.Untrusted, "lines") || !slices.Contains(a.Untrusted, "live") {
		t.Errorf("_untrusted %v: the lines are a file anyone with a terminal writes", a.Untrusted)
	}

	// since: a date.
	if a, _ := callHistory(t, r, map[string]any{"since": "2026-11-17"}); len(a.Lines) != 3 {
		t.Errorf("since 2026-11-17: %d lines, want 3", len(a.Lines))
	}
	if a, _ := callHistory(t, r, map[string]any{"since": "2026-09-01"}); len(a.Lines) != 4 {
		t.Errorf("since 2026-09-01: %d lines, want 4", len(a.Lines))
	}
}

// A time that is not one is refused as USAGE, before any read.
func TestHistory_SinceNotATimeIsUsage(t *testing.T) {
	r, _ := historyRegistry(t, false)
	tl, _ := r.Lookup("history")
	text, isErr := tl.Handler(context.Background(), map[string]any{"since": "last week"})
	if !isErr || !strings.Contains(text, `"USAGE"`) {
		t.Errorf("since «last week»: %s", text)
	}
}

// On a server bound to projects (--allow-project, У6) history answers the
// lines of its projects only; a line with no project — a call on a
// resource by id (R-82) — is not answered either, and the answer names how
// many calls it hid, so an agent knows the inventory is not whole.
func TestHistory_AServerBoundToProjects(t *testing.T) {
	r, dir := historyRegistry(t, true)
	now := clock.Now().UTC().Format(time.RFC3339)
	for _, l := range []history.Line{
		{Time: now, Tool: "disk_create", ProjectID: "p", Outcome: history.Created, ResourceID: "d-1", IntentID: "aa00000000000001"},
		{Time: now, Tool: "disk_create", ProjectID: "other", Outcome: history.Created, ResourceID: "d-2", IntentID: "bb00000000000002"},
		{Time: now, Tool: "disk_delete", Target: "disk_id=d-3", Outcome: history.Unknown, ResourceID: "d-3", IntentID: "cc00000000000003"},
	} {
		if err := journalLine(dir, l); err != nil {
			t.Fatal(err)
		}
	}
	a, text := callHistory(t, r, nil)
	if len(a.Lines) != 1 || a.Lines[0].ResourceID != "d-1" || a.Hidden != 1 || strings.Contains(text, "d-2") || strings.Contains(text, "d-3") {
		t.Errorf("bound to p: %s; want the line of p, and one call hidden for no project", text)
	}
	// A project the server does not serve is refused as the factory
	// refuses it.
	tl, _ := r.Lookup("history")
	if text, isErr := tl.Handler(context.Background(), map[string]any{"project_id": "00000000-0000-4000-8000-0000000000aa"}); !isErr {
		t.Errorf("a project outside the server's: %s", text)
	}
}
