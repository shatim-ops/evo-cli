// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// historyOf reads the lines of the journal of changes of the folder of
// state dir, with the raw text of the files.
func historyOf(t *testing.T, dir string) (lines []history.Line, raw string) {
	t.Helper()
	hdir := history.DirOf(intent.Dir(dir))
	entries, _ := os.ReadDir(hdir)
	var b strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(hdir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			var l history.Line
			if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
				t.Fatalf("not a line: %q", sc.Text())
			}
			lines = append(lines, l)
		}
	}
	return lines, b.String()
}

// The journal of changes follows the calls through the gate (D43): a cut
// create is a line unknown; the repeat that answers it is its own line
// created, with the id of the answer, and the cut one gets a line found —
// the last line of its intent; a call refused with nothing sent is a line
// error. No value of an argument is in a line: the names of the record and
// the ids of the answer only.
func TestHistory_TheCallsOfTheGateAreLines(t *testing.T) {
	r, dir := targetRegistry(t, &goneCloud{})
	args := map[string]any{"name": "disk-1", "size": 10, "secret_hint": "s3cr3t-value", "confirmed": true}
	tool, _ := r.Lookup("disk_create")

	ctx, cancel := context.WithCancel(context.Background())
	h := r.intended(tool, func(ctx context.Context, _ map[string]any) (string, bool) {
		_ = httpretry.NoteWrite(ctx)
		cancel()
		return `{"reason":"INTERRUPTED"}`, true
	})
	h(ctx, args)
	cut := intent.Pending(intent.Dir(dir))
	if len(cut) != 1 {
		t.Fatalf("a cut call left %d records", len(cut))
	}

	h = r.intended(tool, func(context.Context, map[string]any) (string, bool) { return `{"disk":{"id":"d-1"}}`, false })
	h(context.Background(), args)

	h = r.intended(tool, func(context.Context, map[string]any) (string, bool) {
		return `{"error":"quota","reason":"QUOTA_EXCEEDED"}`, true
	})
	h(context.Background(), map[string]any{"name": "disk-2", "size": 10, "confirmed": true})

	lines, raw := historyOf(t, dir)
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4 (unknown, created, found, error): %+v", len(lines), lines)
	}
	if l := lines[0]; l.IntentID != cut[0].ID || l.Outcome != history.Unknown || l.Tool != "disk_create" || l.Name != "disk-1" || l.ProjectID != "p" {
		t.Errorf("the cut call: %+v", l)
	}
	if l := lines[1]; l.Outcome != history.Created || l.ResourceID != "d-1" || l.IntentID == cut[0].ID {
		t.Errorf("the answered call: %+v; want its own line created d-1", l)
	}
	if l := lines[2]; l.IntentID != cut[0].ID || l.Outcome != history.Found || l.ResourceID != "d-1" {
		t.Errorf("the cut call settled by the answer: %+v; want found d-1 of its intent", l)
	}
	if l := lines[3]; l.Outcome != history.Error || l.Reason != "QUOTA_EXCEEDED" || l.Name != "disk-2" {
		t.Errorf("the refused call: %+v", l)
	}
	if strings.Contains(raw, "s3cr3t-value") || strings.Contains(raw, `"size"`) {
		t.Errorf("a value of an argument in the journal:\n%s", raw)
	}
}

// What an answer says for the journal: one reading for every point.
func TestHistoryOutcome_OneReadingOfAnAnswer(t *testing.T) {
	for _, c := range []struct {
		tool, answer string
		args         map[string]any
		want         history.Outcome
	}{
		{"disk_create", `{"disk":{"id":"d-1"},"operation_id":"op-1"}`, nil, history.Outcome{Result: history.Created, ResourceID: "d-1", OperationID: "op-1"}},
		{"secret_create", `{"status":"created"}`, map[string]any{"path": "app/db"}, history.Outcome{Result: history.Created, ResourceID: "app/db"}},
		{"disk_delete", `{"status":"deleted"}`, map[string]any{"disk_id": "d-1"}, history.Outcome{Result: history.Deleted, ResourceID: "d-1"}},
		{"disk_delete", `{"status":"already_deleted"}`, map[string]any{"disk_id": "d-1"}, history.Outcome{Result: history.AlreadyDeleted, ResourceID: "d-1"}},
		{"vm_start", `{"status":"already_running"}`, nil, history.Outcome{Result: history.Found}},
		{"vm_reboot", `{"status":"reboot_accepted"}`, nil, history.Outcome{Result: history.Changed}},
		{"stack_apply", `{"status":"running","job_id":"j-1"}`, nil, history.Outcome{Result: history.Changed, JobID: "j-1"}},
		{"disk_create", `{"error":"x","reason":"QUOTA_EXCEEDED"}`, nil, history.Outcome{Result: history.Error, Reason: "QUOTA_EXCEEDED"}},
	} {
		if got := historyOutcome(c.tool, c.args, c.answer); got != c.want {
			t.Errorf("%s %s: %+v, want %+v", c.tool, c.answer, got, c.want)
		}
	}
}
