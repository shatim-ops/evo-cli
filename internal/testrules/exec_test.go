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

// execSites are the places of production code that start a process:
// "file<TAB>function" → what they run and why. Anything else that starts
// one fails the build.
var execSites = map[string]string{
	"internal/kubetools/kubetools.go\trun": "kubectl or helm (execTools), with KUBECONFIG and an env without evo's secrets (childEnv)",
}

// execTools are the programs evo runs, by the name the site above is
// given: the workload tools of Managed Kubernetes, which evo drives and
// does not reimplement (internal/kubetools).
var execTools = map[string]bool{"kubectl": true, "helm": true}

// processStarts are the calls that start a process, by package path.
// LookPath is not one: finding a file in PATH runs nothing.
var processStarts = map[string]map[string]bool{
	"os/exec":               {"Command": true, "CommandContext": true},
	"os":                    {"StartProcess": true},
	"syscall":               {"Exec": true, "ForkExec": true, "StartProcess": true},
	"golang.org/x/sys/unix": {"Exec": true},
}

// The finding on #771 («1.7 — релиз», 03.11; release plan 1.7, block A):
// evo doctor ran the second evo it found in PATH to read its version, with
// the whole environment — the access key went to a file evo does not own.
// evo runs no file it finds in PATH but the declared tools: a process is
// started only in execSites (an exec.Cmd literal counts as a start too),
// and the one site there runs a name of execTools, given as a literal by
// every caller. Reading a file (debug/buildinfo) or finding it
// (exec.LookPath) is not running it.
func TestExec_NoForeignFileFromPATH(t *testing.T) {
	root := filepath.Join("..", "..")
	found := map[string][]string{}
	var bad []string
	for _, dir := range []string{"cmd", "internal"} {
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
			local := importNames(f)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				key := rel + "\t" + funcName(fd)
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.CallExpr:
						if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
							if id, ok := sel.X.(*ast.Ident); ok && processStarts[local[id.Name]][sel.Sel.Name] {
								found[key] = append(found[key], fset.Position(x.Pos()).String()+": "+local[id.Name]+"."+sel.Sel.Name)
							}
						}
					case *ast.CompositeLit:
						if sel, ok := x.Type.(*ast.SelectorExpr); ok {
							if id, ok := sel.X.(*ast.Ident); ok && local[id.Name] == "os/exec" && sel.Sel.Name == "Cmd" {
								found[key] = append(found[key], fset.Position(x.Pos()).String()+": exec.Cmd{}")
							}
						}
					}
					return true
				})
			}
			if rel == "internal/kubetools/kubetools.go" {
				bad = append(bad, checkToolNames(fset, f)...)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for k, at := range found {
		if _, ok := execSites[k]; !ok {
			bad = append(bad, strings.Replace(k, "\t", ": ", 1)+" starts a process outside execSites — evo runs no file it finds in PATH but the declared tools (#771); read the file instead (debug/buildinfo), or declare the site with why:\n    "+strings.Join(at, "\n    "))
		}
	}
	for k := range execSites {
		if _, ok := found[k]; !ok {
			bad = append(bad, strings.Replace(k, "\t", ": ", 1)+": in execSites, but starts no process there; take the line out")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// importNames maps the local name of each import to its path.
func importNames(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		out[name] = p
	}
	return out
}

// checkToolNames holds the one site to execTools: every call of run in
// kubetools gives the program's name (its fifth argument) as a literal of
// execTools, not a name from a caller.
func checkToolNames(fset *token.FileSet, f *ast.File) []string {
	var bad []string
	calls := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "run" {
			return true
		}
		calls++
		at := fset.Position(call.Pos()).String()
		if len(call.Args) < 5 {
			return true
		}
		lit, ok := call.Args[4].(*ast.BasicLit)
		name, err := "", error(nil)
		if ok && lit.Kind == token.STRING {
			name, err = strconv.Unquote(lit.Value)
		}
		if !ok || err != nil || !execTools[name] {
			bad = append(bad, at+": kubetools.run is given a program that is not a literal of execTools (kubectl, helm)")
		}
		return true
	})
	if calls == 0 {
		bad = append(bad, "internal/kubetools/kubetools.go: no call of run — the check of the program's name sees nothing; follow the site")
	}
	return bad
}
