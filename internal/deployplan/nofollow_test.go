// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"os"
	"path/filepath"
	"testing"
)

// R-67 (а): a plan file replaced by a symlink in a cloned .evo/ — the plan
// is written as a file of its own, the file the link points to stays.
func TestSave_SymlinkIsNotFollowed(t *testing.T) {
	stateDir := t.TempDir()
	dir, err := plansDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("was"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &Plan{PlanID: "plan-1"}
	if err := os.Symlink(target, filepath.Join(dir, p.PlanID+".json")); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	_ = Save(stateDir, p)
	if b, _ := os.ReadFile(target); string(b) != "was" {
		t.Errorf("the plan went through the link: %q", b)
	}
}
