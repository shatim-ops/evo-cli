// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
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

// unquotedCloudSteps are the places that still print a status, a state or
// an id of the cloud bare in the text of a step of progress —
// Printer.StepDone and Printer.Warning: "file|function" → how many. The
// list only shrinks (plan 1.9, «Инвариант quoteCloud: места для убывающего
// списка», risk conclusions 232 and 235): a place fixed by quoteCloud
// leaves it in the same PR, a new place fails the test. The errors and
// refusals closed in 1.9 (owner's decision 14.11, before rc.1). Closes by
// 1.10.
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
	"internal/engine/create_preview.go|ShowCreatePreview":      1,
	"internal/engine/deploy.go|Engine.provisionVM":             1,
	"internal/tools/delete_preview.go|Registry.deletePlan":     1,
	"internal/tools/tools.go|destroyTargetPreview":             1,
	"internal/tools/tools_backup.go|Registry.backupRestored":   1,
	"internal/tools/tools_backup.go|Registry.planBackupCreate": 1,
	"internal/tools/tools_backup.go|backupOfTheName":           1,
	"internal/tools/tools_disk.go|Registry.planDiskCreate":     2,
	"internal/tools/tools_disk.go|diskAttachedError":           4,
	"internal/tools/tools_disk.go|diskCreated":                 5,
	"internal/tools/tools_nlb.go|Registry.planLbCreate":        1,
	"internal/tools/tools_nlb.go|Registry.planTgCreate":        2,
	"internal/tools/tools_pg.go|Registry.planPgCreate":         1,
	"internal/tools/tools_snat.go|Registry.planSnatCreate":     2,
}

// unquotedCloudLocalSteps: the same for the steps of progress. The list only
// shrinks. Closes by 1.9 (owner's decision 17.11).
var unquotedCloudLocalSteps = map[string]int{
	"internal/engine/destroy.go|Engine.waitAndDeleteVM": 1,
}

// unquotedCloudFlows are the places the detector found when it learned
// to follow a value of the cloud by its declaration and in the order of
// the code, into a field of a literal, out of a helper that returns it,
// and into the texts fmt.Sprint, strings.Join, errors.New and + build
// (Астра #2236, #2237; cloudFlow): errors and texts here, steps of
// progress in unquotedCloudFlowSteps. The list only shrinks: a place
// fixed by quoteCloud leaves it in the same PR. Closes by 1.10 (owner's
// decision 17.11: the places of the detector made before the tag 1.9
// close in 1.10).
var unquotedCloudFlows = map[string]int{
	"internal/billing/client.go|Client.consumptionError":          1,
	"internal/cli/configure.go|chooseProject":                     1,
	"internal/cli/project.go|resolveProjectArg":                   1,
	"internal/deployplan/apply.go|ApplyWithTimeout":               2,
	"internal/deployplan/apply.go|finishApply":                    1,
	"internal/engine/create_preview.go|ShowCreatePreview":         1,
	"internal/engine/create_preview.go|flavorNotFound":            1,
	"internal/engine/deploy.go|Engine.Deploy":                     1,
	"internal/engine/deploy.go|Engine.awaitCreated":               5,
	"internal/engine/deploy.go|Engine.provisionVM":                12,
	"internal/engine/deploy.go|Engine.settledBootFIP":             1,
	"internal/engine/deploy.go|Engine.waitBootFIP":                1,
	"internal/engine/deploy.go|Engine.waitVMGone":                 1,
	"internal/engine/destroy.go|Engine.Destroy":                   1,
	"internal/engine/init.go|Engine.Init":                         2,
	"internal/engine/maker.go|Engine.NotVisibleToThisKey":         1,
	"internal/engine/maker.go|makerName":                          1,
	"internal/engine/own_record.go|AdoptedVM":                     2,
	"internal/engine/own_record.go|uncheckedRecord":               1,
	"internal/engine/preview.go|refText":                          1,
	"internal/engine/preview.go|rulePreview":                      3,
	"internal/engine/vm_stopped.go|Engine.vmStopped":              3,
	"internal/engine/vm_stopped.go|inTransit":                     2,
	"internal/mk8s/addons.go|Client.CreateClusterAddon":           1,
	"internal/sgrules/labels.go|CloseLabeled":                     1,
	"internal/stack/store.go|FindResourcefulJobs":                 1,
	"internal/tools/delete_outcome.go|deleteRefusal":              3,
	"internal/tools/intent.go|Registry.checkByID":                 1,
	"internal/tools/intent_targets.go|groupTarget":                1,
	"internal/tools/intent_targets.go|vmTarget":                   1,
	"internal/tools/made_repeat.go|Registry.repeatOfMade":         1,
	"internal/tools/provision_adopt.go|creatingVM":                1,
	"internal/tools/tools.go|Registry.handleDestroy":              2,
	"internal/tools/tools.go|Registry.planDestroyTargeted":        3,
	"internal/tools/tools.go|Registry.planProvision":              2,
	"internal/tools/tools.go|Registry.provisionVM":                12,
	"internal/tools/tools.go|destroyDeleting":                     3,
	"internal/tools/tools.go|destroyTargetPreview":                5,
	"internal/tools/tools.go|destroyUnconfirmed":                  6,
	"internal/tools/tools.go|fipLeftAnswer":                       1,
	"internal/tools/tools_backup.go|Registry.planBackupCreate":    2,
	"internal/tools/tools_backup.go|Registry.planBackupRestore":   1,
	"internal/tools/tools_ca.go|Registry.planCaStartStop":         1,
	"internal/tools/tools_ca.go|caStartStopAnswer":                1,
	"internal/tools/tools_certmanager.go|certNameTaken":           1,
	"internal/tools/tools_deploy.go|Registry.applyDeployPlan":     10,
	"internal/tools/tools_deploy.go|appliedPlan":                  3,
	"internal/tools/tools_disk.go|Registry.listedVM":              1,
	"internal/tools/tools_disk.go|diskAtLimit.Error":              1,
	"internal/tools/tools_dns.go|Registry.dnsZoneLinked":          1,
	"internal/tools/tools_k8s.go|Registry.planK8sCreate":          1,
	"internal/tools/tools_k8s.go|Registry.planK8sNodePoolScale":   2,
	"internal/tools/tools_kafka.go|Registry.planKafkaCreate":      1,
	"internal/tools/tools_pg.go|Registry.planPgCreate":            1,
	"internal/tools/tools_redis.go|Registry.planRedisCreate":      1,
	"internal/tools/tools_sg.go|Registry.planSGRuleDelete":        2,
	"internal/tools/tools_sg_create.go|Registry.planSGRuleCreate": 2,
	"internal/tools/tools_stack.go|Registry.advance":              3,
	"internal/tools/tools_stack.go|Registry.stepRead":             1,
	"internal/tools/tools_stack.go|stepMissing":                   1,
	"internal/tools/tools_subnet.go|Registry.planSubnetCreate":    1,
	"internal/tools/tools_vm.go|Registry.finishVMPower":           9,
	"internal/tools/tools_vm.go|Registry.finishVMResize":          10,
	"internal/tools/tools_vm.go|Registry.planVMPower":             5,
	"internal/tools/tools_vm.go|Registry.planVMResize":            5,
	"internal/tools/tools_vm.go|Registry.vmPowerRefused":          1,
	"internal/tools/tools_vm.go|Registry.vmResizeRefused":         1,
	"internal/tools/tools_vm.go|Registry.waitVM":                  1,
	"internal/tools/tools_vm.go|Registry.waitVMDetail":            1,
	"internal/tools/tools_vm_diagnose.go|vmDiagnosis":             5,
	"internal/tools/tools_vm_diagnose.go|vmGroupsOnRows":          1,
	"internal/tools/tools_vpc.go|Registry.planVpcCreate":          1,
}

