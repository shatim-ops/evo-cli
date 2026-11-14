// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The console of a VM leaves evo nowhere (D110, variant (a); brief
// docs/internal/briefs/vm-console.md §5 and §5.2; «Риск», conclusions 229
// and 240). The world puts a mark into every console field of every answer
// of the VM paths of the compute adapter — the card, an item of the list,
// the answer of PUT, the answer of a create — and into the body of a
// refusal of those paths; every tool of the parity world runs, as its
// command (--json and the human output) and as its tool (the preview and
// the confirmed call); no mark is in what they print or answer, nor in a
// file under the home: the state, the records of the calls, the journal,
// its copy by evo diag export, the file of EVO_RECORD_ANSWERS. The fields
// are the one list of provision (consoleFields): a field added there is
// served here with no change of this test.

// vmPathTools are the tools whose calls go along the VM paths of the
// compute adapter in the parity world (brief §5.2: the list follows the
// paths, not the class of the tool). A tool that reaches a VM path and is
// not here fails TestSecrets_VMConsoleFieldsLeaveNoSurface, and so does a
// tool here that no longer reaches one.
var vmPathTools = map[string]bool{
	"backup_create": true,
	"deploy":        true,
	"deploy_apply":  true,
	"deploy_plan":   true,
	"destroy":       true,
	"disk_attach":   true,
	"disk_detach":   true,
	"fip_attach":    true,
	"logs":          true,
	"provision":     true,
	"status":        true,
	"tg_create":     true,
	"vm_get":        true,
	"vm_list":       true,
	"vm_reboot":     true,
	"vm_resize":     true,
	"vm_start":      true,
	"vm_stop":       true,
}

// consoleFieldsOfProvision reads the list of console fields from the
// source of package provision: the one point the brief names (§5, item 4),
// declared outside its tests.
func consoleFieldsOfProvision(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "provision")
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	var where []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "consoleFields" || len(vs.Values) != 1 {
				return true
			}
			where = append(where, name)
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("provision/%s: consoleFields is not a literal list", name)
			}
			for _, el := range lit.Elts {
				b, ok := el.(*ast.BasicLit)
				if !ok || b.Kind != token.STRING {
					t.Fatalf("provision/%s: an element of consoleFields is not a string literal", name)
				}
				s, _ := strconv.Unquote(b.Value)
				fields = append(fields, s)
			}
			return false
		})
	}
	if len(where) != 1 || len(fields) == 0 {
		t.Fatalf("consoleFields is declared in %d files of package provision outside its tests (%v), with %d fields: the list of the console fields of a VM is one point of provision (brief vm-console.md §5, item 4)", len(where), where, len(fields))
	}
	return fields
}

// consoleMark is the mark of a console field: letters, digits and
// underscores only, so that no escaping of JSON, a URL or a shell changes
// it.
func consoleMark(field string) string { return "CONSOLEMARK7f2e_" + field }

// vmPath is a VM path of the compute adapter: the list and a create
// (/vms), a VM (/vms/{id}) and its actions (/vms/{id}/…).
var vmPath = regexp.MustCompile(`^/api/v1(\.1)?/vms(/|$)`)

// consoleWorld serves the console fields with marks on the VM paths: in
// the answers of the world (refuse 0) or in a refusal of status refuse —
// of every call, or of the writes only (refuseWrites), so that a write
// whose reads come first is refused too. It counts, per case, which paths
// it served marked.
type consoleWorld struct {
	base         http.RoundTripper
	fields       []string
	mu           sync.Mutex
	refuse       int
	refuseWrites bool
	// plain: the refusal is text, not JSON — field=value pairs, as a proxy
	// or a log line of a service could give (release plan, «Процесс · 1.9
	// (5)»: the text outside JSON is served too).
	plain bool
	// served are the paths served marked since the last take, as
	// "GET card", "GET list", "PUT card", "POST create", "<status> …".
	served map[string]bool
	// ever is every kind served in the test.
	ever map[string]bool
}

