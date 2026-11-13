// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const (
	brBackup = "00000000-0000-4000-8000-0000000000f1"
	brSource = "00000000-0000-4000-8000-0000000000f2"
	brZone   = "00000000-0000-4000-8000-0000000000f3"
)

// restoreWorld is the compute API of backup_restore: one backup of a kind
// (disk when ""), in state (created when ""), of project (raProject when
// ""); the project's disks; a restore answered by refuse (a 4xx code), lost
// (a 502 after the disk is made) or taken, making a disk in made (available
// when "") — or none, never; dup makes a second disk of the name.
type restoreWorld struct {
	mu                sync.Mutex
	kind, state, proj string
	made              string
	refuse            string
	lost, never, dup  bool
	disks             []map[string]any
	restores          int
	sent              map[string]any
}

func (w *restoreWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec := httptest.NewRecorder()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups/"+brBackup:
		_, _ = rec.WriteString(w.backup())
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/disks":
		items := append([]map[string]any{}, w.disks...)
		b, _ := json.Marshal(map[string]any{"items": items, "total": len(items)})
		_, _ = rec.Write(b)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/disk-types":
		_, _ = rec.WriteString(`{"items":[{"id":"t-ssd","name":"SSD"},{"id":"t-hdd","name":"HDD"}],"total":2}`)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/backups/"+brBackup+"/action":
		w.restores++
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &w.sent)
		if w.refuse != "" {
			rec.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = rec.WriteString(`[{"code":"` + w.refuse + `","message":"refused"}]`)
			break
		}
		if !w.never {
			name := w.sent["resource"].(map[string]any)["target_config"].(map[string]any)["name"]
			st := w.made
			if st == "" {
				st = "available"
			}
			for i := 0; i <= boolInt(w.dup); i++ {
				w.disks = append(w.disks, map[string]any{"id": "new-" + strconv.Itoa(len(w.disks)), "name": name, "state": st, "size": float64(10)})
			}
		}
		if w.lost {
			rec.WriteHeader(http.StatusBadGateway)
			break
		}
		rec.WriteHeader(http.StatusCreated)
		_, _ = rec.WriteString(w.backup())
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (w *restoreWorld) backup() string {
	kind, st, proj := w.kind, w.state, w.proj
	if kind == "" {
		kind = "disk"
	}
	if st == "" {
		st = "created"
	}
	if proj == "" {
		proj = raProject
	}
	return `{"id":"` + brBackup + `","project_id":"` + proj + `","name":"before-change","state":"` + st + `","type":"full","size":10240,"created_at":"2026-11-12T10:00:00Z",` +
		`"resource":{"resource_id":"` + brSource + `","resource_type":"` + kind + `","resource_exists":true,"resource_state":{"name":"data","size":10,` +
		`"disk_type":{"id":"t-ssd","name":"SSD"},"availability_zone":{"id":"` + brZone + `","name":"ru.AZ-1"}}}}`
}

func restoreRegistry(t *testing.T, w *restoreWorld) *Registry {
	t.Helper()
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: w}, "")
	cloud.SetToken("t")
	old := httpretry.DefaultBase
	httpretry.DefaultBase = w
	t.Cleanup(func() { httpretry.DefaultBase = old })
	wait := backupRestoreWait
	backupRestoreWait = 50 * time.Millisecond // wall-clock: a fraction of the production wait, the world answers at once
	t.Cleanup(func() { backupRestoreWait = wait })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: raProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   cloud,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
			Timings: engine.Timings{VMPollInterval: time.Millisecond},
		}, nil
	})
}

func backupRestore(t *testing.T, w *restoreWorld, args map[string]any) (map[string]any, string, bool) {
	t.Helper()
	a := map[string]any{"backup_id": brBackup, "name": "restored"}
	for k, v := range args {
		a[k] = v
	}
	return callNamed(t, restoreRegistry(t, w), "backup_restore", a)
}

// The preview names the new disk — its size, type and zone from the backup
// — that the backup and its disk are not changed, and its price; nothing is
// sent before the consent (axes 1 and 4).
func TestBackupRestore_Preview(t *testing.T) {
	w := &restoreWorld{}
	m, text, _ := backupRestore(t, w, nil)
	will, _ := m["will_create"].(map[string]any)
	if m["status"] != "confirmation_required" || will["size_gb"] != float64(10) || will["disk_type"] != "SSD" ||
		will["disk_type_from"] != "backup" || will["zone_id"] != brZone || will["source_disk_id"] != brSource || will["unchanged"] == nil {
		t.Fatalf("want the question naming the new disk from the backup: %s", text)
	}
	if !strings.Contains(text, "not changed") || w.restores != 0 {
		t.Errorf("the preview does not say the backup and its disk stay, or sent the restore (%d): %s", w.restores, text)
	}
	// A type asked for: from the argument; one the project has not: USAGE.
	m, text, _ = backupRestore(t, &restoreWorld{}, map[string]any{"disk_type": "HDD"})
	if will, _ := m["will_create"].(map[string]any); will["disk_type"] != "HDD" || will["disk_type_from"] != "argument" {
		t.Errorf("want the type of the argument: %s", text)
	}
	if m, text, _ = backupRestore(t, &restoreWorld{}, map[string]any{"disk_type": "nvme"}); m["reason"] != "USAGE" {
		t.Errorf("a type the project has not: want USAGE: %s", text)
	}
}

