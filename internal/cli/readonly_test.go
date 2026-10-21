// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// roAnswer is what a server answered a tools/call: the tool's text and
// isError, or the message of a JSON-RPC error.
type roAnswer struct {
	text   string
	isErr  bool
	rpcErr string
}

// roRequest is the JSON-RPC body of one request.
func roRequest(method string, params any) []byte {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	return b
}

// roDecode reads the answer of a tools/call (or the tools of a tools/list).
func roDecode(t *testing.T, raw []byte) (roAnswer, []string) {
	t.Helper()
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
			Tools   []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("not a JSON-RPC answer: %s", clipLine(string(raw)))
	}
	var a roAnswer
	switch {
	case resp.Error != nil:
		a.rpcErr = resp.Error.Message
	case len(resp.Result.Content) > 0:
		a.text, a.isErr = resp.Result.Content[0].Text, resp.Result.IsError
	}
	var names []string
	for _, tl := range resp.Result.Tools {
		names = append(names, tl.Name)
	}
	return a, names
}

// roServers are the MCP servers of TestReadOnly_EveryTool on the engines of
// the fake world: without --read-only, and with it over stdio and HTTP.
type roServers struct {
	plain, ro *mcp.Server
	roHTTP    *httptest.Server
}

func newROServers(t *testing.T) roServers {
	t.Helper()
	factory := func(o map[string]any) (*engine.Engine, error) {
		if o == nil {
			o = map[string]any{}
		}
		return toolEngine(o)
	}
	ro := mcp.New(factory, "test")
	ro.SetReadOnly()
	behindHTTP := mcp.New(factory, "test")
	behindHTTP.SetReadOnly()
	h := mcp.NewHTTPHandler(behindHTTP, mcp.HTTPOptions{BearerToken: "parity-token"})
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		ts.Close()
	})
	return roServers{plain: mcp.New(factory, "test"), ro: ro, roHTTP: ts}
}

func (r roServers) stdio(t *testing.T, s *mcp.Server, method string, params any) (roAnswer, []string) {
	t.Helper()
	flagJSON, flagDryRun, flagYes = false, false, false
	out := &bytes.Buffer{}
	if err := s.Run(bytes.NewReader(append(roRequest(method, params), '\n')), out); err != nil {
		t.Fatalf("stdio %s: %v", method, err)
	}
	return roDecode(t, out.Bytes())
}

