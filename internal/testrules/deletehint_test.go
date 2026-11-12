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
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// deleteTool is a word that may name a tool that deletes: *_delete,
// destroy, stack_destroy, release_floating_ip, s3_rm. deleteTools keeps the
// ones the registry has (can_delete is a field, not a tool).
var deleteTool = regexp.MustCompile(`\b(destroy|stack_destroy|release_floating_ip|s3_rm|[a-z0-9_]+_delete)\b`)

// deleteTools are the tools of the registry that delete: destructive, and
// named as one that removes (not s3_put, k8s_apply and the others that
// write over).
func deleteTools() map[string]bool {
	out := map[string]bool{}
	for _, tl := range tools.New(nil).Tools() {
		if tools.IsDestructive(tl) && deleteTool.FindString(tl.Name) == tl.Name {
			out[tl.Name] = true
		}
	}
	return out
}

// deleteFlows are the files that are the flow of a delete the person asked
// for, by the tools they carry on: their texts continue that delete.
var deleteFlows = map[string][]string{
	// The engine of destroy: it deletes the VM and releases its floating IP.
	"internal/engine/destroy.go": {"destroy", "release_floating_ip"},
}

// deleteHintDescribes are the texts that name a delete to say what it does
// or does not do, not to offer it: "file<TAB>function<TAB>tool" → what they
// say.
var deleteHintDescribes = map[string]string{
	"internal/engine/network.go\tShowOpenedPorts\tdestroy":              "destroy will not close ports opened without a rule id",
	"internal/engine/network.go\talreadyOpenText\tdestroy":              "destroy will not touch a rule evo did not create",
	"internal/engine/preview.go\tpendingRulesPreview\tdestroy":          "the rules an earlier destroy left (the preview of the call that removes them)",
	"internal/engine/preview.go\tredeployPreview\tdestroy":              "what an earlier destroy left",
	"internal/engine/preview.go\trulePreview\tdestroy":                  "destroy releases the floating IP the deploy creates",
	"internal/tools/tools_stack.go\tnormalizeManifest\tstack_destroy":   "a taken s3 key: the stack would overwrite the object and stack_destroy would delete it",
	"internal/engine/ports_preview.go\tportsLine\tdestroy":              "destroy closes only the rules it creates",
	"internal/tools/tools_k8s_addons.go\tplanK8sAddAddon\tk8s_delete": "an addon goes with its cluster",
	"internal/tools/tools_stack.go\tadoptVM\tstack_destroy":             "the VM is kept for stack_destroy",
	"internal/tools/tools_stack.go\tadvance\tstack_destroy":             "the resource is kept for stack_destroy",
}

// humanDecision are the words by which a text that names a delete leaves
// it to the person (release plan 1.7, block D: «спросите человека, прежде
// чем удалять»), in English and in Russian.
var humanDecision = []string{"ask the person", "спросите человека"}

// deleteHintScope is the code whose text reaches an agent: the answers,
// hints and previews of the tools, the engine's output, the plan's.
var deleteHintScope = []string{"internal/tools", "internal/engine", "internal/deployplan"}

// deleteHintMisses are the texts of deleteHintScope that offer a delete as
// the next step and do not leave it to the person: "file<TAB>function<TAB>
// tool" → why it is a miss. It only shrinks, and it is empty since 1.8
// (release plan 1.7, block D; owner's decision 03.11, #732): the eight
// lines FB170-3 (Fable audit 1.7) brought when the scan read the bodies
// of cases and the concatenations were closed by their texts (owner's
// decision 05.11, #889: «все 8 — в 1.8»).
var deleteHintMisses = map[string]string{}

