// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package product

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	if got, want := UserAgent(), "evo/1.2.3 (evo CMCP; +https://github.com/shatim-ops/evo-cli)"; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}

// D34: the product is «evo CMCP», never a bare «CMCP» — a CLI of that name
// for MCP servers is on PyPI (cmcp). The user-facing texts — code, README,
// CHANGELOG and its fragments, the user docs, the release and issue
// templates — name it whole; docs/rename.md and the docs for development
// keep what they say.
func TestNoBareCMCP(t *testing.T) {
	root := filepath.Join("..", "..")
	bare := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])cmcp\b`)
	whole := regexp.MustCompile(`evo CMCP`)
	var bad []string
	check := func(path string) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if bare.MatchString(whole.ReplaceAllString(line, "")) {
				rel, _ := filepath.Rel(root, path)
				bad = append(bad, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
	}
	for _, f := range []string{"README.md", "CHANGELOG.md", ".goreleaser.yaml"} {
		check(filepath.Join(root, f))
	}
	// changelog.d: the fragments become CHANGELOG at the fold, checked
	// before it.
	for _, dir := range []string{"internal", "cmd", "docs", "changelog.d", filepath.Join(".github", "ISSUE_TEMPLATE")} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "internal" && strings.HasSuffix(filepath.Dir(path), "docs") {
					return filepath.SkipDir // docs/internal: for development
				}
				return nil
			}
			if strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "rename.md" {
				return nil
			}
			if ext := filepath.Ext(path); ext == ".go" || ext == ".md" || ext == ".yml" || ext == ".yaml" {
				check(path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(bad) > 0 {
		t.Errorf("a bare «CMCP» (write «evo CMCP»):\n%s", strings.Join(bad, "\n"))
	}
	if Name != "evo CMCP" || !strings.HasPrefix(TitleEN, Name+" — ") || !strings.HasPrefix(TitleRU, Name+" — ") {
		t.Errorf("Name %q, TitleEN %q, TitleRU %q", Name, TitleEN, TitleRU)
	}
}
