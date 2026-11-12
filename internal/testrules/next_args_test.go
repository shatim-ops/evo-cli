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
	"strconv"
	"strings"
	"testing"
)

// The consent of a call does not move into the step the answer names
// (R-85; release plan 1.9, «Согласие не переезжает в следующий шаг»): an
// agent that follows next_args would send the change again with no
// question. The worlds see it only where they cut a call
// (TestMCP_NoCallOutlivesTheBudget, TestNextStep_…); the code says it for
// every path. Two ways the consent gets into the arguments of a step, and
// each is a closed list of functions.

// consentWriters are the functions of internal/tools that write the key
// confirmed into a map: a next step that keeps the consent by a decision
// (the declared continuations, bound to the id of what was approved), a
// call of another tool inside a consented one, or a step that is to lose
// it. "file: function" → what it writes and why. A closed list: a new
// function is a decision, not a line added in passing.
var consentWriters = map[string]string{
	"internal/tools/provision_adopt.go: provisionCreating": "the step of provision cut while the VM is created: unbound, to be bound to the VM's id (D102)",
	"internal/tools/tools.go: deployInProgress":            "the step of deploy: a declared continuation, bound to compose_hash (D82)",
	"internal/tools/tools.go: destroyDeleting":             "the step of destroy: a declared continuation, bound to vm_id",
	"internal/tools/tools_deploy.go: applyDeployPlan":      "the step of deploy_apply: unbound, to be bound to the VM's id (D102)",
	"internal/tools/tools_disk.go: diskAttachArgs":         "the step of disk_attach and of disk_create attach_to: a declared continuation, bound to disk_id and vm",
	"internal/tools/tools_stack.go: stackDestroyNext":      "the step of stack_destroy: a declared continuation, bound to job_id",
	"internal/tools/tools_stack.go: advance":               "not a step: stack_apply, consented for the whole stack, calls the tool of each step",
	"internal/tools/tools_stack.go: planStackDestroy":      "not a step: stack_destroy, consented for the whole job, calls the delete of each resource",
}

// argsCopies are the functions of internal/tools that copy the arguments
// of the call whole (maps.Clone(args)): the copy carries confirmed when
// the call had it. The one place a step is made of the arguments of the
// call is nextSameCall (gate.go), which takes confirmed out. "file:
// function" → where the copy goes. A closed list: these steps keep the
// consent by the decision their line of consentWriters or of
// nextStepKeepsConsent (internal/tools) names.
var argsCopies = map[string]string{
	"internal/tools/provision_adopt.go: provisionCreating": "the step of provision, which writes confirmed itself (consentWriters)",
	"internal/tools/tools.go: deployInProgress":            "the step of deploy, which writes confirmed itself (consentWriters)",
	"internal/tools/tools_disk.go: settlingPlan":           "the step of disk_detach and disk_resize while the disk settles: a declared continuation, bound to disk_id",
	"internal/tools/tools_kube.go: helmAtLimit":            "the step of k8s_helm install or upgrade stopped at repo add or repo update, before the release was touched: a declared continuation (owner's decision 12.11)",
}

// argsCopiesToStrip are the copies that go into a step as they are, with
// the consent of the call, where no decision keeps it: a read that was cut
// and k8s_helm stopped before it touched the release. They become
// nextSameCall. Only shrinks. Closes by 1.9.
var argsCopiesToStrip = map[string]string{}

// consentWritesAndCopies are, per function of a file, the writes of
// "confirmed" into a map — a key of a literal, or the left side of an
// assignment — and the copies of a parameter or variable named args.
func consentWritesAndCopies(decl ast.Decl) (writes, copies []token.Pos) {
	ast.Inspect(decl, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			if lit, ok := x.Key.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, _ := strconv.Unquote(lit.Value); s == "confirmed" {
					// A schema or a description names the argument
					// ("confirmed": {…}, "confirmed": "…"): only the value
					// true is the consent written.
					if isTrue(x.Value) {
						writes = append(writes, x.Pos())
					}
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if ix, ok := lhs.(*ast.IndexExpr); ok {
					if lit, ok := ix.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, _ := strconv.Unquote(lit.Value); s == "confirmed" && len(x.Rhs) == 1 && isTrue(x.Rhs[0]) {
							writes = append(writes, x.Pos())
						}
					}
				}
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Clone" || len(x.Args) != 1 {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "maps" {
				return true
			}
			if arg, ok := x.Args[0].(*ast.Ident); ok && arg.Name == "args" {
				copies = append(copies, x.Pos())
			}
		}
		return true
	})
	return writes, copies
}

// isTrue reports whether e is the literal true.
func isTrue(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "true"
}

func TestNextArgs_ConsentIsWrittenOnlyByTheClosedList(t *testing.T) {
	root := filepath.Join("..", "..")
	dir := filepath.Join(root, "internal", "tools")
	seenWrites, seenCopies := map[string]bool{}, map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if rel == gateFile {
			return nil // the gate reads confirmed, and nextSameCall takes it out
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			key := rel + ": " + declName(decl)
			writes, copies := consentWritesAndCopies(decl)
			for _, pos := range writes {
				seenWrites[key] = true
				if consentWriters[key] == "" {
					t.Errorf("%s (%s): writes confirmed into a map — a next step is a read, or the same call through nextSameCall (%s), which carries no consent; a step that keeps it is a decision and a line of consentWriters", key, fset.Position(pos), gateFile)
				}
			}
			for _, pos := range copies {
				seenCopies[key] = true
				if argsCopies[key] == "" && argsCopiesToStrip[key] == "" {
					t.Errorf("%s (%s): copies the arguments of the call (maps.Clone(args)) — the copy carries confirmed; make the step with nextSameCall (%s)", key, fset.Position(pos), gateFile)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range []struct {
		name string
		rows map[string]string
		seen map[string]bool
	}{{"consentWriters", consentWriters, seenWrites}, {"argsCopies", argsCopies, seenCopies}, {"argsCopiesToStrip", argsCopiesToStrip, seenCopies}} {
		var gone []string
		for key := range list.rows {
			if !list.seen[key] {
				gone = append(gone, key)
			}
		}
		sort.Strings(gone)
		for _, key := range gone {
			t.Errorf("%s names %s, which does it no longer: remove the line", list.name, key)
		}
	}
}
