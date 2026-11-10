// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package journal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Findings ledger 1.8, № 29 (Grok R4-F1): journal.jsonl replaced by a
// symlink made evo append its records to the file it points to — and
// diag status and export read that file. The journal and its lock are
// opened without following a link: a record is lost, the other file is
// neither written, nor created, nor read.
func TestJournal_SymlinkIsNotFollowed(t *testing.T) {
	isolate(t)
	Begin(SurfaceCLI, "t")
	if err := config.SetJournal(true); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	const was = "the other file\n"
	if err := os.WriteFile(other, []byte(was), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, Path()); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	Write(Record{Tool: "vm_list"})
	if b, _ := os.ReadFile(other); string(b) != was {
		t.Errorf("a record went through the link: %q", b)
	}
	if _, err := Describe(); err == nil {
		t.Error("diag status read the journal through the link")
	}
	dst := filepath.Join(t.TempDir(), "out.jsonl")
	if _, err := Export(dst); err == nil {
		t.Error("export copied the file the link points to")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("export wrote %s: %v", dst, err)
	}

	// The lock: a link to a missing file is not created through.
	if err := os.Remove(Path()); err != nil {
		t.Fatal(err)
	}
	made, lockPath := filepath.Join(t.TempDir(), "made"), filepath.Join(filepath.Dir(Path()), lockName)
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(made, lockPath); err != nil {
		t.Fatal(err)
	}
	Write(Record{Tool: "vm_list"})
	if _, err := os.Stat(made); !os.IsNotExist(err) {
		t.Errorf("the lock created the file its link points to: %v", err)
	}
}
