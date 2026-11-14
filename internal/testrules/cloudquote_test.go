// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// cloudWordBefore: the text of a format right before a verb whose value is
// a status, a state or an id of the cloud — "status %s", "in state %s",
// "(id %s"; not "--project-id %s", a value of the configuration.
var cloudWordBefore = regexp.MustCompile(`(?i)(^|[^-\w])(status|state|id):? $`)

// formatVerb: a verb of a format string, %% excluded.
var formatVerb = regexp.MustCompile(`%[-+# 0]*(\*|\d+)?(\.(\*|\d+))?[a-zA-Z%]`)

// stepKey marks a key of got that is a step of progress, not an error.
const stepKey = "step: "

// unquotedCloudWords are the places that still print a status, a state or
// an id of the cloud bare in a text of evo: "file|function" → how many.
// The list only shrinks (plan 1.9, «Инвариант quoteCloud: места для
// убывающего списка», risk conclusions 232 and 235): a place fixed by
// quoteCloud leaves it in the same PR, a new place fails the test. Its
// fixes are the development's, by this list. These are the errors and
// refusals: Closes by 1.9 (owner's decision 14.11, before rc.1).
var unquotedCloudWords = map[string]int{
	"internal/deployplan/apply.go|ApplyWithTimeout":            1,
	"internal/deployplan/apply.go|ForeignVMError.Error":        2,
	"internal/engine/delete_wayout.go|DeleteWayOut":            2,
	"internal/engine/delete_wayout.go|FailedDeleteNote":        1,
	"internal/engine/deploy.go|Engine.awaitCreated":            1,
	"internal/engine/deploy.go|Engine.provisionVM":             1,
	"internal/engine/deploy.go|LostAddressError":               1,
	"internal/engine/destroy.go|Engine.RefusedVMDelete":        1,
	"internal/engine/foreign_vm.go|foreignDeployVM":            2,
	"internal/tools/delete_outcome.go|deleteRefusal":           1,
	"internal/tools/delete_preview.go|withStatus":              1,
	"internal/tools/intent.go|Registry.checkByList":            1,
	"internal/tools/namecheck.go|lostCreate":                   2,
	"internal/tools/namecheck.go|nameTakenResult":              2,
	"internal/tools/own_vm.go|foreignVMResult":                 2,
	"internal/tools/own_vm.go|recordedVMResult":                2,
	"internal/tools/tools.go|Registry.provisionVM":             1,
	"internal/tools/tools_deploy.go|Registry.applyDeployPlan":  1,
	"internal/tools/tools_disk.go|settledDisk":                 1,
	"internal/tools/tools_disk.go|waitDiskAvailable":           1,
	"internal/tools/tools_image.go|Registry.planFipAttach":     1,
	"internal/tools/tools_sg.go|Registry.sgRuleDeleted":        1,
	"internal/tools/tools_sg_create.go|Registry.sgRuleCreated": 1,
	"internal/tools/tools_stack.go|Registry.advance":           1,
	"internal/tools/tools_stack.go|stepMissing":                1,
	"internal/tools/tools_vm.go|Registry.finishVMPower":        1,
	"internal/tools/tools_vm.go|Registry.finishVMResize":       1,
	"internal/tools/tools_vm.go|Registry.planVMPower":          4,
	"internal/tools/tools_vm.go|Registry.planVMResize":         3,
	"internal/tools/tools_vm.go|Registry.vmPowerRefused":       1,
	"internal/tools/tools_vm.go|Registry.vmResizeRefused":      1,
}

// unquotedCloudSteps: the same for the text of a step of progress —
// Printer.StepDone and Printer.Warning (owner's decision 14.11: the steps
// later than the errors). The list only shrinks. Closes by 1.10.
var unquotedCloudSteps = map[string]int{
	"internal/engine/deploy.go|Engine.settledBootFIP":   2,
	"internal/engine/destroy.go|Engine.waitAndDeleteVM": 1,
	"internal/tools/tools.go|Registry.provisionVM":      2,
}

// A value that came from the cloud — a status, a state, an id — goes into
// a text of evo (an error, a warning, a hint, a step) only through
// quoteCloud or %q: bare, a newline or an escape sequence in it reads as
// evo's own words to a person and to a model (finding of Астра #1802–#1804,
// #1762, #1763; risk conclusions 232, 235). The check is on the format
// literal of i18n.Tf, i18n.T inside fmt, fmt.Errorf, fmt.Sprintf and
// fmt.Fprintf: a %s or %v right after "status", "state" or "id" must be %q
// or have quoteCloud(…) as its argument.
func TestCloudWords_QuotedInEvoText(t *testing.T) {
	got := map[string]int{}
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Join("..", ".."), path)
		for k, n := range unquotedIn(t, path, filepath.ToSlash(rel)) {
			got[k] += n
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CLOUDQUOTE_PRINT") != "" {
		keys := slices.Sorted(func(yield func(string) bool) {
			for k := range got {
				if !yield(k) {
					return
				}
			}
		})
		for _, k := range keys {
			fmt.Printf("\t%q: %d,\n", k, got[k])
		}
	}
	allowed := map[string]int{}
	for k, n := range unquotedCloudWords {
		allowed[k] = n
	}
	for k, n := range unquotedCloudSteps {
		allowed[stepKey+k] = n
	}
	for k, n := range got {
		if n > allowed[k] {
			t.Errorf("%s: %d status, state or id of the cloud printed bare (%d allowed) — wrap the value in quoteCloud or print it with %%q", k, n, allowed[k])
		}
	}
	for k, n := range allowed {
		if got[k] < n {
			list := "unquotedCloudWords"
			if strings.HasPrefix(k, stepKey) {
				list = "unquotedCloudSteps"
			}
			t.Errorf("%s lists %d for %s, and %d are left — lower the line (the list only shrinks)", list, n, strings.TrimPrefix(k, stepKey), got[k])
		}
	}
}

