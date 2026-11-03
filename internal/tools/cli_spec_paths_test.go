// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"sort"
	"strings"
	"testing"
)

// CLISpec.Files and CLISpec.Paths mean two things: evo reads a file into
// the argument, or the argument is the path the tool opens. An argument
// is one of the two, never both, and each declared one is an argument of
// the tool's schema (#718).
func TestCLISpec_FilesAndPathsApart(t *testing.T) {
	var bad []string
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for a := range tl.CLI.Paths {
			if tl.CLI.Files[a] {
				bad = append(bad, tl.Name+"."+a+": in Files and in Paths")
			}
			if _, ok := props[a]; !ok {
				bad = append(bad, tl.Name+"."+a+": in Paths, not an argument of the schema")
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}