// unquotedCloudFlowSteps are the steps of progress of unquotedCloudFlows.
// The list only shrinks. Closes by 1.10.
var unquotedCloudFlowSteps = map[string]int{
	"internal/engine/deploy.go|Engine.recoverFloatingIP": 1,
	"internal/engine/deploy.go|Engine.releaseBootFIP":    1,
	"internal/engine/init.go|Engine.Init":                2,
}

// evoOwnValues are the values the detector takes for the cloud's by the
// name of their field, and that are evo's own or the person's: the id of
// a deployment and its status and phase in the state of the folder, the
// id and the status of a stack job (stack.newID), a JSON path of a
// declaration, the image of the configuration, the HTTP status of evo's
// own MCP server, the id of a record of a call (intent, D46). Quoted as the cloud's, they would read as the cloud's
// words (Астра #2237). A place here is a count of such values in it, not
// a pass for the function: a value of the cloud in it is still counted.
var evoOwnValues = map[string]int{
	"internal/cli/history_view.go|historyEntry.call":           1,
	"internal/cli/mcp.go|newMCPHealthcheckCmd":                 1,
	"internal/config/config.go|pickImage":                      1,
	"internal/engine/compose_path.go|composeRefusal":           1,
	"internal/engine/compose_path.go|composeUnknown":           1,
	"internal/engine/deploy.go|Engine.Deploy":                  2,
	"internal/engine/deploy.go|sshUserOf":                      1,
	"internal/engine/preview.go|redeployPreview":               1,
	"internal/engine/provenance.go|ProvenanceOf":               2,
	"internal/engine/stage.go|InProgress.Error":                1,
	"internal/engine/unfinished.go|deployUnfinished":           2,
	"internal/sgrules/sgrules.go|Close":                        1,
	"internal/stack/store.go|jobNotFoundError.Error":           1,
	"internal/tools/card.go|cardDoes":                          1,
	"internal/tools/tools.go|deployInProgress":                 1,
	"internal/tools/tools_stack.go|Registry.appliableJob":      1,
	"internal/tools/tools_stack.go|Registry.checkJobProject":   1,
	"internal/tools/tools_stack.go|Registry.handleStackPlan":   1,
	"internal/tools/tools_stack.go|Registry.handleStackStatus": 1,
}