func (c *consoleWorld) consoleObject() map[string]any {
	o := map[string]any{}
	for _, f := range c.fields {
		o[f] = "https://console.example/" + consoleMark(f) + "?token=" + consoleMark(f)
	}
	return o
}

func (c *consoleWorld) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for k := range c.served {
		out = append(out, k)
	}
	c.served = nil
	sort.Strings(out)
	return out
}

func (c *consoleWorld) note(kind string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.served == nil {
		c.served = map[string]bool{}
	}
	if c.ever == nil {
		c.ever = map[string]bool{}
	}
	c.served[kind], c.ever[kind] = true, true
}

// kindOf names the VM path of r.
func kindOf(r *http.Request) string {
	p := strings.TrimSuffix(r.URL.Path, "/")
	switch rest := vmPath.ReplaceAllString(p, ""); {
	case rest == "" && r.Method == http.MethodPost:
		return "POST create " + p
	case rest == "":
		return r.Method + " list"
	case !strings.Contains(rest, "/"):
		return r.Method + " card"
	default:
		return r.Method + " action"
	}
}

func (c *consoleWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "compute.api.cloud.ru" || !vmPath.MatchString(r.URL.Path) {
		return c.base.RoundTrip(r)
	}
	kind := kindOf(r)
	c.mu.Lock()
	refuse := c.refuse
	if c.refuseWrites && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		refuse = 0
	}
	c.mu.Unlock()
	if refuse != 0 {
		// The body of a refusal: the fields at its top and inside the VM
		// it names, as a JSON a service of the platform could give.
		body := c.consoleObject()
		body["message"] = "refused"
		body["vm"] = c.consoleObject()
		b, _ := json.Marshal(body)
		if c.plain {
			var kv []string
			for _, f := range c.fields {
				kv = append(kv, f+"="+consoleMark(f))
			}
			b = []byte("refused: " + strings.Join(kv, " "))
		}
		c.note(fmt.Sprintf("%d %s", refuse, kind))
		return answerWith(refuse, b), nil
	}
	if kind == "GET list" {
		// The list of the project: the VM of parityCloud, read through the
		// adapter here (consoleCloud).
		b, _ := json.Marshal(map[string]any{"items": []any{c.marked(map[string]any{"id": parityVM, "name": "web", "state": "running"})}, "total": 1})
		c.note(kind)
		return answerWith(http.StatusOK, b), nil
	}
	resp, err := c.base.RoundTrip(r)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var v any
	if len(bytes.TrimSpace(b)) == 0 || json.Unmarshal(b, &v) != nil {
		resp.Body = io.NopCloser(bytes.NewReader(b))
		return resp, nil
	}
	n := 0
	switch x := v.(type) {
	case map[string]any:
		if items, ok := x["items"].([]any); ok {
			for _, it := range items {
				if o, ok := it.(map[string]any); ok {
					c.marked(o)
					n++
				}
			}
		} else if _, ok := x["id"]; ok {
			c.marked(x)
			n++
		}
	case []any:
		for _, it := range x {
			if o, ok := it.(map[string]any); ok {
				c.marked(o)
				n++
			}
		}
	}
	if n > 0 {
		c.note(kind)
	}
	nb, _ := json.Marshal(v)
	resp.Body = io.NopCloser(bytes.NewReader(nb))
	resp.ContentLength = int64(len(nb))
	resp.Header.Del("Content-Length")
	return resp, nil
}

// marked is o with the console fields.
func (c *consoleWorld) marked(o map[string]any) map[string]any {
	for k, v := range c.consoleObject() {
		o[k] = v
	}
	return o
}

func answerWith(status int, body []byte) *http.Response {
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "0")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	return w.Result()
}

// consoleCloud is the cloud of the JSON world with the list of VMs read
// through the adapter: parityCloud answers it from Go, and an item of the
// list would never carry the fields.
type consoleCloud struct{ jsonCloud }

func (c consoleCloud) ListVMsInProject(ctx context.Context, project string) ([]*provision.VM, error) {
	if c.fake.gone() {
		return nil, nil
	}
	return c.parityCloud.previewCloud.CloudAPI.ListVMsInProject(ctx, project)
}

