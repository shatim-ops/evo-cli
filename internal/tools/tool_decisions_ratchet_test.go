// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// Each line identifies one literal in a decision, including repeated tests
// in the same function and compound completion keys such as "s3_get key".
// Removing a decision requires deleting its line from the baseline.
func TestToolNameComparisonsOnlyShrink(t *testing.T) {
	r := New(nil)
	var found []string
	for _, file := range []string{"intent.go", "intent_targets.go", "completion.go", "history.go", "tools_history.go", "preview_project.go"} {
		tree, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range tree.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			record := func(x ast.Expr) {
				lit, ok := ast.Unparen(x).(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				name, _, _ := strings.Cut(value, " ")
				if _, ok := r.Lookup(name); ok || name == "login" {
					found = append(found, fmt.Sprintf("%s\t%s\t%s", file, fn.Name.Name, value))
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BinaryExpr:
					if x.Op == token.EQL || x.Op == token.NEQ {
						record(x.X)
						record(x.Y)
					}
				case *ast.SwitchStmt:
					if x.Tag != nil {
						for _, stmt := range x.Body.List {
							for _, v := range stmt.(*ast.CaseClause).List {
								record(v)
							}
						}
					}
				case *ast.CallExpr:
					if f, ok := x.Fun.(*ast.SelectorExpr); ok && f.Sel.Name == "EqualFold" {
						for _, v := range x.Args {
							record(v)
						}
					}
				}
				return true
			})
		}
	}
	checkDecisionTable(t, "tool_name_comparisons.tsv", found)
}

// Record the registry's effective repeat rule, consent and canonical preview
// binding, timing of the intent receipt, and choice of target arguments.
func TestRegistryDecisionSnapshot(t *testing.T) {
	r := contractReg(t, &contractRT{kind: "200"})
	var rows []string
	for _, tool := range r.Tools() {
		rule := everySendIsAChange[tool.Name]
		repeat := rule.read
		if rule.made {
			repeat += ":made"
		}
		if rule.notBilled {
			repeat += ":unbilled"
		}
		if rule.settledBy != nil {
			repeat += ":settle"
		}
		consent := "none"
		if takesConsent(tool) {
			consent = "always"
			if !AlwaysNeedsConsent(tool) {
				consent = "conditional"
			}
		}
		props, _ := tool.InputSchema["properties"].(map[string]any)
		if _, ok := props["preview_id"]; ok {
			consent += ":preview-id"
			if bytes.Equal(r.canonArgs(tool.Name, map[string]any{"preview_id": "a"}), r.canonArgs(tool.Name, map[string]any{"preview_id": "b"})) {
				consent += ":outside-canon"
			}
		}
		receipt := sampleReceipt(t, r, tool)
		sample := map[string]any{}
		for key := range props {
			sample[key] = key
		}
		name, target := intentResource(tool.Name, sample)
		var targets []string
		for _, a := range targetArgs(tool.Name) {
			targets = append(targets, a.name)
		}
		if rule.target != nil {
			targets = append(targets, "resolver")
		}
		fields := []string{tool.Name, repeat, consent, receipt, name, target, strings.Join(targets, ",")}
		for i, v := range fields {
			if v == "" {
				fields[i] = "-"
			}
		}
		rows = append(rows, strings.Join(fields, "\t"))
	}
	checkDecisionTable(t, "registry_decisions.tsv", rows)
}

// Observe the real consent gate with an isolated intent directory. The
// simulated first write marks the transport boundary without sending a request.
func sampleReceipt(t *testing.T, r *Registry, tool Tool) string {
	t.Helper()
	dir := t.TempDir()
	r.SetStateDir(dir)
	args := map[string]any{"confirmed": true}
	if tool.Decl.PartialConsent != nil {
		args = contractArgs(tool)
		args["confirmed"] = true
	}
	receipt := ""
	current := func() bool {
		records := intent.Pending(intent.Dir(dir))
		if len(records) > 1 || len(records) == 1 && records[0].Tool != tool.Name {
			t.Fatalf("%s: unexpected receipt in isolated directory", tool.Name)
		}
		return len(records) == 1
	}
	handler := r.intended(tool, func(ctx context.Context, _ map[string]any) (string, bool) {
		receipt = "none"
		if current() {
			receipt = "before-handler"
		}
		if err := httpretry.NoteWrite(ctx); err != nil {
			t.Fatalf("%s: marking the simulated write: %v", tool.Name, err)
		}
		if current() && receipt == "none" {
			receipt = "before-write"
		}
		return `{"status":"ok"}`, false
	})
	text, failed := handler(context.Background(), args)
	if failed || receipt == "" {
		t.Fatalf("%s: receipt control did not reach the handler: %s", tool.Name, text)
	}
	return receipt
}

func checkDecisionTable(t *testing.T, file string, actual []string) {
	t.Helper()
	sort.Strings(actual)
	path := filepath.Join("testdata", file)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(string(want), "\r\n", "\n")
	expected := strings.Split(strings.TrimSuffix(normalized, "\n"), "\n")
	if slices.Equal(expected, actual) {
		return
	}
	// Sorted multisets preserve duplicate comparisons in one function.
	for len(expected) > 0 || len(actual) > 0 {
		switch {
		case len(expected) > 0 && (len(actual) == 0 || expected[0] < actual[0]):
			t.Errorf("%s: removed: %s", file, expected[0])
			expected = expected[1:]
		case len(actual) > 0 && (len(expected) == 0 || actual[0] < expected[0]):
			t.Errorf("%s: added: %s", file, actual[0])
			actual = actual[1:]
		default:
			expected = expected[1:]
			actual = actual[1:]
		}
	}
}