// A value that came from the cloud — a status, a state, an id — goes into
// a text of evo (an error, a warning, a hint, a step) only through
// quoteCloud or %q: bare, a newline or an escape sequence in it reads as
// evo's own words to a person and to a model (finding of Астра #1802–#1804,
// #1762, #1763; risk conclusions 232, 235). The check is on the format
// literal of i18n.Tf, i18n.T inside fmt, fmt.Errorf, fmt.Sprintf and
// fmt.Fprintf: a %s or %v right after "status", "state" or "id" must be %q
// or have quoteCloud(…) as its argument; so must a %s or %v of a value of
// the cloud whatever words come before it, and a value of the cloud in a
// text fmt.Sprint, strings.Join, errors.New or + builds (cloudFlow).
func TestCloudWords_QuotedInEvoText(t *testing.T) {
	got := map[string]int{}
	root := filepath.Join("..", "..", "internal")
	// The files of each package, parsed in one set of positions and
	// resolved together: a helper of one file returns to another.
	fset := token.NewFileSet()
	var pkgs []pkgFiles
	rels := map[*ast.File]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		var files []*ast.File
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(path, name), nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(filepath.Join("..", ".."), filepath.Join(path, name))
			rels[f] = filepath.ToSlash(rel)
			files = append(files, f)
		}
		if len(files) > 0 {
			pkgs = append(pkgs, pkgFiles{files[0].Name.Name, files, typeInfo(fset, files)})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	results := cloudResults(pkgs)
	for _, p := range pkgs {
		for _, f := range p.files {
			for k, n := range unquotedIn(f, rels[f], p.info, results.of(p.name, p.info)) {
				got[k] += n
			}
		}
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
	for k, n := range unquotedCloudSteps {
		allowed[stepKey+k] = n
	}
	for k, n := range unquotedCloudLocals {
		allowed[k] += n
	}
	for k, n := range unquotedCloudLocalSteps {
		allowed[stepKey+k] += n
	}
	for k, n := range unquotedCloudFlows {
		allowed[k] += n
	}
	for k, n := range unquotedCloudFlowSteps {
		allowed[stepKey+k] += n
	}
	for k, n := range evoOwnValues {
		allowed[k] += n
	}
	for k, n := range got {
		if n > allowed[k] {
			t.Errorf("%s: %d status, state or id of the cloud printed bare (%d allowed) — wrap the value in quoteCloud or print it with %%q", k, n, allowed[k])
		}
	}
	for k, n := range allowed {
		if got[k] < n {
			list := "unquotedCloudLocals, unquotedCloudFlows and evoOwnValues"
			if strings.HasPrefix(k, stepKey) {
				list = "unquotedCloudSteps, unquotedCloudLocalSteps and unquotedCloudFlowSteps"
			}
			t.Errorf("%s lists %d for %s, and %d are left — lower the line (the list only shrinks)", list, n, strings.TrimPrefix(k, stepKey), got[k])
		}
	}
}

// unquotedIn counts, per "file|function", the bare cloud words of a file.
func unquotedIn(f *ast.File, rel string, info *types.Info, calls func(*ast.CallExpr) []bool) map[string]int {
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
		k := rel + "|" + name
		bareIn(fn.Body, info, calls, func(at ast.Node, n int) {
			if call, isCall := at.(*ast.CallExpr); isCall && step[call] {
				out[stepKey+k] += n
			} else {
				out[k] += n
			}
		})
	}
	for k, n := range out {
		if n == 0 {
			delete(out, k)
		}
	}
	return out
}

// bareIn counts the bare values of the cloud in a function body: in a
// text glued with + (at is the chain), in a format call and in the other calls
// that build a text (textSinks).
func bareIn(body *ast.BlockStmt, info *types.Info, calls func(*ast.CallExpr) []bool, count func(at ast.Node, n int)) {
	flow := newCloudFlow(body, info, calls)
	// A text glued with + is a format too: "VM entered state " +
	// cur.Status reached ApplyResult bare (risk conclusion 301-1, #2140).
	ast.Inspect(body, func(n ast.Node) bool {
		b, ok := n.(*ast.BinaryExpr)
		if !ok || b.Op != token.ADD {
			return true
		}
		if c := bareInConcat(concatOperands(b), flow); c > 0 {
			count(b, c)
		}
		return false // the operands of a chain are counted once
	})
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if format, args, ok := formatCall(call); ok {
			count(call, bareCloudWords(format, args, flow))
		} else {
			count(call, textSinks(call, flow))
		}
		return true
	})
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

