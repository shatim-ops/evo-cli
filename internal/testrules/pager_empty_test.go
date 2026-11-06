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
	"strconv"
	"strings"
	"testing"
)

// orgListPrefix is how a list of the Organization API names itself to
// pager: "organization: customers", "organization: projects".
const orgListPrefix = "organization: "

// IAM-10 (acceptance B-5, 06.11; «Риск» to the blocker): pager.AllUntilEmpty
// ends a list at its first empty page, which is right for the Organization
// API only — past its last item it answers empty pages with new tokens. On
// another API an empty page may stand before the rest of the list, and
// ending there cuts it with exit 0 (axis 3). Every call of AllUntilEmpty
// outside tests names an Organization API list: its first argument starts
// with "organization: ". A list of another API read with it fails here.
func TestPager_AllUntilEmptyOnlyForTheOrganizationAPI(t *testing.T) {
	root := filepath.Join("..", "..")
	calls := 0
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "AllUntilEmpty" || len(call.Args) == 0 {
				return true
			}
			calls++
			if what, ok := leadingLiteral(call.Args[0]); !ok || !strings.HasPrefix(what, orgListPrefix) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: pager.AllUntilEmpty for a list not named %q…: an empty page ends only a list of the Organization API (IAM-10); read others with pager.All",
					filepath.ToSlash(rel), fset.Position(call.Pos()).Line, orgListPrefix)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("no call of pager.AllUntilEmpty found: the scan sees nothing")
	}
}

// leadingLiteral is the leftmost string literal of a concatenation, or of
// a literal alone.
func leadingLiteral(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return leadingLiteral(x.X)
		}
	case *ast.ParenExpr:
		return leadingLiteral(x.X)
	}
	return "", false
}
