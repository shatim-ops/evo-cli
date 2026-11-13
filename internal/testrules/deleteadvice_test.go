// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// deleteVerb is a verb of deleting in a text of evo, English or Russian:
// delete, remove, erase, purge, wipe; удалить, удалите, удален (not
// «удалось»), снести, стереть.
var deleteVerb = regexp.MustCompile(`(?i)\b(delet|remov|eras|purg|wip)\w*|удал[иеёяю]\w*|снес\w*|стер[еи]\w*`)

// localCommandFiles are the files whose texts the local commands
// (internal/cli/local_commands.go) print: the command → its files. A new
// local command without a line here fails the test.
var localCommandFiles = map[string][]string{
	"evo configure":             {"internal/cli/configure.go", "internal/cli/credentials.go"},
	"evo configure list":        {"internal/cli/configure.go"},
	"evo configure delete":      {"internal/cli/configure.go"},
	"evo configure s3":          {"internal/cli/configure_s3.go"},
	"evo login":                 {"internal/cli/login.go", "internal/login"},
	"evo logout":                {"internal/cli/login.go", "internal/login"},
	"evo project":               {"internal/cli/project.go"},
	"evo project use":           {"internal/cli/project.go"},
	"evo init":                  {"internal/cli/main.go"},
	"evo verify":                {"internal/cli/main.go"},
	"evo version":               {"internal/cli/main.go", "internal/cli/version.go"},
	"evo completion":            {"internal/cli/completion.go"},
	"evo completion bash":       {"internal/cli/completion.go"},
	"evo completion zsh":        {"internal/cli/completion.go"},
	"evo completion fish":       {"internal/cli/completion.go"},
	"evo completion powershell": {"internal/cli/completion.go"},
	"evo mcp":                   {"internal/cli/mcp.go"},
	"evo mcp healthcheck":       {"internal/cli/mcp.go"},
	"evo tools":                 {"internal/cli/tools.go"},
	"evo tools list":            {"internal/cli/tools.go"},
	"evo tools describe":        {"internal/cli/tools.go"},
	"evo tools call":            {"internal/cli/tools.go"},
	"evo agent":                 {"internal/cli/agent.go"},
	"evo agent setup":           {"internal/cli/agent.go", "internal/cli/agent_clients.go", "internal/agentconf"},
	"evo agent skill":           {"internal/cli/agent.go", "internal/cli/agent_skill.go"},
	"evo diag":                  {"internal/cli/diag.go"},
	"evo diag on":               {"internal/cli/diag.go"},
	"evo diag off":              {"internal/cli/diag.go"},
	"evo diag status":           {"internal/cli/diag.go"},
	"evo diag export":           {"internal/cli/diag.go"},
	"evo crew":                  {"internal/cli/version.go"},
	"evo help":                  {"internal/cli/help_groups.go", "internal/cli/help_ru.go"},
}

// deleteAdviceOwn are the texts of the local commands that name a delete
// of what evo itself made — its access key, its profile, its journal, what
// a broken login left — or say what a delete does or what a role allows:
// "file<TAB>function" → what is deleted and why it is evo's.
var deleteAdviceOwn = map[string]string{
	// What the role and the key allow: a warning, not an advice.
	"internal/login/texts.go\ttRoleNote":        "the role allows deleting project resources",
	"internal/login/texts.go\ttAgentWarn":       "the role lets the agent delete any resources",
	"internal/login/texts.go\ttAgentWarnLine":   "the role lets the agent delete any resources",
	"internal/login/texts.go\tTAgentKeyWarning": "the key of evo login lets the agent delete any resources",
	"internal/cli/main.go\tnewRootCmd":          "--yes and --dry-run: consent to and preview of a delete the person asked for",
	"internal/cli/tools.go\tnewToolsCmd":        "tools call runs a delete tool the person names",
	"internal/cli/tools.go\tnewToolsCallCmd":    "--wait of the delete tools",
	"internal/cli/tools.go\tapplyWait":          "--wait of the delete tools",
	"internal/cli/agent.go\tnewAgentSkillCmd":   "the card says which delete undoes which create",
	// The key evo login made, and its profile.
	"internal/login/texts.go\ttDoneKey":               "the previous key of evo login on this computer was deleted",
	"internal/login/texts.go\ttPlanReplace":           "the previous key of evo login on this computer will be deleted (step 18)",
	"internal/login/texts.go\tTResultReused":          "the previous key of evo login was deleted",
	"internal/login/texts.go\ttRevokeHow":             "the key this login made",
	"internal/login/texts.go\ttAgentRevokeLine":       "evo logout deletes the key of evo login",
	"internal/login/texts.go\teKeyLeft":               "the previous key of the profile, made by evo login (Previous needs creds.Login)",
	"internal/login/texts.go\teNotCompleteHint":       "what this broken login made and could not undo",
	"internal/login/texts.go\ttKilledLogin":           "the key a killed evo login asked for, which no profile holds (№ 37)",
	"internal/login/texts.go\teKeyOutcomeUnknownHint": "the key this evo login asked for, whose answer was lost, and its intent record (FB180-3)",
	"internal/login/texts.go\tTLogoutShort":           "the key evo login made, and the profile",
	"internal/login/texts.go\tTLogoutLong":            "the key evo login made, and the profile",
	"internal/login/texts.go\tTLogoutRevoke":          "the key evo login made, and the profile",
	"internal/login/texts.go\tTLogoutDelete":          "the profile on disk",
	"internal/login/texts.go\tTLogoutDone":            "the profile on disk",
	"internal/login/texts.go\tkeyWords":               "the key evo login made",
	"internal/login/texts.go\tTLogoutRejected":        "the key evo login made, which IAM no longer accepts",
	"internal/login/texts.go\teKeyNotDeleted":         "the key evo login made",
	"internal/login/texts.go\teKeyNotDeletedHint":     "the key evo login made, which evo logout could not delete",
	"internal/login/texts.go\tTLoginLong":             "evo logout deletes the key of evo login",
	// Files of evo on this computer.
	"internal/cli/configure.go\tnewConfigureDeleteCmd": "a profile of evo on disk",
	"internal/cli/diag.go\tprintDiagStatus":            "the journal file of evo",
	// The SSH key at evo's own path that evo did not write: the file is
	// the threat (it may have come with the repository), not a resource.
	"internal/cli/main.go\tensureKeyDir":        "a file at evo's key path that evo did not write",
	"internal/cli/main.go\treadKeyFile":         "a file at evo's key path that evo did not write",
	"internal/cli/main.go\tcheckPrivateKeyMode": "a file at evo's key path that evo did not write",
}

