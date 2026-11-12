// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const (
	bcDisk = "00000000-0000-4000-8000-0000000000d2"
	bcVM   = "00000000-0000-4000-8000-0000000000a2"
	bcNew  = "00000000-0000-4000-8000-0000000000b9"
	// bcForeign is a disk of another project.
	bcForeign = "00000000-0000-4000-8000-0000000000d9"
)

// createWorld is the compute API of backup_create: a disk and a VM to back
// up, the backups already kept, and the backup a POST makes — created after
// readyAfter reads of its card.
type createWorld struct {
	mu         sync.Mutex
	kept       []string // backups of the disk already kept: their JSON
	posts      []string
	readyAfter int
	reads      int
	finalState string // "" for created
	postFails  bool   // the POST is answered 502: a lost answer
	// unlisted: the backup of a lost answer is not in the list yet.
	unlisted bool
	// failReads: the first reads of the new backup are answered 503.
	failReads int
}

func (w *createWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec := httptest.NewRecorder()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/disks/"+bcDisk:
		_, _ = rec.WriteString(`{"id":"` + bcDisk + `","name":"data","size":10,"project_id":"` + raProject + `","availability_zone":{"name":"ru.AZ-1"}}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/disks/"+bcForeign:
		_, _ = rec.WriteString(`{"id":"` + bcForeign + `","name":"theirs","size":10,"project_id":"00000000-0000-4000-8000-0000000000f0"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/vms/"+bcVM:
		_, _ = rec.WriteString(`{"id":"` + bcVM + `","name":"web","state":"running"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/disks":
		_, _ = rec.WriteString(`{"items":[{"id":"x1","name":"boot","size":20,"availability_zone":{"name":"ru.AZ-1"},"vms":[{"id":"` + bcVM + `"}]},` +
			`{"id":"x2","name":"data","size":30,"vms":[{"id":"` + bcVM + `"}]},{"id":"x3","name":"other","size":99,"vms":[]},` +
			`{"id":"` + bcDisk + `","name":"data","size":10,"vms":[]}],"total":4}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/vms":
		_, _ = rec.WriteString(`{"items":[{"id":"` + bcVM + `","name":"web","state":"running"}],"total":1}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups":
		items := append([]string{}, w.kept...)
		if len(w.posts) > 0 && !w.unlisted {
			items = append(items, w.newBackup("creating"))
		}
		_, _ = rec.WriteString(`{"items":[` + strings.Join(items, ",") + `],"total":` + jsonInt(len(items)) + `}`)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/backups":
		b, _ := io.ReadAll(r.Body)
		w.posts = append(w.posts, string(b))
		if w.postFails {
			rec.WriteHeader(http.StatusBadGateway)
			return rec.Result(), nil
		}
		rec.WriteHeader(http.StatusCreated)
		_, _ = rec.WriteString(w.newBackup("creating"))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups/"+bcNew:
		if w.failReads > 0 {
			w.failReads--
			rec.WriteHeader(http.StatusServiceUnavailable)
			return rec.Result(), nil
		}
		w.reads++
		st := "creating"
		if w.reads >= w.readyAfter {
			st = "created"
			if w.finalState != "" {
				st = w.finalState
			}
		}
		_, _ = rec.WriteString(w.newBackup(st))
	default:
		rec.WriteHeader(http.StatusNotFound)
		_, _ = rec.WriteString(`{"detail":"not found"}`)
	}
	return rec.Result(), nil
}

func (w *createWorld) newBackup(state string) string {
	return `{"id":"` + bcNew + `","name":"before-change","state":"` + state + `","type":"full","size":10240,"created_at":"2026-11-12T10:00:00Z",` +
		`"resource":{"resource_id":"` + bcDisk + `","resource_type":"disk","resource_state":{"name":"data"}}}`
}

func keptBackup(id, name, createdAt string) string {
	return keptBackupIn(id, name, createdAt, "created")
}

func keptBackupIn(id, name, createdAt, state string) string {
	return `{"id":"` + id + `","name":"` + name + `","state":"` + state + `","type":"full","size":2048,"created_at":"` + createdAt + `",` +
		`"resource":{"resource_id":"` + bcDisk + `","resource_type":"disk","resource_state":{"name":"data"}}}`
}

func jsonInt(n int) string { b, _ := json.Marshal(n); return string(b) }

// createRegistry is raRegistry over the world, with a poll interval of a
// millisecond: the world does not wait the production interval (CLAUDE.md §5).
func createRegistry(t *testing.T, w *createWorld) *Registry {
	t.Helper()
	return createRegistryIn(t, w, "")
}

// createRegistryIn is createRegistry whose engine keeps its state in dir:
// the records of the calls with consent (D92) are written there.
func createRegistryIn(t *testing.T, w *createWorld, dir string) *Registry {
	t.Helper()
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: w}, "")
	cloud.SetToken("t")
	old := httpretry.DefaultBase
	httpretry.DefaultBase = w
	t.Cleanup(func() { httpretry.DefaultBase = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: raProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			Timings:  engine.Timings{VMPollInterval: time.Millisecond},
			StateDir: dir,
		}, nil
	})
}

// The preview names the source, what is kept already and what the backup
// costs — «price unknown, billable» (no tariff line, Г-21) — and that the
// platform keeps it with no term (axes 1 and 4; «Риск», 164, У2). Nothing is
// sent before the consent.
func TestBackupCreate_Preview(t *testing.T) {
	w := &createWorld{kept: []string{keptBackup("k1", "nightly", "2026-11-10T00:00:00Z"), keptBackup("k2", "weekly", "2026-11-05T00:00:00Z")}}
	m, text, _ := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("want the question: %s", text)
	}
	will, _ := m["will_create"].(map[string]any)
	for k, want := range map[string]any{"resource_type": "disk", "resource_id": bcDisk, "resource_name": "data", "resource_size_gb": float64(10),
		"backups_of_resource": float64(2), "backups_of_resource_size_mb": float64(4096)} {
		if will[k] != want {
			t.Errorf("will_create.%s is %v, want %v: %s", k, will[k], want, text)
		}
	}
	if !strings.Contains(text, "paid_unpriced") || !strings.Contains(text, "until it is deleted") || !strings.Contains(text, "billable backup") {
		t.Errorf("the preview: want the price class «unknown, billable» and the term of keeping: %s", text)
	}
	if len(w.posts) != 0 {
		t.Errorf("the preview sent %v", w.posts)
	}

	// A VM: its size is the sum of its disks.
	m, text, _ = callNamed(t, createRegistry(t, &createWorld{}), "backup_create", map[string]any{"vm_id": bcVM, "name": "vm-copy"})
	will, _ = m["will_create"].(map[string]any)
	if will["resource_type"] != "virtual_machine" || will["resource_size_gb"] != float64(50) || will["resource_disks"] != float64(2) {
		t.Errorf("a VM: want its two disks and 50 GB: %s", text)
	}
}

// A backup of the source with the same name is the answer of a repeat —
// already_exists, no question, nothing sent — and it says its age and that
// no new backup was made (conclusion 164, У1): an old «before-change» is not
// a backup of the resource as it is now.
func TestBackupCreate_RepeatNamesTheAge(t *testing.T) {
	old := clock.Now
	clock.Now = func() time.Time { return time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { clock.Now = old })
	for _, confirmed := range []bool{false, true} {
		w := &createWorld{kept: []string{keptBackup("k-old", "before-change", "2026-08-12T12:00:00Z")}}
		m, text, isErr := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": confirmed})
		note, _ := m["note"].(string)
		if isErr || m["status"] != "already_exists" || m["backup_id"] != "k-old" || m["age_days"] != float64(92) || !strings.Contains(note, "No new backup was made") {
			t.Errorf("confirmed=%v: want already_exists with its age and the words that none was made: %s", confirmed, text)
		}
		if len(w.posts) != 0 {
			t.Errorf("a repeat sent %v", w.posts)
		}
	}
}

// With the consent: one POST of what the preview named, and the answer is
// the backup created, its id declared (W1).
func TestBackupCreate_MakesOneAndWaits(t *testing.T) {
	w := &createWorld{readyAfter: 2}
	reg := createRegistry(t, w)
	m, text, isErr := callNamed(t, reg, "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": true})
	if isErr || m["status"] != "created" || m["backup_id"] != bcNew {
		t.Fatalf("want created with the id: %s", text)
	}
	if len(w.posts) != 1 {
		t.Fatalf("want one POST, got %v", w.posts)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(w.posts[0]), &body)
	res, _ := body["resource"].(map[string]any)
	if body["name"] != "before-change" || body["project_id"] != raProject || res["resource_id"] != bcDisk || res["resource_type"] != "disk" ||
		body["tag_ids"] != nil || body["protection_plan_id"] != nil {
		t.Errorf("the body is what the preview named, and nothing more: %s", w.posts[0])
	}

	// wait=false: the answer of the take, with backup_get as the next step.
	w = &createWorld{readyAfter: 99}
	m, text, isErr = callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": true, "wait": false})
	next, _ := m["next_args"].(map[string]any)
	if isErr || m["status"] != "creating" || m["next_tool"] != "backup_get" || next["backup_id"] != bcNew {
		t.Errorf("wait=false: want creating and backup_get: %s", text)
	}
}

// A backup the platform ends in error: an error with its id, and the way out
// — it is kept and billed.
func TestBackupCreate_EndsInError(t *testing.T) {
	w := &createWorld{readyAfter: 1, finalState: "error_creating"}
	m, text, isErr := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": true})
	hint, _ := m["hint"].(string)
	if !isErr || m["backup_id"] != bcNew || !strings.Contains(hint, "billed") {
		t.Errorf("want the error with the backup's id and that it is billed: %s", text)
	}
}

// The answer of the POST is lost (502 after it went out): the backup is
// looked up by its name among the source's, not made a second time.
func TestBackupCreate_LostAnswerFindsIt(t *testing.T) {
	w := &createWorld{postFails: true}
	m, text, _ := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": true})
	if len(w.posts) != 1 || !strings.Contains(text, bcNew) {
		t.Errorf("want one POST and the backup found by its name: posts %d, %s", len(w.posts), text)
	}
	_ = m
}

// The source is not there: NOT_FOUND before the question, nothing sent.
func TestBackupCreate_NoSource(t *testing.T) {
	w := &createWorld{}
	m, text, isErr := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": "00000000-0000-4000-8000-0000000000ff", "name": "x"})
	if !isErr || m["reason"] != "NOT_FOUND" || len(w.posts) != 0 {
		t.Errorf("want NOT_FOUND and nothing sent: %s", text)
	}
}

// The id of the backup is in a line of progress before the wait: Ctrl-C in
// the tenth minute leaves no billed backup without its id on the screen
// («Риск», 164, У6).
func TestBackupCreate_IDBeforeTheWait(t *testing.T) {
	w := &createWorld{readyAfter: 3}
	tl, _ := createRegistry(t, w).Lookup("backup_create")
	var lines []string
	var mu sync.Mutex
	ctx := context.WithValue(context.Background(), progressKey{}, func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() })
	text, _ := tl.Handler(ctx, map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": true})
	mu.Lock()
	defer mu.Unlock()
	if len(lines) == 0 || !strings.Contains(lines[0], bcNew) || !strings.Contains(lines[0], "is being made") {
		t.Errorf("want the id in the first line of progress, before the wait: %q (%s)", lines, text)
	}
}

// A backup of the source with the name, by its state («Риск», 164, probe
// П1): only one made is already_exists; one being made is waited for; one in
// error or being deleted is no backup to rely on — a refusal, never a
// success. None sends a write.
func TestBackupCreate_TheNameTakenByState(t *testing.T) {
	for _, c := range []struct {
		state, status, reason, next string
		isErr                       bool
	}{
		{"created", "already_exists", "", "", false},
		{"creating", "creating", "", "backup_get", false},
		{"error_creating", "", "USAGE", "", true},
		{"error", "", "USAGE", "", true},
		{"deleting", "", "USAGE", "", true},
	} {
		w := &createWorld{kept: []string{keptBackupIn("k1", "before-change", "2026-11-12T00:00:00Z", c.state)}}
		m, text, isErr := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": true})
		if isErr != c.isErr || c.status != "" && m["status"] != c.status || c.reason != "" && m["reason"] != c.reason ||
			c.next != "" && m["next_tool"] != c.next || m["backup_id"] != "k1" {
			t.Errorf("%s: want status %q reason %q next %q with its id: %s", c.state, c.status, c.reason, c.next, text)
		}
		if c.state == "creating" && m["created_at"] != "2026-11-12T00:00:00Z" {
			t.Errorf("creating: want the time the backup was asked for, to tell it from the call's own: %s", text)
		}
		if c.state != "created" && strings.Contains(text, "already_exists") {
			t.Errorf("%s: a backup that is not made reads as one that exists: %s", c.state, text)
		}
		if len(w.posts) != 0 {
			t.Errorf("%s: sent %v", c.state, w.posts)
		}
	}
}

// Exactly one source, refused before any request (probe П2): both, or none.
func TestBackupCreate_ExactlyOneSource(t *testing.T) {
	for _, args := range []map[string]any{
		{"vm_id": bcVM, "disk_id": bcDisk, "name": "x", "confirmed": true},
		{"name": "x", "confirmed": true},
	} {
		w := &createWorld{}
		m, text, isErr := callNamed(t, createRegistry(t, w), "backup_create", args)
		if !isErr || m["reason"] != "USAGE" || len(w.posts) != 0 || w.reads != 0 {
			t.Errorf("%v: want USAGE and no request: %s", args, text)
		}
	}
}

// The answer of the POST is lost, the backup found by its name: the answer
// names it and backup_get as the next step (the audit, С4).
func TestBackupCreate_LostAnswerFoundNamesTheNextStep(t *testing.T) {
	w := &createWorld{postFails: true}
	m, text, _ := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": true})
	next, _ := m["next_args"].(map[string]any)
	if m["backup_id"] != bcNew || m["next_tool"] != "backup_get" || next["backup_id"] != bcNew || len(w.posts) != 1 {
		t.Errorf("want the found backup and backup_get: %s", text)
	}
}

// A failed read during the wait does not end it: only a backup that is gone
// does (the audit, С5).
func TestBackupCreate_FailedReadDoesNotEndTheWait(t *testing.T) {
	w := &createWorld{readyAfter: 1, failReads: 2}
	m, text, isErr := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcDisk, "name": "before-change", "confirmed": true})
	if isErr || m["status"] != "created" {
		t.Errorf("want created after the failed reads: %s", text)
	}
}

// A source of another project: refused before the question — the backup
// would go to the call's project, not the source's (the audit, С2).
func TestBackupCreate_SourceOfAnotherProject(t *testing.T) {
	w := &createWorld{}
	m, text, isErr := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"disk_id": bcForeign, "name": "x"})
	if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "not in project") || len(w.posts) != 0 {
		t.Errorf("want the refusal naming the project, nothing sent: %s", text)
	}
}

// The preview names the state of the source and that the platform chooses
// the type (the audit, С6); a backup of another resource with the same name
// is not this one's (mutation M2); the description goes into the POST and
// the preview alike (mutation M7).
func TestBackupCreate_PreviewStateTypeAndOnlyItsOwn(t *testing.T) {
	other := `{"id":"o1","name":"before-change","state":"created","created_at":"2026-01-01T00:00:00Z","resource":{"resource_id":"someone-else","resource_type":"disk"}}`
	w := &createWorld{kept: []string{other}, readyAfter: 1}
	m, text, _ := callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"vm_id": bcVM, "name": "before-change", "description": "pre-upgrade"})
	will, _ := m["will_create"].(map[string]any)
	if m["status"] != "confirmation_required" || will["resource_state"] != "running" || !strings.Contains(fmt.Sprint(will["type"]), "platform") ||
		will["backups_of_resource"] != float64(0) || will["description"] != "pre-upgrade" {
		t.Fatalf("want the question with the state, the type, no backup of its own and the description: %s", text)
	}
	_, _, _ = callNamed(t, createRegistry(t, w), "backup_create", map[string]any{"vm_id": bcVM, "name": "before-change", "description": "pre-upgrade", "confirmed": true})
	if len(w.posts) != 1 || !strings.Contains(w.posts[0], `"description":"pre-upgrade"`) {
		t.Errorf("want one POST with the description: %v", w.posts)
	}
}

// In --json the line with the id goes to stderr as well: Step says nothing
// in that mode (the audit, Б3).
func TestBackupCreate_AnnounceInJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	ctx := WithCaller(context.Background(), &Caller{Progress: output.NewWithWriters(&out, &errOut, output.ModeJSON, false)})
	announce(ctx, "backup x ("+bcNew+") is being made")
	if !strings.Contains(errOut.String(), `"progress"`) || !strings.Contains(errOut.String(), bcNew) || out.Len() != 0 {
		t.Errorf("want a progress line with the id on stderr: out %q, err %q", out.String(), errOut.String())
	}
}
