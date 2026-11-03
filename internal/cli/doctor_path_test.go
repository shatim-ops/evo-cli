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
