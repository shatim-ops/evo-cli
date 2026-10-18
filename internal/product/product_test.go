// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package product

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spdxHeader opens every Go file of the repository.
const spdxHeader = "// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)\n" +
	"// SPDX-FileContributor: Тимур Шакирбеков\n" +
	"// SPDX-License-Identifier: Apache-2.0\n\n"

// TestAuthorship: the author is named in NOTICE and at the top of every Go
// file, so a file or a module copied out of evo keeps it. A new file
// without the header fails here: copy it from any other file.
func TestAuthorship(t *testing.T) {
	root := filepath.Join("..", "..")
	notice, err := os.ReadFile(filepath.Join(root, "NOTICE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Тимур Шакирбеков", "Timur Shakirbekov"} {
		if !strings.Contains(string(notice), want) {
			t.Errorf("NOTICE does not name %s", want)
		}
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(b), spdxHeader) {
			t.Errorf("%s: no SPDX header", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUserAgent(t *testing.T) {
	SetVersion("1.2.3")
	t.Cleanup(func() { SetVersion("") })
	if got, want := UserAgent(), "evo/1.2.3 (Cloud.ru Evolution CLI CREW edition; +https://github.com/shatim-ops/evo-cli)"; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}
