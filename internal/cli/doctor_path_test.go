// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// One evo under two names — a link in another directory of PATH, as
// install scripts make — is one file: doctor names it once, by the first
// name PATH gives.
func TestDoctor_OtherEvoLinkedTwiceNamedOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
	a, b := t.TempDir(), t.TempDir()
	evo := fakeEvoInPath(t, a, "9.8.5")
	if err := os.Symlink(evo, filepath.Join(b, "evo")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", b+string(os.PathListSeparator)+a)
	checks := otherEvoChecks()
	if len(checks) != 1 || !strings.Contains(checks[0].Detail, filepath.Join(b, "evo")) || !strings.Contains(checks[0].Detail, "9.8.5") {
		t.Fatalf("want one check naming %s and its version, got %+v", filepath.Join(b, "evo"), checks)
	}
}

// evo doctor names another evo without running it: a file named evo lower
// in PATH may be anyone's, and a run would hand it the environment with
// the keys of evo. The world: an evo that, run, leaves a mark — doctor
// names it, the mark is not there.
func TestDoctor_OtherEvoIsNotRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script is not an evo PATHEXT finds")
	}
	dir := t.TempDir()
	mark := filepath.Join(t.TempDir(), "ran")
	evo := filepath.Join(dir, "evo")
	if err := os.WriteFile(evo, []byte("#!/bin/sh\ntouch '"+mark+"'\necho 'evo version 9.9.9'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	checks := otherEvoChecks()
	if len(checks) != 1 || !strings.Contains(checks[0].Detail, evo) {
		t.Fatalf("want one check naming %s, got %+v", evo, checks)
	}
	if strings.Contains(checks[0].Detail, "9.9.9") {
		t.Errorf("the version came from a run of the file: %s", checks[0].Detail)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Errorf("evo doctor ran %s", evo)
	}
}
