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
	"slices"
	"strings"
	"testing"
	"time"

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

// What an answer says for the journal: one reading for every point. A
// line closes only on an outcome that is certain; in doubt it is live
// («Риск», conclusion 300, У1–У5): an «already» of a create carries its id;
// a refusal that names what the call made is unknown with that id; a
// delete that goes on is unknown; a create with no id is unknown, not a
// change.
func TestHistoryOutcome_OneReadingOfAnAnswer(t *testing.T) {
	for _, c := range []struct {
		tool, answer string
		args         map[string]any
		want         history.Outcome
	}{
		{"disk_create", `{"disk":{"id":"d-1"},"operation_id":"op-1"}`, nil, history.Outcome{Result: history.Created, Kind: "disk", ResourceID: "d-1", OperationID: "op-1"}},
		{"secret_create", `{"status":"created"}`, map[string]any{"path": "app/db"}, history.Outcome{Result: history.Created, Kind: "secret", ResourceID: "app/db"}},
		{"disk_delete", `{"status":"deleted"}`, map[string]any{"disk_id": "d-1"}, history.Outcome{Result: history.Deleted, Kind: "disk", ResourceID: "d-1"}},
		{"disk_delete", `{"status":"already_deleted"}`, map[string]any{"disk_id": "d-1"}, history.Outcome{Result: history.AlreadyDeleted, Kind: "disk", ResourceID: "d-1"}},
		{"vm_start", `{"status":"already_running"}`, nil, history.Outcome{Result: history.Changed}},
		{"vm_reboot", `{"status":"reboot_accepted"}`, nil, history.Outcome{Result: history.Changed}},
		// API_ERROR is an outcome not known (D92): the line stays live.
		{"disk_create", `{"error":"x","reason":"API_ERROR"}`, nil, history.Outcome{Result: history.Unknown, Kind: "disk", Reason: "API_ERROR"}},
		// A refusal that is certain: nothing made.
		{"disk_create", `{"error":"x","reason":"USAGE"}`, nil, history.Outcome{Result: history.Error, Kind: "disk", Reason: "USAGE"}},
		// У1: the create's «already» names the resource of the name.
		{"disk_create", `{"status":"already_exists","disk":{"id":"d-1"}}`, nil, history.Outcome{Result: history.Found, Kind: "disk", ResourceID: "d-1"}},
		// У2: a refusal that names what was made — a VM in an error state,
		// billed — and a stack in part.
		{"provision", `{"error":"x","reason":"API_ERROR","details":{"vm_id":"vm-1","floating_ip_id":"fip-1"}}`, nil, history.Outcome{Result: history.Unknown, Kind: "VM", Reason: "API_ERROR", ResourceID: "vm-1"}},
		// 300-1: a create named by its argument, refused: no id from the
		// argument — the refusal made nothing.
		{"secret_create", `{"error":"x","reason":"USAGE"}`, map[string]any{"path": "app/db"}, history.Outcome{Result: history.Error, Kind: "secret", Reason: "USAGE"}},
		// У6: a stack is told by its job — its steps go round the record
		// of an intent (journalDepthKey), so the line of the job is what
		// keeps what the stack made live.
		{"stack_apply", `{"status":"running","job_id":"j-1"}`, nil, history.Outcome{Result: history.Created, Kind: "stack", ResourceID: "j-1", JobID: "j-1"}},
		{"stack_apply", `{"status":"done","job_id":"j-1"}`, nil, history.Outcome{Result: history.Created, Kind: "stack", ResourceID: "j-1", JobID: "j-1"}},
		{"stack_apply", `{"status":"error","job_id":"j-1","reason":"API_ERROR"}`, nil, history.Outcome{Result: history.Unknown, Kind: "stack", ResourceID: "j-1", JobID: "j-1", Reason: "API_ERROR"}},
		{"stack_destroy", `{"status":"destroyed","job_id":"j-1"}`, nil, history.Outcome{Result: history.Deleted, Kind: "stack", ResourceID: "j-1", JobID: "j-1"}},
		{"stack_destroy", `{"status":"already_destroyed","job_id":"j-1"}`, nil, history.Outcome{Result: history.Deleted, Kind: "stack", ResourceID: "j-1", JobID: "j-1"}},
		{"stack_destroy", `{"status":"deleting","job_id":"j-1"}`, nil, history.Outcome{Result: history.Unknown, Kind: "stack", ResourceID: "j-1", JobID: "j-1"}},
		{"stack_destroy", `{"status":"destroy_failed","job_id":"j-1","reason":"API_ERROR"}`, nil, history.Outcome{Result: history.Unknown, Kind: "stack", ResourceID: "j-1", JobID: "j-1", Reason: "API_ERROR"}},
		// 300-1: a call of a stack refused whole — a job_id mistyped —
		// made nothing; 300-2: a job_id in the answer of a tool that is
		// not a stack's tells no stack.
		{"stack_apply", `{"error":"no such job","reason":"NOT_FOUND"}`, map[string]any{"job_id": "j-typo"}, history.Outcome{Result: history.Error, Kind: "stack", JobID: "j-typo", Reason: "NOT_FOUND"}},
		{"disk_create", `{"disk":{"id":"d-1"},"job_id":"j-1"}`, nil, history.Outcome{Result: history.Created, Kind: "disk", ResourceID: "d-1", JobID: "j-1"}},
		// У3: a delete that goes on.
		{"lb_delete", `{"status":"deleting","operation_id":"op-2","next_tool":"lb_operation"}`, map[string]any{"id": "lb-1"}, history.Outcome{Result: history.Unknown, Kind: "load balancer", ResourceID: "lb-1", OperationID: "op-2"}},
		{"destroy", `{"status":"deleting"}`, map[string]any{"vm_id": "vm-1"}, history.Outcome{Result: history.Unknown, Kind: "VM", ResourceID: "vm-1"}},
		// У5: a create whose answer names no id.
		{"pg_create", `{"status":"creating","operation_id":"op-1"}`, nil, history.Outcome{Result: history.Unknown, Kind: "PostgreSQL cluster", OperationID: "op-1"}},
	} {
		if got := historyOutcome(c.tool, c.args, c.answer); got != c.want {
			t.Errorf("%s %s:\n got %+v\nwant %+v", c.tool, c.answer, got, c.want)
		}
	}
}

