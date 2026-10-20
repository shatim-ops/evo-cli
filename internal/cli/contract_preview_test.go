// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

const parityVM = "00000000-0000-4000-8000-000000000001"

// parityCloud is previewCloud plus the reads destroy performs before its
// preview. No address is attached, so the preview does not bill a floating IP.
type parityCloud struct{ previewCloud }

func (parityCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: parityVM, Name: "web", Status: "running"}}, nil
}

func (parityCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return nil, nil
}

// GetImage: the image of previewCloud with the login and key fields evo
// deploy fills before it asks (it refuses an image without a login).
func (parityCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: id, Name: "Ubuntu 22.04", MinDisk: 20, MetadataFields: []provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
		{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
	}}, nil
}

// TestContract_PreviewParity: the preview of every tool that asks for
// consent — Mutating, and the two that reveal a secret (tools.IsSensitive)
// — is one preview on the four surfaces (spec REQ-6): its command with
// --dry-run --json (preview.details), 'evo tools call' with --dry-run
// --json, MCP over stdio and MCP over HTTP. The cases come from the
// registry; the arguments of each are parityArgs, and a gated tool without
// an entry there fails the test, as does one whose preview any surface
// refuses. A tool without a command yet (tools.CLIPending) has the three
// other surfaces. The one difference allowed is H7 of the closed list of
// the HTTP mode (docs/mcp.md): credential fields masked, and named in
// "redacted". deploy is checked in both states of the directory: no
// deployment (the preview prices a new VM) and a recorded one (it updates
// that VM); destroy both by vm_id (evo vm delete) and of the recorded
// deployment (evo destroy). The engine commands (evo deploy, evo destroy,
// engineCommands) ask at the engine's gate, after its checks, with the
// tool's answer as the details (spec REQ-4). The command and 'evo tools
// call' print the same envelope, except for a preview the CLI shows in its
// own words (a tool's "preview" object, or the engine's lines of an engine
// command: summary and lines differ, the details do not).
func TestContract_PreviewParity(t *testing.T) {
	world := withParityWorld(t)
	fakeKubeBinaries(t)
	srv := parityServers(t)

	reg := tools.New(nil)
	commands := commandsByTool()
	var cases []previewCase
	var bad []string
	for _, tl := range reg.Tools() {
		if !tl.Mutating && !tools.IsSensitive(tl) {
			continue
		}
		args, ok := parityArgs[tl.Name]
		if !ok {
			bad = append(bad, tl.Name+": asks for consent and has no entry in parityArgs")
			continue
		}
		cases = append(cases, previewCase{name: tl.Name, tool: tl, args: args, setup: paritySetup[tl.Name]})
	}
	for name := range parityArgs {
		if tl, ok := reg.Lookup(name); !ok || !tl.Mutating && !tools.IsSensitive(tl) {
			bad = append(bad, name+": in parityArgs, but not a tool that asks for consent")
		}
	}
	if dep, ok := reg.Lookup("deploy"); ok {
		cases = append(cases, previewCase{name: "deploy (deployment recorded)", tool: dep,
			args: parityArgs["deploy"], setup: recordDeployment})
	}
	if d, ok := reg.Lookup("destroy"); ok {
		cases = append(cases, previewCase{name: "destroy (deployment recorded)", tool: d,
			args: map[string]any{}, setup: recordDeployment})
	}
	// The command line of each case that is an engine command.
	engineTree := map[string][]string{
		"deploy":                        {"deploy"},
		"deploy (deployment recorded)":  {"deploy"},
		"destroy (deployment recorded)": {"destroy"},
	}

	counts := map[string]int{}
	masked := map[string]string{} // case → the fields H7 masks over HTTP
	for _, c := range cases {
		args := make(map[string]any, len(c.args))
		for k, v := range c.args {
			args[k] = v
		}
		teardown := func() {}
		if c.setup != nil {
			extra, done := c.setup(t, world)
			for k, v := range extra {
				args[k] = v
			}
			teardown = done
		}
		cmd := commands[c.tool.Name]
		pc := parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args)
		if line, ok := engineTree[c.name]; ok {
			pc.tree, pc.engine = line, true
		}
		world.rt.takeWrites()
		why := comparePreviews(t, c.name, pc, srv, masked)
		teardown()
		if w := world.rt.takeWrites(); len(w) > 0 {
			bad = append(bad, c.name+": the preview wrote to the cloud: "+strings.Join(w, ", "))
		}
		if why != "" {
			bad = append(bad, why)
			continue
		}
		if c.name != c.tool.Name {
			continue // a second state of a tool already counted
		}
		switch {
		case tools.IsSensitive(c.tool):
			counts["sensitive"]++
		case pc.tree == nil:
			counts["mutating, no command yet (tools call)"]++
		default:
			counts["mutating with a command"]++
		}
	}
	keys := make([]string, 0, len(counts))
	total := 0
	for k, n := range counts {
		keys = append(keys, fmt.Sprintf("%s: %d", k, n))
		total += n
	}
	sort.Strings(keys)
	t.Logf("previews equal on every surface: %d (%s)", total, strings.Join(keys, "; "))
	names := make([]string, 0, len(masked))
	for name := range masked {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Logf("H7 over HTTP: %s masks %s", name, masked[name])
	}
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// The previews of provision and deploy carry the price and the names of
// the zone, the flavor and the image, read by the tool (spec REQ-7, owner's
// decision Q6): an agent approves a billable VM with the price the command
// shows, and 'evo vm create web --dry-run --json' gives what MCP gives,
// cost included. The fake has no price-calculation API: the estimate of
// the rate card with price_api_error, as deploy_plan gives it. In a
// directory that has a deployment, deploy updates its VM: no new VM, no
// preview of one.
func TestContract_PreviewPriceInTool(t *testing.T) {
	w := withParityWorld(t)
	srv := parityServers(t)
	writeFile(t, filepath.Join(w.dir, "docker-compose.yml"), "services:\n  web:\n    image: nginx:1.27\n    ports:\n      - \"80:80\"\n")
	for _, c := range []struct {
		tool, name, flavor string
		args               map[string]any
	}{
		{"provision", "web", "lowcost10-2-4", map[string]any{"name": "web"}},
		{"deploy", "evo-", "free-tier1", map[string]any{}}, // evo-<directory>, as evo deploy names it
	} {
		got := stdioDetails(t, srv.stdio, c.tool, c.args)
		var p engine.CreatePreview
		b, _ := json.Marshal(got["preview"])
		if err := json.Unmarshal(b, &p); err != nil || p.VM == nil {
			t.Fatalf("%s: no VM preview: %v", c.tool, got)
		}
		vm := p.VM
		if !strings.HasPrefix(p.Name, c.name) || vm.Zone.Name != "ru.AZ-1" || vm.Flavor.Name != c.flavor || vm.Flavor.VCPUs == 0 ||
			vm.Image.Name != "Ubuntu 22.04" || vm.Disk.SizeGB != 20 {
			t.Errorf("%s: names and sizes not read: %s", c.tool, b)
		}
		if p.Cost == nil || p.Cost.TotalMonthlyRub <= 0 || p.Cost.PriceAPIError == "" || p.CostNote != "evo rate-card estimate, not a bill" {
			t.Errorf("%s: not the rate-card estimate with price_api_error: %s", c.tool, b)
		}
	}

	newProcess()
	code, out, errOut := runCLI(t, "", "vm", "create", "web", "--dry-run", "--json")
	cli, ok := dryRunDetails(out)
	if code != 0 || !ok {
		t.Fatalf("evo vm create web --dry-run --json: exit %d %s %s", code, out, errOut)
	}
	if mcpPreview := stdioDetails(t, srv.stdio, "provision", map[string]any{"name": "web"}); !reflect.DeepEqual(cli, mcpPreview) {
		t.Errorf("evo vm create ≠ MCP provision: %s", valueDiff(cli, mcpPreview))
	}

	_, done := recordDeployment(t, w)
	defer done()
	if got := stdioDetails(t, srv.stdio, "deploy", map[string]any{}); got["status"] != "confirmation_required" || got["preview"] != nil {
		t.Errorf("deploy over a recorded deployment: %v", got)
	}
}

