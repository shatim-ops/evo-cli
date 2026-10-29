// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The weekly mutation run (release plan 1.6) breaks each place of
// .github/mutations.tsv and wants its test red.
func TestWorkflows_WeeklyMutation(t *testing.T) {
	w := weeklyWorkflow(t, "weekly-mutation.yml")
	if !runsStep(w, ".github/scripts/mutate.sh .github/mutations.tsv") {
		t.Error("weekly-mutation: no step runs .github/scripts/mutate.sh .github/mutations.tsv")
	}
}

// A mutation that no longer applies rots silently until the weekly run:
// on every change, each line of .github/mutations.tsv finds its text in
// its file exactly once, its replacement differs, and the test its -run
// names exists in its package.
func TestWorkflows_MutationsApply(t *testing.T) {
	root := filepath.Join("..", "..")
	b, err := os.ReadFile(filepath.Join(root, ".github", "mutations.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	n := 0
	for i, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Errorf("mutations.tsv:%d: %d fields, want 6 (id, file, find, replace, package, -run)", i+1, len(f))
			continue
		}
		id, file, find, replace, pkg, run := f[0], f[1], f[2], f[3], f[4], f[5]
		n++
		if ids[id] {
			t.Errorf("mutations.tsv:%d: id %s twice", i+1, id)
		}
		ids[id] = true
		src, err := os.ReadFile(filepath.Join(root, file))
		switch {
		case err != nil:
			t.Errorf("%s: %v", id, err)
		case strings.Count(string(src), find) != 1:
			t.Errorf("%s: %q is in %s %d times, want once — the code moved: follow it", id, find, file, strings.Count(string(src), find))
		case find == replace:
			t.Errorf("%s: the replacement is the text itself", id)
		}
		if _, err := regexp.Compile(run); err != nil {
			t.Errorf("%s: -run %q: %v", id, run, err)
			continue
		}
		if !testExists(t, filepath.Join(root, strings.TrimPrefix(pkg, "./")), run) {
			t.Errorf("%s: no test of %s matches -run %s", id, pkg, run)
		}
	}
	if n == 0 {
		t.Error("mutations.tsv: no mutation")
	}
}

// testExists reports whether a Test function of the package in dir matches
// the -run expression run.
func testExists(t *testing.T, dir, run string) bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(run)
	fn := regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\)`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range fn.FindAllStringSubmatch(string(b), -1) {
			if re.MatchString(m[1]) {
				return true
			}
		}
	}
	return false
}