// What a settled record says (У1): the cut call made what its repeat made
// or found — found, with the id; with no id, unknown; a read of evo that
// lists a create with no id leaves it unknown, not found.
func TestHistory_ASettledRecordKeepsTheResource(t *testing.T) {
	for _, c := range []struct {
		in   history.Outcome
		want string
	}{
		{history.Outcome{Result: history.Created, ResourceID: "d-1"}, history.Found},
		{history.Outcome{Result: history.Found, ResourceID: "d-1"}, history.Found},
		{history.Outcome{Result: history.Created}, history.Unknown},
		{history.Outcome{Result: history.Unknown, ResourceID: "d-1"}, history.Unknown},
		{history.Outcome{Result: history.Changed}, history.Changed},
	} {
		if got := settledOf(c.in).Result; got != c.want {
			t.Errorf("settledOf(%+v) = %s, want %s", c.in, got, c.want)
		}
	}
	if o := settledOutcome(intent.Intent{Tool: "disk_create", Name: "data"}, ""); o.Result != history.Unknown {
		t.Errorf("a create listed with no id: %+v, want unknown", o)
	}
	if o := settledOutcome(intent.Intent{Tool: "disk_create", Name: "data"}, "d-1"); o.Result != history.Found || o.ResourceID != "d-1" || o.Kind != "disk" {
		t.Errorf("a create listed with its id: %+v", o)
	}
	if o := settledOutcome(intent.Intent{Tool: "disk_delete", Target: "disk_id=d-1"}, ""); o.Result != history.Deleted || o.ResourceID != "d-1" {
		t.Errorf("a delete whose resource is gone: %+v", o)
	}
	// The cut create, then its repeat answered «already exists»: the
	// resource is live (the probe of У1).
	lines := []history.Line{
		{Format: history.Format, Tool: "disk_create", IntentID: "a", Outcome: history.Unknown},
		{Format: history.Format, Tool: "disk_create", IntentID: "b", Outcome: history.Found, Kind: "disk", ResourceID: "d-1"},
		{Format: history.Format, Tool: "disk_create", IntentID: "a", Outcome: history.Found, Kind: "disk", ResourceID: "d-1"},
	}
	if live := history.Live(lines); len(live) == 0 {
		t.Error("a create cut and found by its repeat is not live")
	}
}

