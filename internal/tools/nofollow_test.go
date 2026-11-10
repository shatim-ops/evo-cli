// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// R-67 (а), «Риск», conclusion 61: known_hosts of .evo/ and the completion
// cache replaced by a symlink — committed to a repository, it comes with
// git clone — deploy rewrote the file it points to (pinHostKey by
// os.WriteFile). Each is written as a file of its own; the target stays.
func TestWrites_SymlinkIsNotFollowed(t *testing.T) {
	for name, write := range map[string]func(path string){
		"known_hosts": func(path string) { _ = pinHostKey(path, "203.0.113.5", "ssh-ed25519 AAAAnew") },
		"completion":  func(path string) { writeCompletionCache(path, []map[string]string{{"id": "x"}}) },
	} {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, []byte("was\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "file")
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("no symlink here: %v", err)
		}
		write(path)
		if b, _ := os.ReadFile(target); string(b) != "was\n" {
			t.Errorf("%s: the write went through the link: %q", name, b)
		}
	}
}

// «Риск», conclusion 66 (condition to #1179): pinHostKey read
// .evo/known_hosts through a link and wrote what it read back as a file of
// the project — a link to ~/.ssh/id_ed25519 from a cloned repository put
// the person's private key into a file git commit -a takes. A file of .evo/
// that is read and rewritten, or whose lines go to a VM, is read without
// following a link: none of the target's content leaves it.
func TestReads_SymlinkTargetDoesNotLeak(t *testing.T) {
	const marker = "PRIVATE-KEY-MARKER"
	target := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(target, []byte("-----BEGIN "+marker+"-----\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl leak\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// known_hosts: refused, and nothing of the target written.
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
	if err := pinHostKey(path, "203.0.113.5", "ssh-ed25519 AAAAnew"); err == nil {
		t.Error("known_hosts: a link was read as the file")
	}
	if fi, err := os.Lstat(path); err != nil {
		t.Fatal(err)
	} else if b, _ := os.ReadFile(path); fi.Mode().IsRegular() && strings.Contains(string(b), marker) {
		t.Errorf("known_hosts: the target's content is in the project: %q", b)
	}

	// The keys of .evo/ that go to a VM: none read through a link.
	stateDir := t.TempDir()
	evo := config.DataDir(stateDir)
	if err := os.MkdirAll(evo, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(evo, userKeysFile)); err != nil {
		t.Fatal(err)
	}
	if keys, _, _ := loadUserKeyLines(stateDir); len(keys) != 0 {
		t.Errorf("user keys: read through a link: %q", keys)
	}
}
