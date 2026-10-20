// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The commands the parity plan added (spec.md REQ-3, PR 3) are the tools
// themselves, on a fake cloud that answers every request with an empty
// list: for each command, the same arguments given as positionals and flags
// and given to 'evo tools call' with --arg give the same stdout, stderr and
// exit code with --json (a mutating one with --yes, so that it runs past the
// gate). The previews of every command and tool that asks for consent are
// TestContract_PreviewParity (contract_preview_test.go, REQ-6), on a fake
// answer per API; the answers of every command against its tool, on a fake
// per API, are TestParity_JSONEqualsTool (REQ-8). The arguments are those of
// parityArgs, the one table of the three.

// parityCommand is one new command with its arguments: as the tree takes
// them and as 'evo tools call' does.
type parityCommand struct {
	path string // "dns zones"
	tool tools.Tool
	args map[string]any
	tree []string
	call []string
	// engine: tree is an engine command (engineCommands), which shows the
	// engine's work and asks at the engine's gate.
	engine bool
}

func newParityCommands(t *testing.T) []parityCommand {
	t.Helper()
	reg := tools.New(nil)
	var out []parityCommand
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if !parityTrees[g.Name] && !parityVerbs[g.Name+" "+c.Verb] {
				continue
			}
			tl, _ := reg.Lookup(c.Tool)
			args, ok := parityArgs[c.Tool]
			if !ok {
				t.Fatalf("%s: no entry in parityArgs", c.Tool)
			}
			out = append(out, parityCommandOf(t, g, c, tl, args))
		}
	}
	if len(out) == 0 {
		t.Fatal("no new commands")
	}
	return out
}

// parityCommandOf is command c of tree g with the arguments args: as the
// tree takes them (positionals, then flags in key order) and as 'evo tools
// call' does (--arg each). With g and c zero (a tool without a command of
// the trees) there is only the call.
func parityCommandOf(t *testing.T, g treeGroup, c treeCmd, tl tools.Tool, args map[string]any) parityCommand {
	t.Helper()
	pc := parityCommand{path: strings.TrimSpace(g.Name + " " + c.Verb), tool: tl, args: args}
	pc.call = []string{"tools", "call", tl.Name}
	for _, k := range sortedKeys(args) {
		pc.call = append(pc.call, "--arg", k+"="+flagText(args[k]))
	}
	if c.Verb == "" {
		pc.path = "tools call " + tl.Name
		return pc
	}
	if g.Name != "" {
		pc.tree = append(pc.tree, g.Name)
	}
	pc.tree = append(pc.tree, c.Verb)
	used := map[string]bool{}
	for _, pos := range parsePositionals(c.Args) {
		v, ok := args[pos.names[0]]
		if !ok && len(pos.names) == 2 {
			v, ok = args[pos.names[1]]
			used[pos.names[1]] = true
		}
		used[pos.names[0]] = true
		if !ok {
			continue // an optional positional
		}
		pc.tree = append(pc.tree, fmt.Sprint(v))
	}
	params := map[string]param{}
	for _, p := range c.flagArgs(tl) {
		params[p.Name] = p
	}
	for _, k := range sortedKeys(args) {
		if used[k] {
			continue
		}
		if _, ok := params[k]; !ok {
			t.Fatalf("%s: argument %s is neither positional nor a flag", pc.path, k)
		}
		if m, ok := args[k].(map[string]any); ok && c.Pairs[k] {
			// A repeatable key=value flag (--meta k=v).
			for _, mk := range sortedKeys(m) {
				pc.tree = append(pc.tree, "--"+c.flagName(k)+"="+mk+"="+fmt.Sprint(m[mk]))
			}
			continue
		}
		value := flagText(args[k])
		if c.Files[k] {
			// The flag names a file that holds the value.
			path := filepath.Join(t.TempDir(), k+".json")
			if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
				t.Fatal(err)
			}
			value = path
		}
		pc.tree = append(pc.tree, "--"+c.flagName(k)+"="+value)
	}
	return pc
}

// flagText is a value as a flag or --arg takes it.
func flagText(v any) string {
	switch x := v.(type) {
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = fmt.Sprint(e)
		}
		return strings.Join(parts, ",")
	case map[string]any:
		b, _ := json.Marshal(x)
		return string(b)
	}
	return fmt.Sprint(v)
}