// A resource of a parent is told by its parent too (Астра, #2165): a
// database, a user and an object of the same name in two parents are two
// resources, and the delete in one closes the create in that one only.
func TestHistory_TheParentTellsTheResource(t *testing.T) {
	for _, c := range []struct{ create, delete, parent, name string }{
		{"pg_db_create", "pg_db_delete", "cluster_id", "name"},
		{"pg_user_create", "pg_user_delete", "cluster_id", "name"},
		{"s3_put", "s3_rm", "bucket", "key"},
	} {
		line := func(intentID, tool, parent string) history.Line {
			out := historyOutcome(tool, map[string]any{c.parent: parent, c.name: "same-name"}, `{"status":"ok"}`)
			return history.Line{Format: history.Format, IntentID: intentID, ProjectID: "p", Kind: out.Kind,
				Parent: out.Parent, ResourceID: out.ResourceID, Outcome: out.Result}
		}
		lines := []history.Line{line("a", c.create, "parent-A"), line("b", c.create, "parent-B"), line("c", c.delete, "parent-A")}
		live := history.Live(lines)
		if len(live) != 1 || live[0].IntentID != "b" {
			t.Errorf("%s in two parents, deleted in one: live %+v, want the create in parent-B", c.create, live)
		}
	}
}

// A stack of two steps through the gate (Астра, #2166; «Риск», conclusion
// 300, У6): its steps go round the record of an intent, so the line of
// stack_apply names the job — created, kind stack, its id — and the line
// of stack_destroy closes it.
func TestHistory_AStackIsToldByItsJob(t *testing.T) {
	stackHome(t)
	a := &faultAPI{made: []faultResource{{host: faultS3Host, path: "bucket", ID: "evo-bucket", Name: "evo-bucket", before: true}}}
	r := faultAPIRegistry(t, a)
	j := planJob(t, `{"name":"st","secrets":[{"path":"evo-s","value":"synthetic"}],"s3":[{"bucket":"evo-bucket","key":"site/k","content":"c"}]}`)
	if out, failed := stackCall(t, r, "stack_apply", map[string]any{"job_id": j.ID, "confirmed": true}); failed || out["status"] != "done" {
		t.Fatalf("apply: %v", out)
	}
	dir, _ := r.stateDir()
	lines, raw := historyOf(t, dir)
	var applied *history.Line
	for i, l := range lines {
		if l.Tool == "stack_apply" {
			applied = &lines[i]
		}
	}
	if applied == nil || applied.Outcome != history.Created || applied.Kind != "stack" || applied.ResourceID != j.ID {
		t.Fatalf("the line of stack_apply: %+v; want created, kind stack, the job's id", applied)
	}
	if strings.Contains(raw, "synthetic") {
		t.Errorf("the value of a secret in the journal:\n%s", raw)
	}
	if out, failed := stackCall(t, r, "stack_destroy", map[string]any{"job_id": j.ID, "confirmed": true}); failed {
		t.Fatalf("destroy: %v", out)
	}
	lines, _ = historyOf(t, dir)
	last := lines[len(lines)-1]
	if last.Tool != "stack_destroy" || last.Outcome != history.Deleted || last.Kind != "stack" || last.ResourceID != j.ID {
		t.Errorf("the line of stack_destroy: %+v; want deleted, kind stack, the job's id", last)
	}
}

