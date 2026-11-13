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

// sendsPastHTTP are the places of production code that send a write past
// evo's HTTP clients — a process or a remote command that may change what
// it reaches: "file<TAB>function" → what. Each tells the write to the
// counter of its call (httpretry.NoteWrite) before it goes out: a
// consented call cut with no outcome that counted no write closes its
// record («Риск», conclusion 187, У2), so a send the counter does not hear
// would drop the record of a call that changed something.
var sendsPastHTTP = map[string]string{
	"internal/kubetools/kubetools.go\trun":           "kubectl or helm, which talk to the cluster themselves",
	"internal/deploy/ssh.go\tSSHDeployer.exec":       "a command on a host over ssh",
	"internal/deploy/ssh.go\tSSHDeployer.ExecStream": "a streamed command on a host over ssh",
}

// sendCalls are the calls that send a process or a remote command, in a
// file that imports os/exec or golang.org/x/crypto/ssh.
var sendCalls = map[string]bool{"CombinedOutput": true, "Output": true, "Run": true, "Start": true, "Shell": true}

// uncountedClients are the HTTP clients of production code whose requests
// are not counted as writes of a call: "file<TAB>function" → why; such a
// function sends GET or HEAD alone. Every
// other client goes over httpretry.Transport or httpretry.UserAgent, which
// count (httpretry.CountWrites).
var uncountedClients = map[string]string{
	"internal/verify/verify.go\tget":            "a liveness probe of a port: a GET, which is no write",
	"internal/cli/mcp.go\tnewMCPHealthcheckCmd": "the healthcheck of evo's own server: a GET of 127.0.0.1",
}

// notAChange are the places that tell a request is no write of its call
// (httpretry.NotAChange): "file<TAB>function" → what the request is. Such
// a request leaves no record when its call is cut, so the mark is an
// exception from the guard: only the request of an exchange for a token
// carries it, set as the context of that request alone — the context of
// the call would hand it to everything the call sends after («Риск»,
// conclusion 187, У2-1).
var notAChange = map[string]string{
	"internal/auth/iam.go\tIAMProvider.exchangeOnce":  "the exchange of the key for an IAM token",
	"internal/mk8s/kubeconfig.go\tfetchOIDCTokenOnce": "the exchange of the key for the OIDC token of a cluster",
}