// stackJobID is the id stack_plan gives each plan it makes: two calls make
// two plans.
var stackJobID = regexp.MustCompile(`stk-[0-9a-f]{10}`)

// sameJobID reads every stack job id as one.
func sameJobID(s string) string { return stackJobID.ReplaceAllString(s, "stk-<id>") }

// withEmptyCloud: a key and a project, a token without IAM, and every API
// answering 200 with an empty list.
func withEmptyCloud(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLOUDRU_CONFIG_DIR", home)
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	t.Setenv("CLOUDRU_KEY_ID", "00000000000000000000000000000000")
	t.Setenv("CLOUDRU_SECRET", "00000000000000000000000000000000")
	t.Setenv("EVO_LANG", "en")
	t.Chdir(home)
	installContractTransport(t, 200, `{"items":[],"total":0}`)
	old := toolEngine
	toolEngine = emptyCloudEngine
	t.Cleanup(func() { toolEngine = old })
}

func emptyCloudEngine(o map[string]any) (*engine.Engine, error) {
	if o == nil {
		o = map[string]any{}
	}
	e, err := buildEngine(o)
	if err != nil {
		return nil, err
	}
	e.Auth = previewToken{}
	return e, nil
}

func TestParity_NewCommandsJSONEqualsToolsCall(t *testing.T) {
	withEmptyCloud(t)
	var bad []string
	ok := 0
	for _, pc := range newParityCommands(t) {
		extra := []string{"--json"}
		if pc.tool.Mutating || tools.IsSensitive(pc.tool) {
			extra = append(extra, "--yes")
		}
		newProcess()
		treeCode, treeOut, treeErr := runCLI(t, "", append(append([]string{}, pc.tree...), extra...)...)
		newProcess()
		callCode, callOut, callErr := runCLI(t, "", append(append([]string{}, pc.call...), extra...)...)
		treeOut, callOut = sameJobID(treeOut), sameJobID(callOut)
		if treeCode != callCode || treeOut != callOut || treeErr != callErr {
			bad = append(bad, fmt.Sprintf("evo %s ≠ evo tools call %s:\n  tree: exit %d out=%s err=%s\n  call: exit %d out=%s err=%s",
				strings.Join(pc.tree, " "), strings.Join(pc.call[2:], " "),
				treeCode, clipLine(treeOut), clipLine(treeErr), callCode, clipLine(callOut), clipLine(callErr)))
			continue
		}
		if why := jsonImpure(treeOut, treeErr); why != "" {
			bad = append(bad, "evo "+strings.Join(pc.tree, " ")+": "+why)
			continue
		}
		ok++
	}
	engine := 0
	for _, c := range engineParity {
		extra := []string{"--json"}
		if c.yes {
			extra = append(extra, "--yes")
		}
		line := append(append([]string{}, c.line...), extra...)
		newProcess()
		cmdCode, cmdOut, cmdErr := runCLI(t, "", line...)
		newProcess()
		callCode, callOut, callErr := runCLI(t, "", append([]string{"tools", "call", c.tool}, extra...)...)
		why := ""
		compat, isCompat := jsonAliasCompat["evo "+strings.Join(c.line, " ")]
		switch {
		case cmdCode != callCode:
			why = fmt.Sprintf("exit %d ≠ %d", cmdCode, callCode)
		case jsonImpure(cmdOut, cmdErr) != "":
			why = jsonImpure(cmdOut, cmdErr)
		case cmdOut != callOut && isCompat && compat.check != nil:
			if why = compat.check(cmdOut, cmdErr, callOut); why == "" {
				t.Logf("evo %s: %s", strings.Join(c.line, " "), compat.reason)
			}
		case cmdOut != callOut:
			why = "stdout differs"
		default:
			why = sameEngineError(c.tool, cmdErr, callErr)
		}
		if why != "" {
			bad = append(bad, fmt.Sprintf("evo %s ≠ evo tools call %s: %s\n  cmd: exit %d out=%s err=%s\n  call: exit %d out=%s err=%s",
				strings.Join(line, " "), c.tool, why, cmdCode, clipLine(cmdOut), clipLine(cmdErr), callCode, clipLine(callOut), clipLine(callErr)))
			continue
		}
		engine++
	}
	if engine != len(engineParity) {
		t.Errorf("engine commands with the JSON of their tool: %d of %d", engine, len(engineParity))
	}
	tested := map[string]bool{}
	for _, c := range engineParity {
		tested["evo "+strings.Join(c.line, " ")] = true
	}
	for _, ec := range engineCommands {
		if !tested[ec.Path] {
			t.Errorf("%s: not in engineParity", ec.Path)
		}
	}
	t.Logf("new commands with the JSON of their tool: %d; engine commands: %d", ok, engine)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// engineParity are the engine commands (engineCommands, spec REQ-4) and
// evo vm zones, the command of list_zones, against 'evo tools call' of their
// tools on the empty cloud: the same exit code, stdout and error (see
// sameEngineError). A command whose --json is not the tool's answer as is
// is in jsonAliasCompat (parity_json_test.go), with the reason and the form
// it gives instead: the closed list of REQ-8.
var engineParity = []struct {
	line []string
	tool string
	yes  bool
}{
	{line: []string{"deploy"}, tool: "deploy", yes: true},
	{line: []string{"status"}, tool: "status"},
	{line: []string{"logs"}, tool: "logs"},
	{line: []string{"destroy"}, tool: "destroy", yes: true},
	{line: []string{"vm", "zones"}, tool: "list_zones"},
	{line: []string{"list-zones"}, tool: "list_zones"},
	{line: []string{"project", "list"}, tool: "org_projects"},
	{line: []string{"list-images"}, tool: "list_images"},
}

// arrayOfAnswer checks the --json of a legacy alias: the list key of the
// tool's answer as a bare array (nothing when it is empty), and the warning
// that the alias is deprecated.
func arrayOfAnswer(cmdOut, cmdErr, callOut, key, warning string) string {
	var call map[string]json.RawMessage
	if err := json.Unmarshal([]byte(callOut), &call); err != nil {
		return "the tool's answer is not an object"
	}
	var list []any
	_ = json.Unmarshal(call[key], &list)
	if !strings.Contains(cmdErr, warning) {
		return "no deprecation warning"
	}
	if strings.TrimSpace(cmdOut) == "" && len(list) == 0 && key == "images" {
		return ""
	}
	var got []any
	if err := json.Unmarshal([]byte(cmdOut), &got); err != nil || !reflect.DeepEqual(got, list) && !(len(got) == 0 && len(list) == 0) {
		return "stdout is not the array " + key + " of the tool's answer"
	}
	return ""
}

// sameEngineError: the command and 'evo tools call' fail alike — the same
// code, reason and hint — and the message of the command is the tool's
// without "<tool>: ": an engine command prints the engine's error as it
// did before it called its tool. The tool-level consent of 'evo tools call'
// ({"status":"confirmed"} before the call) is not the engine command's: it
// asks at the engine's gate, which an error before it never reaches.
func sameEngineError(tool, cmdErr, callErr string) string {
	last := func(stderr string) (e struct {
		Code    int    `json:"code"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	}, ok bool) {
		for _, line := range strings.Split(stderr, "\n") {
			var env struct {
				Error *struct {
					Code    int    `json:"code"`
					Reason  string `json:"reason"`
					Message string `json:"message"`
					Hint    string `json:"hint"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(line), &env) == nil && env.Error != nil {
				e, ok = *env.Error, true
			}
		}
		return e, ok
	}
	ce, cok := last(cmdErr)
	te, tok := last(callErr)
	switch {
	case cok != tok:
		return "one of them failed"
	case !cok:
		if cmdErr != callErr {
			return "stderr differs"
		}
		return ""
	case ce.Code != te.Code || ce.Reason != te.Reason || ce.Hint != te.Hint:
		return fmt.Sprintf("error %d %s %q ≠ %d %s %q", ce.Code, ce.Reason, ce.Hint, te.Code, te.Reason, te.Hint)
	case tool+": "+ce.Message != te.Message:
		return fmt.Sprintf("message %q ≠ %q", ce.Message, te.Message)
	}
	return ""
}