func (r roServers) http(t *testing.T, method string, params any) (roAnswer, []string) {
	t.Helper()
	flagJSON, flagDryRun, flagYes = false, false, false
	req, err := http.NewRequest(http.MethodPost, r.roHTTP.URL+"/mcp", bytes.NewReader(roRequest(method, params)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer parity-token")
	resp, err := r.roHTTP.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return roDecode(t, raw)
}

// stackID is the id of a job stack_plan saves: each call makes a new one.
var stackID = regexp.MustCompile(`stk-[0-9a-f]{6,}`)

// roSame is a text with what differs between two calls made the same:
// sameVolatile, and the id of a planned stack.
func roSame(s string) string { return stackID.ReplaceAllString(sameVolatile(s), "stk-…") }

// roRefused reports whether a answers the refusal of H11.
func roRefused(a roAnswer) bool {
	obj := parseObject(a.text)
	msg, _ := obj["error"].(string)
	return a.isErr && obj["reason"] == "USAGE" && strings.Contains(msg, "this server runs with --read-only")
}

// Q12 (spec.md §8): evo mcp --read-only, for every tool of the registry, in
// the fake world of TestParity_JSONEqualsTool, where every call with
// confirmed=true succeeds and every write is recorded. A tool every call of
// which needs consent (tools.AlwaysNeedsConsent) is not in tools/list, and
// a call of it — with confirmed=true or without — is refused before it
// reaches anything: no write, no preview, no secret. Any other tool answers
// as without the flag, over stdio and HTTP alike (H7 masking aside, and
// H10 over HTTP); k8s_kubectl and k8s_helm run their reads and refuse the
// rest. The gate of the handlers and tools.NeedsConsent are one decision:
// a call answers confirmation_required without the flag exactly when
// NeedsConsent says so.
func TestReadOnly_EveryTool(t *testing.T) {
	w, fake := withJSONWorld(t)
	srv := newROServers(t)
	takeWrites := func() []string {
		fake.reset()
		ws := w.rt.takeWrites()
		sort.Strings(ws)
		return ws
	}

	listOf := func(names []string) map[string]bool {
		out := map[string]bool{}
		for _, n := range names {
			out[n] = true
		}
		return out
	}
	_, stdioNames := srv.stdio(t, srv.ro, "tools/list", map[string]any{})
	_, httpNames := srv.http(t, "tools/list", map[string]any{})
	stdioList, httpList := listOf(stdioNames), listOf(httpNames)

	var bad, hidden, mixed []string
	for _, tl := range newRegistry().Tools() {
		args, ok := parityArgs[tl.Name]
		if !ok {
			bad = append(bad, tl.Name+": no entry in parityArgs")
			continue
		}
		always := tools.AlwaysNeedsConsent(tl)
		if stdioList[tl.Name] == always || httpList[tl.Name] == always {
			bad = append(bad, fmt.Sprintf("%s: listed over stdio %v, over HTTP %v; every call needs consent: %v",
				tl.Name, stdioList[tl.Name], httpList[tl.Name], always))
		}
		switch {
		case always:
			hidden = append(hidden, tl.Name)
		case tl.Mutating:
			mixed = append(mixed, tl.Name)
		}
		bad = append(bad, roCase(t, srv, w, tl, args, takeWrites)...)
	}

	// k8s_kubectl and k8s_helm stay for their reads; what changes the
	// cluster, or prints a credential, is refused.
	for _, c := range []struct {
		tool    string
		args    map[string]any
		refused string // "" runs as without the flag
	}{
		{"k8s_kubectl", map[string]any{"args": "get pods -A"}, ""},
		{"k8s_kubectl", map[string]any{"args": "delete pod web"}, "this server runs with --read-only"},
		{"k8s_kubectl", map[string]any{"args": "delete pod web", "confirmed": true}, "this server runs with --read-only"},
		{"k8s_kubectl", map[string]any{"args": "get pods -f https://example.com/x.yaml"}, "this server runs with --read-only"},
		{"k8s_kubectl", map[string]any{"args": "config view --raw"}, "on this --read-only server"},
		{"k8s_kubectl", map[string]any{"args": "get secret db -o yaml"}, "on this --read-only server"},
		{"k8s_kubectl", map[string]any{"args": "get secret db"}, ""},
		{"k8s_helm", map[string]any{"action": "list"}, ""},
		{"k8s_helm", map[string]any{"action": "install", "release": "web", "chart": "repo/web", "confirmed": true}, "this server runs with --read-only"},
		{"k8s_kubeconfig", map[string]any{}, "on this --read-only server"},
		{"k8s_kubeconfig", map[string]any{"embed_token": false}, ""},
	} {
		args := copyArgs(c.args)
		args["cluster_id"] = addonClusterID
		name := fmt.Sprintf("%s %v", c.tool, c.args)
		params := map[string]any{"name": c.tool, "arguments": args}
		takeWrites()
		plain, _ := srv.stdio(t, srv.plain, "tools/call", params)
		plainWrites := takeWrites()
		for _, surface := range []string{"stdio", "http"} {
			var a roAnswer
			if surface == "http" {
				a = first(srv.http(t, "tools/call", params))
			} else {
				a = first(srv.stdio(t, srv.ro, "tools/call", params))
			}
			writes := takeWrites()
			switch {
			case c.refused == "":
				if roSame(a.text) != roSame(plain.text) || a.isErr != plain.isErr {
					bad = append(bad, fmt.Sprintf("%s over %s: not the answer without --read-only:\n  ro:    %s\n  plain: %s", name, surface, clipLine(a.text), clipLine(plain.text)))
				}
			case !a.isErr || !strings.Contains(a.text, c.refused) || parseObject(a.text)["reason"] != "USAGE":
				bad = append(bad, fmt.Sprintf("%s over %s: not refused (%q): %s", name, surface, c.refused, clipLine(a.text)))
			}
			if c.refused != "" && len(writes) > 0 || c.refused == "" && strings.Join(writes, "|") != strings.Join(plainWrites, "|") {
				bad = append(bad, fmt.Sprintf("%s over %s: writes %v (without --read-only %v)", name, surface, writes, plainWrites))
			}
		}
	}

	// A running stack: stack_status reports it and does not advance it
	// (its steps would create resources on stack_apply's consent); without
	// the flag the same poll creates the next step.
	job := parityStackJob(t, "")
	job.Status = "running"
	if err := job.Save(); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"name": "stack_status", "arguments": map[string]any{"job_id": job.ID, "wait_seconds": float64(1)}}
	takeWrites()
	for surface, a := range map[string]roAnswer{
		"stdio": first(srv.stdio(t, srv.ro, "tools/call", params)),
		"http":  first(srv.http(t, "tools/call", params)),
	} {
		if note, _ := parseObject(a.text)["note"].(string); a.isErr || !strings.Contains(note, "--read-only") {
			bad = append(bad, "stack_status of a running stack over "+surface+": no note of --read-only: "+clipLine(a.text))
		}
	}
	if ws := takeWrites(); len(ws) > 0 {
		bad = append(bad, fmt.Sprintf("stack_status of a running stack advanced it with --read-only: %v", ws))
	}
	srv.stdio(t, srv.plain, "tools/call", params)
	if ws := takeWrites(); len(ws) == 0 {
		bad = append(bad, "stack_status without --read-only did not advance the running stack: the case above proves nothing")
	}

	sort.Strings(hidden)
	sort.Strings(mixed)
	t.Logf("read-only: %d tools of %d listed; hidden (%d): %s; listed with refused calls (%d): %s",
		len(stdioNames), len(newRegistry().Tools()), len(hidden), strings.Join(hidden, ", "), len(mixed), strings.Join(mixed, ", "))
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// first is the answer of a tools/call, without the tools of a tools/list.
func first(a roAnswer, _ []string) roAnswer { return a }

// roCase checks one tool with its arguments of parityArgs: without the
// flag, the call gates on confirmed exactly when tools.NeedsConsent says so,
// and with confirmed=true it goes past the gate; with the flag, a call that
// needs consent is refused with confirmed and without, writing nothing, and
// any other call answers, and reads, as without the flag.
func roCase(t *testing.T, srv roServers, w parityWorld, tl tools.Tool, args map[string]any, takeWrites func() []string) []string {
	t.Helper()
	var bad []string
	call := func(confirmed bool, do func(params any) roAnswer) (roAnswer, []string) {
		a := copyArgs(args)
		teardown := func() {}
		if setup := paritySetup[tl.Name]; setup != nil {
			extra, done := setup(t, w)
			for k, v := range extra {
				a[k] = v
			}
			teardown = done
		}
		if confirmed {
			a["confirmed"] = true
		}
		takeWrites()
		ans := do(map[string]any{"name": tl.Name, "arguments": a})
		writes := takeWrites()
		teardown()
		return ans, writes
	}
	plainCall := func(params any) roAnswer { return first(srv.stdio(t, srv.plain, "tools/call", params)) }
	roStdio := func(params any) roAnswer { return first(srv.stdio(t, srv.ro, "tools/call", params)) }
	roHTTP := func(params any) roAnswer { return first(srv.http(t, "tools/call", params)) }

	needs := tools.NeedsConsent(tl, args)
	plain, plainWrites := call(false, plainCall)
	if gated := parseObject(plain.text)["status"] == "confirmation_required"; gated != needs {
		bad = append(bad, fmt.Sprintf("%s: without confirmed the handler gates: %v, tools.NeedsConsent: %v: %s", tl.Name, gated, needs, clipLine(plain.text)))
	}
	if !needs {
		for surface, do := range map[string]func(any) roAnswer{"stdio": roStdio, "http": roHTTP} {
			a, writes := call(false, do)
			want := plain.text
			if surface == "http" && tl.Name == "doctor" {
				continue // H10: doctor is disabled over HTTP, --read-only or not
			}
			if tl.Name == "k8s_kubeconfig" {
				// H6 on this --read-only server: the default embeds the
				// cluster token (checked with the cases of TestReadOnly_EveryTool).
				if !a.isErr || !strings.Contains(a.text, "on this --read-only server") {
					bad = append(bad, tl.Name+" over "+surface+": the embedded token is not refused: "+clipLine(a.text))
				}
				continue
			}
			got := parseObject(a.text)
			if _, masked := got["redacted"]; masked {
				// H7 on a --read-only server: the masked fields are the
				// only difference.
				unmasked, why := unmaskHTTP(got, parseObject(plain.text))
				if why != "" {
					bad = append(bad, tl.Name+" over "+surface+": "+why)
				}
				b, _ := json.Marshal(unmasked)
				p, _ := json.Marshal(parseObject(plain.text))
				a.text, want = string(b), string(p)
			}
			if roSame(a.text) != roSame(want) || a.isErr != plain.isErr {
				bad = append(bad, fmt.Sprintf("%s over %s: a read answers otherwise with --read-only:\n  ro:    %s\n  plain: %s", tl.Name, surface, clipLine(a.text), clipLine(want)))
			}
			if roSame(strings.Join(writes, "|")) != roSame(strings.Join(plainWrites, "|")) {
				bad = append(bad, fmt.Sprintf("%s over %s: sent %v with --read-only, %v without", tl.Name, surface, writes, plainWrites))
			}
		}
		return bad
	}
	if a, _ := call(true, plainCall); parseObject(a.text)["status"] == "confirmation_required" || roRefused(a) {
		bad = append(bad, tl.Name+": with confirmed=true and without --read-only it does not go past its gate: the refusal below proves nothing: "+clipLine(a.text))
	}
	for _, confirmed := range []bool{false, true} {
		for surface, do := range map[string]func(any) roAnswer{"stdio": roStdio, "http": roHTTP} {
			a, writes := call(confirmed, do)
			if !roRefused(a) {
				bad = append(bad, fmt.Sprintf("%s over %s (confirmed=%v): not the refusal of --read-only: %s", tl.Name, surface, confirmed, clipLine(a.text)))
			}
			if len(writes) > 0 {
				bad = append(bad, fmt.Sprintf("%s over %s (confirmed=%v): wrote with --read-only: %v", tl.Name, surface, confirmed, writes))
			}
		}
	}
	return bad
}