// Every write of a call is heard by its counter: a send past HTTP is a
// declared point that tells it, and every HTTP client counts — or is
// declared with why its requests are no writes.
func TestSendPoints_EveryWriteIsCounted(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	sends, clients, marks := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "testdata" || d.Name() == "vendor") {
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
			local := importNames(f)
			sender := false
			for _, p := range local {
				sender = sender || p == "os/exec" || p == "golang.org/x/crypto/ssh"
			}
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				key := rel + "\t" + funcName(fd)
				sendsHere, notes := false, false
				ofRequest := map[ast.Expr]bool{}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.CallExpr:
						sel, ok := x.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}
						if id, ok := sel.X.(*ast.Ident); ok && local[id.Name] == "net/http" && sel.Sel.Name == "NewRequestWithContext" && len(x.Args) > 1 {
							ofRequest[x.Args[0]] = true
							// A client declared uncounted sends reads alone.
							if _, declared := uncountedClients[key]; declared && !readMethodArg(x.Args[1], local) {
								bad = append(bad, fset.Position(x.Pos()).String()+": a request of a client in uncountedClients that is not a GET or HEAD — its writes would not be counted")
							}
						}
						if id, ok := sel.X.(*ast.Ident); ok && local[id.Name] == "github.com/shatim-ops/evo-cli/internal/httpretry" && sel.Sel.Name == "NotAChange" {
							marks[key] = true
							if _, declared := notAChange[key]; !declared {
								bad = append(bad, fset.Position(x.Pos()).String()+": httpretry.NotAChange outside notAChange — a request so marked leaves no record when its call is cut; declare the place with what the request is")
							} else if !ofRequest[x] {
								bad = append(bad, fset.Position(x.Pos()).String()+": httpretry.NotAChange is not the context of the request itself (the first argument of http.NewRequestWithContext): the call would hand it to all it sends after")
							}
						}
						if id, ok := sel.X.(*ast.Ident); ok && local[id.Name] == "github.com/shatim-ops/evo-cli/internal/httpretry" && sel.Sel.Name == "NoteWrite" {
							notes = true
						}
						if sender && sendCalls[sel.Sel.Name] {
							sendsHere = true
						}
					case *ast.CompositeLit:
						if sel, ok := x.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Client" {
							if id, ok := sel.X.(*ast.Ident); ok && local[id.Name] == "net/http" && !counts(x, local) {
								clients[key] = true
								if _, declared := uncountedClients[key]; !declared {
									bad = append(bad, fset.Position(x.Pos()).String()+": an HTTP client whose writes are not counted — give it httpretry.Transport or httpretry.UserAgent, or declare it in uncountedClients with why its requests are no writes")
								}
							}
						}
					}
					return true
				})
				if sendsHere {
					sends[key] = true
					if _, declared := sendsPastHTTP[key]; !declared {
						bad = append(bad, strings.Replace(key, "\t", ": ", 1)+" sends a process or a remote command outside sendsPastHTTP: declare it, and tell the write (httpretry.NoteWrite) before it goes out")
					} else if !notes {
						bad = append(bad, strings.Replace(key, "\t", ": ", 1)+" sends past HTTP and does not tell the counter of its call (httpretry.NoteWrite)")
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for k := range sendsPastHTTP {
		if !sends[k] {
			bad = append(bad, strings.Replace(k, "\t", ": ", 1)+": in sendsPastHTTP, but sends nothing there; take the line out")
		}
	}
	for k := range notAChange {
		if !marks[k] {
			bad = append(bad, strings.Replace(k, "\t", ": ", 1)+": in notAChange, but marks no request there; take the line out")
		}
	}
	for k := range uncountedClients {
		if !clients[k] {
			bad = append(bad, strings.Replace(k, "\t", ": ", 1)+": in uncountedClients, but makes no such client there; take the line out")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// freshContexts are the places of production code that start a context of
// their own (context.Background or context.TODO): "file<TAB>function" →
// why its sends need no counter. The counter of a call's writes
// (httpretry.CountWrites) lives in the call's context; a context made anew
// does not carry it, so a write sent on it is not heard («Риск» on #1675,
// R-86: the record of a cut call then stays open — the careful side, but a
// blind spot of TestSendPoints_EveryWriteIsCounted). A root before any call
// (main, a server) is fine: the gate counts on what it derives. A send
// inside a call that must outlive its cancel keeps the counter with
// context.WithoutCancel, which needs no line here.
var freshContexts = map[string]string{
	"internal/cli/main.go\trun":                         "the root of the process; the gate adds the counter per call",
	"internal/cli/main.go\tnewRootCmd":                  "the root when cobra runs without a context (tests); nothing is sent before the gate",
	"internal/cli/completion.go\tcompleteValues":        "shell completion runs no consented call: reads alone",
	"internal/cli/trees.go\tsecretRef.readRaw":          "reads a secret from the terminal; sends nothing",
	"internal/cli/configure_s3.go\trunConfigureS3":      "the preview of a local file write; sends nothing",
	"internal/mcp/http.go\tNewHTTPHandler":              "the root of the HTTP server; the gate adds the counter per call",
	"internal/mcp/http.go\tHTTPHandler.logf":            "a log line; sends nothing",
	"internal/mcp/http.go\tServer.ListenAndServe":       "the shutdown of the HTTP server; sends nothing",
	"internal/mcp/server.go\tServer.Run":                "the root of the stdio server; the gate adds the counter per call",
	"internal/provision/findvm.go\tFindCreatedVM":       "a lookup by list after a lost answer: a GET, which is no write",
	"internal/engine/deploy.go\tEngine.provisionVM":     "deletes the VM and its address after Ctrl-C, when the call's context is gone; the create was counted before, so the record is already marked sent",
	"internal/engine/deploy.go\tEngine.releaseBootFIP":  "the rollback of an address the call created and counted; bounded on its own clock so a slow API does not hold the rollback",
	"internal/engine/deploy.go\tEngine.handleInterrupt": "deletes the VM after Ctrl-C, when the call's context is gone; the create was counted before",
}

// Every context made anew in production code is declared: a write sent on
// it would not be counted as a write of its call (freshContexts).
func TestSendPoints_FreshContextsDeclared(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	seen := map[string]bool{}
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "testdata" || d.Name() == "vendor") {
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
			local := importNames(f)
			for _, decl := range f.Decls {
				key := rel + "\t(package level)"
				if fd, ok := decl.(*ast.FuncDecl); ok {
					key = rel + "\t" + funcName(fd)
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "Background" && sel.Sel.Name != "TODO") {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); !ok || local[id.Name] != "context" {
						return true
					}
					seen[key] = true
					if _, declared := freshContexts[key]; !declared {
						bad = append(bad, fset.Position(call.Pos()).String()+": context."+sel.Sel.Name+" outside freshContexts — a write sent on it is not counted as a write of its call; derive from the call's context (context.WithoutCancel to outlive its cancel) or declare the place with why it sends no write")
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
	for k := range freshContexts {
		if !seen[k] {
			bad = append(bad, strings.Replace(k, "\t", ": ", 1)+": in freshContexts, but makes no fresh context there; take the line out")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// readMethodArg tells a method argument that is http.MethodGet or
// http.MethodHead.
func readMethodArg(e ast.Expr, local map[string]string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && local[id.Name] == "net/http" && (sel.Sel.Name == "MethodGet" || sel.Sel.Name == "MethodHead")
}

// counts tells an http.Client literal whose Transport counts the writes of
// a call: httpretry.Transport or httpretry.UserAgent (or Transport within
// the package itself).
func counts(lit *ast.CompositeLit, local map[string]string) bool {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Transport" {
			continue
		}
		v := kv.Value
		if u, ok := v.(*ast.UnaryExpr); ok {
			v = u.X
		}
		if c, ok := v.(*ast.CompositeLit); ok {
			v = c.Type
		}
		if c, ok := v.(*ast.CallExpr); ok {
			v = c.Fun
		}
		switch x := v.(type) {
		case *ast.Ident:
			return x.Name == "Transport" // httpretry's own
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			return ok && local[id.Name] == "github.com/shatim-ops/evo-cli/internal/httpretry" && (x.Sel.Name == "Transport" || x.Sel.Name == "UserAgent")
		}
	}
	return false
}