// dryRunDetails is preview.details of a dry_run envelope.
func dryRunDetails(stdout string) (map[string]any, bool) {
	var env struct {
		Status  string `json:"status"`
		Preview struct {
			Details map[string]any `json:"details"`
		} `json:"preview"`
	}
	if json.Unmarshal([]byte(stdout), &env) != nil || env.Status != "dry_run" || env.Preview.Details == nil {
		return nil, false
	}
	return env.Preview.Details, true
}

// The tables of the trees the parity plan added speak Russian, as those of
// vm and disk (PAR-16); a title added for them leaves the vm and disk
// tables as they were.
func TestRenderAnswer_NewRussianTrees(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	render := func(group, verb, text string) []string {
		t.Helper()
		var c treeCmd
		for _, g := range allTreeGroups() {
			for _, cmd := range g.Cmds {
				if g.Name == group && cmd.Verb == verb {
					c = cmd
				}
			}
		}
		if c.Tool == "" {
			t.Fatalf("no %s %s", group, verb)
		}
		var buf bytes.Buffer
		p := output.NewWithWriter(&buf, output.ModePlain, false)
		if err := renderAnswer(p, &buf, text, tableView{Columns: c.Columns, Russian: c.russian, Headers: c.headers}); err != nil {
			t.Fatal(err)
		}
		return strings.Split(buf.String(), "\n")
	}
	lines := render("dns", "zones", `{"zones":[{"id":"z-1","name":"ex","domain":"example.com","visibility":"public","state":"ok","delegated":true,"valid":true}]}`)
	if h := strings.Join(strings.Fields(lines[0]), " "); h != "ID Имя Домен Видимость Состояние Делегирована" {
		t.Errorf("dns zones header: %q", h)
	}
	if r := strings.Join(strings.Fields(lines[1]), " "); r != "z-1 ex example.com public ok да" {
		t.Errorf("dns zones row: %q", r)
	}
	for tree := range parityTrees {
		if !russianTrees[tree] {
			t.Errorf("tree %s: its tables are not Russian (russianTrees)", tree)
		}
	}
	// vm zones: "default" has no title in columnHeaderRU, and keeps none.
	lines = render("vm", "zones", `{"zones":[{"id":"z-1","name":"ru.AZ-1","enabled":true,"default":true}]}`)
	if h := strings.Join(strings.Fields(lines[0]), " "); h != "ID Имя DEFAULT Открыта" {
		t.Errorf("vm zones header: %q", h)
	}
	// iam: "enabled" is a service account that is on, not an open zone.
	lines = render("iam", "service-accounts", `{"service_accounts":[{"id":"sa-1","name":"ci","enabled":true}]}`)
	if h := strings.Join(strings.Fields(lines[0]), " "); h != "ID Имя Включён" {
		t.Errorf("iam service-accounts header: %q", h)
	}
}