// A create by name cut after it sent — the network, a time limit, Ctrl-C —
// may have made its resource: the line is unknown, with the name of its
// argument, and live; only a refusal that is certain is an error («Риск»,
// conclusion 311, У2). Through the gate, end to end.
func TestHistory_ACutCreateByNameStaysLive(t *testing.T) {
	for _, reason := range []string{"API_UNREACHABLE", "INTERRUPTED", "API_ERROR"} {
		r, dir := targetRegistry(t, &goneCloud{})
		tool, _ := r.Lookup("secret_create")
		h := r.intended(tool, func(ctx context.Context, _ map[string]any) (string, bool) {
			_ = httpretry.NoteWrite(ctx)
			return `{"error":"cut","reason":"` + reason + `"}`, true
		})
		h(context.Background(), map[string]any{"path": "app/db", "value": "s3cr3t-value", "confirmed": true})
		lines, raw := historyOf(t, dir)
		live := history.Live(lines)
		if len(lines) != 1 || lines[0].Outcome != history.Unknown || lines[0].ResourceID != "app/db" || len(live) != 1 {
			t.Errorf("%s: lines %+v, live %d; want unknown with the path, live", reason, lines, len(live))
		}
		if strings.Contains(raw, "s3cr3t-value") {
			t.Errorf("%s: the value of the secret in the journal", reason)
		}
	}
}

