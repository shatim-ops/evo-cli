// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The registry of tools is built once a run of the CLI (release plan 1.9,
// «Реестр инструментов строится один раз за запуск CLI»): it was built
// four times — by allTreeGroups, newRootCmd, newTreeCmds and the
// completions — and the tests of internal/cli, which run the CLI hundreds
// of times, paid for each. TestRegistry_BuiltOnceARun counts the builds of
// a run; this rule holds the code: a new call of tools.New in internal/cli
// is a second point that builds the registry, and it takes the one of the
// run instead (registryOf, or an argument).

// registryBuiltIn are the functions of internal/cli that call tools.New,
// "file: function" → why they build a registry of their own. A closed
// list: a call not here fails the test, and so does a line with no call.
var registryBuiltIn = map[string]string{
	"internal/cli/tools.go: newRegistry":          "the registry of the run: the one build",
	"internal/cli/agent.go: toolsetsHint":         "reads the names of the tools with no engine, outside a run's tree",
	"internal/cli/agent_skill.go: agentSkillBody": "reads the descriptions of the tools with no engine",
	"internal/cli/configure.go: configureZones":   "one tool over the engine the wizard has just made, before a run has a registry",
}

func TestCLI_BuildsTheRegistryInOnePlace(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "internal", "cli"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			key := rel + ": " + declName(decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "New" {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "tools" {
					return true
				}
				seen[key] = true
				if registryBuiltIn[key] == "" {
					t.Errorf("%s (%s): calls tools.New — the registry of a run is built once (newRegistry): take it by registryOf(cmd) or as an argument, or name here why this one is its own", key, fset.Position(sel.Pos()))
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var gone []string
	for key := range registryBuiltIn {
		if !seen[key] {
			gone = append(gone, key)
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		t.Errorf("registryBuiltIn names %s, which calls tools.New no longer: remove the line", key)
	}
}
