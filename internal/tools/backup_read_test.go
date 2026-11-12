// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

const (
	backupDisk = "00000000-0000-4000-8000-0000000000b1"
	backupVM   = "00000000-0000-4000-8000-0000000000b2"
	backupGone = "00000000-0000-4000-8000-0000000000b3"
	backupSrc  = "00000000-0000-4000-8000-0000000000d1"
)

// backupItem is a backup of a disk as the compute API gives it (the
// published specification; the fields both runs of Г-16 saw).
func backupItem(id, name, state string, exists bool) string {
	ex := "false"
	if exists {
		ex = "true"
	}
	return `{"id":"` + id + `","name":"` + name + `","state":"` + state + `","type":"full","size":10240,` +
		`"project_id":"` + raProject + `","created_at":"2026-11-12T10:00:00Z","last_restore_time":null,"description":"before the change",` +
		`"tags":[{"id":"t1","name":"env","color":"red"}],"protection_plan":null,` +
		`"resource":{"resource_id":"` + backupSrc + `","resource_type":"disk","resource_exists":` + ex + `,` +
		`"resource_state":{"name":"data-disk","size":10,"disk_type":{"id":"dt","name":"SSD"},"availability_zone":{"id":"az","name":"ru.AZ-1"}}}}`
}

const backupVMItem = `{"id":"` + backupVM + `","name":"vm-copy","state":"creating","type":"full","size":0,"created_at":"2026-11-12T10:01:00Z",` +
	`"tags":[],"resource":{"resource_id":"` + backupSrc + `","resource_type":"virtual_machine","resource_exists":true,` +
	`"resource_state":{"name":"web","flavor":{"name":"gen-2-4"},"availability_zone":{"name":"ru.AZ-1"},` +
	`"disks":[{"name":"boot","size":20,"disk_type":{"name":"SSD"},"bootable":true}],` +
	`"interfaces":[{"name":"eth0","subnet":{"name":"main"},"ip_address":"10.0.0.5"}]}}}`

// backupWorld is the compute API of backups: a list and a card by id; it
// records what it was asked.
type backupWorld struct {
	mu   sync.Mutex
	reqs []string
}

func (w *backupWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	w.reqs = append(w.reqs, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	w.mu.Unlock()
	rec := httptest.NewRecorder()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups":
		_, _ = rec.WriteString(`{"items":[` + backupItem(backupDisk, "disk-copy", "created", true) + `,` + backupVMItem + `],"total":2,"limit":100,"offset":0}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups/"+backupDisk:
		_, _ = rec.WriteString(backupItem(backupDisk, "disk-copy", "created", true))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups/"+backupGone:
		_, _ = rec.WriteString(backupItem(backupGone, "orphan", "created", false))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/backups/"+backupVM:
		_, _ = rec.WriteString(backupVMItem)
	default:
		rec.WriteHeader(http.StatusNotFound)
		_, _ = rec.WriteString(`{"detail":"not found"}`)
	}
	return rec.Result(), nil
}

func (w *backupWorld) writes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, r := range w.reqs {
		if !strings.HasPrefix(r, "GET ") {
			out = append(out, r)
		}
	}
	return out
}

func backupRegistry(t *testing.T, w *backupWorld) *Registry {
	t.Helper()
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: w}, "")
	cloud.SetToken("t")
	return raRegistry(t, w, cloud, nil)
}

// A backup made in the console was not found by evo: no tool read
// /api/v1/backups (wave В5, axis 3). backup_list lists them with the
// source of each, the project's only, and writes nothing.
func TestBackupList_RowsWithTheirSource(t *testing.T) {
	w := &backupWorld{}
	m, text, isErr := callNamed(t, backupRegistry(t, w), "backup_list", map[string]any{})
	rows, _ := m["backups"].([]any)
	if isErr || len(rows) != 2 {
		t.Fatalf("want two backups: %s", text)
	}
	row, _ := rows[0].(map[string]any)
	for k, want := range map[string]any{
		"id": backupDisk, "name": "disk-copy", "state": "created", "type": "full", "size_mb": float64(10240),
		"resource_type": "disk", "resource_id": backupSrc, "resource_name": "data-disk", "created_at": "2026-11-12T10:00:00Z",
	} {
		if row[k] != want {
			t.Errorf("row.%s is %v, want %v: %s", k, row[k], want, text)
		}
	}
	if !strings.Contains(strings.Join(w.reqs, " "), "project_id="+raProject) {
		t.Errorf("the list is not of the call's project: %v", w.reqs)
	}
	if len(w.writes()) != 0 {
		t.Errorf("a read sent %v", w.writes())
	}

	// The filters of the list (listSpecs): the backups of one source, of one kind.
	m, text, _ = callNamed(t, backupRegistry(t, &backupWorld{}), "backup_list", map[string]any{"resource_type": "virtual_machine"})
	if rows, _ := m["backups"].([]any); len(rows) != 1 {
		t.Errorf("resource_type=virtual_machine: want the backup of the VM only: %s", text)
	}
}

// backup_get answers the card: the row, and the source as the backup holds
// it — of a disk its size, type and zone; of a VM its flavor, disks and
// interfaces (axis 4).
func TestBackupGet_TheSourceAsTheBackupHoldsIt(t *testing.T) {
	r := backupRegistry(t, &backupWorld{})
	m, text, isErr := callNamed(t, r, "backup_get", map[string]any{"backup_id": backupDisk})
	src, _ := m["source"].(map[string]any)
	if isErr || m["resource_exists"] != true || m["description"] != "before the change" || src["size_gb"] != float64(10) || src["disk_type"] != "SSD" || src["zone"] != "ru.AZ-1" {
		t.Errorf("the card of a disk's backup: %s", text)
	}
	if tags, _ := m["tags"].([]any); len(tags) != 1 || tags[0] != "env" {
		t.Errorf("tags: want their names: %s", text)
	}
	if m["note"] != nil {
		t.Errorf("a backup whose source exists needs no note: %s", text)
	}

	m, text, _ = callNamed(t, r, "backup_get", map[string]any{"backup_id": backupVM})
	src, _ = m["source"].(map[string]any)
	disks, _ := src["disks"].([]any)
	ifs, _ := src["interfaces"].([]any)
	if src["flavor"] != "gen-2-4" || len(disks) != 1 || len(ifs) != 1 {
		t.Errorf("the card of a VM's backup: want its flavor, disks and interfaces: %s", text)
	}
}

// The source is gone: the backup stays and is billed until it is deleted —
// the card says so (the specification: resource_exists false, the backups
// are kept with no term).
func TestBackupGet_SourceGone(t *testing.T) {
	m, text, _ := callNamed(t, backupRegistry(t, &backupWorld{}), "backup_get", map[string]any{"backup_id": backupGone})
	note, _ := m["note"].(string)
	if m["resource_exists"] != false || !strings.Contains(note, "billed") {
		t.Errorf("want resource_exists false and the note: %s", text)
	}
}

// No backup by the id: NOT_FOUND, as every *_get.
func TestBackupGet_NotFound(t *testing.T) {
	m, text, isErr := callNamed(t, backupRegistry(t, &backupWorld{}), "backup_get", map[string]any{"backup_id": "00000000-0000-4000-8000-0000000000ff"})
	if !isErr || m["reason"] != "NOT_FOUND" {
		t.Errorf("want NOT_FOUND: %s", text)
	}
}