// bareCloudWords counts the %s and %v of a format that are not
// quoteCloud(…) and either follow a cloud word or print a value that holds
// a status, a state or an id the cloud gave (f, cloudFlow): the
// words before the verb do not tell what a value holds («does not find %s»
// with the resource id of the operation, «Риск», 312-2; Астра #2236). A
// nil f judges by the words alone.
func bareCloudWords(format string, args []ast.Expr, f *cloudFlow) int {
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
		var a ast.Expr
		if arg < len(args) {
			a = args[arg]
		}
		fromCloud := a != nil && !isConcat(a) && f != nil && f.cloud(a)
		switch {
		case (v == 's' || v == 'v') && (a == nil || !quoted(a)) &&
			(fromCloud || cloudWordBefore.MatchString(format[:loc[0]]) && (a == nil || f == nil || !f.isGuarded(a))):
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

// configField: a field or a key that is not the cloud's — of the
// configuration (the project, the key) or one evo gives itself (the id of
// a plan of deploy_plan; C(7) on #2211).
var configField = regexp.MustCompile(`(?i)^(project_?id|key_?id|plan_?id)$`)

// cloudSource: an expression that reads a status, a state or an id of an
// answer of the cloud: a field (x.Status, x.ResourceID) or a key of an
// answer (m["resource_id"], m["status"].(string)).
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

// cloudFlow follows a status, a state or an id of the cloud through a
// function body, by declaration (the object the parser resolves a name
// to: a shadowing name is another variable) and in the order of the code
// (Астра #2236, #2237). A variable holds the cloud's value from an
// assignment of one: a field or a key of an answer, another such variable,
// a field or an element of one, a literal that holds one, a function
// literal that returns one, one changed in form (carries). An assignment of evo's
// own value in the block of the declaration ends it; in an inner block it
// does not (the branch may not run). Every use of a name records what it
// held there; the sinks read the record.
type cloudFlow struct {
	held    map[types.Object]bool
	declAt  map[types.Object]int
	use     map[*ast.Ident]bool
	guarded map[*ast.Ident]bool
	// fields: the fields of a struct variable made by a literal that hold
	// a value of the cloud (v := T{Text: vm.Status}; v.Text); the other
	// fields of it hold evo's own.
	fields map[types.Object]map[string]bool
	depth  int
	// calls: what each result of a call of a function of internal holds
	// (cloudResults); nil knows no function.
	calls func(*ast.CallExpr) []bool
	info  *types.Info
}

// newCloudFlow walks body once.
func newCloudFlow(body *ast.BlockStmt, info *types.Info, calls func(*ast.CallExpr) []bool) *cloudFlow {
	f := &cloudFlow{held: map[types.Object]bool{}, declAt: map[types.Object]int{},
		use: map[*ast.Ident]bool{}, guarded: map[*ast.Ident]bool{}, fields: map[types.Object]map[string]bool{}, calls: calls, info: info}
	f.stmt(body, nil)
	return f
}

// obj: the variable a name denotes, by the scopes of go/types; nil for
// no variable.
func (f *cloudFlow) obj(id *ast.Ident) types.Object {
	if v, ok := f.info.ObjectOf(id).(*types.Var); ok {
		return v
	}
	return nil
}

// cloud reports whether e holds a value of the cloud at its place in the
// code, not checked by isUUID there.
func (f *cloudFlow) cloud(e ast.Expr) bool {
	return f.from(e) && !f.isGuarded(e)
}

func (f *cloudFlow) isGuarded(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && f.guarded[id]
}

// from: e holds a value of the cloud.
func (f *cloudFlow) from(e ast.Expr) bool {
	switch v := e.(type) {
	case nil:
		return false
	case *ast.Ident:
		return f.use[v]
	case *ast.ParenExpr:
		return f.from(v.X)
	case *ast.StarExpr:
		return f.from(v.X)
	case *ast.UnaryExpr:
		return f.from(v.X)
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok && f.obj(x) != nil && f.fields[f.obj(x)][v.Sel.Name] {
			return true
		}
		return cloudSource(v) || f.from(v.X)
	case *ast.IndexExpr:
		return cloudSource(v) || f.from(v.X)
	case *ast.TypeAssertExpr:
		return cloudSource(v) || f.from(v.X)
	case *ast.SliceExpr:
		return f.from(v.X)
	case *ast.CompositeLit:
		for _, el := range v.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if _, isField := kv.Key.(*ast.Ident); isField {
					// A field of a struct: the struct as a whole is not
					// the cloud's (fields).
					continue
				}
				el = kv.Value
			}
			if f.from(el) {
				return true
			}
		}
	case *ast.CallExpr:
		if quoted(v) {
			return false
		}
		if res := f.results(v); len(res) > 0 {
			return res[0]
		}
		if lit, ok := v.Fun.(*ast.FuncLit); ok {
			held := false
			ast.Inspect(lit.Body, func(n ast.Node) bool {
				if r, ok := n.(*ast.ReturnStmt); ok {
					for _, x := range r.Results {
						held = held || f.from(x)
					}
				}
				_, nested := n.(*ast.FuncLit)
				return !nested
			})
			return held
		}
		// A text or an error built of the value carries it on.
		if carries(v) {
			for _, a := range v.Args {
				if f.from(a) {
					return true
				}
			}
		}
	}
	return false
}

// results: what each result of the call holds, by the summary of the
// function it calls.
func (f *cloudFlow) results(c *ast.CallExpr) []bool {
	if f.calls == nil {
		return nil
	}
	return f.calls(c)
}

// cloudCalls is, by "package.Function" and ".Method", what each result of
// a function of internal returns: a value of the cloud or not. A method is
// known by its name alone, whatever its type: a getter of an id of the
// cloud on one type marks the name on all.
type cloudCalls map[string][]bool

// of: the summary of a call made in package pkg — a function of pkg by its
// name, pkg2.F by its package, x.M by the method.
func (r cloudCalls) of(pkg string, info *types.Info) func(*ast.CallExpr) []bool {
	return func(c *ast.CallExpr) []bool {
		switch fn := c.Fun.(type) {
		case *ast.Ident:
			if _, isFunc := info.ObjectOf(fn).(*types.Func); isFunc {
				return r[pkg+"."+fn.Name]
			}
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok {
				if p, isPkg := info.ObjectOf(x).(*types.PkgName); isPkg {
					return r[p.Imported().Name()+"."+fn.Sel.Name]
				}
			}
			return r["."+fn.Sel.Name]
		}
		return nil
	}
}