// An earlier delete does not close a later create of the same resource
// whatever the form of their times: a line whose time is not evo's form —
// a fraction of a second — is skipped and counted, not ordered by its text
// before or after the others (Астра, #2229).
func TestHistory_ATimeNotOfEvosFormDoesNotReorder(t *testing.T) {
	r, dir := targetRegistry(t, &goneCloud{})
	hdir := history.DirOf(intent.Dir(dir))
	if err := os.MkdirAll(hdir, 0o700); err != nil {
		t.Fatal(err)
	}
	lines := `{"format":1,"time":"2026-11-17T10:00:00Z","tool":"disk_delete","project_id":"p","kind":"disk","resource_id":"same","outcome":"deleted","intent_id":"0000000000000001"}` + "\n" +
		`{"format":1,"time":"2026-11-17T10:00:00.5Z","tool":"disk_create","project_id":"p","kind":"disk","resource_id":"same","outcome":"created","intent_id":"0000000000000002"}` + "\n"
	if err := os.WriteFile(filepath.Join(hdir, "2026-11-17.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	text, _ := r.handlers["history"](context.Background(), map[string]any{"since": "2026-01-01"})
	var a struct {
		Lines   []historyLine `json:"lines"`
		Skipped int           `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		t.Fatal(err)
	}
	if len(a.Lines) != 1 || a.Lines[0].Outcome != history.Deleted || a.Skipped != 1 {
		t.Errorf("lines %+v, skipped %d; want the delete, and the line of a fraction of a second skipped\n%s", a.Lines, a.Skipped, text)
	}
}

// The journal is pruned after a call that changes the cloud, not on its
// way (the ТЗ of the journal, §0, 9): an old file of a day goes, its live
// line stays.
func TestHistory_ACallPrunesTheJournalAfterItsLine(t *testing.T) {
	r, dir := targetRegistry(t, &goneCloud{})
	hdir := history.DirOf(intent.Dir(dir))
	if err := os.MkdirAll(hdir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"format":1,"time":"2020-01-01T10:00:00Z","tool":"disk_create","kind":"disk","resource_id":"d-old","outcome":"created","intent_id":"f000000000000001"}`
	if err := os.WriteFile(filepath.Join(hdir, "2020-01-01.jsonl"), []byte(old+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool, _ := r.Lookup("disk_create")
	h := r.intended(tool, func(context.Context, map[string]any) (string, bool) { return `{"disk":{"id":"d-1"}}`, false })
	h(context.Background(), map[string]any{"name": "disk-1", "size": 10, "confirmed": true})
	if _, err := os.Stat(filepath.Join(hdir, "2020-01-01.jsonl")); !os.IsNotExist(err) {
		t.Error("the old file of a day is still there after a call")
	}
	lines, _ := historyOf(t, dir)
	var ids []string
	for _, l := range lines {
		ids = append(ids, l.ResourceID)
	}
	if len(ids) != 2 || !slices.Contains(ids, "d-old") || !slices.Contains(ids, "d-1") {
		t.Errorf("the journal after the call: %v; want the live line of the old file and the call's", ids)
	}
}

// The pruning after a call checks each line as history does (historyCheck):
// a delete of a tool the registry does not know, or with a reason not of
// evo, closes nothing — the create it would close stays live, and both are
// kept (Астра, #2297).
func TestHistory_TheCallsPruningChecksLinesAsHistoryDoes(t *testing.T) {
	for _, mode := range []string{"tool", "reason"} {
		r, dir := targetRegistry(t, &goneCloud{})
		hdir := history.DirOf(intent.Dir(dir))
		if err := os.MkdirAll(hdir, 0o700); err != nil {
			t.Fatal(err)
		}
		made := `{"format":1,"time":"2020-01-01T10:00:00Z","tool":"disk_create","kind":"disk","resource_id":"still-live","outcome":"created","intent_id":"a000000000000001"}`
		odd := `{"format":1,"time":"2020-01-01T11:00:00Z","tool":"future_delete","kind":"disk","resource_id":"still-live","outcome":"deleted","intent_id":"a000000000000002"}`
		if mode == "reason" {
			odd = `{"format":1,"time":"2020-01-01T11:00:00Z","tool":"disk_delete","kind":"disk","resource_id":"still-live","outcome":"deleted","reason":"FUTURE_REASON","intent_id":"a000000000000002"}`
		}
		if err := os.WriteFile(filepath.Join(hdir, "2020-01-01.jsonl"), []byte(made+"\n"+odd+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		tool, _ := r.Lookup("disk_create")
		h := r.intended(tool, func(context.Context, map[string]any) (string, bool) { return `{"disk":{"id":"d-1"}}`, false })
		h(context.Background(), map[string]any{"name": "disk-1", "size": 10, "confirmed": true})
		j, _ := history.ReadAll(hdir, r.historyCheck)
		var live []string
		for _, l := range history.Live(j.Lines) {
			live = append(live, l.ResourceID)
		}
		if !slices.Contains(live, "still-live") {
			t.Errorf("%s: live after the call's pruning %v; want still-live kept", mode, live)
		}
	}
}

// doctor says a pruning of the journal was put off and names the files in
// the way, in its detail and in a field of their own — a pruning left undone
// in silence is a journal that grows with no one told why («Риск» to 324-3).
func TestDoctor_SaysAPruningPutOffAndTheFilesInTheWay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), history.DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A delete in LiveName, which evo never writes there, closes a create
	// of a file of a day that goes (TestPrune_LeftUndoneNamesTheFileInTheWay).
	write("2026-07-01.jsonl", `{"format":1,"time":"2026-07-01T10:00:00Z","tool":"disk_create","kind":"disk","resource_id":"d-1","outcome":"created","intent_id":"a000000000000001"}`)
	write(history.LiveName, `{"format":1,"time":"2026-07-15T10:00:00Z","tool":"disk_delete","kind":"disk","resource_id":"d-1","outcome":"deleted","intent_id":"a000000000000002"}`)
	write("retired", "2026-08-01")
	r := New(nil)
	if p, err := history.Prune(dir, time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC), r.historyCheck); err != nil || len(p.Deferred) == 0 {
		t.Fatalf("pruned %+v, %v; want it put off", p, err)
	}
	c, ok := r.journalCheck(dir)
	data, _ := json.Marshal(c)
	if !ok || c.Status != CheckWarn || !strings.Contains(c.Detail, history.LiveName) || !slices.Equal(c.Files, []string{history.LiveName}) {
		t.Errorf("doctor: %s; want a warning naming %s, in detail and in files", data, history.LiveName)
	}
}
