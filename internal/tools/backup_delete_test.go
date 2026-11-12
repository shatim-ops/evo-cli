// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	bdID     = "00000000-0000-4000-8000-0000000000e7"
	bdSource = "00000000-0000-4000-8000-0000000000e8"
)

// deleteWorld is the compute API of backup_delete: one backup in state, of a
// source that exists or not, restored at restored; a DELETE answered by
// refuse (a code of a 422) or taken, the backup gone after it; dependents
// name it as their full one.
type deleteWorld struct {
	mu         sync.Mutex
	state      string
	gone       bool
	exists     bool
	restored   string
	refuse     string
	dependents int
	deletes    int
}

func (w *deleteWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec := httptest.NewRecorder()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups/"+bdID:
		if w.gone || w.deletes > 0 && w.refuse == "" {
			rec.WriteHeader(http.StatusNotFound)
			_, _ = rec.WriteString(`{"detail":"not found"}`)
			break
		}
		_, _ = rec.WriteString(w.backup())
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups":
		items := []string{w.backup()}
		for i := 0; i < w.dependents; i++ {
			items = append(items, `{"id":"dep-`+strconv.Itoa(i)+`","name":"inc","state":"created","type":"incremental","parent_full_backup_id":"`+bdID+`"}`)
		}
		_, _ = rec.WriteString(`{"items":[` + strings.Join(items, ",") + `],"total":` + strconv.Itoa(len(items)) + `}`)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/backups/"+bdID:
		w.deletes++
		if w.refuse != "" {
			rec.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = rec.WriteString(`[{"code":"` + w.refuse + `","message":"refused"}]`)
			break
		}
		rec.WriteHeader(http.StatusNoContent)
	default:
		rec.WriteHeader(http.StatusNotFound)
	}
	return rec.Result(), nil
}

func (w *deleteWorld) backup() string {
	ex, rt := "false", "null"
	if w.exists {
		ex = "true"
	}
	if w.restored != "" {
		rt = `"` + w.restored + `"`
	}
	return `{"id":"` + bdID + `","name":"before-change","state":"` + w.state + `","type":"full","size":10240,"created_at":"2026-11-12T10:00:00Z",` +
		`"last_restore_time":` + rt + `,"resource":{"resource_id":"` + bdSource + `","resource_type":"disk","resource_exists":` + ex + `,"resource_state":{"name":"data"}}}`
}

func deleteRegistry(t *testing.T, w *deleteWorld) *Registry {
	t.Helper()
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: w}, "")
	cloud.SetToken("t")
	old := httpretry.DefaultBase
	httpretry.DefaultBase = w
	t.Cleanup(func() { httpretry.DefaultBase = old })
	wait := deleteOutcomeWait
	deleteOutcomeWait = 1
	t.Cleanup(func() { deleteOutcomeWait = wait })
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

func backupDelete(t *testing.T, w *deleteWorld, confirmed bool) (map[string]any, string, bool) {
	t.Helper()
	return callNamed(t, deleteRegistry(t, w), "backup_delete", map[string]any{"backup_id": bdID, "confirmed": confirmed})
}

// A backup is gone or being deleted: the answer of a repeat, with no
// question and no DELETE (D26, D61).
func TestBackupDelete_RepeatSendsNothing(t *testing.T) {
	for _, c := range []struct {
		w      *deleteWorld
		status string
	}{{&deleteWorld{gone: true}, "already_deleted"}, {&deleteWorld{state: "deleting", exists: true}, "deleting"}} {
		for _, confirmed := range []bool{false, true} {
			m, text, _ := backupDelete(t, c.w, confirmed)
			if m["status"] != c.status || c.w.deletes != 0 {
				t.Errorf("want %s and no DELETE (sent %d): %s", c.status, c.w.deletes, text)
			}
		}
	}
}

