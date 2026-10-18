// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/provision"
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

func TestContract_PreviewParity(t *testing.T) {
	withParityWorld(t)
	factory := func(o map[string]any) (*engine.Engine, error) {
		if o == nil {
			o = map[string]any{}
		}
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth, e.Cloud = previewToken{}, parityCloud{}
		return e, nil
	}
	stdio := mcp.New(factory, "test")
	hs := mcp.New(factory, "test")
	h := mcp.NewHTTPHandler(hs, mcp.HTTPOptions{BearerToken: "parity-token"})
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		ts.Close()
	})

	cases := []parityCase{
		{
			name: "vm create",
			tree: []string{"vm", "create", "web"},
			call: []string{"tools", "call", "provision", "--arg", "name=web"},
			tool: "provision", args: map[string]any{"name": "web"},
		},
		{
			name: "disk delete",
			tree: []string{"disk", "delete", parityVM},
			call: []string{"tools", "call", "disk_delete", "--arg", "disk_id=" + parityVM},
			tool: "disk_delete", args: map[string]any{"disk_id": parityVM},
		},
		{
			name: "destroy",
			tree: []string{"vm", "delete", parityVM},
			call: []string{"tools", "call", "destroy", "--arg", "vm_id=" + parityVM},
			tool: "destroy", args: map[string]any{"vm_id": parityVM},
		},
		{
			// open_ports has no tree command. The CLI surface is tools call.
			name: "open_ports",
			call: []string{"tools", "call", "open_ports", "--arg", "security_group=web", "--arg", "ports=80,443"},
			tool: "open_ports", args: map[string]any{"security_group": "web", "ports": []any{float64(80), float64(443)}},
		},
		{
			name: "k8s_add_addon",
			tree: []string{"k8s", "add-addon", addonClusterID, "cert-manager"},
			call: []string{"tools", "call", "k8s_add_addon", "--arg", "cluster_id=" + addonClusterID, "--arg", "name=cert-manager"},
			tool: "k8s_add_addon", args: map[string]any{"cluster_id": addonClusterID, "name": "cert-manager"},
		},
		{
			name: "pg_create",
			tree: []string{"pg", "create", "web", "--specification-id", "std"},
			call: []string{"tools", "call", "pg_create", "--arg", "name=web", "--arg", "specification_id=std"},
			tool: "pg_create", args: map[string]any{"name": "web", "specification_id": "std"},
		},
	}
	// The tree completes a CreatePreview (zone, flavor, price, project) before
	// printing it. tools call and both MCP transports return the tool's own
	// answer. docs/internal/standalone/BACKLOG.md [03]: the MCP provision
	// preview has no price because the gate does not call the cloud; the
	// price section of the same file says evo vm create (CLI) does estimate.
	// Those two commands are compared tools ≡ stdio ≡ http, and the tree is
	// only checked to still be a dry_run. Every other command compares all
	// four surfaces.
	completedByTree := map[string]bool{"vm create": true, "k8s_add_addon": true}
	var bad []string
	for _, c := range cases {
		got := map[string]map[string]any{}
		take := func(label string, args []string) {
			m, why := dryDetails(t, args...)
			if why != "" {
				bad = append(bad, c.name+" "+label+": "+why)
				return
			}
			got[label] = m
		}
		if c.tree != nil {
			take("cli", c.tree)
		}
		take("tools", c.call)
		got["stdio"] = stdioDetails(t, stdio, c.tool, c.args)
		got["http"] = httpDetails(t, ts, c.tool, c.args)
		agent := map[string]map[string]any{"tools": got["tools"], "stdio": got["stdio"], "http": got["http"]}
		if why := parityDiff(c.name, agent); why != "" {
			bad = append(bad, why)
		}
		if c.tree != nil && !completedByTree[c.name] {
			if why := parityDiff(c.name, map[string]map[string]any{"cli": got["cli"], "tools": got["tools"]}); why != "" {
				bad = append(bad, why)
			}
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%s", strings.Join(bad, "\n"))
	}
}

type parityCase struct {
	name string
	tree []string
	call []string
	tool string
	args map[string]any
}

func withParityWorld(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLOUDRU_CONFIG_DIR", home)
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	t.Setenv("CLOUDRU_REGION", pvZone)
	t.Setenv("CLOUDRU_IMAGE_ID", pvImage)
	t.Setenv("CLOUDRU_FLAVOR", "")
	t.Setenv("EVO_LANG", "en")
	api := &addonAPI{}
	oldRT := http.DefaultTransport
	http.DefaultTransport = parityRT{addon: api}
	old := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		if o == nil {
			o = map[string]any{}
		}
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth, e.Cloud = previewToken{}, parityCloud{}
		return e, nil
	}
	t.Cleanup(func() { toolEngine, http.DefaultTransport = old, oldRT })
}

type parityRT struct{ addon *addonAPI }

func (p parityRT) RoundTrip(r *http.Request) (*http.Response, error) {
	switch r.URL.Host {
	case "mk8s.api.cloud.ru":
		return p.addon.RoundTrip(r)
	case "postgresql.api.cloud.ru":
		return pgParity(r)
	default:
		w := httptest.NewRecorder()
		_, _ = io.WriteString(w, `{"items":[],"total":0}`)
		return w.Result(), nil
	}
}

func pgParity(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	switch {
	case r.URL.Path == "/v1/clusters":
		_, _ = io.WriteString(w, `{"clusters":[]}`)
	case r.URL.Path == "/v1/versions":
		_, _ = io.WriteString(w, `{"versions":[{"id":"v17","name":"17"}]}`)
	case r.URL.Path == "/v1/specifications":
		_, _ = io.WriteString(w, `{"specifications":[{"id":"std","display_name":"std","cpu":2,"memory":4,"deployment_mode":"STANDARD","min_storage_gb":20,"max_hosts":2}]}`)
	default:
		http.Error(w, `{"message":"no such path"}`, 404)
	}
	return w.Result(), nil
}

func dryDetails(t *testing.T, args ...string) (map[string]any, string) {
	t.Helper()
	out, errOut, code := execCLI(t, append(args, "--dry-run", "--json")...)
	var env struct {
		Status  string `json:"status"`
		Preview struct {
			Details map[string]any `json:"details"`
		} `json:"preview"`
	}
	if json.Unmarshal([]byte(out), &env) != nil || env.Status != "dry_run" || env.Preview.Details == nil {
		return nil, "exit " + strconv.Itoa(code) + " out=" + clipLine(out) + " err=" + clipLine(errOut)
	}
	return env.Preview.Details, ""
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