// pkgFiles are the files of a package of internal, with the scopes of
// their names (typeInfo).
type pkgFiles struct {
	name  string
	files []*ast.File
	info  *types.Info
}

// typeInfo resolves the names of the files of one package to their
// declarations, the scopes of go/types without the packages they import:
// what a name of a function body denotes needs no import, and an import
// that is not loaded leaves its names unknown, not wrong.
func typeInfo(fset *token.FileSet, files []*ast.File) *types.Info {
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			return nil, fmt.Errorf("%s is not loaded", path)
		}),
		Error: func(error) {},
	}
	_, _ = conf.Check(files[0].Name.Name, fset, files, info)
	return info
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// cloudResults sums up the functions of files: a result is the cloud's
// when a return of the function gives a value of the cloud there (an id
// a helper picks from a list, Астра #2236). The sums feed each other, so
// they are walked again until they hold.
func cloudResults(pkgs []pkgFiles) cloudCalls {
	r := cloudCalls{}
	for round := 0; round < 10; round++ {
		changed := false
		for _, file := range filesOf(pkgs) {
			calls := r.of(file.pkg.name, file.pkg.info)
			for _, decl := range file.ast.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || fn.Type.Results == nil {
					continue
				}
				n := 0
				for _, fld := range fn.Type.Results.List {
					n += max(1, len(fld.Names))
				}
				key := file.pkg.name + "." + fn.Name.Name
				if fn.Recv != nil {
					key = "." + fn.Name.Name
				}
				res := slices.Clone(r[key])
				if len(res) < n {
					res = append(res, make([]bool, n-len(res))...)
				}
				flow := newCloudFlow(fn.Body, file.pkg.info, calls)
				ast.Inspect(fn.Body, func(nd ast.Node) bool {
					switch v := nd.(type) {
					case *ast.FuncLit:
						return false
					case *ast.ReturnStmt:
						if len(v.Results) == n {
							for i, e := range v.Results {
								if i < len(res) && !res[i] && flow.from(e) {
									res[i], changed = true, true
								}
							}
						} else if len(v.Results) == 1 {
							if c, isCall := v.Results[0].(*ast.CallExpr); isCall {
								for i, h := range flow.results(c) {
									if i < len(res) && h && !res[i] {
										res[i], changed = true, true
									}
								}
							}
						}
					}
					return true
				})
				r[key] = res
			}
		}
		if !changed {
			break
		}
	}
	return r
}

type pkgFile struct {
	pkg *pkgFiles
	ast *ast.File
}

func filesOf(pkgs []pkgFiles) []pkgFile {
	var out []pkgFile
	for i := range pkgs {
		for _, f := range pkgs[i].files {
			out = append(out, pkgFile{&pkgs[i], f})
		}
	}
	return out
}

// carries: a call whose result is its argument changed in form, not a
// text of evo built of it — string(…), fmt.Sprint(…) of no literal, the
// functions of strings but Join. A text built with words of evo
// (fmt.Sprintf, fmt.Errorf, errors.New, strings.Join, +) is counted where
// it is built (bareIn) and carries nothing on: one bare value is one
// place, wherever its text goes then.
func carries(c *ast.CallExpr) bool {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "string"
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok {
			return false
		}
		switch pkg.Name {
		case "strings":
			return fn.Sel.Name != "Join"
		case "fmt":
			if fn.Sel.Name != "Sprint" {
				return false
			}
			for _, a := range c.Args {
				if _, ok := literal(a); ok {
					return false
				}
			}
			return true
		}
	}
	return false
}

