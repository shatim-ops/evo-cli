// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A restriction of the server is a promise of docs/mcp.md (one row each,
// TestHTTPRestrictionsClosed): each is kept by a test that names it — its
// ID in a test's comment or name, in a test of any package (1.4.0, S2).
// A new restriction without one fails here, and so does an ID a test names
// that no restriction has.
func TestHTTPRestrictions_EachHasATest(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range HTTPRestrictions {
		ids[r.ID] = true
	}
	named := map[string][]string{}
	word := regexp.MustCompile(`\bH\d+\b`)
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		switch filepath.Base(path) {
		case "restriction_tests_test.go", "http_policy_closed_test.go":
			return nil // the tests of the list itself, not of a restriction
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, id := range word.FindAllString(string(b), -1) {
			named[id] = append(named[id], filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range HTTPRestrictions {
		if len(named[r.ID]) == 0 {
			t.Errorf("%s: no test names it — the row of docs/mcp.md is kept by nothing", r.ID)
		}
	}
	for id, files := range named {
		if !ids[id] {
			t.Errorf("%s is named by %s, but HTTPRestrictions has no %s", id, files[0], id)
		}
	}
}