// unmaskHTTP takes back only what H7 masked and "redacted" names: no
// preview of the registry echoes a credential today, so the comparison of
// a masked answer is checked here.
func TestUnmaskHTTP(t *testing.T) {
	const mask = "[redacted over HTTP: start the server with --allow-secret-export]"
	stdio := map[string]any{"status": "confirmation_required", "user": map[string]any{"name": "app", "password": "p"},
		"users": []any{map[string]any{"password": "a"}, map[string]any{"password": "b"}}}
	h := map[string]any{"status": "confirmation_required", "user": map[string]any{"name": "app", "password": mask},
		"users":    []any{map[string]any{"password": mask}, map[string]any{"password": mask}},
		"redacted": []any{"user.password", "users[].password"}}
	got, why := unmaskHTTP(h, stdio)
	if why != "" || !reflect.DeepEqual(got, stdio) {
		t.Errorf("unmasked %v (%s), want %v", got, why, stdio)
	}
	// A field named but not masked, or masked but not named, is a difference.
	h["redacted"] = []any{"user.name"}
	if _, why := unmaskHTTP(h, stdio); why == "" {
		t.Error("user.name is named in redacted but not masked: no difference reported")
	}
	h["redacted"] = []any{"user.password"}
	if got, _ := unmaskHTTP(h, stdio); reflect.DeepEqual(got, stdio) {
		t.Error("users[].password is masked but not named: the answers compare equal")
	}
}

