// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package testrules checks the tests of the repository against rules that
// keep them deterministic on a loaded CI runner.
package testrules

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// sinceCompared is a measure of the wall clock compared with a bound on
// one line: time.Since(start) > 3*time.Second, d := time.Since(start); d > x.
var sinceCompared = regexp.MustCompile(`time\.Since\(|time\.Now\(\)\.Sub\(`)

// A test that bounds the wall clock fails on a loaded runner without a
// defect (CI of 1.2.0 and 1.2.1, Windows under -race). A bound is allowed
// only where it guards a long production wait — the backoff, the timeout,
// the poll interval the code under test must not wait out — and is a
// fraction of that wait, not a guess about the runner: the line or the one
// above it says so with a comment "wall-clock:" naming the wait (process
// audit 1.2.1, PR 4). A lower bound after a sleep needs it too.
func TestNoUnguardedWallClockBounds(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "wallclock_test.go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		prev, n := "", 0
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			n++
			line := sc.Text()
			if sinceCompared.MatchString(line) && strings.ContainsAny(line, "<>") &&
				!strings.Contains(line, "wall-clock:") && !strings.Contains(prev, "wall-clock:") {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: a bound on the wall clock without a \"wall-clock:\" comment naming the production wait it guards", rel, n)
			}
			prev = line
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}
