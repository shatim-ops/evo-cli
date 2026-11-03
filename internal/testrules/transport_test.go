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

// transportFactory is the one place that may build on net/http's own
// transport: the base every client of evo goes over (httpretry.DefaultBase).
const transportFactory = "internal/httpretry/httpretry.go\tNewBaseTransport"

// P9 (agent runs of Grok on 1.6.0, G2-6; release plan 1.7, block A, the
// transport): every HTTP client of evo goes over one base transport — the
// factory's, with the ping of HTTP/2 that drops a dead connection. Outside
// the factory, production code does not reach net/http's defaults: no
// http.DefaultTransport (nor its Clone), no http.DefaultClient, no
// http.Get/Post/Head, no http.Transport of its own, no http.Client without
// a Transport (it would go over http.DefaultTransport). A new client
// outside the base fails here on every OS; the behaviour (the ping) is
// internal/httpretry's TestBaseTransport_ChecksHTTP2Connections.
func TestTransport_OneBase(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && path != root || d.Name() == "testdata" || d.Name() == "vendor") {
			return filepath.SkipDir
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
		httpName := ""
		for _, imp := range f.Imports {
			if imp.Path.Value == `"net/http"` {
				httpName = "http"
				if imp.Name != nil {
					httpName = imp.Name.Name
				}
			}
		}
		if httpName == "" {
			return nil
		}
		where := func(n ast.Node, fn string) string {
			return fset.Position(n.Pos()).String() + " (" + fn + ")"
		}
		check := func(fn string, body ast.Node) {
			ast.Inspect(body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SelectorExpr:
					if id, ok := x.X.(*ast.Ident); ok && id.Name == httpName {
						switch x.Sel.Name {
						case "DefaultTransport", "DefaultClient", "Get", "Post", "Head", "PostForm":
							if rel+"\t"+fn != transportFactory {
								bad = append(bad, where(x, fn)+": http."+x.Sel.Name+" — net/http's own transport, not the base of httpretry")
							}
						}
					}
				case *ast.CompositeLit:
					sel, ok := x.Type.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); !ok || id.Name != httpName {
						return true
					}
					switch sel.Sel.Name {
					case "Transport":
						if rel+"\t"+fn != transportFactory {
							bad = append(bad, where(x, fn)+": an http.Transport of its own — build on httpretry.NewBaseTransport")
						}
					case "Client":
						if !hasKey(x, "Transport") {
							bad = append(bad, where(x, fn)+": an http.Client without a Transport goes over http.DefaultTransport — give it httpretry's (NewClient, UserAgent)")
						}
					}
				}
				return true
			})
		}
		for _, decl := range f.Decls {
			switch dd := decl.(type) {
			case *ast.FuncDecl:
				if dd.Body != nil {
					check(funcName(dd), dd.Body)
				}
			case *ast.GenDecl:
				check("package level", dd)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

func hasKey(lit *ast.CompositeLit, key string) bool {
	for _, e := range lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == key {
				return true
			}
		}
	}
	return false
}
