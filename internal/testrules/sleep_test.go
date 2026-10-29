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
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The reasons a wait of production code is not in a world of the machine
// sleeping.
const (
	// The wait asks no Cloud.ru API with a token: a lock, TCP to a VM's
	// port, the transport's repeat of one request, a server's loop.
	sleepNoToken = "no token"
	// A gap: the wait asks the cloud and no world sleeps through it — the
	// line of the process backlog that brings it in.
	sleepGap = "gap"
)

// sleepWaits are the functions of production code that wait in a loop
// (waitCalls in a loop, or a poll ticker, a wait line, sleepShowing):
// "path<TAB>function" → the test of the machine sleeping that runs through
// it, or why there is none. A new wait fails the test until it is named
// here, and so does a line whose function no longer waits or whose test is
// gone (CLAUDE.md §6, axes 2 and 3; process backlog 1.6, priority 1: the
// environment of the Grok run of 1.5.0-rc.1 slept in a Kafka create).
var sleepWaits = map[string]struct{ test, why, item string }{
	"internal/config/profile.go\tlockProfiles":             {why: sleepNoToken},
	"internal/journal/journal.go\tlock":                    {why: sleepNoToken},
	"internal/state/lock.go\tFileLock":                     {why: sleepNoToken},
	"internal/state/lock.go\tLockWait":                     {why: sleepNoToken},
	"internal/httpretry/httpretry.go\tTransport.RoundTrip": {why: sleepNoToken},
	"internal/mcp/server.go\tServer.RunContext":            {test: "TestStdio_CallSurvivesTheProcessStopped"},
	"internal/mk8s/kubeconfig.go\tFetchOIDCToken":          {why: sleepNoToken}, // the repeat of one exchange of the token itself
	"internal/tools/userkeys.go\tappendUserKeys":           {why: sleepNoToken},
	"internal/deployplan/apply.go\twaitForSSHPort":         {why: sleepNoToken},
	"internal/mcp/http.go\tHTTPHandler.postSSE":            {why: sleepNoToken}, // the keep-alive of a server stream
	"internal/mcp/http.go\tHTTPHandler.stream":             {why: sleepNoToken},
	"internal/engine/deploy.go\tEngine.waitForSSH":         {why: sleepNoToken},
	"internal/engine/deploy.go\tEngine.prepareDocker":      {why: sleepNoToken},

	"internal/engine/deploy.go\tEngine.provisionVM":             {test: "TestSleep_OneRefusedTokenAfterTheWriteDoesNotEndTheCommand"},
	"internal/engine/deploy.go\tEngine.waitBootFIP":             {test: "TestSleep_OneRefusedTokenAfterTheWriteDoesNotEndTheCommand"},
	"internal/engine/deploy.go\tEngine.settledBootFIP":          {test: "TestSleep_OneRefusedTokenAfterTheWriteDoesNotEndTheCommand"},
	"internal/engine/destroy.go\tEngine.Destroy":                {test: "TestSleep_OneRefusedTokenAfterTheWriteDoesNotEndTheCommand"},
	"internal/engine/destroy.go\tEngine.releaseFloatingIP":      {test: "TestSleep_OneRefusedTokenAfterTheWriteDoesNotEndTheCommand"},
	"internal/engine/destroy.go\tEngine.waitAndDeleteVM":        {test: "TestSleep_OneRefusedTokenAfterTheWriteDoesNotEndTheCommand"},
	"internal/tools/tools.go\tRegistry.provisionVM":             {test: "TestFault_OneRefusedTokenAfterTheWrite"},
	"internal/tools/opwait.go\twaitForOpJSON":                   {test: "TestWait_TakesANewTokenAfterTheSleep"},
	"internal/tools/tools_disk.go\twaitDisk":                    {test: "TestDiskWait_GoesOnAfterOneRefusedToken"},
	"internal/tools/tools_stack.go\tRegistry.handleStackStatus": {test: "TestStackStatus_GoesOnAfterOneRefusedToken"},

	// The paths of a failure or an interruption.
	"internal/engine/deploy.go\tassignFloatingIP":      {test: "TestSleep_OneRefusedTokenOnThePathsOfFailure"},
	"internal/engine/deploy.go\tEngine.findBootFIP":    {test: "TestSleep_OneRefusedTokenOnThePathsOfFailure"},
	"internal/engine/deploy.go\tEngine.releaseBootFIP": {test: "TestSleep_OneRefusedTokenOnThePathsOfFailure"},
	"internal/engine/deploy.go\tEngine.waitVMGone":     {test: "TestSleep_OneRefusedTokenOnThePathsOfFailure"},
	"internal/deployplan/apply.go\tfinishApply":        {test: "TestApply_VMWaitGoesOnAfterOneRefusedToken"},
}

// sleepHelpers are the functions a wait of evo calls to wait: a poll
// ticker, a wait line, the sleep of waitForOpJSON.
var sleepHelpers = map[string]bool{"newPollTicker": true, "NewWaitLine": true, "VMWaitLine": true, "sleepShowing": true}

// waitsInALoop reports whether fd waits in a loop: a wait call in the body
// of a for, or a for and a call of a ticker or a sleepHelper anywhere.
func waitsInALoop(fd *ast.FuncDecl) bool {
	loop, inLoop, helper := false, false, false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if l, ok := n.(*ast.ForStmt); ok {
			loop = true
			ast.Inspect(l.Body, func(m ast.Node) bool {
				if f, ok := callFun(m).(*ast.SelectorExpr); ok {
					if id, ok := f.X.(*ast.Ident); ok && id.Name == "time" && waitCalls[f.Sel.Name] {
						inLoop = true
					}
				}
				return true
			})
		}
		switch f := callFun(n).(type) {
		case *ast.SelectorExpr:
			helper = helper || sleepHelpers[f.Sel.Name] || f.Sel.Name == "NewTicker"
		case *ast.Ident:
			helper = helper || sleepHelpers[f.Name]
		}
		return true
	})
	return inLoop || loop && helper
}

var testFunc = regexp.MustCompile(`(?m)^func (Test\w+)\(`)

func TestWaits_SleepThroughIsTested(t *testing.T) {
	root := filepath.Join("..", "..")
	waits := map[string]bool{}
	tests := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(path, "_test.go") {
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range testFunc.FindAllSubmatch(src, -1) {
				tests[string(m[1])] = true
			}
			return nil
		}
		if !strings.HasPrefix(rel, "internal/") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil && !sleepHelpers[fd.Name.Name] && waitsInALoop(fd) {
				waits[rel+"\t"+funcName(fd)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(waits))
	for k := range waits {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, ok := sleepWaits[k]
		name := strings.Replace(k, "\t", ": ", 1)
		switch {
		case !ok:
			t.Errorf("%s waits in a loop and no world of the machine sleeping goes through it: name its test in sleepWaits (a token the service refuses once after the sleep does not end the wait), or declare why there is none", name)
		case w.test != "" && !tests[w.test]:
			t.Errorf("sleepWaits: %s names the test %s, which no longer exists", name, w.test)
		case w.test == "" && w.why == "":
			t.Errorf("sleepWaits: %s has neither a test nor a reason", name)
		case w.why == sleepGap && w.item == "":
			t.Errorf("sleepWaits: %s is a gap without its work item", name)
		}
	}
	for k := range sleepWaits {
		if !waits[k] {
			t.Errorf("sleepWaits names %q, which no longer waits in a loop: remove the line", k)
		}
	}
}
