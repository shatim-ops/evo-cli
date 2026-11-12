// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The checklist of a new operation (docs/internal/add-operation.md; release
// plan 1.9, «Инварианты до новых изменяющих операций») names, at every
// step, the test that fails until the step is done. A step that names a
// test nobody has is a promise the build does not keep: every Test… of the
// checklist is a test function of the repository, and the checklist names
// the invariants a new operation meets first.
func TestAddOperation_NamesExistingTests(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, "docs", "internal", "add-operation.md"))
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, m := range regexp.MustCompile("`(Test[A-Za-z0-9_]+)`").FindAllStringSubmatch(string(doc), -1) {
		named[m[1]] = true
	}
	if len(named) < 10 {
		t.Fatalf("the checklist names %d tests: the scan sees nothing", len(named))
	}
	declared := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	exists := map[string]bool{}
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range declared.FindAllStringSubmatch(string(src), -1) {
			exists[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for name := range named {
		if !exists[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("docs/internal/add-operation.md names %s, and no test of the repository is called that: name the test that holds the step, or take the step out", name)
	}
	// The invariants every new changing operation meets (Н1–Н3 and the
	// refusal before the question): the checklist is not one without them.
	for _, name := range []string{
		"TestConsent_EveryChangeDeclaresItsRepeat",
		"TestContract_WorldBodiesInTheInventory",
		"TestProgress_ChangeGoingOnNamesTheNextStep",
		"TestConsent_RefusalWithoutWriteComesBeforeTheQuestion",
	} {
		if !named[name] {
			t.Errorf("docs/internal/add-operation.md no longer names %s", name)
		}
	}
}
