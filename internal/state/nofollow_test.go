// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// R-67 (а): .evo/ comes with git clone; a symlink committed in place of a
// lock is not a way to create the file it points to.
func TestLocks_SymlinkIsNotFollowed(t *testing.T) {
	for name, take := range map[string]func(dir string) (func(), error){
		"state.Lock": func(dir string) (func(), error) { return Lock(filepath.Join(dir, "state.json")) },
		"FileLock":   func(dir string) (func(), error) { return FileLock(filepath.Join(dir, "state.lock"), 50*time.Millisecond) },
	} {
		dir := filepath.Join(t.TempDir(), ".evo")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		made := filepath.Join(t.TempDir(), "made")
		if err := os.Symlink(made, filepath.Join(dir, "state.lock")); err != nil {
			t.Skipf("no symlink here: %v", err)
		}
		if unlock, err := take(dir); err == nil {
			unlock()
			t.Errorf("%s: the lock was taken through a link", name)
		}
		if _, err := os.Lstat(made); !os.IsNotExist(err) {
			t.Errorf("%s: the lock created the file its link points to: %v", name, err)
		}
	}
}
