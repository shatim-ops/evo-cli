// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fake servers of the test worlds (scripts/fake-servers.json) serve
// the API of Cloud.ru instead of calling it: the contract check does not
// read their routes as requests of evo, and deadcode does not read their
// functions as dead product code. That holds only while tests alone import
// them — a fake IAM in the binary would accept any login — and while both
// checks read the one list.
func TestFakeServers_OnlyTestsImport(t *testing.T) {
	root := filepath.Join("..", "..")
	b, err := os.ReadFile(filepath.Join(root, "scripts", "fake-servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var list map[string]string
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	fake := map[string]bool{}
	for dir, why := range list {
		if strings.HasPrefix(dir, "_") {
			continue
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("%s: no reason", dir)
		}
		if m, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*.go")); len(m) == 0 {
			t.Errorf("%s: no Go package there — remove the line", dir)
		}
		fake["github.com/shatim-ops/evo-cli/"+dir] = true
	}
	if len(fake) == 0 {
		t.Fatal("scripts/fake-servers.json names no package: the check sees nothing")
	}
	for _, f := range []struct{ file, reads string }{
		{".github/workflows/ci.yml", "scripts/fake-servers.json"},
		{"scripts/api-contract-check.py", `"fake-servers.json"`},
	} {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.file)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), f.reads) {
			t.Errorf("%s does not read %s: the fake servers are excepted in two places", f.file, f.reads)
		}
	}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, im := range f.Imports {
			if path := strings.Trim(im.Path.Value, `"`); fake[path] {
				rel, _ := filepath.Rel(root, p)
				t.Errorf("%s imports the fake server %s outside a test", filepath.ToSlash(rel), path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
