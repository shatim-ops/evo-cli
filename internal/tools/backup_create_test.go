// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const (
	bcDisk = "00000000-0000-4000-8000-0000000000d2"
	bcVM   = "00000000-0000-4000-8000-0000000000a2"
	bcNew  = "00000000-0000-4000-8000-0000000000b9"
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
}

func (w *createWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec := httptest.NewRecorder()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/disks/"+bcDisk:
		_, _ = rec.WriteString(`{"id":"` + bcDisk + `","name":"data","size":10,"availability_zone":{"name":"ru.AZ-1"}}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/vms/"+bcVM:
		_, _ = rec.WriteString(`{"id":"` + bcVM + `","name":"web","state":"running"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/disks":
		_, _ = rec.WriteString(`{"items":[{"id":"x1","name":"boot","size":20,"availability_zone":{"name":"ru.AZ-1"},"vms":[{"id":"` + bcVM + `"}]},` +
			`{"id":"x2","name":"data","size":30,"vms":[{"id":"` + bcVM + `"}]},{"id":"x3","name":"other","size":99,"vms":[]}],"total":3}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups":
		items := append([]string{}, w.kept...)
		if len(w.posts) > 0 && !w.postFails || w.postFails && len(w.posts) > 0 {
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
	return `{"id":"` + id + `","name":"` + name + `","state":"created","type":"full","size":2048,"created_at":"` + createdAt + `",` +
		`"resource":{"resource_id":"` + bcDisk + `","resource_type":"disk","resource_state":{"name":"data"}}}`
}

func jsonInt(n int) string { b, _ := json.Marshal(n); return string(b) }

func createRegistry(t *testing.T, w *createWorld) *Registry {
	t.Helper()
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: w}, "")
	cloud.SetToken("t")
	return raRegistry(t, w, cloud, nil)
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
	for k, want := range map[string]any{"source_type": "disk", "source_id": bcDisk, "source_name": "data", "source_size_gb": float64(10),
		"backups_of_source": float64(2), "backups_of_source_size_mb": float64(4096)} {
		if will[k] != want {
			t.Errorf("will_create.%s is %v, want %v: %s", k, will[k], want, text)
		}
	}
	if !strings.Contains(text, "paid_unpriced") || !strings.Contains(text, "until it is deleted") {
		t.Errorf("the preview: want the price class «unknown, billable» and the term of keeping: %s", text)
	}
	if len(w.posts) != 0 {
		t.Errorf("the preview sent %v", w.posts)
	}

	// A VM: its size is the sum of its disks.
	m, text, _ = callNamed(t, createRegistry(t, &createWorld{}), "backup_create", map[string]any{"vm_id": bcVM, "name": "vm-copy"})
	will, _ = m["will_create"].(map[string]any)
	if will["source_type"] != "virtual_machine" || will["source_size_gb"] != float64(50) || will["source_disks"] != float64(2) {
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
