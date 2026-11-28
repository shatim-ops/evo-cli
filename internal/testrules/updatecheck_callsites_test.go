// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdateCheck_AllowedCallSites enforces the SECURITY.md promise that only
// evo doctor (internal/cli/doctor.go) and evo mcp startup (internal/cli/mcp.go)
// may perform network update checks via updatecheck.RefreshIfStale, and that
// local cache reading / notification marking stays in the declared CLI files.
func TestUpdateCheck_AllowedCallSites(t *testing.T) {
	allowed := map[string]map[string]bool{
		"RefreshIfStale": {
			"internal/cli/doctor.go": true,
			"internal/cli/mcp.go":    true,
		},
		"ReadCached": {
			"internal/cli/main.go": true,
			"internal/cli/mcp.go":  true,
		},
		"MarkNotifiedCLI": {
			"internal/cli/main.go": true,
		},
		"MarkNotifiedMCP": {
			"internal/cli/mcp.go": true,
		},
	}

	root := filepath.Join("..", "..")
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "internal/updatecheck/") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || id.Name != "updatecheck" {
					return true
				}
				if files, guarded := allowed[sel.Sel.Name]; guarded && !files[rel] {
					t.Errorf("updatecheck.%s called from disallowed file %s (%s)", sel.Sel.Name, rel, fset.Position(sel.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestDockerfile_DisablesUpdateCheck ensures the MCP Docker image keeps
// EVO_NO_UPDATE_CHECK=1 by default.
func TestDockerfile_DisablesUpdateCheck(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ENV EVO_NO_UPDATE_CHECK=1") {
		t.Fatal("Dockerfile must set ENV EVO_NO_UPDATE_CHECK=1 by default")
	}
}
