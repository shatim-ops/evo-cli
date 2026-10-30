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

// The reasons a wait of production code says nothing while it waits.
const (
	// A lock of a local file: milliseconds, another evo process on the
	// same file.
	silentLock = "a lock of a local file"
	// The transport's repeat of one request: its caller reports the call.
	silentRetry = "a repeat of one request"
	// Not a wait for the cloud: the loop of a server or a poll whose
	// answer is the report.
	silentLoop = "not a wait for a resource"
	// A few seconds, said before the loop.
	silentShort = "a few seconds, said before the loop"
	// A gap of axis 2: the wait is silent and should not be — the line of
	// the release plan that fixes it.
	silentGap = "gap"
)

// silentWaits are the functions of production code that wait in a loop
// (time.After, time.Sleep, a timer or a ticker in a for) and say nothing
// while they wait: "path<TAB>function" → why. A new one fails the test, and
// so does a line whose function no longer waits silently. A gap is a wait
// that must say what it waits for and in which state the resource is
// (CLAUDE.md §6, axis 2), listed with its work item until it does.
var silentWaits = map[string]struct{ why, item string }{
	"internal/config/profile.go\tlockProfiles":             {why: silentLock},
	"internal/journal/journal.go\tlock":                    {why: silentLock},
	"internal/state/lock.go\tFileLock":                     {why: silentLock},
	"internal/state/lock.go\tLockWait":                     {why: silentLock},
	"internal/httpretry/httpretry.go\tTransport.roundTrip": {why: silentRetry},
	"internal/mk8s/kubeconfig.go\tFetchOIDCToken":          {why: silentRetry},
	"internal/mcp/server.go\tServer.RunContext":            {why: silentLoop},
	// stack_status answers the state of the job: its answer is the report.
	"internal/tools/tools_stack.go\tRegistry.handleStackStatus": {why: silentLoop},
	// After a Ctrl-C during the create, up to 3 lookups 2 s apart, said
	// before («Looking for floating IP … made before the interruption»).
	"internal/engine/deploy.go\tEngine.findBootFIP": {why: silentShort},
}

// waitCalls are the calls that make a loop a wait. A loop that only
// receives from a channel or ctx.Done() is not seen: the waits of evo go
// through time.After or a timer (the select of each also takes ctx).
var waitCalls = map[string]bool{"After": true, "Sleep": true, "NewTimer": true, "NewTicker": true, "Tick": true}

// reportCalls are the calls that say what a wait waits for: the steps and
// warnings of the CLI's printer and of the progress of a tool call, and a
// function a wait is given or makes to say it (assignFloatingIP: progress,
// report).
var reportCalls = map[string]bool{"Step": true, "StepDone": true, "Warning": true, "progress": true, "report": true}

// CLAUDE.md §6, axis 2: «Долгая операция сообщает, что делает и в каком
// состоянии ресурс, а не только сколько секунд прошло; одинаково в CLI и
// MCP». Every function of production code that waits in a loop says so
// while it waits, or is declared in silentWaits with its reason. The world
// of a disk created slowly (waitDiskAvailable, TestDiskWait_*) holds what
// one such wait says; this holds that none is silent unseen.
func TestWaits_SayWhatTheyWaitFor(t *testing.T) {
	root := filepath.Join("..", "..")
	silent := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			// A wait reports when the loop that waits says something: a
			// line before the loop alone leaves the wait silent.
			silentLoop := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				loop, ok := n.(*ast.ForStmt)
				if !ok {
					return true
				}
				waits, reports := false, false
				ast.Inspect(loop.Body, func(m ast.Node) bool {
					switch f := callFun(m).(type) {
					case *ast.SelectorExpr:
						if id, ok := f.X.(*ast.Ident); ok && id.Name == "time" && waitCalls[f.Sel.Name] {
							waits = true
						}
						reports = reports || reportCalls[f.Sel.Name]
					case *ast.Ident:
						reports = reports || reportCalls[f.Name]
					}
					return true
				})
				silentLoop = silentLoop || waits && !reports
				return true
			})
			if silentLoop {
				silent[filepath.ToSlash(rel)+"\t"+funcName(fd)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(silent))
	for k := range silent {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, ok := silentWaits[k]
		switch {
		case !ok:
			t.Errorf("%s waits in a loop and says nothing while it waits: say what it waits for and the state of the resource (pp.Step, Printer.StepDone), or declare it in silentWaits with its reason", strings.Replace(k, "\t", ": ", 1))
		case w.why == silentGap && w.item == "":
			t.Errorf("silentWaits: %s is a gap without its work item", k)
		}
	}
	for k := range silentWaits {
		if !silent[k] {
			t.Errorf("silentWaits names %q, which no longer waits silently: remove the line", k)
		}
	}
}

// callFun is the function of a call, nil for anything else.
func callFun(n ast.Node) ast.Expr {
	if c, ok := n.(*ast.CallExpr); ok {
		return c.Fun
	}
	return nil
}