// consoleLeaks names the marks in text.
func consoleLeaks(text string, fields []string) []string {
	var out []string
	for _, f := range fields {
		if leaks(text, consoleMark(f)) != "" {
			out = append(out, f)
		}
	}
	return out
}

// consoleInFiles walks every file under root and reports each that holds
// a mark.
func consoleInFiles(root string, fields []string) (bad []string, files int) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if l := consoleLeaks(string(b), fields); len(l) > 0 {
			rel, _ := filepath.Rel(root, p)
			bad = append(bad, fmt.Sprintf("file %s holds %v", rel, l))
		}
		return nil
	})
	return bad, files
}

func TestSecrets_VMConsoleFieldsLeaveNoSurface(t *testing.T) { walkConsoleWorld(t, false) }

// The same walk with the refusals as text outside JSON: vnc_url=… in a body
// that is not JSON is not hidden yet (hideConsole reads JSON keys only).
func TestSecrets_VMConsoleFieldsOutsideJSON(t *testing.T) { walkConsoleWorld(t, true) }

func walkConsoleWorld(t *testing.T, plain bool) {
	fields := consoleFieldsOfProvision(t)
	w, fake := withJSONWorld(t)
	answers := filepath.Join(w.dir, "answers.jsonl")
	t.Setenv(httpretry.EnvRecordAnswers, answers)
	if code, out, errOut := runCLI(t, "", "diag", "on"); code != 0 {
		t.Fatalf("diag on: exit %d: %s%s", code, out, errOut)
	}

	world := &consoleWorld{base: w.rt, fields: fields, plain: plain}
	oldBase := httpretry.DefaultBase
	httpretry.DefaultBase = world
	t.Cleanup(func() { httpretry.DefaultBase = oldBase })
	prev := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := prev(o)
		if err != nil {
			return nil, err
		}
		jc := e.Cloud.(jsonCloud)
		// The compute client of production: its transport records the
		// answers (EVO_RECORD_ANSWERS) and goes through the world.
		a := provision.NewCloudRuAdapter(&http.Client{Transport: httpretry.UserAgent(nil)}, "")
		a.SetToken("fake-token")
		a.Backoff = time.Millisecond
		jc.parityCloud.previewCloud.CloudAPI = a
		e.Cloud = consoleCloud{jc}
		return e, nil
	}
	t.Cleanup(func() { toolEngine = prev })

	cases, _, bad := jsonCases(t)
	reached := map[string]bool{}
	walked := 0
	run := func(c jsonCase, mode string) {
		name := c.path
		if c.state != "" {
			name += " (" + c.state + ")"
		}
		name += ", " + mode
		check := func(what, text string) {
			if l := consoleLeaks(text, fields); len(l) > 0 {
				bad = append(bad, fmt.Sprintf("%s: %s holds the console fields %v: %s", name, what, l, clipLine(text)))
			}
		}
		files := func(after string) {
			b, n := consoleInFiles(w.dir, fields)
			walked = n
			for _, x := range b {
				bad = append(bad, name+", after "+after+": "+x)
			}
		}
		prepare := func() (map[string]any, func()) {
			args := copyArgs(c.args)
			if c.setup == nil {
				return args, func() {}
			}
			extra, done := c.setup(t, w)
			for k, v := range extra {
				args[k] = v
			}
			return args, done
		}
		line := func(args map[string]any) []string {
			if c.line != nil {
				return c.line
			}
			cmd := commandsByTool()[c.tool.Name]
			return parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
		}
		var served []string
		for _, surface := range []string{"command --json", "command", "tool preview", "tool"} {
			args, done := prepare()
			fake.reset()
			newProcess()
			switch surface {
			case "command --json", "command":
				flags := []string{"--yes"}
				if surface == "command --json" {
					flags = append([]string{"--json"}, flags...)
				}
				_, out, errOut := runCLI(t, c.stdin, append(flags, line(args)...)...)
				check(surface+" stdout", out)
				check(surface+" stderr", errOut)
			default:
				if surface == "tool" {
					args["confirmed"] = true
				}
				flagJSON, flagDryRun, flagYes = false, false, false
				tl, _ := newRegistry().Lookup(c.tool.Name)
				text, _ := tl.Handler(context.Background(), args)
				check(surface+" answer", text)
			}
			served = append(served, world.take()...)
			files(surface)
			done()
			forgetRun(w)
		}
		if len(served) > 0 {
			reached[c.tool.Name] = true
		}
	}
	for _, c := range cases {
		run(c, "answers")
	}
	refusals := []struct {
		status int
		writes bool
	}{
		{http.StatusForbidden, false}, {http.StatusUnprocessableEntity, false}, {http.StatusInternalServerError, false},
		{http.StatusForbidden, true}, {http.StatusUnprocessableEntity, true},
	}
	for _, f := range refusals {
		world.mu.Lock()
		world.refuse, world.refuseWrites = f.status, f.writes
		world.mu.Unlock()
		mode := fmt.Sprintf("refused %d", f.status)
		if f.writes {
			mode += " on writes"
		}
		for _, c := range cases {
			if vmPathTools[c.tool.Name] {
				run(c, mode)
			}
		}
	}
	world.mu.Lock()
	world.refuse, world.refuseWrites = 0, false
	world.mu.Unlock()

	// A create whose answer evo does not know: its body goes to the error.
	world.base = createOfUnknownShape{world.base, world}
	for _, c := range cases {
		if c.tool.Name == "provision" && c.state == "" {
			run(c, "create of an unknown shape")
		}
	}

	// The journal and its copy.
	if code, out, errOut := runCLI(t, "", "diag", "export", "--out", "journal-copy.jsonl"); code != 0 {
		t.Errorf("diag export: exit %d: %s%s", code, out, errOut)
	}
	b, n := consoleInFiles(w.dir, fields)
	bad = append(bad, b...)

	// The walk saw what it walks.
	for _, f := range []string{answers, filepath.Join(w.dir, "journal-copy.jsonl")} {
		if st, err := os.Stat(f); err != nil || st.Size() == 0 {
			bad = append(bad, fmt.Sprintf("%s is empty or missing (%v): the walk sees nothing there", filepath.Base(f), err))
		}
	}
	for _, kind := range []string{"GET card", "GET list", "PUT card", "POST create /api/v1.1/vms",
		"403 GET card", "422 GET card", "500 GET card", "403 GET list", "403 PUT card", "422 PUT card",
		"403 POST create /api/v1.1/vms", "422 POST create /api/v1.1/vms", "422 POST action", "422 DELETE card", "201 POST create /api/v1.1/vms"} {
		if !world.ever[kind] {
			bad = append(bad, "no call was served the console fields on "+kind+": the world does not reach that path (brief vm-console.md §5.2)")
		}
	}
	for name := range reached {
		if !vmPathTools[name] {
			bad = append(bad, name+": its calls reach a VM path of the compute adapter, but it is not in vmPathTools")
		}
	}
	for name := range vmPathTools {
		if !reached[name] {
			bad = append(bad, name+": in vmPathTools, but no call of it reaches a VM path: drop it, or give the world what it needs")
		}
	}
	var kinds []string
	for k := range world.ever {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	t.Logf("console fields: %v; tools on VM paths: %d; paths served: %s; files walked: %d (%d at the end)", fields, len(reached), strings.Join(kinds, ", "), walked, n)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// createOfUnknownShape answers a create of a VM 201 with a body evo does
// not read as a VM: CreateVM puts it into its error (brief §5.1).
type createOfUnknownShape struct {
	base  http.RoundTripper
	world *consoleWorld
}

func (c createOfUnknownShape) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "compute.api.cloud.ru" && r.Method == http.MethodPost && vmPath.MatchString(r.URL.Path) && kindOf(r) != "POST action" {
		body := map[string]any{"result": c.world.consoleObject(), "status": "accepted"}
		b, _ := json.Marshal(body)
		c.world.note("201 " + kindOf(r))
		return answerWith(http.StatusCreated, b), nil
	}
	return c.base.RoundTrip(r)
}
