// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package testrules checks the tests of the repository against rules that
// keep them deterministic on a loaded CI runner.
package testrules

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These two elapsed comparisons run inside testing/synctest bubbles. Their
// 45-second limits are protocol values, so both keep a numerical contract
// without needing a comment justifying a wait on a real runner.
const h14Families = "mcp/TestP77H14Families >"
const h14Provision = "mcp/checkH14Provision >"

// Each entry names a comparison with elapsed wall time and its independent
// numerical contract. Symbolic waits stay tied to the production expression.
// Where the test world sets a literal wait, give its value and the fraction.
// Keep the special lower bounds, diagnostic log and virtual clock explicit.
var wallClockBounds = map[string]string{
	"cli/emptyCloud.run >":                                             "tools.MCPCallBudget/4",
	"deployplan/TestPriceVM_TimeoutFallsBack >":                        "old/2", // saved production priceTimeout
	"engine/TestDeploy_DockerPermanentRefusalNotRetried >":             "eng.Timings.DockerWaitTimeout/2",
	"engine/TestDeploy_FirstVMPollIsImmediate >":                       "eng.Timings.VMPollTimeout/2",
	"engine/TestDestroy_FirstDeletionPollIsImmediate >":                "eng.Timings.DestroyPollTimeout/2",
	"engine/TestDestroy_DeletionWaitUsesTimings >=":                    "destroyPollTimeout/2",
	"engine/TestDeployPreview_PriceTimeoutFallsBack >":                 "PreviewLookupTimeout/2",
	"engine/TestStatus_RefreshIsBounded >":                             "statusRefreshTimeout/2",
	"engine/TestFault_HTTP_CreateVM429UsesBackoffNotRetryAfter >":      "time.Hour/120",
	"engine/TestFault_HTTP_RetryAfterCancelled >":                      "time.Hour/120",
	"httpretry/TestContextStopsBackoff >":                              "tr.Backoff/2",
	"httpretry/TestClientTimeoutStopsBackoff >":                        "time.Hour/2",
	"httpretry/TestPolicyBackoffOnlyIgnoresRetryAfter >":               "maxRetryAfter/2",
	"tools/TestDeployApply_ACutFloatingIPIsLookedForAtTheWorldsPace >": "2*time.Second/2",
	"tools/TestWaitForOpJSON_StopsOnTerminal >":                        "time.Minute/2",
	"tools/TestK8sNodePoolScale_GoneWhileSettling >":                   "time.Minute/6",
	"tools/TestJournal_HeldLockDoesNotDelayTheAnswer >":                "2*time.Second/2",
	"tools/TestQA_StackStatusHonoursShortBudget >":                     "stackCrankInterval/2",
	"tools/faultRun >": "fipPollWait/2",
	"updatecheck/TestSecurity_SlowlorisBoundedByTimeout >": "10*time.Second/2",
	"verify/TestCheck_SlowServiceTimesOut >":               "10*time.Second/2",

	"state/TestLockWait_WaitsForHolder <": "100*time.Millisecond*4/5", // holder sleeps 100 ms
	"engine/slowVM <":                     "d",                        // the fake's transition
	"login/startChrome >":                 "browserCall",              // logs slow starts
	h14Families:                           "45*time.Second",           // synctest virtual clock
	h14Provision:                          "45*time.Second",           // synctest virtual clock
}

// The comment explains the production wait. The expression in the assertion
// must independently agree with its reviewed bound. A changed number with an
// unchanged explanation must fail even when the runner happens to be fast.
func TestNoUnguardedWallClockBounds(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, fail := range wallClockFindings(t, rel, src, seen) {
			t.Error(fail)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for key := range wallClockBounds {
		if !seen[key] {
			t.Errorf("wallClockBounds: %s comparison missing", key)
		}
	}
}

func wallClockFindings(t *testing.T, rel string, src []byte, seen map[string]bool) []string {
	t.Helper()
	// Most test files do not use either clock measurement.
	if !bytes.Contains(src, []byte("Since")) && !bytes.Contains(src, []byte("Sub")) {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	comments := map[int]string{}
	for _, group := range f.Comments {
		for _, c := range group.List {
			comments[fset.Position(c.Pos()).Line] = c.Text
		}
	}
	var findings []string
	pkg := filepath.ToSlash(filepath.Dir(rel))
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			cmp, ok := n.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			op, bound := elapsedComparison(cmp)
			if bound == nil {
				return true
			}
			pos := fset.Position(cmp.Pos())
			key := strings.TrimPrefix(pkg, "internal/") + "/" + funcName(fn) + " " + op.String()
			if key != h14Families && key != h14Provision && !strings.Contains(comments[pos.Line], "wall-clock:") && !strings.Contains(comments[pos.Line-1], "wall-clock:") {
				findings = append(findings, fmt.Sprintf("%s: a bound on the wall clock without a wall-clock: comment naming the production wait", pos))
			}
			want, ok := wallClockBounds[key]
			if !ok {
				findings = append(findings, fmt.Sprintf("%s: declare the production wait and its fraction for %s in wallClockBounds", pos, key))
				return true
			}
			seen[key] = true
			expr, err := parser.ParseExpr(want)
			if err != nil {
				t.Fatal(err)
			}
			if !sameWallClockBound(bound, expr) {
				findings = append(findings, fmt.Sprintf("%s: %s bound %s; want %s", pos, key, types.ExprString(bound), want))
			}
			return true
		})
	}
	return findings
}

