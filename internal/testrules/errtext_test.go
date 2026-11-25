// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	cliErrorsImport = "github.com/shatim-ops/evo-cli/internal/errors"
	i18nImport      = "github.com/shatim-ops/evo-cli/internal/i18n"
)

// P1, release 1.10 (docs/internal/release-plan.md, "Язык ошибок CLI"):
// check evo's error text wherever a message is built, with or without a
// formatting verb. Status envelopes belong to the common errors package.
func TestErrorWrapsSpeakCLILanguage(t *testing.T) {
	root := filepath.Join("..", "..")
	counts := make(map[string]int)
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(name string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return err
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, name, nil, 0)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			imports := errorTextImports(f)
			for _, decl := range f.Decls {
				owner := ""
				if fn, ok := decl.(*ast.FuncDecl); ok {
					owner = fn.Name.Name
					if fn.Recv != nil {
						recv := fn.Recv.List[0].Type
						if ptr, ok := recv.(*ast.StarExpr); ok {
							recv = ptr.X
						}
						if typ, ok := recv.(*ast.Ident); ok {
							owner = typ.Name + "." + owner
						}
					}
				}
				checkErrorText(imports, decl, path.Dir(rel), func(kind, msg string, pos token.Pos) {
					key := strings.Join([]string{rel, owner, kind, msg}, "\t")
					counts[key]++
					if counts[key] > inheritedErrorText[key] {
						t.Errorf("%s:%d: localize error text or use the common cloud error: %q", rel, fset.Position(pos).Line, key)
					}
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for k, n := range inheritedErrorText {
		if counts[k] < n {
			t.Errorf("remove fixed occurrence from inheritedErrorText: %q (found %d, allowed %d)", k, counts[k], n)
		}
	}
}

// The package paths distinguish the standard errors.New from the structured
// CLI constructor even when a source file imports either under an alias.
func errorTextImports(f *ast.File) map[string]string {
	imports := map[string]string{}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = p
	}
	return imports
}

func errorTextCall(c *ast.CallExpr, imports map[string]string) string {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Obj != nil {
		return ""
	}
	return imports[id.Name] + "." + sel.Sel.Name
}

// Fold constant concatenations before classifying their language.
func literalErrorText(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			a, ok := literalErrorText(e.X)
			b, okB := literalErrorText(e.Y)
			return a + b, ok && okB
		}
	case *ast.ParenExpr:
		return literalErrorText(e.X)
	case *ast.Ident:
		if e.Obj != nil && e.Obj.Kind == ast.Con {
			vs, ok := e.Obj.Decl.(*ast.ValueSpec)
			if ok && len(vs.Values) == len(vs.Names) {
				for i, n := range vs.Names {
					if n.Name == e.Name {
						return literalErrorText(vs.Values[i])
					}
				}
			}
		}
	}
	return "", false
}

// Interpolated arguments may carry unmodified cloud output in any language.
// Inspect the format and the Russian branch of a translation.
func errorTextLiterals(expr ast.Expr, imports map[string]string, emit func(string, token.Pos)) {
	if s, ok := literalErrorText(expr); ok {
		emit(s, expr.Pos())
		return
	}
	switch e := expr.(type) {
	case *ast.ParenExpr:
		errorTextLiterals(e.X, imports, emit)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			errorTextLiterals(e.X, imports, emit)
			errorTextLiterals(e.Y, imports, emit)
		}
	case *ast.CallExpr:
		switch errorTextCall(e, imports) {
		case i18nImport + ".T", i18nImport + ".Tf":
			if len(e.Args) >= 2 {
				errorTextLiterals(e.Args[1], imports, emit)
			}
		case "fmt.Sprintf":
			if len(e.Args) > 0 {
				errorTextLiterals(e.Args[0], imports, emit)
			}
		}
	}
}

var errorTextTerm = regexp.MustCompile(`--?[A-Za-z][A-Za-z0-9_-]*|\b(API|HTTP|HTTPS|ID|UUID|DNS|VM|CPU|SSH|TCP|TLS|JSON)\b`)