// With the consent: one restore, its body the new disk's name and the type,
// and the disk it made is found by its name and answered with its id.
func TestBackupRestore_RestoresAndNamesTheDisk(t *testing.T) {
	w := &restoreWorld{}
	m, text, isErr := backupRestore(t, w, map[string]any{"confirmed": true})
	if isErr || m["status"] != "created" || m["disk_id"] != "new-0" || w.restores != 1 {
		t.Fatalf("want the disk created and named after one restore (sent %d): %s", w.restores, text)
	}
	cfg := w.sent["resource"].(map[string]any)["target_config"].(map[string]any)
	if w.sent["action"] != "restore" || cfg["name"] != "restored" || cfg["disk_type_name"] != "SSD" {
		t.Errorf("the body of the restore: %v", w.sent)
	}
}

// Before the question, with nothing sent: a backup not created, of a VM, of
// another project; a name a disk of the project has.
func TestBackupRestore_RefusedBeforeTheQuestion(t *testing.T) {
	for _, c := range []struct {
		what string
		w    *restoreWorld
	}{
		{"a VM's backup", &restoreWorld{kind: "virtual_machine"}},
		{"another project's backup", &restoreWorld{proj: "00000000-0000-0000-0000-0000000000e9"}},
		{"a name taken", &restoreWorld{disks: []map[string]any{{"id": "d1", "name": "restored", "state": "available"}}}},
	} {
		for _, confirmed := range []bool{false, true} {
			m, text, isErr := backupRestore(t, c.w, map[string]any{"confirmed": confirmed})
			if !isErr || m["reason"] != "USAGE" || c.w.restores != 0 {
				t.Errorf("%s: want USAGE and nothing sent (sent %d): %s", c.what, c.w.restores, text)
			}
		}
	}
}

// A lost answer of the restore: the disk is found by its name, not asked
// for again; a refusal of the platform is its error; a restore taken whose
// disk is not listed yet is restoring with disk_list as the next step; two
// disks of the name are not chosen between (conclusion 164, У5).
func TestBackupRestore_TheAnswerByWhatTheDisksAre(t *testing.T) {
	w := &restoreWorld{lost: true}
	if m, text, isErr := backupRestore(t, w, map[string]any{"confirmed": true}); isErr || m["disk_id"] != "new-0" || w.restores != 1 {
		t.Errorf("a lost answer: want the disk found by name, one restore (sent %d): %s", w.restores, text)
	}
	w = &restoreWorld{refuse: "backup_restore_in_progress"}
	if m, text, isErr := backupRestore(t, w, map[string]any{"confirmed": true}); !isErr || m["reason"] == "USAGE" || len(w.disks) != 0 {
		t.Errorf("a refusal: want its error: %s", text)
	}
	w = &restoreWorld{never: true}
	if m, text, isErr := backupRestore(t, w, map[string]any{"confirmed": true}); isErr || m["status"] != "restoring" || m["next_tool"] != "backup_restore" {
		t.Errorf("no disk yet: want restoring and the same call next: %s", text)
	}
	w = &restoreWorld{dup: true}
	m, text, isErr := backupRestore(t, w, map[string]any{"confirmed": true})
	if ids, _ := m["disk_ids"].([]any); isErr || m["status"] != "restoring" || len(ids) != 2 || !strings.Contains(text, "Ask the person") {
		t.Errorf("two disks of the name: want both ids and the person's choice: %s", text)
	}
	w = &restoreWorld{made: "error"}
	if m, text, isErr := backupRestore(t, w, map[string]any{"confirmed": true}); !isErr || m["reason"] != "API_ERROR" || m["disk_id"] != "new-0" {
		t.Errorf("a disk in error: want API_ERROR with its id: %s", text)
	}
}

// backupRestoreStateRows is what backup_restore answers without the consent
// for a backup in each state of the specification (compute:BackupState):
// only a created backup is restored from — the question; any other state
// is a refusal before it, nothing sent. The world of the states.
var backupRestoreStateRows = map[string]string{
	"created": "confirmation_required", "creating": "USAGE", "updating": "USAGE", "error_creating": "USAGE",
	"error": "USAGE", "deleting": "USAGE", "error_deleting": "USAGE",
}

func init() {
	addStateRows(stateRowsOf, map[string]map[string]string{"backup_restore": backupRestoreStateRows})
}

func TestBackupRestore_EveryStateOfTheSpec(t *testing.T) {
	for st, want := range backupRestoreStateRows {
		w := &restoreWorld{state: st}
		m, text, _ := backupRestore(t, w, nil)
		got, _ := m["status"].(string)
		if want == "USAGE" {
			got, _ = m["reason"].(string)
		}
		if got != want || w.restores != 0 {
			t.Errorf("%s: want %s and nothing sent (sent %d): %s", st, want, w.restores, text)
		}
	}
}
