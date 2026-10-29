// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var windowsPRList = regexp.MustCompile(`(?m)^windowsPRPackages=\(([^)]*)\)`)

// On a pull request Windows runs only the packages with code of Windows
// (go_test_ci.sh, windowsPRPackages); main, the night and the release run
// everything (owner's decision 29.10, audit #578, item 10). Every package
// whose code has a *_windows.go file or reads runtime.GOOS is in the list,
// and every package of the list is one: a new package of Windows code is
// not left to the night.
func TestWorkflows_WindowsPRCoversWindowsCode(t *testing.T) {
	root := filepath.Join("..", "..")
	b, err := os.ReadFile(filepath.Join(root, ".github", "scripts", "go_test_ci.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := windowsPRList.FindSubmatch(b)
	if m == nil {
		t.Fatal("go_test_ci.sh: no windowsPRPackages=(…)")
	}
	listed := map[string]bool{}
	for _, p := range strings.Fields(string(m[1])) {
		listed[strings.TrimPrefix(p, "./")] = true
	}
	windows := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, "_windows.go") || strings.Contains(string(src), "runtime.GOOS") {
			dir, _ := filepath.Rel(root, filepath.Dir(path))
			windows[filepath.ToSlash(dir)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing, extra []string
	for p := range windows {
		if !listed[p] {
			missing = append(missing, p)
		}
	}
	for p := range listed {
		if !windows[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	for _, p := range missing {
		t.Errorf("%s has code of Windows, but a pull request does not test it on Windows: add ./%s to windowsPRPackages (go_test_ci.sh)", p, p)
	}
	for _, p := range extra {
		t.Errorf("windowsPRPackages names ./%s, which has no code of Windows: remove it", p)
	}
	ci := readWorkflows(t)["ci.yml"]
	if _, ok := ci.On["schedule"]; !ok {
		t.Error("ci: no schedule — the full matrix on Windows runs only on main and the release, not at night")
	}
}
