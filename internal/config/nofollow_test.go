// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// R-67 (а), «Риск», conclusion 61: a symlink in place of a file evo writes
// in its configuration directory or a project's .evo/ — committed to a
// repository, it comes with git clone — is not a way to write, create or
// read the file it points to. The lock of the profiles is one of them.
func TestLockProfiles_SymlinkIsNotFollowed(t *testing.T) {
	t.Setenv(EnvConfigDir, filepath.Join(t.TempDir(), "evo"))
	if err := MkdirPrivate(Dir()); err != nil {
		t.Fatal(err)
	}
	made := filepath.Join(t.TempDir(), "made")
	if err := os.Symlink(made, filepath.Join(Dir(), lockName)); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	if unlock, err := lockProfiles(); err == nil {
		unlock()
		t.Error("the lock was taken through a link")
	}
	if _, err := os.Lstat(made); !os.IsNotExist(err) {
		t.Errorf("the lock created the file its link points to: %v", err)
	}
}

// OpenNoFollow: a regular file opens; a link, live or dangling, does not,
// and the file it points to stays as it was.
func TestOpenNoFollow(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	if f, err := OpenNoFollow(plain, os.O_CREATE|os.O_RDWR, 0o600); err != nil {
		t.Fatalf("a new regular file: %v", err)
	} else {
		f.Close()
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("was"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, to := range map[string]string{"live": target, "dangling": filepath.Join(dir, "none")} {
		link := filepath.Join(dir, name)
		if err := os.Symlink(to, link); err != nil {
			t.Skipf("no symlink here: %v", err)
		}
		if f, err := OpenNoFollow(link, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600); err == nil {
			f.Close()
			t.Errorf("%s link opened", name)
		}
	}
	if b, _ := os.ReadFile(target); string(b) != "was" {
		t.Errorf("the target changed: %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dir, "none")); !os.IsNotExist(err) {
		t.Errorf("a dangling link created its target: %v", err)
	}
}
