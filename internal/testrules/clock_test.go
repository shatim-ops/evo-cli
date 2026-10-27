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
	"strings"
	"testing"
)

// The reasons a measure of the wall clock in production code gives.
const (
	// A wait, a deadline, an interval between reports, a TTL: the wall
	// clock is the point of it.
	wallWait = "wait or deadline"
	// A line of the server's log, not an answer.
	wallLog = "log"
	// A duration evo puts into its output — an answer, a progress line, a
	// line of stderr: a value of the time of the call, which goes through
	// internal/clock (CLAUDE.md §5), so that two runs of one operation
	// answer alike. Measured with time.Since, the CLI and MCP runs of one
	// vm create printed «готова: … 0с» and «… 1с» (#380, Windows: the
	// duration is rounded to the second). The fix is «разработка»'s
	// (clock.Since); the list only shrinks.
	wallInOutput = "duration in the output: through internal/clock"
)

// wallClockReads are the functions of production code that measure the
// wall clock — time.Since or time.Now().Sub — "path<TAB>function" → how
// many times and why. A new one, or one more in a function, fails the
// test, and so does a line whose count is no longer there: a duration in
// the output goes through internal/clock, a wait or a log is declared
// here with its reason.
var wallClockReads = map[string]struct {
	n   int
	why string
}{
	"internal/deployplan/apply.go\tApplyWithTimeout": {2, wallInOutput}, // elapsed_sec of the answer
	"internal/deployplan/apply.go\tfinishApply":      {4, wallInOutput}, // elapsed_sec of the answer
	"internal/engine/deploy.go\tassignFloatingIP":    {1, wallInOutput}, // progress lines
	// The interval between reports and the slow-SSH note are waits; the
	// report itself prints the duration.
	"internal/engine/deploy.go\tEngine.waitForSSH":       {3, wallInOutput},
	"internal/engine/deploy.go\tEngine.prepareDocker":    {1, wallInOutput}, // retry line
	"internal/engine/destroy.go\tEngine.waitAndDeleteVM": {2, wallInOutput}, // interval, and its report
	"internal/engine/waitline.go\tWaitLine.Elapsed":      {1, wallInOutput}, // «ВМ … активна (Nс)»
	"internal/mcp/http.go\tHTTPHandler.dispatch":         {1, wallLog},      // duration_ms of the log
	"internal/mcp/http.go\tHTTPHandler.touchSession":     {1, wallWait},     // TTL of the session
	"internal/tools/tools.go\tRegistry.provisionVM":      {1, wallInOutput}, // «ВМ … готова: …, Nс» (#380)
	"internal/tools/tools_auth.go\tprobe":                {1, wallInOutput}, // latency of doctor
	"internal/verify/verify.go\tprobe":                   {2, wallInOutput}, // latency_ms of the answer
	"internal/verify/verify.go\tget":                     {1, wallInOutput}, // latency_ms of the answer
}

// A value of the time that goes into an answer goes through internal/clock
// (CLAUDE.md §5); internal/clock covered the names (of a VM, a plan, a
// floating IP), not the durations, and TestNoUnguardedWallClockBounds reads
// only the tests. In production code of internal and cmd, every
// time.Since and time.Now().Sub is declared in wallClockReads.
func TestWallClockReadsDeclared(t *testing.T) {
	root := filepath.Join("..", "..")
	got := map[string]int{}
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
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				key := filepath.ToSlash(rel) + "\t" + funcName(fd)
				ast.Inspect(fd, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok && readsWallClock(call) {
						got[key]++
						if wallClockReads[key].n == 0 {
							t.Errorf("%s:%d: %s measures the wall clock — a duration in the output through internal/clock; a wait or a log declared in wallClockReads (wallWait, wallLog)",
								filepath.ToSlash(rel), fset.Position(call.Pos()).Line, funcName(fd))
						}
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
	for key, want := range wallClockReads {
		if n := got[key]; n != want.n {
			t.Errorf("wallClockReads: %q (%s) measures the wall clock %d times, the line says %d — update or remove the line", key, want.why, n, want.n)
		}
	}
}

// readsWallClock: time.Since(…) or time.Now().Sub(…).
func readsWallClock(call *ast.CallExpr) bool {
	if isSel(call.Fun, "time", "Since") {
		return true
	}
	s, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != "Sub" {
		return false
	}
	now, ok := s.X.(*ast.CallExpr)
	return ok && isSel(now.Fun, "time", "Now")
}

// funcName is a function's name, with its receiver's type for a method.
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	typ := fd.Recv.List[0].Type
	if st, ok := typ.(*ast.StarExpr); ok {
		typ = st.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}
