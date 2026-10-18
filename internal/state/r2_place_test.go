// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
)

// R2-1: neither a symlinked .evo nor a symlinked state.json is followed by
// Read, Write or Lock; a directory that does not exist yet is fine.
func TestStatePlace_SymlinksRefused(t *testing.T) {
	victim := t.TempDir()
	if err := Write(filepath.Join(victim, "state.json"), &File{Version: 1}); err != nil {
		t.Fatal(err)
	}

	linkedDir := t.TempDir()
	if err := os.Symlink(victim, filepath.Join(linkedDir, ".evo")); err != nil {
		t.Skip("symlink: " + err.Error())
	}
	linkedFile := t.TempDir()
	if err := os.Mkdir(filepath.Join(linkedFile, ".evo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(victim, "state.json"), filepath.Join(linkedFile, ".evo", "state.json")); err != nil {
		t.Fatal(err)
	}

	for name, root := range map[string]string{"symlinked .evo": linkedDir, "symlinked state.json": linkedFile} {
		path := DefaultPath(root)
		if _, err := Read(path); clierr.ExitCode(err) != clierr.ExitAuth {
			t.Errorf("%s: Read = %v, want CONFIG_INVALID", name, err)
		}
		if err := Write(path, &File{Version: 1}); clierr.ExitCode(err) != clierr.ExitAuth {
			t.Errorf("%s: Write = %v, want CONFIG_INVALID", name, err)
		}
		if unlock, err := Lock(path); err == nil {
			unlock()
			t.Errorf("%s: Lock succeeded", name)
		}
	}

	fresh := DefaultPath(t.TempDir())
	if err := Write(fresh, &File{Version: 1}); err != nil {
		t.Fatalf("first write into a new .evo: %v", err)
	}
}
