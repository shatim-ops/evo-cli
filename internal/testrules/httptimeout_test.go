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
	"strings"
	"testing"
)

// unboundedClients are the HTTP clients of production code without a
// Timeout of their own, each bounded another way: "file: where" → why.
var unboundedClients = map[string]string{
	"internal/tools/tools_auth.go: probeClient": "every probe runs under context.WithTimeout(doctorProbeTimeout)",
	"internal/cli/mcp.go: newMCPHealthcheckCmd": "http.DefaultClient under context.WithTimeout of 5 s",
}

// A request to a cloud that does not answer must end: a client without a
// timeout waits on it for ever, and a deploy, a poll or a whole MCP server
// waits with it. Every http.Client of production code has a Timeout, or a
// line of unboundedClients says how its requests are bounded. It stands
// for the "slow" fault of the fault matrix (process audit 1.2.1, 6.1): the
// fake answers below the HTTP client, where only this rule can see a
// missing timeout.
func TestHTTPClientsHaveTimeouts(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, decl := range f.Decls {
				where := declName(decl)
				ast.Inspect(decl, func(n ast.Node) bool {
					if !unbounded(n) {
						return true
					}
					key := rel + ": " + where
					seen[key] = true
					if unboundedClients[key] == "" {
						t.Errorf("%s (%s): an HTTP client without a Timeout; set one, or bound its requests and say how in unboundedClients",
							key, fset.Position(n.Pos()))
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for key := range unboundedClients {
		if !seen[key] {
			t.Errorf("unboundedClients names %s, which has no unbounded client any more: remove the line", key)
		}
	}
}

// unbounded reports an http.Client literal without a Timeout field, a zero
// client (new(http.Client), var c http.Client), or a use of
// http.DefaultClient (it has none).
func unbounded(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.CallExpr:
		fn, ok := x.Fun.(*ast.Ident)
		return ok && fn.Name == "new" && len(x.Args) == 1 && isHTTP(x.Args[0], "Client")
	case *ast.ValueSpec:
		return len(x.Values) == 0 && x.Type != nil && isHTTP(x.Type, "Client")
	case *ast.CompositeLit:
		if !isHTTP(x.Type, "Client") {
			return false
		}
		for _, e := range x.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Timeout" {
					return false
				}
			}
		}
		return true
	case *ast.SelectorExpr:
		return isHTTP(x, "DefaultClient")
	}
	return false
}

func isHTTP(e ast.Expr, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "http"
}

// declName is the function or the first variable a declaration declares.
func declName(d ast.Decl) string {
	switch x := d.(type) {
	case *ast.FuncDecl:
		return x.Name.Name
	case *ast.GenDecl:
		for _, s := range x.Specs {
			if v, ok := s.(*ast.ValueSpec); ok && len(v.Names) > 0 {
				return v.Names[0].Name
			}
		}
	}
	return "?"
}

// The rule sees a client however it is made: a literal, its address, new,
// a zero variable, and http.DefaultClient.
func TestUnboundedClientForms(t *testing.T) {
	for src, want := range map[string]bool{
		`var c = http.Client{}`:                          true,
		`var c = &http.Client{Transport: nil}`:           true,
		`var c = &http.Client{Timeout: time.Second}`:     false,
		`var c = new(http.Client)`:                       true,
		`var c http.Client`:                              true,
		`var c *http.Client`:                             false,
		`var c = http.DefaultClient`:                     true,
		`func f() { _, _ = http.DefaultClient.Do(nil) }`: true,
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\n"+src, 0)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		got := false
		ast.Inspect(f, func(n ast.Node) bool {
			got = got || unbounded(n)
			return true
		})
		if got != want {
			t.Errorf("%s: unbounded %v, want %v", src, got, want)
		}
	}
}