// deleteAdviceMisses are the texts that advise deleting what evo did not
// make; they go before rc.1 (release plan 1.8, «ложный лимит», R-56); the
// last, TLogoutKept of evo logout, went in #1078. The list only shrinks.
// Empty: its deadline 1.8 is met, and a new line comes with a deadline of
// its own and its plan line.
var deleteAdviceMisses = map[string]string{}

// Release plan 1.8, «ложный лимит» (acceptance B-5, 06.11; «Риск», R-56,
// conclusion 33): the hint «delete an unused service account» of evo login
// led the owner to delete accounts evo had not made — among them the
// service accounts of Managed Kubernetes clusters. The texts of evo login
// and of every local command name a delete only of what evo made (its key,
// its profile, its files), or to say what a delete or a role does. A new
// text with a verb of deleting is a line of deleteAdviceOwn with what it
// deletes, or it is the defect.
func TestLocalCommands_AdviseNoDeleteOfWhatEvoDidNotMake(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, c := range localCommandPaths(t, root) {
		if len(localCommandFiles[c]) == 0 {
			t.Errorf("local command %q has no files in localCommandFiles: its texts are not read", c)
		}
	}
	files := map[string]bool{}
	for _, fs := range localCommandFiles {
		for _, f := range fs {
			files[f] = true
		}
	}
	hits := map[string][]string{}
	for f := range files {
		for _, path := range goFilesOf(t, root, f) {
			for key, text := range deleteVerbTexts(t, root, path) {
				hits[key] = append(hits[key], text)
			}
		}
	}
	if len(hits) < 10 {
		t.Fatalf("%d texts with a verb of deleting: the scan sees nothing", len(hits))
	}
	keys := make([]string, 0, len(hits))
	for k := range hits {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, own := deleteAdviceOwn[k]
		_, miss := deleteAdviceMisses[k]
		if !own && !miss {
			t.Errorf("%s: a text names a delete — %q.\nSay what evo made that it deletes (a line of deleteAdviceOwn), or do not advise deleting: what evo did not make may be a cluster's (R-56)",
				strings.ReplaceAll(k, "\t", ": "), hits[k][0])
		}
	}
	for _, list := range []map[string]string{deleteAdviceOwn, deleteAdviceMisses} {
		for k := range list {
			if _, ok := hits[k]; !ok {
				t.Errorf("%s: no text with a verb of deleting there any more — drop the line", strings.ReplaceAll(k, "\t", ": "))
			}
		}
	}
	t.Logf("deleteAdviceMisses: %d lines", len(deleteAdviceMisses))
}

// localCommandPaths reads the paths of localCommands.
func localCommandPaths(t *testing.T, root string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal", "cli", "local_commands.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "localCommands" {
			return true
		}
		for _, el := range vs.Values[0].(*ast.CompositeLit).Elts {
			if l, ok := el.(*ast.CompositeLit).Elts[0].(*ast.BasicLit); ok {
				s, _ := strconv.Unquote(l.Value)
				out = append(out, s)
			}
		}
		return false
	})
	if len(out) < 20 {
		t.Fatalf("%d local commands read: update localCommandPaths", len(out))
	}
	return out
}

// goFilesOf is the file, or the non-test Go files of the directory.
func goFilesOf(t *testing.T, root, rel string) []string {
	t.Helper()
	if strings.HasSuffix(rel, ".go") {
		return []string{rel}
	}
	ents, err := os.ReadDir(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if n := e.Name(); strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			out = append(out, rel+"/"+n)
		}
	}
	return out
}

// deleteVerbTexts are the texts of i18n.T and i18n.Tf in a file that carry
// a verb of deleting: "file<TAB>function" → the first such text.
func deleteVerbTexts(t *testing.T, root, rel string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, rel), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			s, ok := c.Fun.(*ast.SelectorExpr)
			if !ok || (s.Sel.Name != "T" && s.Sel.Name != "Tf") {
				return true
			}
			if x, ok := s.X.(*ast.Ident); !ok || x.Name != "i18n" {
				return true
			}
			for _, a := range c.Args {
				l, ok := a.(*ast.BasicLit)
				if !ok || l.Kind != token.STRING {
					continue
				}
				v, _ := strconv.Unquote(l.Value)
				key := rel + "\t" + fn.Name.Name
				if _, seen := out[key]; !seen && deleteVerb.MatchString(v) {
					out[key] = v
				}
			}
			return true
		})
	}
	return out
}