// project_id of org quotas, iam service-accounts and billing is a filter of
// what the tool reads: --project-id of the command passes it to the tool,
// before the command name too (cobra parses every flag with the command's
// set, where its own --project-id shadows the root one).
func TestTreeArgs_ProjectIDFilter(t *testing.T) {
	for _, path := range [][]string{
		{"org", "quotas"}, {"iam", "service-accounts"}, {"billing", "consumption"}, {"billing", "finops"},
	} {
		for _, argv := range [][]string{
			append(append([]string{}, path...), "--project-id", pvProject),
			append([]string{"--project-id", pvProject}, path...),
		} {
			cmd, rest, err := newRootCmd().Find(argv)
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.ParseFlags(rest); err != nil {
				t.Fatal(err)
			}
			_, c, tl := findLeaf(t, path[0], path[1])
			if c.flagName("project_id") != "project-id" {
				t.Fatalf("%v: project_id has the flag --%s", path, c.flagName("project_id"))
			}
			got, err := treeArgs(cmd, c, tl, parsePositionals(c.Args), c.flagArgs(tl), cmd.Flags().Args())
			if err != nil && !strings.Contains(err.Error(), "--start") { // billing consumption: --start is required
				t.Fatal(err)
			}
			if err == nil && got["project_id"] != pvProject {
				t.Errorf("evo %s: project_id %v, want %s", strings.Join(argv, " "), got["project_id"], pvProject)
			}
			if flagProjectID != "" {
				t.Errorf("evo %s: the root --project-id is set too (%q)", strings.Join(argv, " "), flagProjectID)
			}
		}
	}
}
