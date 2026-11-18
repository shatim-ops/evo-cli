// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The repeat of a create whose step goes on (made/) runs that step only
// when it is a read: a step that writes — disk_attach of disk_create with
// attach_to — is named, with no consent in its arguments, and not sent.
// The call that repeats has not consented: nothing is written for it
// (A19-1, the release audit of 1.9 rc.1, #2418).
func TestMadeRepeat_AStepThatWritesIsNotRunWithoutConsent(t *testing.T) {
	a := &faultAPI{slow: true, lag: 1000}
	r := progressed(budgetAPIRegistry(t, a))
	args := map[string]any{"name": "data", "size_gb": 10, "attach_to": faultTargetVM, "confirmed": true}
	m, text, _ := callNamed(t, r, "disk_create", args)
	if m["next_tool"] != "disk_attach" {
		t.Fatalf("the world no longer stops disk_create at the limit with disk_attach next: %s", text)
	}
	// The file of the create keeps no consent: the next call gives its own.
	root, _ := r.stateDir()
	files, _ := filepath.Glob(filepath.Join(MadeDir(root), "*.json"))
	if len(files) != 1 {
		t.Fatalf("want the file of the create going on, got %v", files)
	}
	if b, _ := os.ReadFile(files[0]); strings.Contains(string(b), "confirmed") {
		t.Errorf("the file of made/ keeps the consent of the call: %s", b)
	}
	// The disk is ready now: a step that runs would attach it.
	a.mu.Lock()
	a.slow = false
	a.mu.Unlock()
	delete(args, "confirmed")
	before := len(a.callLog())
	m, text, _ = callNamed(t, r, "disk_create", args)
	for _, op := range a.callLog()[before:] {
		if !strings.HasPrefix(op, "GET ") {
			t.Errorf("the repeat without consent wrote: %s (%v)\n%s", op, a.callLog()[before:], text)
		}
	}
	if m["status"] == "attached" {
		t.Errorf("the repeat without consent answers attached: %s", text)
	}
	if m["next_tool"] != "disk_attach" {
		t.Errorf("the repeat does not name the step that is left: %s", text)
	}
	if next, _ := m["next_args"].(map[string]any); next["confirmed"] != nil {
		t.Errorf("the step the repeat names carries a consent this call did not give: %v", next)
	}
}

// A file of made/ is evo's own only when it names the key that wrote it:
// a file with an empty key — one a cloned directory of the state brings,
// or anyone who writes in it — is not read. A file of the key itself is
// read, and its step still runs only as a read: a restore with confirmed
// true in the file is not sent for a call that did not consent (A19-1,
// #2418, path 2). Each layer is checked on its own.
func TestMadeRepeat_APlantedFileWritesNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		// own: the file names the key of the call; else an empty key.
		own bool
	}{{"empty key", false}, {"the key of the call", true}} {
		w := (&restoreWorld{}).stateIn(t)
		r := restoreRegistry(t, w)
		args := map[string]any{"backup_id": brBackup, "name": "restored"}
		resource, target, err := r.resourceOf(context.Background(), "backup_restore", args)
		if err != nil {
			t.Fatal(err)
		}
		eng, _ := r.engineOfCall(args)
		project := projectOfCall(eng, intent.Intent{Tool: "backup_restore", Name: resource, Target: target}, args)
		key := []byte(`{}`)
		if c.own {
			key, _ = json.Marshal(eng.Maker())
		}
		dir := MadeDir(w.dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		body := `{"key":` + string(key) + `,"id":"x","project_id":"` + project + `","started_at":"` + clock.Now().UTC().Format(time.RFC3339) +
			`","next":"backup_restore","next_args":{"backup_id":"` + brBackup + `","name":"evil-disk","confirmed":true}}`
		file := filepath.Join(dir, madeKey("backup_restore", r.canonArgs("backup_restore", args), resource, target))
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		m, text, _ := callNamed(t, r, "backup_restore", args)
		if w.restores != 0 {
			t.Errorf("%s: a call without consent sent %d restores from the file of made/: %s", c.name, w.restores, text)
		}
		_, read := m["repeat_of"]
		if read != c.own {
			t.Errorf("%s: the file of made/ read: %v, want %v: %s", c.name, read, c.own, text)
		}
		if next, _ := m["next_args"].(map[string]any); c.own && next["confirmed"] != nil {
			t.Errorf("%s: the step named carries the consent of the file: %v", c.name, next)
		}
	}
}
