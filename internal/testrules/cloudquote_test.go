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
	"internal/engine/deploy.go|Engine.awaitCreated":       1,
	"internal/engine/deploy.go|Engine.provisionVM":        1,
	"internal/engine/deploy.go|LostAddressError":          1,
	"internal/tools/tools_vm.go|Registry.finishVMPower":   1,
	"internal/tools/tools_vm.go|Registry.finishVMResize":  1,
	"internal/tools/tools_vm.go|Registry.planVMPower":     4,
	"internal/tools/tools_vm.go|Registry.planVMResize":    3,
	"internal/tools/tools_vm.go|Registry.vmPowerRefused":  1,
	"internal/tools/tools_vm.go|Registry.vmResizeRefused": 1,
}

// unquotedCloudSteps: the same for the text of a step of progress —
// Printer.StepDone and Printer.Warning (owner's decision 14.11: the steps
// later than the errors). The list only shrinks. Closes by 1.10.
var unquotedCloudSteps = map[string]int{
	"internal/engine/deploy.go|Engine.settledBootFIP":   2,
	"internal/engine/destroy.go|Engine.waitAndDeleteVM": 1,
	"internal/tools/tools.go|Registry.provisionVM":      2,
}

// unquotedCloudLocals are the places the detector found when it learned to
// follow a status, a state or an id of the cloud through a local variable
// (cloudLocals; «Риск», conclusion 312-2): errors and texts here,
// steps of progress in unquotedCloudLocalSteps. The list only shrinks: a
// place fixed by quoteCloud leaves it in the same PR. Its fixes are the
// development's, by this list: A — tools_disk, tools_nlb, tools_snat,
// engine/deploy.go, engine/destroy.go; B — tools_backup, tools_pg;
// C — tools_deploy, made.go, tools_stack, opanswer.go,
// engine/create_preview.go, delete_preview.go, tools.go. Closes by 1.9
// (owner's decision 17.11: before the tag, not a condition of rc.1).
var unquotedCloudLocals = map[string]int{
	"internal/engine/create_preview.go|ShowCreatePreview":           1,
	"internal/engine/deploy.go|Engine.provisionVM":                  1,
	"internal/tools/delete_preview.go|Registry.deletePlan":          1,
	"internal/tools/made.go|madeAnswer":                             10,
	"internal/tools/opanswer.go|Registry.addResourceStatus":         1,
	"internal/tools/tools.go|destroyTargetPreview":                  1,
	"internal/tools/tools_backup.go|Registry.backupRestored":        1,
	"internal/tools/tools_backup.go|Registry.planBackupCreate":      1,
	"internal/tools/tools_backup.go|backupOfTheName":                1,
	"internal/tools/tools_deploy.go|Registry.applyDeployPlan":       3,
	"internal/tools/tools_deploy.go|Registry.planReleaseFloatingIP": 2,
	"internal/tools/tools_deploy.go|appliedPlan":                    4,
	"internal/tools/tools_disk.go|Registry.planDiskCreate":          2,
	"internal/tools/tools_disk.go|diskAttachedError":                4,
	"internal/tools/tools_disk.go|diskCreated":                      5,
	"internal/tools/tools_nlb.go|Registry.planLbCreate":             1,
	"internal/tools/tools_nlb.go|Registry.planTgCreate":             2,
	"internal/tools/tools_pg.go|Registry.planPgCreate":              1,
	"internal/tools/tools_snat.go|Registry.planSnatCreate":          2,
	"internal/tools/tools_stack.go|Registry.advance":                2,
}

