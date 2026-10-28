// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"
)

// dispatchedMethods are the methods Server.call has a case for, read from
// its source: a method added there is in the test below without a line in
// it.
func dispatchedMethods(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "call" || fd.Recv == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if cc, ok := n.(*ast.CaseClause); ok {
				for _, e := range cc.List {
					if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						m, _ := strconv.Unquote(lit.Value)
						out = append(out, m)
					}
				}
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatal("no case of Server.call found: the test reads the wrong function")
	}
	sort.Strings(out)
	return out
}

// JSON-RPC 2.0, section 4: a request with an id is not a notification, and
// the server answers it, whatever its method — a notification method sent
// with an id included (Fable, probe of 1.4.0: notifications/initialized
// with an id got no answer, and a client that waits for one hangs). Only a
// request without an id gets none.
func TestRequestWithID_GetsOneAnswer(t *testing.T) {
	methods := append(dispatchedMethods(t), "no/such/method")
	for i, m := range methods {
		id := 100 + i
		req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":{}}`, id, m)
		got := runLines(t, newTestServer(), req)
		if len(got) != 1 {
			t.Errorf("%s with id %d: %d answers, want one", m, id, len(got))
			continue
		}
		if a, _ := got[0].(map[string]any); a["id"] != float64(id) {
			t.Errorf("%s with id %d: the answer carries id %v", m, id, a["id"])
		}
	}
}
