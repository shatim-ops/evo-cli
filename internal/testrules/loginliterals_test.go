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
	"sort"
	"strconv"
	"strings"
	"testing"
)

// loginTable is the one file that spells the addresses and constants of
// the login (A0-4): every other place takes them from internal/auth.
const loginTable = "internal/auth/oidc.go"

// loginLiteral finds, in a string literal, what only the table may spell:
// the hosts of Cloud.ru IAM's OpenID Connect, its issuer path, evo's client
// and the role the login grants as a whole value. The role named inside a
// sentence (a hint, a tool description) is prose, not a value.
var loginLiteral = []struct {
	what string
	re   *regexp.Regexp
}{
	{"an IAM OIDC host", regexp.MustCompile(`(^|[^a-z0-9.-])(id\.cloud\.ru|auth\.iam\.cloud\.ru)([^a-z0-9.-]|$)`)},
	{"the issuer path", regexp.MustCompile(`/auth/system(/|$)`)},
	{"a redirect port in an address", regexp.MustCompile(`:(18386|23386|28386)([^0-9]|$)`)},
	{"the client id", regexp.MustCompile(`^evo_cmcp$`)},
	{"the login role as a value", regexp.MustCompile(`^platform\.project\.admin$`)},
}

// loginPorts are the redirect ports of evo_cmcp: an integer literal of one
// of them outside the table is a port spelled twice.
var loginPorts = map[string]bool{"18386": true, "23386": true, "28386": true}

// A0-4 (release-plan 1.8): an address or constant of the login is spelled
// in the table and only there — the issuer, its hosts, the client, the
// redirect ports, the role. A second spelling is a second point of
// decision: a host that drifts from the closed list, a port the provider
// does not know. Production code and the shared test worlds (non-test
// files) are walked; comments are not code.
func TestLoginLiterals_OnlyInTheTable(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var bad []string
	tableSeen := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "vendor" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok {
				return true
			}
			pos := fset.Position(lit.Pos())
			switch lit.Kind {
			case token.STRING:
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				for _, l := range loginLiteral {
					if l.re.MatchString(s) {
						if rel == loginTable {
							tableSeen++
						} else {
							bad = append(bad, rel+":"+strconv.Itoa(pos.Line)+": "+l.what+" "+strconv.Quote(s))
						}
					}
				}
			case token.INT:
				if loginPorts[lit.Value] {
					if rel == loginTable {
						tableSeen++
					} else {
						bad = append(bad, rel+":"+strconv.Itoa(pos.Line)+": a redirect port of the login, "+lit.Value)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if tableSeen < 6 {
		t.Fatalf("%s spells %d of the login's literals: the table moved or the scan sees nothing, update loginTable", loginTable, tableSeen)
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("%s — take it from %s (A0-4)", b, loginTable)
	}
}