// unquotedCloudLocalSteps: the same for the steps of progress. The list only
// shrinks. Closes by 1.9 (owner's decision 17.11).
var unquotedCloudLocalSteps = map[string]int{
	"internal/engine/destroy.go|Engine.waitAndDeleteVM": 1,
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
	for k, n := range unquotedCloudLocals {
		allowed[k] += n
	}
	for k, n := range unquotedCloudLocalSteps {
		allowed[stepKey+k] += n
	}
	for k, n := range got {
		if n > allowed[k] {
			t.Errorf("%s: %d status, state or id of the cloud printed bare (%d allowed) — wrap the value in quoteCloud or print it with %%q", k, n, allowed[k])
		}
	}
	for k, n := range allowed {
		if got[k] < n {
			list := "unquotedCloudWords and unquotedCloudLocals"
			if strings.HasPrefix(k, stepKey) {
				list = "unquotedCloudSteps and unquotedCloudLocalSteps"
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
		// A text glued with + is a format too: "VM entered state " +
		// cur.Status reached ApplyResult bare (risk conclusion 301-1, #2140).
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			b, ok := n.(*ast.BinaryExpr)
			if !ok || b.Op != token.ADD {
				return true
			}
			if c := bareInConcat(concatOperands(b)); c > 0 {
				out[rel+"|"+name] += c
			}
			return false // the operands of a chain are counted once
		})
		cloud := cloudLocals(fn.Body)
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
			out[k] += bareCloudWords(format, args, cloud)
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

// bareCloudWords counts the %s and %v of a format that are not quoteCloud(…)
// and either follow a cloud word or print a local variable that holds a
// status, a state or an id the cloud gave (cloudLocals): the words before
// the verb do not tell what a variable holds («does not find %s» with the
// resource id of the operation, «Риск», 312-2).
func bareCloudWords(format string, args []ast.Expr, cloud map[string]bool) int {
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
		if (v == 's' || v == 'v') && (arg >= len(args) || !quoted(args[arg])) &&
			(cloudWordBefore.MatchString(format[:loc[0]]) || arg < len(args) && isCloudLocal(args[arg], cloud)) {
			n++
		}
		arg++
	}
	return n
}

// cloudField: a field or a key of an answer that holds a status, a state
// or an id the cloud gave. The project and the key are the configuration's:
// the person gives them (as "--project-id %s" above).
var cloudField = regexp.MustCompile(`(?i)^(id|status|state|phase|resource_?id|[a-z_]*_id|[a-z]*ID)$`)

// configField: a field or a key of the configuration, not the cloud's.
var configField = regexp.MustCompile(`(?i)^(project_?id|key_?id)$`)

// cloudLocals are the local variables of a function body assigned a status,
// a state or an id of the cloud: a field (x.Status, x.ResourceID) or a key
// of an answer (m["resource_id"], m["status"].(string)) — the variable keeps
// what the cloud wrote, whatever its name.
func cloudLocals(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		a, ok := n.(*ast.AssignStmt)
		if !ok || len(a.Rhs) == 0 {
			return true
		}
		// v := src, and v, ok := src.(T) / m[k]
		if id, ok := a.Lhs[0].(*ast.Ident); ok && cloudSource(a.Rhs[0]) {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// cloudSource: an expression that reads a status, a state or an id of an
// answer of the cloud.
func cloudSource(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.ParenExpr:
		return cloudSource(v.X)
	case *ast.TypeAssertExpr:
		return cloudSource(v.X)
	case *ast.SelectorExpr:
		return cloudField.MatchString(v.Sel.Name) && !configField.MatchString(v.Sel.Name)
	case *ast.IndexExpr:
		if lit, ok := v.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			k := strings.Trim(lit.Value, "\"`")
			return cloudField.MatchString(k) && !configField.MatchString(k)
		}
	}
	return false
}

// isCloudLocal: the argument is a local variable of cloudLocals.
func isCloudLocal(e ast.Expr, cloud map[string]bool) bool {
	id, ok := e.(*ast.Ident)
	return ok && cloud[id.Name]
}

// concatOperands flattens a chain a + b + c into its operands.
func concatOperands(e ast.Expr) []ast.Expr {
	switch v := e.(type) {
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return append(concatOperands(v.X), concatOperands(v.Y)...)
		}
	case *ast.ParenExpr:
		return concatOperands(v.X)
	}
	return []ast.Expr{e}
}

