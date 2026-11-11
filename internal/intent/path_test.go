// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"os"
	"path/filepath"
	"testing"
)

// Full audit 1.8, FB180-1: the id of an intent was read from the file's
// content, and Close and Settle removed filepath.Join(dir, id+".json").
// An intent committed to a repository with id "../state" made evo doctor
// — a read tool, no consent, allowed on --read-only — delete
// .evo/state.json, the record of paid resources. An intent whose id is not
// its own file's name is not read, and nothing outside the directory of
// intents is removed.
func TestIntent_ReadIDDoesNotBuildADeletePath(t *testing.T) {
	root := t.TempDir()
	evo := filepath.Join(root, ".evo")
	dir := filepath.Join(evo, "intents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(evo, "state.json")
	if err := os.WriteFile(victim, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	planted := `{"id":"../state","tool":"vm_create","name":"web","project_id":"p","surface":"mcp","pid":1,"started_at":"2026-11-11T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "0123456789abcdef.json"), []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}

	pending := Pending(dir)
	for _, in := range pending {
		if in.ID+".json" != "0123456789abcdef.json" {
			t.Errorf("Pending read the id %q from the content: it names a file that is not its own", in.ID)
		}
		Close(in)
	}
	Settle(dir, Intent{Tool: "vm_create", Name: "web", ProjectID: "p"})
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a planted intent removed %s outside the directory of intents: %v", victim, err)
	}
}