type previewCase struct {
	name  string
	tool  tools.Tool
	args  map[string]any
	setup func(t *testing.T, w parityWorld) (map[string]any, func())
}

// comparePreviews runs one case on the four surfaces and says what differs
// ("" when nothing does).
func comparePreviews(t *testing.T, name string, pc parityCommand, srv parityMCP, masked map[string]string) string {
	t.Helper()
	got := map[string]map[string]any{}
	var lines []string
	newProcess()
	callCode, callOut, callErr := runCLI(t, "", append([]string{"--dry-run", "--json"}, pc.call...)...)
	call, ok := dryRunDetails(callOut)
	if !ok {
		return fmt.Sprintf("%s: evo %s --dry-run --json gave no preview: exit %d out=%s err=%s",
			name, strings.Join(pc.call, " "), callCode, clipLine(callOut), clipLine(callErr))
	}
	got["tools"] = call
	if pc.tree != nil {
		newProcess()
		treeCode, treeOut, treeErr := runCLI(t, "", append([]string{"--dry-run", "--json"}, pc.tree...)...)
		tree, ok := dryRunDetails(treeOut)
		if !ok {
			return fmt.Sprintf("%s: evo %s --dry-run --json gave no preview: exit %d out=%s err=%s",
				name, strings.Join(pc.tree, " "), treeCode, clipLine(treeOut), clipLine(treeErr))
		}
		got["cli"] = tree
		_, structured := call["preview"]
		if pc.engine {
			// An engine command prints the engine's steps and warnings as
			// it always did (JSON lines with --json) and shows the preview
			// in the engine's words: the code and the details are the
			// tool's.
			if why := jsonImpure("", treeErr); why != "" {
				lines = append(lines, fmt.Sprintf("%s: evo %s --dry-run --json: %s", name, strings.Join(pc.tree, " "), why))
			}
			treeErr, callErr, structured = "", "", true
		}
		if treeCode != callCode || treeErr != callErr || !structured && treeOut != callOut {
			lines = append(lines, fmt.Sprintf("%s: evo %s ≠ evo %s (--dry-run --json):\n  tree: exit %d out=%s err=%s\n  call: exit %d out=%s err=%s",
				name, strings.Join(pc.tree, " "), strings.Join(pc.call, " "),
				treeCode, clipLine(treeOut), clipLine(treeErr), callCode, clipLine(callOut), clipLine(callErr)))
		}
	}
	got["stdio"] = stdioDetails(t, srv.stdio, pc.tool.Name, pc.args)
	got["http --allow-secret-export"] = httpDetails(t, srv.export, pc.tool.Name, pc.args)
	h := httpDetails(t, srv.http, pc.tool.Name, pc.args)
	if httpDisabled()[pc.tool.Name] {
		// H5: disabled over HTTP, before its preview.
		if msg, _ := h["error"].(string); h["status"] != "error" || !strings.Contains(msg, "is disabled over HTTP") {
			lines = append(lines, name+": http: not the refusal of H5: "+clipLine(fmt.Sprint(h)))
		}
	} else {
		if r, ok := h["redacted"]; ok {
			masked[name] = fmt.Sprint(r)
		}
		h, why := unmaskHTTP(h, got["stdio"])
		if why != "" {
			lines = append(lines, name+": http: "+why)
		}
		got["http"] = h
	}
	if why := parityDiff(name, got); why != "" {
		lines = append(lines, why)
	}
	return strings.Join(lines, "\n")
}