// unquotedIn counts, per "file|function", the bare cloud words of a file.
func unquotedIn(t *testing.T, path, rel string) map[string]int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) == 1 {
			name = recvName(fn.Recv.List[0].Type) + "." + name
		}
		// The format calls that are the text of a step of progress:
		// Printer.StepDone and Printer.Warning.
		step := map[*ast.CallExpr]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "StepDone" || sel.Sel.Name == "Warning") {
				for _, a := range call.Args {
					ast.Inspect(a, func(m ast.Node) bool {
						if c, ok := m.(*ast.CallExpr); ok {
							step[c] = true
						}
						return true
					})
				}
			}
			return true
		})
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			format, args, ok := formatCall(call)
			if !ok {
				return true
			}
			k := rel + "|" + name
			if step[call] {
				k = stepKey + k
			}
			out[k] += bareCloudWords(format, args)
			return true
		})
	}
	for k, n := range out {
		if n == 0 {
			delete(out, k)
		}
	}
	return out
}

func recvName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.StarExpr:
		return recvName(v.X)
	case *ast.IndexExpr:
		return recvName(v.X)
	case *ast.Ident:
		return v.Name
	}
	return "?"
}

// formatCall: the English format literal of a call and the arguments of
// its verbs. i18n.Tf(en, ru, args…); fmt.Errorf, fmt.Sprintf(format,
// args…) with a literal or i18n.T(en, ru) as the format; fmt.Fprintf(w,
// format, args…).
func formatCall(call *ast.CallExpr) (string, []ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", nil, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", nil, false
	}
	switch pkg.Name + "." + sel.Sel.Name {
	case "i18n.Tf":
		if len(call.Args) < 2 {
			return "", nil, false
		}
		s, ok := literal(call.Args[0])
		return s, call.Args[2:], ok
	case "fmt.Errorf", "fmt.Sprintf":
		if len(call.Args) < 1 {
			return "", nil, false
		}
		s, ok := formatOf(call.Args[0])
		return s, call.Args[1:], ok
	case "fmt.Fprintf":
		if len(call.Args) < 2 {
			return "", nil, false
		}
		s, ok := formatOf(call.Args[1])
		return s, call.Args[2:], ok
	}
	return "", nil, false
}

// formatOf: a literal, or the English literal of i18n.T(en, ru).
func formatOf(e ast.Expr) (string, bool) {
	if c, ok := e.(*ast.CallExpr); ok {
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "i18n" && sel.Sel.Name == "T" && len(c.Args) == 2 {
				return literal(c.Args[0])
			}
		}
		return "", false
	}
	return literal(e)
}

// literal: a string literal, or a concatenation of them.
func literal(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		a, ok1 := literal(v.X)
		b, ok2 := literal(v.Y)
		return a + b, ok1 && ok2
	case *ast.ParenExpr:
		return literal(v.X)
	}
	return "", false
}

// bareCloudWords counts the %s and %v of a format that follow a cloud word
// and whose argument is not quoteCloud(…).
func bareCloudWords(format string, args []ast.Expr) int {
	n, arg := 0, 0
	for _, loc := range formatVerb.FindAllStringIndex(format, -1) {
		verb := format[loc[0]:loc[1]]
		if verb == "%%" {
			continue
		}
		if strings.Contains(verb, "*") {
			// A width or precision from the arguments: the count of
			// arguments is lost, stop rather than guess.
			return n
		}
		v := verb[len(verb)-1]
		if (v == 's' || v == 'v') && cloudWordBefore.MatchString(format[:loc[0]]) &&
			(arg >= len(args) || !quoted(args[arg])) {
			n++
		}
		arg++
	}
	return n
}

// quoted: the argument is quoteCloud(…).
func quoted(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := c.Fun.(*ast.Ident)
	return ok && id.Name == "quoteCloud"
}

// The detector itself: what counts as bare and what does not.
func TestCloudWords_Detector(t *testing.T) {
	q := &ast.CallExpr{Fun: ast.NewIdent("quoteCloud")}
	x := ast.NewIdent("x")
	for _, c := range []struct {
		format string
		args   []ast.Expr
		want   int
	}{
		{"VM %s is in state %s", []ast.Expr{x, x}, 1},
		{"VM %s is in state %s", []ast.Expr{x, q}, 0},
		{"VM %s is in state %q", []ast.Expr{x, x}, 0},
		{"(id %s, status %s)", []ast.Expr{x, x}, 2},
		{"(id %v, status: %s)", []ast.Expr{q, x}, 1},
		{"--project-id %s", []ast.Expr{x}, 0},
		{"100%% done, state %s", []ast.Expr{x}, 1},
		{"took %d tries, state %d", []ast.Expr{x, x}, 0},
		{"State %s", []ast.Expr{x}, 1},
	} {
		if got := bareCloudWords(c.format, c.args); got != c.want {
			t.Errorf("%q: %d bare, want %d", c.format, got, c.want)
		}
	}
}