// stmt walks n in the order of the code; guards are the variables the
// branch has checked with isUUID — the one check of the form of an id (as
// vmLeftNote; «Риск», 291-2, 314): the body of «if isUUID(x)», the «else»
// of «if !isUUID(x)», «case isUUID(x):» of a switch with no tag, and the
// cases after «case !isUUID(x):» in it.
func (f *cloudFlow) stmt(n ast.Node, guards map[types.Object]bool) {
	if n == nil {
		return
	}
	with := func(name *ast.Ident) map[types.Object]bool {
		c := maps.Clone(guards)
		if c == nil {
			c = map[types.Object]bool{}
		}
		if o := f.obj(name); o != nil {
			c[o] = true
		}
		return c
	}
	switch x := n.(type) {
	case *ast.BlockStmt:
		f.depth++
		for _, s := range x.List {
			f.stmt(s, guards)
		}
		f.depth--
	case *ast.AssignStmt:
		for _, r := range x.Rhs {
			f.expr(r, guards)
		}
		for i, l := range x.Lhs {
			held := false
			switch {
			case len(x.Rhs) == len(x.Lhs):
				held = f.from(x.Rhs[i])
			case len(x.Rhs) == 1:
				// id, ok := g(…): each result as g returns it; v, ok :=
				// m[k].(T): the value is the first.
				if c, isCall := x.Rhs[0].(*ast.CallExpr); isCall {
					res := f.results(c)
					held = i < len(res) && res[i]
				} else {
					held = i == 0 && f.from(x.Rhs[0])
				}
			}
			f.assign(l, held, guards)
			if len(x.Rhs) == len(x.Lhs) {
				f.structFields(l, x.Rhs[i])
			}
		}
	case *ast.DeclStmt:
		g, ok := x.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, sp := range g.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				f.expr(v, guards)
			}
			for i, name := range vs.Names {
				held := i < len(vs.Values) && f.from(vs.Values[i])
				f.assign(name, held, guards)
				if i < len(vs.Values) {
					f.structFields(name, vs.Values[i])
				}
			}
		}
	case *ast.RangeStmt:
		f.expr(x.X, guards)
		held := f.from(x.X)
		if x.Value != nil {
			f.assign(x.Value, held, guards)
		}
		f.stmt(x.Body, guards)
	case *ast.IfStmt:
		f.stmt(x.Init, guards)
		f.expr(x.Cond, guards)
		then, other := guards, guards
		if name, neg, ok := isUUIDCall(x.Cond); ok {
			if neg {
				other = with(name)
			} else {
				then = with(name)
			}
		}
		f.stmt(x.Body, then)
		f.stmt(x.Else, other)
	case *ast.SwitchStmt:
		f.stmt(x.Init, guards)
		f.expr(x.Tag, guards)
		after := guards
		for _, s := range x.Body.List {
			cc := s.(*ast.CaseClause)
			here := after
			for _, e := range cc.List {
				f.expr(e, after)
				if name, neg, ok := isUUIDCall(e); ok && !neg && len(cc.List) == 1 && x.Tag == nil {
					here = with(name)
				}
			}
			f.depth++
			for _, st := range cc.Body {
				f.stmt(st, here)
			}
			f.depth--
			for _, e := range cc.List {
				if name, neg, ok := isUUIDCall(e); ok && neg && x.Tag == nil {
					after = with(name)
				}
			}
		}
	case ast.Stmt:
		// Any other statement: its expressions and inner statements, in
		// order.
		ast.Inspect(x, func(c ast.Node) bool {
			if c == x {
				return true
			}
			switch c := c.(type) {
			case ast.Stmt:
				f.stmt(c, guards)
				return false
			case ast.Expr:
				f.expr(c, guards)
				return false
			}
			return true
		})
	}
}

// expr records the uses of names in e at this place; a function literal's
// body is walked where it stands (its captured names hold what they hold
// there).
func (f *cloudFlow) expr(e ast.Expr, guards map[types.Object]bool) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(c ast.Node) bool {
		switch c := c.(type) {
		case *ast.FuncLit:
			f.stmt(c.Body, guards)
			return false
		case *ast.Ident:
			if o := f.obj(c); o != nil {
				f.use[c] = f.held[o]
				f.guarded[c] = guards[o]
			}
		case *ast.SelectorExpr:
			// The selector's name is no variable.
			f.expr(c.X, guards)
			return false
		case *ast.KeyValueExpr:
			if _, isName := c.Key.(*ast.Ident); isName {
				f.expr(c.Value, guards)
				return false
			}
		}
		return true
	})
}

// assign records that l holds the cloud's value or not: a name — by its
// declaration (a value of evo's own ends it in the block of the
// declaration only); x.f = v — the field f of x; x[i] = v — x.
func (f *cloudFlow) assign(l ast.Expr, held bool, guards map[types.Object]bool) {
	switch v := l.(type) {
	case *ast.Ident:
		o := f.obj(v)
		if o == nil {
			return
		}
		if f.info.Defs[v] != nil {
			// Declared here (a name := reuses one declared before).
			f.declAt[o] = f.depth
			f.held[o] = held
			return
		}
		if at, ok := f.declAt[o]; ok && at == f.depth {
			f.held[o] = held
		} else {
			f.held[o] = f.held[o] || held
		}
	case *ast.SelectorExpr:
		// x.f = v: the field f of x holds it; a deeper chain is not
		// followed.
		f.expr(l, guards)
		if x, ok := v.X.(*ast.Ident); ok && f.obj(x) != nil && held {
			o := f.obj(x)
			if f.fields[o] == nil {
				f.fields[o] = map[string]bool{}
			}
			f.fields[o][v.Sel.Name] = true
		}
	case *ast.IndexExpr:
		// x[i] = v: an element of the list or the map x holds it.
		f.expr(l, guards)
		if x, ok := v.X.(*ast.Ident); ok && f.obj(x) != nil && held {
			f.held[f.obj(x)] = true
		}
	}
}

// structFields records the fields of a struct literal assigned to a
// variable that hold a value of the cloud.
func (f *cloudFlow) structFields(l, r ast.Expr) {
	id, ok := l.(*ast.Ident)
	if !ok || f.obj(id) == nil {
		return
	}
	o := f.obj(id)
	if u, isAddr := r.(*ast.UnaryExpr); isAddr && u.Op == token.AND {
		r = u.X
	}
	lit, ok := r.(*ast.CompositeLit)
	if !ok {
		delete(f.fields, o)
		return
	}
	held := map[string]bool{}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if k, isField := kv.Key.(*ast.Ident); isField && f.from(kv.Value) {
				held[k.Name] = true
			}
		}
	}
	f.fields[o] = held
}

