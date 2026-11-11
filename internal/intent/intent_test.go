// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// FB180-1 (full audit of 1.8; R-67): Pending took the id from the record's
// content, and Close and Settle removed dir/<id>.json — a record that came
// with the project (git clone) made evo doctor, a read without consent,
// remove a file outside the intents, .evo/state.json among them. The id
// is the file's name, of evo's own form; a record of any other form is not
// read, and nothing is removed by it.
func TestPending_ARecordNotOfItsOwnFormIsNotReadAndRemovesNothing(t *testing.T) {
	root := t.TempDir()
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(filepath.Dir(dir), "state.json")
	if err := os.WriteFile(state, []byte(`{"deployments":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	put := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// An id that climbs out of the directory, in a file of evo's name form.
	put("0123456789abcdef.json", `{"id":"../state","tool":"vm_create","name":"x","started_at":"2026-11-11T00:00:00Z"}`)
	// A name not of evo's form.
	put("notanid.json", `{"id":"notanid","tool":"vm_create","name":"x"}`)
	if runtime.GOOS != "windows" {
		target := filepath.Join(root, "outside.json")
		if err := os.WriteFile(target, []byte(`{"id":"fedcba9876543210","tool":"vm_create","name":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, "fedcba9876543210.json")); err != nil {
			t.Fatal(err)
		}
	}
	if got := Pending(dir); len(got) != 0 {
		t.Errorf("records not of their own form were read: %+v", got)
	}
	// Even an intent handed in with such an id removes nothing.
	Close(Intent{ID: "../state", Dir: dir})
	Settle(dir, Intent{Tool: "vm_create", Name: "x"})
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("a record removed a file outside the intents: %v", err)
	}
}

// A record evo wrote is read with its id and closed.
func TestPending_EvosRecordIsReadAndClosed(t *testing.T) {
	dir := Dir(t.TempDir())
	if _, err := Begin(dir, Intent{Tool: "vm_create", Name: "web", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got := Pending(dir)
	if len(got) != 1 || !validID(got[0].ID) {
		t.Fatalf("evo's record: %+v", got)
	}
	Close(got[0])
	if left := Pending(dir); len(left) != 0 {
		t.Errorf("closed record still there: %+v", left)
	}
}