// Release plan 1.7, block D (owner's decision 03.11, #732: «убери за собой»
// is not in the core; the agent creates, the person decides what to
// delete): a text of evo that names a tool that deletes is either the
// tool's own — its preview or its answer, in a function named after it
// (planDiskDelete for disk_delete, destroyAnswer for destroy) or in a file
// of its flow (deleteFlows) — says what the delete does without offering
// it (deleteHintDescribes), or leaves the delete to the person
// (humanDecision). Anything else is a line of deleteHintMisses: the names
// of deletes in texts are a closed list. A text only for the agent's eye, not a sentence (a
// tool's name, a map key, a case), is not a hint: the scan reads the
// literals with a space in them, outside the declarations of the tools
// (registerTools: Name, Description) and outside comparisons and cases.
func TestHints_DeleteIsThePersonsDecision(t *testing.T) {
	root := filepath.Join("..", "..")
	dels := deleteTools()
	if len(dels) < 20 {
		t.Fatalf("%d tools that delete in the registry: the filter sees nothing", len(dels))
	}
	found := map[string][]string{}
	described := map[string]bool{}
	for _, dir := range deleteHintScope {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil || fd.Name.Name == "registerTools" {
					continue
				}
				own := strings.ToLower(strings.ReplaceAll(fd.Name.Name, "_", ""))
				text := func(pos token.Pos, s string) {
					if !strings.Contains(s, " ") || leavesItToThePerson(s) {
						return
					}
					for _, m := range deleteTool.FindAllString(s, -1) {
						if !dels[m] || strings.Contains(own, strings.ReplaceAll(m, "_", "")) || slices.Contains(deleteFlows[rel], m) {
							continue // not a tool, or the tool's own preview or answer
						}
						k := rel + "\t" + fd.Name.Name + "\t" + m
						if _, ok := deleteHintDescribes[k]; ok {
							described[k] = true
							continue
						}
						found[k] = append(found[k], fset.Position(pos).String()+": "+s)
					}
				}
				ast.Inspect(fd.Body, inspectNode(text))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var bad []string
	for k, texts := range found {
		if _, ok := deleteHintMisses[k]; !ok {
			bad = append(bad, strings.ReplaceAll(k, "\t", ": ")+" — a text offers the delete and does not leave it to the person (\""+humanDecision[0]+"\"):\n    "+strings.Join(texts, "\n    "))
		}
	}
	for k := range deleteHintDescribes {
		if !described[k] {
			bad = append(bad, strings.ReplaceAll(k, "\t", ": ")+": in deleteHintDescribes, but no text there names the delete; take the line out")
		}
	}
	for k := range deleteHintMisses {
		if _, ok := found[k]; !ok {
			bad = append(bad, strings.ReplaceAll(k, "\t", ": ")+": in deleteHintMisses, but no text there offers the delete; take the line out (the list only shrinks)")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	t.Logf("deleteHintMisses: %d lines (Closes by 1.8)", len(deleteHintMisses))
}

// inspectNode reads the texts under a node for text: a literal, or a
// concatenation of literals as the one text it makes ("…, " + "delete it
// with destroy" is a sentence whose parts alone may not carry "ask the
// person"). It does not read the operands of a comparison (x == "destroy")
// or an index (m["destroy"]), or the values of a case (case "destroy":):
// they name tools, not texts; the body of a case is code like any other.
// Before FB170-3
// (Fable audit 1.7) the scan left every BinaryExpr and CaseClause unread,
// and seven texts of provision and deploy_apply went past it.
func inspectNode(text func(token.Pos, string)) func(ast.Node) bool {
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CaseClause:
			for _, st := range x.Body {
				ast.Inspect(st, visit)
			}
			return false
		case *ast.IndexExpr:
			ast.Inspect(x.X, visit)
			return false
		case *ast.BinaryExpr:
			if x.Op != token.ADD {
				return false
			}
			if s, ok := concat(x); ok {
				text(x.Pos(), s)
				return false
			}
			return true
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if s, err := strconv.Unquote(x.Value); err == nil {
					text(x.Pos(), s)
				}
			}
		}
		return true
	}
	return visit
}

// concat is the text of a concatenation whose parts are all string
// literals; ok is false when one is not (a variable, a call).
func concat(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return concat(x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, ok := concat(x.X)
		if !ok {
			return "", false
		}
		r, ok := concat(x.Y)
		return l + r, ok
	}
	return "", false
}

func leavesItToThePerson(s string) bool {
	low := strings.ToLower(s)
	for _, w := range humanDecision {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// FB170-3 (Fable audit 1.7): the scan of TestHints_DeleteIsThePersonsDecision
// reads the body of a case and a concatenation as one text, and leaves the
// values of a case, the operands of a comparison and an index unread.
func TestHints_ScanReadsCasesAndConcatenations(t *testing.T) {
	src := `package p
func f(m map[string]int, x, y string) string {
	if x == "call destroy now" { return "" }
	_ = m["call destroy now"]
	switch x {
	case "call destroy now":
	default:
		return "in a case: " + "delete it with destroy"
	}
	return "a variable: " + y + " delete it with destroy"
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	ast.Inspect(f, inspectNode(func(_ token.Pos, s string) { got = append(got, s) }))
	want := []string{"", "in a case: delete it with destroy", "a variable: ", " delete it with destroy"}
	if !slices.Equal(got, want) {
		t.Errorf("texts read: %q, want %q", got, want)
	}
}
