// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// engineCallsAllowed are the functions of internal/cli that may still call
// listProjects, with the reason: the commands that keep it (spec REQ-4:
// configure and project use). whoami and doctor count the projects in
// their tools (REQ-11).
var engineCallsAllowed = map[string]string{
	"chooseProject":     "evo configure: выбор проекта при записи профиля",
	"resolveProjectArg": "evo project use: проект по имени",
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

// evo deploy is an action and the group of the deploy wizard at once
// (spec.md R7): 'evo deploy' still deploys (here: no compose file), its
// subcommands are plan, apply and list, and a word that is none of them is
// "unknown command" (exit 1, USAGE), as it was with cobra.NoArgs, now with
// a suggestion.
func TestEngineCommands_DeployGroupR7(t *testing.T) {
	setupCLI(t)
	t.Setenv("EVO_LANG", "en")
	code, _, stderr := runCLI(t, "", "--json", "deploy", "--dry-run")
	if c, reason, _ := cliJSONError(t, stderr); code != 1 || c != 1 || reason != "COMPOSE_NOT_FOUND" {
		t.Errorf("evo deploy --dry-run without a compose file: exit %d, %s", code, stderr)
	}
	for _, typo := range []string{"pln", "aply", "lst", "status"} {
		newProcess()
		code, _, stderr := runCLI(t, "", "--json", "deploy", typo)
		c, reason, msg := cliJSONError(t, stderr)
		if code != 1 || c != 1 || reason != "USAGE" || !strings.Contains(msg, `unknown command "`+typo+`" for "evo deploy"`) {
			t.Errorf("evo deploy %s: exit %d, %s", typo, code, stderr)
		}
	}
	newProcess()
	if _, _, stderr := runCLI(t, "", "deploy", "pln"); !strings.Contains(stderr, "did you mean plan?") {
		t.Errorf("evo deploy pln: no suggestion: %s", stderr)
	}
	newProcess()
	code, stdout, _ := runCLI(t, "", "deploy", "--help")
	for _, want := range []string{"evo deploy [flags]", "evo deploy [command]", "  apply ", "  list ", "  plan ", "--compose-file", "--non-interactive"} {
		if code != 0 || !strings.Contains(stdout, want) {
			t.Errorf("evo deploy --help: exit %d, no %q:\n%s", code, want, stdout)
		}
	}
	for _, verb := range []string{"plan", "apply", "list"} {
		newProcess()
		if code, stdout, _ := runCLI(t, "", "deploy", verb, "--help"); code != 0 || !strings.Contains(stdout, "evo deploy "+verb) {
			t.Errorf("evo deploy %s --help: exit %d\n%s", verb, code, stdout)
		}
	}
}

// One gate for deploy (owner's decision Q1, spec REQ-12): a repeated
// evo deploy that would update the VM of the deployment asks for consent,
// as MCP deploy does — without a terminal and without --yes it is exit 3
// and nothing is done (it was exit 0 and an update); --dry-run and --json
// show the tool's answer (update, vm_id, compose_changed, no price); --yes
// goes on. The same compose file running there: evo deploy asks nothing
// (nothing to do), MCP and evo tools call ask as in v1.1 and say
// unchanged: true (bringing them together waits for v2.0).
func TestEngineCommands_RepeatedDeployGate(t *testing.T) {
	w := withParityWorld(t)
	srv := parityServers(t)
	_, done := recordDeployment(t, w) // compose hash "0000": the file changed
	defer done()
	recordIs := func(t *testing.T, hash string) {
		t.Helper()
		sf, err := state.Read(state.DefaultPath(w.dir))
		if a := sf.ActiveDeployment(); err != nil || a == nil || a.ComposeHash != hash || a.Status != "running" {
			t.Fatalf("the record changed: %+v (%v)", a, err)
		}
	}

	newProcess()
	code, stdout, stderr := runCLI(t, "", "deploy")
	if code != 3 || !strings.Contains(stderr, "About to update VM evo-web") || !strings.Contains(stderr, "--yes") || strings.Contains(stdout, "[4/5]") {
		t.Fatalf("evo deploy without --yes and a terminal: exit %d\n%s\n%s", code, stdout, stderr)
	}
	recordIs(t, "0000")

	newProcess()
	code, stdout, stderr = runCLI(t, "", "deploy", "--json")
	var env struct {
		Status  string `json:"status"`
		Preview struct {
			Details map[string]any `json:"details"`
		} `json:"preview"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || code != 3 || env.Status != "confirmation_required" {
		t.Fatalf("evo deploy --json: exit %d\n%s\n%s", code, stdout, stderr)
	}
	d := env.Preview.Details
	if d["update"] != true || d["vm_id"] != parityVM || d["compose_changed"] != true || d["preview"] != nil || d["status"] != "confirmation_required" {
		t.Errorf("details are not the update the tool answers: %v", d)
	}
	if c, reason, _ := cliJSONError(t, stderr); c != 3 || reason != "CONFIRMATION_REQUIRED" {
		t.Errorf("evo deploy --json: error %d %s", c, reason)
	}
	recordIs(t, "0000")

	newProcess()
	code, stdout, _ = runCLI(t, "", "deploy", "--dry-run", "--json")
	cli, ok := dryRunDetails(stdout)
	if code != 0 || !ok {
		t.Fatalf("evo deploy --dry-run --json: exit %d\n%s", code, stdout)
	}
	if mcp := stdioDetails(t, srv.stdio, "deploy", map[string]any{}); !reflect.DeepEqual(cli, mcp) {
		t.Errorf("evo deploy --dry-run ≠ MCP deploy: %s", valueDiff(cli, mcp))
	}
	recordIs(t, "0000")

	newProcess()
	code, stdout, stderr = runCLI(t, "", "deploy", "--yes")
	if code == 3 || !strings.Contains(stderr, "Confirmed: update VM evo-web") || !strings.Contains(stdout, "[4/5]") {
		t.Fatalf("evo deploy --yes did not go past the gate: exit %d\n%s\n%s", code, stdout, stderr)
	}

	// The same compose file, running: nothing to do, nothing asked.
	compose, err := os.ReadFile(filepath.Join(w.dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	sf, _ := state.Read(state.DefaultPath(w.dir))
	rec := sf.ActiveDeployment()
	rec.ComposeHash, rec.Status = fmt.Sprintf("%x", sha256.Sum256(compose)), "running"
	sf.UpsertDeployment(*rec)
	if err := state.Write(state.DefaultPath(w.dir), sf); err != nil {
		t.Fatal(err)
	}
	newProcess()
	if code, stdout, stderr := runCLI(t, "", "deploy"); code != 0 || !strings.Contains(stdout, "No changes detected") {
		t.Errorf("unchanged evo deploy: exit %d\n%s\n%s", code, stdout, stderr)
	}
	newProcess()
	code, stdout, _ = runCLI(t, "", "tools", "call", "deploy", "--json")
	if code != 3 || !strings.Contains(stdout, `"unchanged": true`) {
		t.Errorf("unchanged evo tools call deploy: exit %d, want 3 as in v1.1\n%s", code, stdout)
	}
	if got := stdioDetails(t, srv.stdio, "deploy", map[string]any{}); got["status"] != "confirmation_required" || got["unchanged"] != true ||
		got["compose_changed"] != false || got["vm_id"] != parityVM {
		t.Errorf("unchanged MCP deploy: %v", got)
	}
	// --force redeploys the same file: asked again.
	newProcess()
	code, _, stderr = runCLI(t, "", "deploy", "--force")
	if code != 3 || !strings.Contains(stderr, "redeployed because of --force") {
		t.Errorf("evo deploy --force on the same file: exit %d\n%s", code, stderr)
	}
}

// evo deploy --dry-run --json before a new VM: its details are the deploy
// tool's answer (spec REQ-4, REQ-6), and that answer still has every key
// the details had in v1.1 — the preview the engine makes itself, which is
// that of v1.1 (Gate nil) — so a script reading .preview.details.cost or
// .subnet_id goes on working (deprecated keys, removed in v2.0: preview).
// Only keys are added.
func TestDeployDryRunKeepsV11Keys(t *testing.T) {
	w := withParityWorld(t)
	writeFile(t, filepath.Join(w.dir, "docker-compose.yml"), "services:\n  web:\n    image: nginx:1.27\n    ports:\n      - \"80:80\"\n")
	eng, err := toolEngine(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	eng.Printer = output.NewWithWriters(&out, &errOut, output.ModeJSON, false)
	if err := eng.Deploy(context.Background(), engine.DeployOptions{ComposeFile: eng.Config.ComposeFile, Flavor: eng.Config.Flavor,
		Region: eng.Config.Region, DryRun: true}); err != nil {
		t.Fatalf("v1.1 preview: %v\n%s", err, errOut.String())
	}
	v11, ok := dryRunDetails(out.String())
	if !ok {
		t.Fatalf("v1.1 preview: no details:\n%s", out.String())
	}
	newProcess()
	code, stdout, stderr := runCLI(t, "", "deploy", "--dry-run", "--json")
	now, ok := dryRunDetails(stdout)
	if code != 0 || !ok {
		t.Fatalf("evo deploy --dry-run --json: exit %d\n%s\n%s", code, stdout, stderr)
	}
	have := keyPaths("", now)
	var missing []string
	for p := range keyPaths("", v11) {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("keys of v1.1 missing from the details: %v\nv1.1: %v\nnow: %v", missing, v11, now)
	}
	for _, k := range []string{"project_id", "project_from", "subnet_id", "security_group", "floating_ip", "open_ports", "open_udp_ports"} {
		if !reflect.DeepEqual(v11[k], now[k]) {
			t.Errorf("%s: %v in v1.1, %v now", k, v11[k], now[k])
		}
	}
	if mcp := stdioDetails(t, parityServers(t).stdio, "deploy", map[string]any{}); !reflect.DeepEqual(now, mcp) {
		t.Errorf("details ≠ MCP deploy: %s", valueDiff(now, mcp))
	}
}

// keyPaths are the paths of every key in v ("cost.total_monthly_rub",
// "vm.flavor.name"; "[]" for the elements of a list).
func keyPaths(prefix string, v any) map[string]bool {
	out := map[string]bool{}
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out[p] = true
			for q := range keyPaths(p, e) {
				out[q] = true
			}
		}
	case []any:
		for _, e := range x {
			for q := range keyPaths(prefix+"[]", e) {
				out[q] = true
			}
		}
	}
	return out
}

// listImagesCloud: the shared catalogue has A and B, the project B (seen
// twice) and its own C.
type listImagesCloud struct{ previewCloud }

var listImagesShared = []*provision.Image{
	{ID: "00000000-0000-0000-0000-0000000000a0", Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Type: "public", Public: true, MinDisk: 10, OS: "linux",
		AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: pvZone, AvailabilityZoneName: "ru.AZ-1", State: "ready", Size: 3, Uploaded: true}}},
	{ID: "00000000-0000-0000-0000-0000000000b0", Name: "Debian-12", Type: "public", Status: "active"},
}

func (listImagesCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return listImagesShared, nil
}

func (listImagesCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return []*provision.Image{listImagesShared[1], {ID: "00000000-0000-0000-0000-0000000000c0", Name: "my-app", Type: "private", ProjectID: pvProject}}, nil
}

// evo list-images (deprecated) calls list_images, which adds the
// project's own images: --json is the array it always was — the shared
// images first, each as before and in the same order — with the project's
// images after them (an addition only).
func TestEngineCommands_ListImagesOnlyAdds(t *testing.T) {
	withCloud(t, listImagesCloud{})
	code, stdout, stderr := runCLI(t, "", "list-images", "--json")
	if code != 0 || !strings.Contains(stderr, "deprecated") {
		t.Fatalf("evo list-images --json: exit %d\n%s\n%s", code, stdout, stderr)
	}
	var v11 bytes.Buffer
	// What v1.1 printed: engine.ListImages, Printer.JSON of the shared list.
	_ = output.NewWithWriter(&v11, output.ModeJSON, false).JSON(listImagesShared)
	var old, got []json.RawMessage
	if err := json.Unmarshal(v11.Bytes(), &old); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil || len(got) != 3 {
		t.Fatalf("not an array of 3 images: %s", stdout)
	}
	for i := range old {
		if string(got[i]) != string(old[i]) {
			t.Errorf("image %d changed:\n%s\nwas\n%s", i, got[i], old[i])
		}
	}
	if !strings.Contains(string(got[2]), `"name": "my-app"`) {
		t.Errorf("the project's image is not last: %s", got[2])
	}
}

// whoami and doctor over HTTP (spec REQ-11, HTTPRestrictions H10): whoami
// answers as over stdio, the key ID masked and no path of the server in
// it; doctor is refused, since its checks name the server's files and
// network.
func TestEngineCommands_WhoamiDoctorOverHTTP(t *testing.T) {
	w, _ := withJSONWorld(t)
	srv := parityServers(t)
	stdio, overHTTP := stdioDetails(t, srv.stdio, "whoami", map[string]any{}), httpDetails(t, srv.http, "whoami", map[string]any{})
	if !reflect.DeepEqual(stdio, overHTTP) || overHTTP["key_id"] != "0000****0000" {
		t.Errorf("whoami over HTTP ≠ stdio, or the key ID is not masked: %s", valueDiff(stdio, overHTTP))
	}
	if b, _ := json.Marshal(overHTTP); strings.Contains(string(b), w.dir) || strings.Contains(string(b), "00000000000000000000000000000000") {
		t.Errorf("whoami over HTTP names a path or the key ID: %s", b)
	}
	got := httpDetails(t, srv.http, "doctor", map[string]any{})
	if msg, _ := got["error"].(string); got["reason"] != "USAGE" || !strings.Contains(msg, "doctor is disabled over HTTP") || got["checks"] != nil {
		t.Errorf("doctor over HTTP: %v", got)
	}
	if got := stdioDetails(t, srv.stdio, "doctor", map[string]any{}); got["ok"] != true || got["checks"] == nil {
		t.Errorf("doctor over stdio: %v", got)
	}
}

// FB140-6: the token of the parity worlds expires an hour after the
// world's stopped clock, so whoami tells the same TTL on every call and
// over every transport, unmasked. A world on a moving clock or a token on
// the wall clock answers anything but 3600.
func TestParityWorld_TokenTTLIsExact(t *testing.T) {
	withJSONWorld(t)
	srv := parityServers(t)
	for name, got := range map[string]map[string]any{
		"stdio": stdioDetails(t, srv.stdio, "whoami", map[string]any{}),
		"http":  httpDetails(t, srv.http, "whoami", map[string]any{}),
	} {
		if ttl, _ := got["token_ttl_seconds"].(float64); ttl != 3600 {
			t.Errorf("whoami over %s: token_ttl_seconds = %v, want 3600 — the world's clock moves or its token reads the wall clock", name, got["token_ttl_seconds"])
		}
	}
}