// bareInConcat counts the operands of a glued text that are a status, a
// state or an id of the cloud and not quoteCloud(…): an operand right
// after a literal that ends with a cloud word, or a field named Status or
// State whatever comes before it. A chain with no string literal is no
// text.
func bareInConcat(ops []ast.Expr) int {
	text := false
	for _, op := range ops {
		if _, ok := literal(op); ok {
			text = true
		}
	}
	if !text {
		return 0
	}
	n := 0
	for i, op := range ops {
		if _, ok := literal(op); ok || quoted(op) {
			continue
		}
		after := false
		if i > 0 {
			if prev, ok := literal(ops[i-1]); ok && cloudWordBefore.MatchString(prev) {
				after = true
			}
		}
		if sel, ok := op.(*ast.SelectorExpr); after || ok && (sel.Sel.Name == "Status" || sel.Sel.Name == "State") {
			n++
		}
	}
	return n
}

// quoted: the argument is quoteCloud(…), or strconv.Quote(…) where a
// package has no quoteCloud — it escapes as %q does.
func quoted(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name == "quoteCloud"
	case *ast.SelectorExpr:
		pkg, ok := f.X.(*ast.Ident)
		return ok && pkg.Name == "strconv" && f.Sel.Name == "Quote"
	}
	return false
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
		if got := bareCloudWords(c.format, c.args, nil); got != c.want {
			t.Errorf("%q: %d bare, want %d", c.format, got, c.want)
		}
	}
}

// The detector of glued text: a value after a cloud word, a Status or State
// field — bare; quoteCloud, a literal, a chain with no text — not.
func TestCloudWords_DetectorOfConcat(t *testing.T) {
	for src, want := range map[string]int{
		`"VM entered state " + cur.Status + "; retry"`:                    1,
		`"VM entered state " + quoteCloud(cur.Status) + "; retry"`:        0,
		`"it is " + vm.Status + ": start it"`:                             1,
		`"(id " + id + ", status " + st + ")"`:                            2,
		`"VM " + name + " is ready"`:                                      0,
		`a + b`:                                                           0,
		`"disk " + quoteCloud(d.ID) + " in state " + quoteCloud(d.State)`: 0,
	} {
		e, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		if got := bareInConcat(concatOperands(e)); got != want {
			t.Errorf("%s: %d bare, want %d", src, got, want)
		}
	}
}

// The detector follows a status, a state or an id of the cloud through a
// local variable, whatever words come before its verb («Риск», 312-2: the
// resource id of an operation, printed after «does not find»); a value of
// the configuration and a quoted one are not bare.
func TestCloudWords_DetectorFollowsLocals(t *testing.T) {
	for src, want := range map[string]int{
		`id, _ := m["resource_id"].(string); _ = fmt.Sprintf("%s does not find %s", r, id)`:             1,
		`id, _ := m["resource_id"].(string); _ = fmt.Sprintf("%s does not find %s", r, quoteCloud(id))`: 0,
		`st := vm.Status; _ = fmt.Sprintf("it is %s now", st)`:                                          1,
		`v := op.ResourceID; _ = fmt.Errorf("made %v", v)`:                                              1,
		`p := cfg.ProjectID; _ = fmt.Sprintf("in %s", p)`:                                               0,
		`n := vm.Name; _ = fmt.Sprintf("VM %s", n)`:                                                     0,
		`id := stringArg(args, "vm_id"); _ = fmt.Sprintf("VM %s", id)`:                                  0,
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "p.go", "package p\nfunc f() {"+src+"}", 0)
		if err != nil {
			t.Fatal(err)
		}
		body := f.Decls[0].(*ast.FuncDecl).Body
		cloud, got := cloudLocals(body), 0
		ast.Inspect(body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if format, args, ok := formatCall(call); ok {
					got += bareCloudWords(format, args, cloud)
				}
			}
			return true
		})
		if got != want {
			t.Errorf("%s: %d bare, want %d", src, got, want)
		}
	}
}