// unmaskHTTP is the HTTP answer as stdio gives it, where H7 is the
// difference: each field "redacted" names is masked over HTTP (and only
// those), and takes the stdio value back; "redacted" itself goes.
func unmaskHTTP(h, stdio map[string]any) (map[string]any, string) {
	raw, ok := h["redacted"]
	if !ok {
		return h, ""
	}
	out := deepCopy(h).(map[string]any)
	delete(out, "redacted")
	list, _ := raw.([]any)
	if len(list) == 0 {
		return out, fmt.Sprintf("redacted is %v, not a list of fields", raw)
	}
	var bad []string
	for _, p := range list {
		path, _ := p.(string)
		if why := restoreAt(out, stdio, strings.Split(path, ".")); why != "" {
			bad = append(bad, path+": "+why)
		}
	}
	return out, strings.Join(bad, "; ")
}

// restoreAt puts the stdio value back at the path (a "[]" suffix: each
// element of a list) after checking that HTTP masked it there.
func restoreAt(h, s any, path []string) string {
	key, each := strings.CutSuffix(path[0], "[]")
	hm, ok1 := h.(map[string]any)
	sm, ok2 := s.(map[string]any)
	if !ok1 || !ok2 {
		return "not an object on both transports"
	}
	if each {
		ha, ok1 := hm[key].([]any)
		sa, ok2 := sm[key].([]any)
		if !ok1 || !ok2 || len(ha) != len(sa) {
			return "not the same list on both transports"
		}
		for i := range ha {
			if len(path) == 1 {
				if !masked(ha[i]) {
					return "an element is not masked"
				}
				ha[i] = sa[i]
				continue
			}
			if why := restoreAt(ha[i], sa[i], path[1:]); why != "" {
				return why
			}
		}
		return ""
	}
	if len(path) > 1 {
		return restoreAt(hm[key], sm[key], path[1:])
	}
	if !masked(hm[key]) {
		return "named in redacted, but not masked"
	}
	hm[key] = deepCopy(sm[key])
	return ""
}

// masked: every string in v is the mask of the HTTP mode.
func masked(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.HasPrefix(x, "[redacted over HTTP")
	case []any:
		for _, e := range x {
			if !masked(e) {
				return false
			}
		}
		return len(x) > 0
	case map[string]any:
		for _, e := range x {
			if !masked(e) {
				return false
			}
		}
		return len(x) > 0
	}
	return false
}

func deepCopy(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// treeCommand is the command of a tool in the trees.
type treeCommand struct {
	group treeGroup
	cmd   treeCmd
}

// commandsByTool are the commands of the trees by tool name.
func commandsByTool() map[string]treeCommand {
	out := map[string]treeCommand{}
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			out[c.Tool] = treeCommand{group: g, cmd: c}
		}
	}
	return out
}

// parityMCP are the MCP servers of the parity tests, on the engines of the
// tools (toolEngine: the fake cloud of the world): over stdio, over HTTP as
// it starts by default, and over HTTP with --allow-secret-export (no H5,
// H6, H7: the answers as over stdio).
type parityMCP struct {
	stdio        *mcp.Server
	http, export *httptest.Server
}

func parityServers(t *testing.T) parityMCP {
	t.Helper()
	factory := func(o map[string]any) (*engine.Engine, error) {
		if o == nil {
			o = map[string]any{}
		}
		return toolEngine(o)
	}
	serve := func(opts mcp.HTTPOptions) *httptest.Server {
		opts.BearerToken = "parity-token"
		h := mcp.NewHTTPHandler(mcp.New(factory, "test"), opts)
		ts := httptest.NewServer(h)
		t.Cleanup(func() {
			h.Close()
			ts.Close()
		})
		return ts
	}
	return parityMCP{stdio: mcp.New(factory, "test"), http: serve(mcp.HTTPOptions{}),
		export: serve(mcp.HTTPOptions{AllowSecretExport: true})}
}

// httpDisabled are the tools the HTTP mode disables without
// --allow-secret-export (H5 of mcp.HTTPRestrictions): they answer a refusal
// there instead of their preview.
func httpDisabled() map[string]bool {
	out := map[string]bool{}
	for _, r := range mcp.HTTPRestrictions {
		if r.ID == "H5" {
			for _, name := range r.Tools {
				out[name] = true
			}
		}
	}
	return out
}

