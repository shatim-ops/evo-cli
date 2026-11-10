// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"path/filepath"
	"testing"
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
