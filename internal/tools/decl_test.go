// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every declaration is of a tool the registry has: a declaration left
// after its tool was renamed or removed would say nothing of any tool,
// and the rule it carries would be lost without a failing test.
func TestDecl_NamesATool(t *testing.T) {
	r := New(nil)
	for name := range decls {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("decls names %q, which is not a tool", name)
		}
	}
}

// The command and the Russian description of a tool are read from its
// declaration (Decl.CLI, Decl.RU): the maps that hold their texts are
// read only by declareTexts, so a reader cannot see a text the
// declaration does not carry (PR 6 of the series Tool.Decl).
func TestDecl_TextsReadFromTheDeclaration(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	direct := regexp.MustCompile(`\b(cliSpecs|toolsRU)\[`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if direct.MatchString(line) {
				t.Errorf("%s:%d reads %s: read Decl.CLI or Decl.RU (commandOf)", f, i+1, direct.FindString(line))
			}
		}
	}
	for _, tl := range New(nil).Tools() {
		if tl.CLI.Verb != cliSpecs[tl.Name].Verb || tl.ShortRU != toolsRU[tl.Name].Short {
			t.Errorf("%s: the tool's command or description is not its declaration's", tl.Name)
		}
	}
}