// The preview names the backup and its source, that restored resources
// stay, and warns: a backup of a source that is gone is the last copy of
// its data (У3а); the backups that depend on it (У3б); a restore of the
// last minutes. Nothing is sent before the consent.
func TestBackupDelete_Preview(t *testing.T) {
	old := clock.Now
	clock.Now = func() time.Time { return time.Date(2026, 11, 12, 12, 5, 0, 0, time.UTC) }
	t.Cleanup(func() { clock.Now = old })
	w := &deleteWorld{state: "created", exists: false, restored: "2026-11-12T12:01:00Z", dependents: 2}
	m, text, _ := backupDelete(t, w, false)
	warn, _ := m["warning"].(string)
	will, _ := m["will_delete"].(map[string]any)
	deps, _ := will["dependent_backups"].([]any)
	if m["status"] != "confirmation_required" || will["resource_exists"] != false || len(deps) != 2 || will["resource_name"] != "data" {
		t.Fatalf("want the preview with the source gone and two dependents: %s", text)
	}
	for _, says := range []string{"no longer exists", "2 backups name this one", "restore from this backup was asked for"} {
		if !strings.Contains(warn, says) {
			t.Errorf("the warning does not say %q: %s", says, text)
		}
	}
	if w.deletes != 0 {
		t.Errorf("the preview sent a DELETE")
	}

	// A backup of a source that exists, restored long ago, alone: no warning.
	m, text, _ = backupDelete(t, &deleteWorld{state: "created", exists: true, restored: "2026-11-01T00:00:00Z"}, false)
	if m["warning"] != nil {
		t.Errorf("nothing to warn of: %s", text)
	}
}

// With the consent: one DELETE, and the answer is the backup gone (D74).
func TestBackupDelete_Deletes(t *testing.T) {
	w := &deleteWorld{state: "created", exists: true}
	m, text, isErr := backupDelete(t, w, true)
	if isErr || m["status"] != "deleted" || w.deletes != 1 {
		t.Errorf("want deleted after one DELETE (sent %d): %s", w.deletes, text)
	}
}

// With wait=false: one DELETE, and the answer is deleting with the read
// that shows the end — no wait, and no operation to name (D74).
func TestBackupDelete_NoWait(t *testing.T) {
	w := &deleteWorld{state: "created", exists: true}
	m, text, isErr := callNamed(t, deleteRegistry(t, w), "backup_delete", map[string]any{"backup_id": bdID, "confirmed": true, "wait": false})
	if _, named := m["operation_id"]; isErr || m["status"] != "deleting" || w.deletes != 1 || m["next_tool"] != "backup_get" || named {
		t.Errorf("want deleting after one DELETE, backup_get next and no operation_id (sent %d): %s", w.deletes, text)
	}
}

// A restore under way: the platform refuses the DELETE though the backup
// reads created (one run of Г-16). The answer is an error with its cause and
// the same call, without the consent, as the next step — not a failure of
// the delete with no way on («Риск», 164, answer 2).
func TestBackupDelete_RestoreUnderWay(t *testing.T) {
	w := &deleteWorld{state: "created", exists: true, refuse: "backup_restore_in_progress"}
	m, text, isErr := backupDelete(t, w, true)
	details, _ := m["details"].(map[string]any)
	next, _ := m["next_args"].(map[string]any)
	if !isErr || m["reason"] != "API_ERROR" || details["cause"] != "restore_in_progress" || m["next_tool"] != "backup_delete" || next["backup_id"] != bdID {
		t.Fatalf("want the error with its cause and backup_delete as the next step: %s", text)
	}
	if _, carried := next["confirmed"]; carried {
		t.Errorf("the consent carried over to the next step (R-85): %s", text)
	}

	// A delete already under way, refused as not ready: deleting (D61).
	w = &deleteWorld{state: "created", exists: true, refuse: "backup_not_ready"}
	m, text, isErr = backupDelete(t, w, true)
	if isErr || m["status"] != "deleting" {
		t.Errorf("backup_not_ready: want deleting: %s", text)
	}
}

// A backup still being created or in error: not refused before the question
// — no run has shown the platform's answer (creatingUnseen, failedUnseen);
// the preview names the state.
func TestBackupDelete_StatesWithNoRuleYet(t *testing.T) {
	for _, st := range []string{"creating", "error", "error_creating", "error_deleting"} {
		w := &deleteWorld{state: st, exists: true}
		m, text, _ := backupDelete(t, w, false)
		target, _ := m["target"].(map[string]any)
		if m["status"] != "confirmation_required" || target["status"] != st || w.deletes != 0 {
			t.Errorf("%s: want the question naming the state: %s", st, text)
		}
	}
}