// textSinks counts the other ways a text of evo is built of the cloud's
// value: fmt.Sprint* with a literal among its arguments, strings.Join of a
// list that holds one, errors.New of one.
func textSinks(call *ast.CallExpr, f *cloudFlow) int {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return 0
	}
	n := 0
	switch pkg.Name + "." + sel.Sel.Name {
	case "fmt.Sprint", "fmt.Sprintln", "fmt.Fprint", "fmt.Fprintln":
		text := false
		for _, a := range call.Args {
			if _, ok := literal(a); ok {
				text = true
			}
		}
		if !text {
			return 0
		}
		for _, a := range call.Args {
			if !quoted(a) && !isConcat(a) && f.cloud(a) {
				n++
			}
		}
	case "strings.Join":
		if len(call.Args) == 2 && f.cloud(call.Args[0]) {
			n++
		}
	case "errors.New":
		if len(call.Args) == 1 && !isConcat(call.Args[0]) && f.cloud(call.Args[0]) {
			n++
		}
	}
	return n
}

// isUUIDCall: e is isUUID(x) or !isUUID(x) of a variable x.
func isUUIDCall(e ast.Expr) (name *ast.Ident, neg, ok bool) {
	if u, isNot := e.(*ast.UnaryExpr); isNot && u.Op == token.NOT {
		e, neg = u.X, true
	}
	if p, isParen := e.(*ast.ParenExpr); isParen {
		e = p.X
	}
	c, isCall := e.(*ast.CallExpr)
	if !isCall || len(c.Args) != 1 {
		return nil, false, false
	}
	if f, isIdent := c.Fun.(*ast.Ident); !isIdent || f.Name != "isUUID" {
		return nil, false, false
	}
	arg, isIdent := c.Args[0].(*ast.Ident)
	if !isIdent {
		return nil, false, false
	}
	return arg, neg, true
}

