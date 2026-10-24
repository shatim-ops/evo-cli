// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package repolayout checks what the repository holds against closed lists,
// so that a tool run in the wrong directory does not ship its output.
package repolayout

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// rootDirs and rootFiles are the directories and files at the root of the
// repository.
var rootDirs = map[string]bool{
	".github":     true,
	"changelog.d": true,
	"cmd":         true,
	"docs":        true,
	"internal":    true,
	"scripts":     true,
	"skills":      true,
	"specs":       true,
}

var rootFiles = map[string]bool{
	".dockerignore":    true,
	".gitignore":       true,
	".goreleaser.yaml": true,
	"CHANGELOG.md":     true,
	"CLAUDE.md":        true,
	"Dockerfile":       true,
	"LICENSE":          true,
	"NOTICE":           true,
	"README.md":        true,
	"SECURITY.md":      true,
	"go.mod":           true,
	"go.sum":           true,
	"install.sh":       true,
}

// apiSpecs are the OpenAPI and Swagger files kept in the repository: "path"
// → why. The others are fetched by scripts/cloudru-specs.py into the
// git-ignored .cache (docs/internal/api-contract.md).
var apiSpecs = map[string]string{
	"specs/ccm.swagger.yaml": "the copy internal/certmanager was built from (docs/internal/api-contract.md, 3.11)",
}

// specName and specYAMLKey tell an OpenAPI or Swagger file by its name or
// by its top-level version key; a JSON file by that key at its top level.
var (
	specName    = regexp.MustCompile(`(?i)(openapi|swagger)[^/]*\.(ya?ml|json)$`)
	specYAMLKey = regexp.MustCompile(`(?m)^(openapi|swagger)\s*:`)
)

func isSpec(path string, b []byte) bool {
	if specName.MatchString(path) {
		return true
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return specYAMLKey.Match(b)
	case ".json":
		var top map[string]json.RawMessage
		if json.Unmarshal(b, &top) != nil {
			return false
		}
		_, openapi := top["openapi"]
		_, swagger := top["swagger"]
		return openapi || swagger
	}
	return false
}

// files lists the files git would commit: tracked ones and untracked ones
// not ignored, so a stray directory fails the tests before it is added.
func files(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Skip("not a git work tree:", err)
	}
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var paths []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" {
			continue
		}
		// A file deleted in the work tree but still in the index is not
		// shipped by the next commit -a.
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

// A script run with an option where it takes a directory created "--help/"
// at the root and 18 Cloud.ru specs were committed into it (audit FB13-1,
// #267). Every directory at the root is in rootDirs, every file in
// rootFiles.
func TestLayout_RootListed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range files(t) {
		dir, _, nested := strings.Cut(p, "/")
		switch {
		case nested && !rootDirs[dir] && !seen[dir]:
			seen[dir] = true
			t.Errorf("directory %q at the root of the repository is not in rootDirs: output of a tool run in the wrong place? (FB13-1)", dir)
		case !nested && !rootFiles[p]:
			t.Errorf("file %q at the root of the repository is not in rootFiles: output of a tool run in the wrong place? (FB13-1)", p)
		}
	}
}

// Cloud.ru specs are fetched for a check, not kept: a copy in the
// repository goes stale and its licence is Cloud.ru's. Every OpenAPI or
// Swagger file, by name or by content, is in apiSpecs.
func TestLayout_APISpecsListed(t *testing.T) {
	var found []string
	for _, p := range files(t) {
		var b []byte
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml", ".json":
			var err error
			if b, err = os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(p))); err != nil {
				t.Fatal(err)
			}
		}
		if isSpec(p, b) {
			found = append(found, p)
		}
	}
	sort.Strings(found)
	kept := map[string]bool{}
	for _, p := range found {
		kept[p] = true
		if _, ok := apiSpecs[p]; !ok {
			t.Errorf("%s is an OpenAPI or Swagger file not in apiSpecs: specs are fetched by scripts/cloudru-specs.py, not committed (FB13-1)", p)
		}
	}
	for p := range apiSpecs {
		if !kept[p] {
			t.Errorf("apiSpecs lists %s, which is not an OpenAPI or Swagger file of the repository", p)
		}
	}
}

func TestLayout_IsSpec(t *testing.T) {
	cases := []struct {
		path, body string
		want       bool
	}{
		{"x/openapi-v3.yaml", "", true},
		{"x/ccm.swagger.yaml", "", true},
		{"x/api.yaml", "info: {}\nopenapi: 3.0.1\n", true},
		{"x/api.yml", "swagger: \"2.0\"\n", true},
		{"x/api.json", `{"openapi": "3.1.0", "paths": {}}`, true},
		{"x/compose.yaml", "services:\n  openapi: {}\n", false},
		{"x/manifest.json", `{"specs": [{"openapi": "3.0.1"}]}`, false},
		{"x/notes.md", "openapi: 3.0.1\n", false},
	}
	for _, c := range cases {
		if got := isSpec(c.path, []byte(c.body)); got != c.want {
			t.Errorf("isSpec(%s) = %v, want %v", c.path, got, c.want)
		}
	}
}
