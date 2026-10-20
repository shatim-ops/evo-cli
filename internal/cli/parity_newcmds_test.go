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
// answer per API; the sweep of every answer with a fake per API is PR 6
// (REQ-8), this is the smoke test of the new commands.

// parityArgsOf are the arguments a new command gets in these tests beyond
// its required ones: a required one-of pair (dns record-create: --value or
// --values), a value the tool checks before the cloud.
var parityArgsOf = map[string]map[string]any{
	"dns_record_create": {"value": "203.0.113.10"},
	"dns_zone_delete":   {"type": "public"},
	"dns_record_delete": {"type": "public"},
	"tg_create":         {"vm_id": parityVM},
	"lb_create":         {"vpc_id": parityVM, "zone": pvZone},
	"snat_create":       {"availability_zone_id": pvZone},
	"stack_apply":       {"job_id": "stk-0123456789"},
	"stack_status":      {"job_id": "stk-0123456789"},
	"stack_destroy":     {"job_id": "stk-0123456789"},
	// project_id is a filter of these tools, given with --project-id of
	// the command (TestTreeArgs_ProjectIDFilter).
	"org_quotas":           {"project_id": pvProject},
	"iam_service_accounts": {"project_id": pvProject},
	"billing_consumption":  {"project_id": pvProject},
}

// parityValue is a value of argument p for these tests: an enum's first
// value, a UUID for an id, a name otherwise.
func parityValue(t tools.Tool, p param) any {
	props, _ := t.InputSchema["properties"].(map[string]any)
	prop, _ := props[p.Name].(map[string]any)
	switch enum := prop["enum"].(type) {
	case []any:
		if len(enum) > 0 {
			return enum[0]
		}
	case []string:
		if len(enum) > 0 {
			return enum[0]
		}
	}
	switch p.Type {
	case "integer", "number":
		return float64(80)
	case "boolean":
		return true
	case "array":
		if p.ItemType == "integer" {
			return []any{float64(80)}
		}
		return []any{"example.com"}
	case "object":
		return map[string]any{"name": "web"}
	}
	if p.Name == "id" || strings.HasSuffix(p.Name, "_id") {
		return parityVM
	}
	return "web"
}

// parityCommand is one new command with its arguments: as the tree takes
// them and as 'evo tools call' does.
type parityCommand struct {
	path string // "dns zones"
	tool tools.Tool
	args map[string]any
	tree []string
	call []string
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
			args := map[string]any{}
			for _, p := range schemaParams(tl) {
				if p.Required {
					args[p.Name] = parityValue(tl, p)
				}
			}
			for k, v := range parityArgsOf[c.Tool] {
				args[k] = v
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
// call' does (--arg each). A tool without a command (tools.CLIPending) has
// only the call: g and c are zero then.
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
	t.Logf("new commands with the JSON of their tool: %d", ok)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
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
