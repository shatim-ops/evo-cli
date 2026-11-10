// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"os"
	"path/filepath"
	"testing"
)

// R-67 (а): login.lock replaced by a symlink — the lock wrote its pid and
// port into the start of the file it points to (note), or created it. The
// lock is a file of its own or none.
func TestLoginLock_SymlinkIsNotFollowed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("was\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "login.lock")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	if l, err := lockLogin(path); err == nil {
		l.note(18386)
		l.release()
		t.Error("the lock was taken through a link")
	}
	if b, _ := os.ReadFile(target); string(b) != "was\n" {
		t.Errorf("the lock wrote the link's target: %q", b)
	}
}