// fakeKubeBinaries puts kubectl and helm on PATH: over HTTP, k8s_kubectl,
// k8s_apply and k8s_helm refuse when the server has none (H1), before the
// preview. The previews never run them.
func fakeKubeBinaries(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"kubectl", "helm"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// parityWorld is the directory of the parity tests: the working directory
// (the state of deploy), HOME and the configuration; rt is the fake of
// every API, which also counts the writes that reach it.
type parityWorld struct {
	dir string
	rt  *parityRT
}

// withParityWorld: a project, a zone and an image configured, a token
// without IAM, the reads of the previews answered by previewCloud and the
// rest of the compute API, and every other API, by parityRT.
func withParityWorld(t *testing.T) parityWorld {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLOUDRU_CONFIG_DIR", home)
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	t.Setenv("CLOUDRU_REGION", pvZone)
	t.Setenv("CLOUDRU_IMAGE_ID", pvImage)
	t.Setenv("CLOUDRU_FLAVOR", "")
	t.Setenv("CLOUDRU_DISK_SIZE", "")
	t.Setenv("EVO_LANG", "en")
	t.Chdir(home)
	rt := &parityRT{addon: &addonAPI{}}
	oldRT := http.DefaultTransport
	http.DefaultTransport = rt
	compute := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
	compute.SetToken("fake-token")
	old := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		if o == nil {
			o = map[string]any{}
		}
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth, e.Cloud = previewToken{}, parityCloud{previewCloud{CloudAPI: compute}}
		return e, nil
	}
	t.Cleanup(func() { toolEngine, http.DefaultTransport = old, oldRT })
	return parityWorld{dir: home, rt: rt}
}

func stdioDetails(t *testing.T, s *mcp.Server, name string, args map[string]any) map[string]any {
	t.Helper()
	flagJSON, flagDryRun, flagYes = false, false, false
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	out := &bytes.Buffer{}
	if err := s.Run(bytes.NewReader(append(body, '\n')), out); err != nil {
		t.Fatalf("%s stdio: %v", name, err)
	}
	return toolTextDetails(t, name, out.Bytes())
}

func httpDetails(t *testing.T, ts *httptest.Server, name string, args map[string]any) map[string]any {
	t.Helper()
	flagJSON, flagDryRun, flagYes = false, false, false
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer parity-token")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return toolTextDetails(t, name+" http", raw)
}

func toolTextDetails(t *testing.T, name string, raw []byte) map[string]any {
	t.Helper()
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &resp) != nil || resp.Error != nil || len(resp.Result.Content) == 0 {
		t.Fatalf("%s: not a tool result: %s", name, clipLine(string(raw)))
	}
	var obj map[string]any
	if json.Unmarshal([]byte(resp.Result.Content[0].Text), &obj) != nil {
		t.Fatalf("%s: tool text is not an object: %s", name, clipLine(resp.Result.Content[0].Text))
	}
	// docs/mcp.md «Подтверждения»: the server adds approval to every
	// confirmation_required answer. The CLI envelope does not. The field is
	// not part of preview.details.
	delete(obj, "approval")
	return obj
}

func parityDiff(name string, got map[string]map[string]any) string {
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	baseName := keys[0]
	base := got[baseName]
	if base == nil {
		return name + ": " + baseName + " produced no preview"
	}
	var lines []string
	for _, k := range keys[1:] {
		other := got[k]
		if other == nil {
			lines = append(lines, name+": "+k+" produced no preview")
			continue
		}
		bk, ok := keySet(base), keySet(other)
		if !reflect.DeepEqual(bk, ok) {
			lines = append(lines, name+": key set "+baseName+" vs "+k+": "+strings.Join(bk, ",")+" | "+strings.Join(ok, ","))
			continue
		}
		if !reflect.DeepEqual(base, other) {
			lines = append(lines, name+": values differ "+baseName+" vs "+k+": "+valueDiff(base, other))
		}
	}
	return strings.Join(lines, "\n")
}

func valueDiff(a, b map[string]any) string {
	var parts []string
	for _, k := range keySet(a) {
		if reflect.DeepEqual(a[k], b[k]) {
			continue
		}
		am, aok := a[k].(map[string]any)
		bm, bok := b[k].(map[string]any)
		if aok && bok {
			extra := ""
			if !reflect.DeepEqual(keySet(am), keySet(bm)) {
				extra = " keys " + strings.Join(keySet(am), ",") + " | " + strings.Join(keySet(bm), ",") + " "
			}
			parts = append(parts, k+"{"+extra+valueDiff(am, bm)+"}")
			continue
		}
		ab, _ := json.Marshal(a[k])
		bb, _ := json.Marshal(b[k])
		parts = append(parts, k+" ["+clipLine(string(ab))+" | "+clipLine(string(bb))+"]")
	}
	return strings.Join(parts, "; ")
}

func keySet(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
