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
	// The caller says what is waited for before the wait, and the thing
	// waited for has no state to say (a TCP port).
	silentCallerSays = "the caller says what it waits for; no state to say"
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
	"internal/httpretry/httpretry.go\tTransport.RoundTrip": {why: silentRetry},
	"internal/mk8s/kubeconfig.go\tFetchOIDCToken":          {why: silentRetry},
	"internal/mcp/server.go\tServer.RunContext":            {why: silentLoop},
	// stack_status answers the state of the job: its answer is the report.
	"internal/tools/tools_stack.go\tRegistry.handleStackStatus": {why: silentLoop},
	// deploy_apply says «Waiting for SSH on <ip>…» before it.
	"internal/deployplan/apply.go\twaitForSSHPort": {why: silentCallerSays},
	// destroy vm_id over MCP waits up to destroyPollTimeout (2 min) for the
	// VM to be gone, silent: no line, no state of the VM.
	"internal/engine/deploy.go\tEngine.waitVMGone": {why: silentGap, item: "release plan 1.5, axis 2: the wait for a deleted VM says its state"},
	// provision with the person's keys repeats SSH up to 4 times, 10 s
	// apart, silent.
	"internal/tools/userkeys.go\tappendUserKeys": {why: silentGap, item: "release plan 1.5, axis 2: the repeats of SSH for the person's keys say so"},
}

// waitCalls are the calls that make a loop a wait.
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
			waits, reports := false, false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if loop, ok := n.(*ast.ForStmt); ok {
					ast.Inspect(loop.Body, func(m ast.Node) bool {
						if s, ok := callFun(m).(*ast.SelectorExpr); ok && waitCalls[s.Sel.Name] {
							if id, ok := s.X.(*ast.Ident); ok && id.Name == "time" {
								waits = true
							}
						}
						return true
					})
				}
				switch f := callFun(n).(type) {
				case *ast.SelectorExpr:
					reports = reports || reportCalls[f.Sel.Name]
				case *ast.Ident:
					reports = reports || reportCalls[f.Name]
				}
				return true
			})
			if waits && !reports {
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
