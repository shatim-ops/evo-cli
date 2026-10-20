// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// engineCallsAllowed are the functions of internal/cli that may still call
// listProjects, with the reason: the commands that keep it (spec REQ-4:
// configure and project use) and those that become tools in PR 9 (REQ-11).
var engineCallsAllowed = map[string]string{
	"chooseProject":     "evo configure: выбор проекта при записи профиля",
	"resolveProjectArg": "evo project use: проект по имени",
	"newWhoamiCmd":      "evo whoami: до PR 9 (REQ-11), станет инструментом whoami",
	"keyScopeCheck":     "evo doctor: до PR 9 (REQ-11), станет инструментом doctor",
}

// engineMethods are the engine calls the commands of internal/cli made
// themselves before they called their tools (spec REQ-4): engine.Deploy,
// Status, Logs, Destroy, ListImages and Cloud.ListAvailabilityZones. A call
// of a method by one of these names, on any receiver, is refused.
var engineMethods = map[string]bool{"Deploy": true, "Status": true, "Logs": true, "Destroy": true,
	"ListImages": true, "ListAvailabilityZones": true}

// The engine commands call their tools, not the engine (spec REQ-4): no
// code of internal/cli (tests aside) calls engine.Deploy, Status, Logs,
// Destroy, ListImages or Cloud.ListAvailabilityZones, and listProjects only
// in the functions engineCallsAllowed names. A static check, by the AST,
// as codeEnvVars in docs_test.go reads the code.
func TestParity_NoEngineCallsInCLI(t *testing.T) {
	bad := engineCallsIn(t, ".")
	if len(bad) > 0 {
		t.Fatalf("internal/cli calls the engine where a command must call its tool (spec REQ-4):\n%s", strings.Join(bad, "\n"))
	}
}

// engineCallsIn lists the refused calls in the non-test Go files of dir.
func engineCallsIn(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var bad []string
	files := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files++
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				where := fset.Position(call.Pos()).String() + " (" + fn.Name.Name + ")"
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					if engineMethods[fun.Sel.Name] {
						bad = append(bad, where+": ."+fun.Sel.Name+"(…)")
					}
				case *ast.Ident:
					if fun.Name == "listProjects" {
						if _, ok := engineCallsAllowed[fn.Name.Name]; !ok {
							bad = append(bad, where+": listProjects(…)")
						}
					}
				}
				return true
			})
		}
	}
	if files == 0 {
		t.Fatalf("no Go files in %s", dir)
	}
	sort.Strings(bad)
	return bad
}

// The check sees a call: a file that calls eng.Status, as evo status did
// before PR 5, is refused (the mutation test of the check itself).
func TestParity_NoEngineCallsInCLI_SeesACall(t *testing.T) {
	dir := t.TempDir()
	src := "package cli\n\nfunc newStatusCmdOld(eng interface{ Status(int) error }) error {\n\treturn eng.Status(0)\n}\n\n" +
		"func projectsOld() { _, _ = listProjects(nil, \"\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "old.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := engineCallsIn(t, dir)
	if len(bad) != 2 || !strings.Contains(bad[0], ".Status(") || !strings.Contains(bad[1], "listProjects") {
		t.Fatalf("the check missed a call: %v", bad)
	}
}

