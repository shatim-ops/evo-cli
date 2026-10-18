// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUserKeys(t *testing.T) {
	dir := t.TempDir()
	keyDir := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Missing file → no keys, no error.
	if got, _ := loadUserKeys(dir); got != nil {
		t.Fatalf("missing file: want nil, got %v", got)
	}

	content := `# user keys
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@mac

-----BEGIN OPENSSH PRIVATE KEY-----
garbage-that-must-never-ship
ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ backup@key
`
	if err := os.WriteFile(filepath.Join(keyDir, userKeysFile), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	got, file := loadUserKeys(dir)
	if file != userKeysFile {
		t.Errorf("keys read from %q, want %q", file, userKeysFile)
	}
	want := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@mac",
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ backup@key",
	}
	if len(got) != len(want) {
		t.Fatalf("want %d keys, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestShellQuoteMCP(t *testing.T) {
	if got := shellQuoteMCP(`k'ey`); got != `'k'\''ey'` {
		t.Errorf("quote: got %q", got)
	}
}

// Until v1.1 the pre-v1.0 file name is still read, so keys set up for the
// old engine keep reaching new VMs; the new name wins when both exist.
func TestLoadUserKeys_LegacyFile(t *testing.T) {
	dir := t.TempDir()
	keyDir := filepath.Join(dir, ".evo")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILegacy old@mac\n"
	if err := os.WriteFile(filepath.Join(keyDir, legacyUserKeysFile), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if got, file := loadUserKeys(dir); len(got) != 1 || file != legacyUserKeysFile {
		t.Fatalf("legacy only: got %v from %q", got, file)
	}

	current := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINew new@mac\n"
	if err := os.WriteFile(filepath.Join(keyDir, userKeysFile), []byte(current), 0600); err != nil {
		t.Fatal(err)
	}
	if got, file := loadUserKeys(dir); len(got) != 1 || got[0] != strings.TrimSpace(current) || file != userKeysFile {
		t.Fatalf("both files: got %v from %q, want the new file", got, file)
	}
}

// pinHostKey keeps one entry per host: the key of an earlier VM on the same
// reused IP goes, other hosts stay; an empty key only drops the old entry.
func TestPinHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	old := "203.0.113.5 ssh-ed25519 AAAAold\n[203.0.113.5]:22 ssh-rsa AAAAold2\n198.51.100.7,203.0.113.5 ssh-ed25519 AAAAboth\n198.51.100.7 ssh-ed25519 AAAAother\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pinHostKey(path, "203.0.113.5", "ssh-ed25519 AAAAnew"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "198.51.100.7 ssh-ed25519 AAAAother\n203.0.113.5 ssh-ed25519 AAAAnew\n"
	if string(got) != want {
		t.Fatalf("known_hosts:\n%s\nwant:\n%s", got, want)
	}
	if err := pinHostKey(path, "203.0.113.5", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "198.51.100.7 ssh-ed25519 AAAAother\n" {
		t.Fatalf("after drop:\n%s", got)
	}
	// IPv6: plain and bracketed with the port.
	v6 := "[2001:db8::1]:22 ssh-ed25519 AAAAold\n2001:db8::1 ssh-ed25519 AAAAold2\n"
	if err := os.WriteFile(path, []byte(v6), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pinHostKey(path, "2001:db8::1", "ssh-ed25519 AAAAnew"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "2001:db8::1 ssh-ed25519 AAAAnew\n" {
		t.Fatalf("IPv6:\n%s", got)
	}

	// No file yet and nothing to pin: an empty file, no error.
	fresh := filepath.Join(t.TempDir(), "known_hosts")
	if err := pinHostKey(fresh, "203.0.113.5", ""); err != nil {
		t.Fatal(err)
	}
}
