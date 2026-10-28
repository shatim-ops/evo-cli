// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// linkOnly are the addresses outside Cloud.ru that production code names,
// each one evo prints and never requests: "file: host" → why.
var linkOnly = map[string]string{
	"internal/cli/agent_clients.go: code.claude.com":       "the docs of an MCP client, printed by evo agent setup",
	"internal/cli/agent_clients.go: cursor.com":            "the docs of an MCP client, printed by evo agent setup",
	"internal/cli/agent_clients.go: code.visualstudio.com": "the docs of an MCP client, printed by evo agent setup",
	"internal/cli/agent_clients.go: geminicli.com":         "the docs of an MCP client, printed by evo agent setup",
	"internal/cli/agent_clients.go: developers.openai.com": "the docs of an MCP client, printed by evo agent setup",
	"internal/cli/agent_clients.go: evo":                   "a placeholder entry whose keys evo owns in a client's config, never a request",
	"internal/product/product.go: t.me":                    "the feedback links of evo --help and README",
	"internal/product/product.go: github.com":              "the repository, in evo version and NOTICE",
}

// outboundHost is the host of an address literal ("" when s is none):
// userinfo and port dropped, a format verb kept as it is.
func outboundHost(s string) string {
	for _, scheme := range []string{"https://", "http://"} {
		if rest, ok := strings.CutPrefix(s, scheme); ok {
			host, _, _ := strings.Cut(rest, "/")
			if _, after, ok := strings.Cut(host, "@"); ok {
				host = after
			}
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			return strings.Trim(host, "[]")
		}
	}
	return ""
}

// Release plan 1.5, S6, «no telemetry»: evo sends nothing anywhere but
// Cloud.ru (*.cloud.ru) and the addresses of its configuration (an S3 or
// Foundation Models endpoint, a loopback MCP server). Every address
// production code spells — a request or a link — is on *.cloud.ru or
// loopback, or linkOnly says why it is only printed; a new host fails the
// build. Releases and install.sh fetch from GitHub outside the binary.
func TestOutbound_OnlyCloudRuOrDeclaredLinks(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	var found []string
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
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				host := outboundHost(s)
				if host == "" || host == "cloud.ru" || strings.HasSuffix(host, ".cloud.ru") ||
					host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback() {
					return true
				}
				key := rel + ": " + host
				seen[key] = true
				if linkOnly[key] == "" {
					found = append(found, key+" ("+fset.Position(lit.Pos()).String()+")")
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(found)
	for _, k := range found {
		t.Errorf("%s: an address outside Cloud.ru — evo sends nothing but to Cloud.ru and its configuration (S6); a link it only prints goes into linkOnly with why", k)
	}
	for k := range linkOnly {
		if !seen[k] {
			t.Errorf("linkOnly names %q, which production code no longer spells: remove the line", k)
		}
	}
}
