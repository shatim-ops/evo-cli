// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// One Go for CI, the release and the image: a bump that misses a file
// (GO-2026-6859, #1228: 1.26.10 → 1.26.11 in 14 files) leaves a job
// building with the vulnerable library while govulncheck passes on
// another. Every literal go-version of the workflows and GO_VERSION of
// the Dockerfile name the same version.
func TestWorkflows_OneGoVersion(t *testing.T) {
	root := filepath.Join("..", "..")
	lit := regexp.MustCompile(`(?m)^\s*go-version:\s*(?:"([^"$]+)"|([^"\s$#]+))\s*(?:#.*)?$`)
	seen := map[string][]string{}
	files, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range lit.FindAllStringSubmatch(string(b), -1) {
			v := m[1] + m[2] // quoted or plain YAML
			seen[v] = append(seen[v], filepath.Base(f))
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^ARG GO_VERSION=(\S+)$`).FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("Dockerfile: no ARG GO_VERSION")
	}
	seen[m[1]] = append(seen[m[1]], "Dockerfile")
	// The image of the build is the one of GO_VERSION, pinned by digest: a
	// tag alone can be rewritten in the registry.
	if !regexp.MustCompile(`(?m)^FROM golang:\$\{GO_VERSION\}@sha256:[0-9a-f]{64} `).Match(b) {
		t.Error("Dockerfile: the build image is not golang:${GO_VERSION}@sha256:<digest>")
	}
	if len(seen) != 1 {
		t.Errorf("Go versions differ across CI, the release and the image (bump them together): %v", seen)
	}
}