func cloudStatusFormat(s string) bool {
	return strings.Contains(s, " API %d") || strings.HasPrefix(s, "API %d")
}

func checkErrorText(imports map[string]string, decl ast.Decl, dir string, report func(string, string, token.Pos)) {
	read := func(kind string, expr ast.Expr) {
		// The MCP server always runs in English (D6).
		if dir == "internal/mcp" {
			return
		}
		errorTextLiterals(expr, imports, func(msg string, pos token.Pos) {
			prose := wrapIdent.ReplaceAllString(wrapVerb.ReplaceAllString(msg, " "), " ")
			if dir != "internal/errors" && cloudStatusFormat(msg) {
				return
			}
			if !cyrillic.MatchString(msg) && wrapWord.MatchString(errorTextTerm.ReplaceAllString(prose, " ")) {
				report(kind, msg, pos)
			}
		})
	}
	fn, isFunc := decl.(*ast.FuncDecl)
	if isFunc && fn.Body != nil && fn.Type.Results != nil && len(fn.Type.Results.List) == 1 {
		typ, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
		name := fn.Name.Name
		kind := ""
		if ok && typ.Name == "string" {
			switch {
			case fn.Recv != nil && name == "Error" && fn.Type.Params.NumFields() == 0:
				kind = "Error()"
			case strings.HasPrefix(name, "hint") || strings.HasSuffix(name, "Hint"):
				kind = "hint"
			}
		}
		if kind != "" {
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if _, nested := node.(*ast.FuncLit); nested {
					return false
				}
				if ret, ok := node.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
					read(kind, ret.Results[0])
				}
				return true
			})
		}
	}
	ast.Inspect(decl, func(node ast.Node) bool {
		switch e := node.(type) {
		case *ast.BinaryExpr:
			if dir != "internal/errors" && e.Op == token.ADD {
				if v, ok := literalErrorText(e); ok && cloudStatusFormat(v) {
					report("cloud status", v, e.Pos())
					return false
				}
			}
		case *ast.BasicLit:
			if dir != "internal/errors" && e.Kind == token.STRING {
				msg, _ := strconv.Unquote(e.Value)
				if cloudStatusFormat(msg) {
					report("cloud status", msg, e.Pos())
				}
			}
		case *ast.CallExpr:
			callee := errorTextCall(e, imports)
			if id, ok := e.Fun.(*ast.Ident); ok && dir == "internal/errors" {
				callee = cliErrorsImport + "." + id.Name
			}
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Hint" && len(e.Args) > 0 {
				read("hint", e.Args[0])
			}
			switch callee {
			case "fmt.Errorf", "errors.New", cliErrorsImport + ".Prefix":
				if len(e.Args) >= 1 {
					read("message", e.Args[0])
				}
			case cliErrorsImport + ".New", cliErrorsImport + ".Wrap":
				if len(e.Args) >= 3 {
					read("message", e.Args[1])
					read("hint", e.Args[2])
				}
			case cliErrorsImport + ".Suffix":
				if len(e.Args) >= 2 {
					read("message", e.Args[1])
				}
			}
		case *ast.KeyValueExpr:
			switch k := e.Key.(type) {
			case *ast.Ident:
				if k.Name == "Message" || k.Name == "Hint" {
					read(k.Name, e.Value)
				}
			case *ast.BasicLit:
				name, _ := strconv.Unquote(k.Value)
				if name == "hint" || name == "error" {
					read(name, e.Value)
				}
			}
		case *ast.AssignStmt:
			if len(e.Lhs) == len(e.Rhs) {
				for i, lhs := range e.Lhs {
					switch k := lhs.(type) {
					case *ast.SelectorExpr:
						if k.Sel.Name == "Message" || k.Sel.Name == "Hint" {
							read(k.Sel.Name, e.Rhs[i])
						}
					case *ast.IndexExpr:
						if key, ok := literalErrorText(k.Index); ok && (key == "hint" || key == "error") {
							read(key, e.Rhs[i])
						}
					}
				}
			}
		}
		return true
	})
}