var reverseComparison = map[token.Token]token.Token{
	token.LSS: token.GTR, token.LEQ: token.GEQ, token.GTR: token.LSS, token.GEQ: token.LEQ,
}

func elapsedComparison(cmp *ast.BinaryExpr) (token.Token, ast.Expr) {
	op, ok := reverseComparison[cmp.Op]
	if !ok {
		return token.ILLEGAL, nil
	}
	if elapsedValue(cmp.X) {
		return cmp.Op, cmp.Y
	}
	if elapsedValue(cmp.Y) {
		return op, cmp.X
	}
	return token.ILLEGAL, nil
}

// Follow a local declaration to recognize a measurement on an earlier line
// and the difference between the durations of two runs. Obj is scoped by
// the Go parser, so unrelated variables with the same name stay distinct.
func elapsedValue(e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		return readsWallClock(x) && !isSel(x.Fun, "time", "Until")
	case *ast.Ident:
		if x.Obj != nil {
			switch decl := x.Obj.Decl.(type) {
			case *ast.AssignStmt:
				for i, name := range decl.Lhs {
					id, ok := name.(*ast.Ident)
					if ok && id.Obj == x.Obj && len(decl.Rhs) == len(decl.Lhs) {
						return elapsedValue(decl.Rhs[i])
					}
				}
			case *ast.ValueSpec:
				for i, name := range decl.Names {
					if name.Obj == x.Obj && len(decl.Values) == len(decl.Names) {
						return elapsedValue(decl.Values[i])
					}
				}
			}
		}
	case *ast.BinaryExpr:
		return x.Op == token.SUB && elapsedValue(x.X) && elapsedValue(x.Y)
	}
	return false
}

func sameWallClockBound(actual, want ast.Expr) bool {
	actual, want = ast.Unparen(actual), ast.Unparen(want)
	if types.ExprString(actual) == types.ExprString(want) {
		return true
	}
	// A named constant can carry a fraction of a production wait, such as
	// emptyCloudWait = tools.MCPCallBudget / 4. Check the declaration too.
	if id, ok := actual.(*ast.Ident); ok && id.Obj != nil && id.Obj.Kind == ast.Con {
		if decl, ok := id.Obj.Decl.(*ast.ValueSpec); ok {
			for i, name := range decl.Names {
				if name.Obj == id.Obj && len(decl.Values) == len(decl.Names) {
					return sameWallClockBound(decl.Values[i], want)
				}
			}
		}
	}
	// Accept equivalent literal durations, e.g. 30*time.Second and time.Minute/2.
	a, knownA := durationOf(actual, nil)
	b, knownB := durationOf(want, nil)
	return knownA && knownB && a == b
}

// Both limits are checked on the same source and explanation. A missing
// explanation still fails for a real clock, even with the correct number.
func TestWallClockBound_RejectsChangedNumber(t *testing.T) {
	const reason = "// wall-clock: half of the production refresh timeout."
	for _, tc := range []struct{ name, comment, bound, finding string }{
		{"reviewed fraction", reason, "statusRefreshTimeout/2", ""},
		{"too long", reason, "1000*time.Hour", "want statusRefreshTimeout/2"},
		{"wrong fraction", reason, "statusRefreshTimeout/8", "want statusRefreshTimeout/2"},
		{"no real-clock comment", "", "statusRefreshTimeout/2", "without a wall-clock:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package engine\nfunc TestStatus_RefreshIsBounded() {\nd := time.Since(start)\n" +
				tc.comment + "\nif d > " + tc.bound + " {}\n}"
			checkFindings(t, "internal/engine/status_test.go", []byte(src), tc.finding)
		})
	}
}

// Read the actual continuation test. Its separate assignment and unannotated
// comparison must still enforce the H14 limit on the virtual clock.
func TestWallClockBound_VirtualH14StillChecksTheNumber(t *testing.T) {
	const rel = "internal/mcp/wait_continue_test.go"
	src, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	good := []byte("elapsed > 45*time.Second")
	if bytes.Count(src, good) != 1 {
		t.Fatal("expected one elapsed comparison in the H14 provision fixture")
	}
	checkFindings(t, rel, src, "")
	bad := bytes.Replace(src, good, []byte("elapsed > 4500*time.Second"), 1)
	checkFindings(t, rel, bad, "bound 4500 * time.Second; want 45*time.Second")
}

func checkFindings(t *testing.T, rel string, src []byte, want string) {
	t.Helper()
	findings := wallClockFindings(t, rel, src, map[string]bool{})
	if want == "" && len(findings) == 0 {
		return
	}
	if want != "" && len(findings) == 1 && strings.Contains(findings[0], want) {
		return
	}
	t.Fatalf("findings %q; want %q", findings, want)
}