// engineCommands names every engine command, its tool, what each flag
// gives the tool and why the rest are as they are: every flag of the
// command (its own, not the root's) gives an argument of the tool's schema
// or is CLIOnly with the reason; every argument of the schema (confirmed
// aside) has a flag or is in Omit with the reason. A command without an
// alias is the tool's own command, at the path its CLISpec names.
func TestParity_EngineCommands(t *testing.T) {
	reg := tools.New(nil)
	root := newRootCmd()
	paths := map[string]bool{}
	own := map[string]int{}
	for _, ec := range engineCommands {
		if paths[ec.Path] {
			t.Errorf("%s twice", ec.Path)
		}
		paths[ec.Path] = true
		tl, ok := reg.Lookup(ec.Tool)
		if !ok {
			t.Errorf("%s: no tool %s", ec.Path, ec.Tool)
			continue
		}
		cmd, _, err := root.Find(strings.Fields(ec.Path)[1:])
		if err != nil || cmd.CommandPath() != ec.Path {
			t.Errorf("%s: no such command (%v)", ec.Path, err)
			continue
		}
		if got := cmd.Annotations[toolAnnotation]; got != ec.Tool {
			t.Errorf("%s calls %q, want %s", ec.Path, got, ec.Tool)
		}
		want := strings.Join(strings.Fields("evo "+tl.CLI.Group+" "+tl.CLI.Verb), " ")
		switch {
		case ec.Alias == "":
			own[ec.Tool]++
			if ec.Path != want {
				t.Errorf("%s: the command of %s is %s by its CLISpec; an alias needs the reason", ec.Path, ec.Tool, want)
			}
		case ec.Path == want:
			t.Errorf("%s is the command of %s by its CLISpec, not an alias", ec.Path, ec.Tool)
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		given := map[string]bool{}
		for flag, arg := range ec.Flags {
			if _, ok := props[arg]; !ok {
				t.Errorf("%s --%s: %s has no argument %s", ec.Path, flag, ec.Tool, arg)
			}
			if cmd.LocalNonPersistentFlags().Lookup(flag) == nil {
				t.Errorf("%s: no flag --%s", ec.Path, flag)
			}
			given[arg] = true
		}
		for flag, why := range ec.CLIOnly {
			if strings.TrimSpace(why) == "" {
				t.Errorf("%s --%s: CLIOnly without the reason", ec.Path, flag)
			}
			if _, both := ec.Flags[flag]; both {
				t.Errorf("%s --%s: both a tool argument and CLIOnly", ec.Path, flag)
			}
		}
		cmd.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "help" {
				return
			}
			_, mapped := ec.Flags[f.Name]
			_, cliOnly := ec.CLIOnly[f.Name]
			if !mapped && !cliOnly {
				t.Errorf("%s --%s: gives no argument of %s and is not CLIOnly", ec.Path, f.Name, ec.Tool)
			}
		})
		for arg := range props {
			why, omitted := ec.Omit[arg]
			switch {
			case arg == "confirmed":
			case given[arg] && omitted:
				t.Errorf("%s: %s has a flag and is in Omit", ec.Path, arg)
			case given[arg]:
			case !omitted || strings.TrimSpace(why) == "":
				t.Errorf("%s: no flag for argument %s of %s, and it is not in Omit with the reason", ec.Path, arg, ec.Tool)
			}
		}
		for arg := range ec.Omit {
			if _, ok := props[arg]; !ok {
				t.Errorf("%s: Omit %s, which is no argument of %s", ec.Path, arg, ec.Tool)
			}
		}
	}
	for tool, n := range own {
		if n != 1 {
			t.Errorf("%s: %d commands without an alias", tool, n)
		}
	}
}

// evo status prints the answer of the status tool: with --json as is, in
// a table otherwise. The names of the zone (and of the VM) are the tool's
// now (spec REQ-4: withNames moved into the tool), so MCP gets them too.
func TestEngineCommands_StatusIsTheTool(t *testing.T) {
	w := withParityWorld(t)
	_, done := recordDeployment(t, w)
	defer done()
	newProcess()
	code, cmdOut, cmdErr := runCLI(t, "", "status", "--json")
	newProcess()
	callCode, callOut, _ := runCLI(t, "", "tools", "call", "status", "--json")
	if code != 0 || callCode != 0 || cmdOut != callOut {
		t.Fatalf("evo status --json ≠ evo tools call status --json:\n%s\n%s\n%s", cmdOut, cmdErr, callOut)
	}
	if !strings.Contains(cmdOut, `"zone_name": "ru.AZ-1"`) || !strings.Contains(cmdOut, `"cloud_checked": true`) {
		t.Errorf("no zone name or cloud check in the answer:\n%s", cmdOut)
	}
	newProcess()
	code, out, errOut := runCLI(t, "", "status")
	if code != 0 || !strings.Contains(out, "ru.AZ-1 (") || !strings.Contains(out, "In the cloud") || !strings.Contains(out, "evo-web") {
		t.Errorf("evo status: exit %d\n%s\n%s", code, out, errOut)
	}
	newProcess()
	code, out, _ = runCLI(t, "", "status", "--no-refresh")
	if code != 0 || strings.Contains(out, "In the cloud") || strings.Contains(out, "ru.AZ-1") {
		t.Errorf("evo status --no-refresh asked the cloud: exit %d\n%s", code, out)
	}
}