// isConcat: e is a text glued with +, counted by bareInConcat itself.
func isConcat(e ast.Expr) bool {
	b, ok := e.(*ast.BinaryExpr)
	return ok && b.Op == token.ADD
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
// after a literal that ends with a cloud word, a field named Status or
// State whatever comes before it, or a value of the cloud (f). A
// chain with no literal of words — no space, colon, equals sign or
// bracket — is no text but a name or a key (id + ".json").
func bareInConcat(ops []ast.Expr, f *cloudFlow) int {
	text := false
	for _, op := range ops {
		if lit, ok := literal(op); ok && strings.ContainsAny(lit, " :=(") {
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
		sel, isSel := op.(*ast.SelectorExpr)
		if after || isSel && (sel.Sel.Name == "Status" || sel.Sel.Name == "State") || f != nil && f.cloud(op) {
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
		if got := bareInConcat(concatOperands(e), nil); got != want {
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
		`id := plan.PlanID; _ = fmt.Sprintf("plan %s", id)`:                                             0,
		`n := vm.Name; _ = fmt.Sprintf("VM %s", n)`:                                                     0,
		`id := stringArg(args, "vm_id"); _ = fmt.Sprintf("VM %s", id)`:                                  0,
		// A branch under isUUID: the id in the form of an id may stand bare
		// in a command («Риск», 291-2, 314); the look of the value does not
		// count, nor another check.
		`id, _ := m["id"].(string); if isUUID(id) { _ = fmt.Sprintf("vm_id=%s", id) }`:                                                                 0,
		`id, _ := m["id"].(string); _ = fmt.Sprintf("vm_id=%s", id)`:                                                                                   1,
		`id, _ := m["id"].(string); if looksUUID(id) { _ = fmt.Sprintf("vm_id=%s", id) }`:                                                              1,
		`id, _ := m["id"].(string); if !isUUID(id) { _ = fmt.Sprintf("VM %s", id) } else { _ = fmt.Sprintf("vm_id=%s", id) }`:                          1,
		`id, _ := m["id"].(string); if !isUUID(id) { _ = 1 } else { _ = fmt.Sprintf("vm_id=%s", id) }`:                                                 0,
		`id, _ := m["id"].(string); if !isUUID(id) { _ = fmt.Sprintf("vm_id=%s", id) }`:                                                                1,
		`id, _ := m["id"].(string); switch { case !isUUID(id): _ = 1; case p: _ = fmt.Sprintf("vm_id=%s", id); default: _ = fmt.Sprintf("x %s", id) }`: 0,
		`id, _ := m["id"].(string); switch { case !isUUID(id) && p: _ = 1; default: _ = fmt.Sprintf("x %s", id) }`:                                     1,
		`id, _ := m["id"].(string); switch { case isUUID(id): _ = fmt.Sprintf("vm_id=%s", id); default: _ = fmt.Sprintf("x %s", id) }`:                 1,
	} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "p.go", "package p\nfunc f() {"+src+"}", 0)
		if err != nil {
			t.Fatal(err)
		}
		body := f.Decls[0].(*ast.FuncDecl).Body
		got := 0
		bareIn(body, typeInfo(fset, []*ast.File{f}), nil, func(_ ast.Node, n int) { got += n })
		if got != want {
			t.Errorf("%s: %d bare, want %d", src, got, want)
		}
	}
}

// The detector follows a value of the cloud by its declaration and in the
// order of the code, through the ways Астра #2236 carried it past the
// detector of 1.9 (each a text with a real newline of the cloud in it),
// and stops where evo's own word takes its place (Астра #2237).
func TestCloudWords_DetectorFollowsFlows(t *testing.T) {
	const helper = `func pick(vms []astraVM) (string, bool) { for _, vm := range vms { return vm.ID, true }; return "", false }
func own() string { return "unknown" }
`
	unsafe := []string{
		`v := vm.Status; return fmt.Sprintf("it is %s now", v)`,
		`a := vm.Status; b := a; return fmt.Sprintf("it is %s now", b)`,
		`v := struct{Text string}{Text: vm.Status}; return fmt.Sprintf("it is %s now", v.Text)`,
		`v := []string{vm.Status}; return strings.Join(v, ", ")`,
		`v := vm.Status; return fmt.Sprint("it is ", v)`,
		`return fmt.Sprintf("it is %v now", vm.Status)`,
		`v := vm.Status; return fmt.Sprintf("it is %v now", v)`,
		`v := func() string { return vm.Status }(); return fmt.Sprintf("it is %s now", v)`,
		`v := vm.Status; f := func() string { return fmt.Sprintf("it is %s now", v) }; return f()`,
		`m := map[string]any{"status": vm.Status}; v := m["status"].(string); return fmt.Sprintf("it is %s now", v)`,
		`m := map[string]any{"status": vm.Status}; return fmt.Sprintf("it is %s now", m["status"].(string))`,
		`v := vm.Status; return i18n.Tf("it is %s now", "сейчас это %s", v)`,
		`return i18n.Tf("it is %s now", "сейчас это %s", vm.Status)`,
		`v := vm.Status; err := errors.New(v); return fmt.Errorf("read failed: %w", err).Error()`,
		`v := vm.Status; err := fmt.Errorf("it is %s now", v); return fmt.Errorf("read failed: %w", err).Error()`,
		`var v = vm.Status; return fmt.Sprintf("it is %s now", v)`,
		`_, v := 0, vm.Status; return fmt.Sprintf("it is %s now", v)`,
		`v := vm.Status; return "it is " + v`,
		// Beyond #2236: a helper that picks an id of the cloud from a list
		// (the gateway of a zone, snat_create), an element of a range, a
		// field set after the literal, an id under another check than
		// isUUID, a value kept in a branch.
		`id, ok := pick(vms); if ok { return fmt.Sprintf("use it (snat_get id=%s)", id) }; return ""`,
		`for _, x := range vms { return fmt.Sprintf("VM %s", x.ID) }; return ""`,
		`for _, s := range []string{vm.Status} { return fmt.Sprintf("it is %s now", s) }; return ""`,
		`var r struct{ Text string }; r.Text = vm.Status; return fmt.Sprintf("it is %s now", r.Text)`,
		`v := "unknown"; if vm.Status != "" { v = vm.Status }; return fmt.Sprintf("it is %s now", v)`,
		`v := vm.ID; if looksUUID(v) { return fmt.Sprintf("vm_id=%s", v) }; return ""`,
		`v := strings.TrimSpace(vm.Status); return fmt.Sprintf("it is %s now", v)`,
	}
	safe := []string{
		`v := vm.Status; return fmt.Sprintf("it is %s now", quoteCloud(v))`,
		`v := vm.Status; return fmt.Sprintf("it is %q now", v)`,
		`return fmt.Sprintf("it is %s now", quoteCloud(vm.Status))`,
		`v := vm.Status; return fmt.Sprintf("it took %d tries", len(v))`,
		`p := cfg.ProjectID; return fmt.Sprintf("in %s", p)`,
		`v := struct{Text, Name string}{Text: vm.Status, Name: "x"}; return fmt.Sprintf("VM %s", v.Name)`,
		`v := own(); return fmt.Sprintf("it is %s now", v)`,
		`v := vm.ID; if isUUID(v) { return fmt.Sprintf("vm_id=%s", v) }; return ""`,
		`id := strings.Join([]string{quoteCloud(vm.ID)}, ", "); return fmt.Sprintf("ids %s", id)`,
		// Астра #2237: evo's own word over the cloud's, and a name that
		// shadows it.
		`v := vm.Status; v = "unknown"; return fmt.Sprintf("it is %s now", v)`,
		`v := vm.Status; _ = v; { v := "unknown"; return fmt.Sprintf("it is %s now", v) }`,
		// A name or a key glued of an id is no text.
		`return vm.ID + ".json"`,
	}
	count := func(body string) int {
		src := "package p\n" + helper + "func probe(vm astraVM, vms []astraVM, cfg config) string {" + body + "}"
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "p.go", src, 0)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		pkgs := []pkgFiles{{"p", []*ast.File{f}, typeInfo(fset, []*ast.File{f})}}
		results := cloudResults(pkgs)
		got := 0
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "probe" {
				bareIn(fn.Body, pkgs[0].info, results.of("p", pkgs[0].info), func(_ ast.Node, n int) { got += n })
			}
		}
		return got
	}
	for _, body := range unsafe {
		if count(body) == 0 {
			t.Errorf("a value of the cloud reaches the text bare, and the detector does not see it:\n%s", body)
		}
	}
	for _, body := range safe {
		if n := count(body); n != 0 {
			t.Errorf("the detector counts %d bare in a text with no bare value of the cloud:\n%s", n, body)
		}
	}
}
