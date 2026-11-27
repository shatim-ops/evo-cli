// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Release plan 1.7, P3: a delete preview says whether this folder's evo
// made what goes (provenance), the same on every surface — the command's
// --dry-run --json, 'evo tools call', MCP over stdio and HTTP
// (comparePreviews, the mechanism of TestContract_PreviewParity) — and the
// person at the terminal reads the value in words (CLAUDE.md §6, axis 4).
func TestContract_DeletePreviewNamesProvenance(t *testing.T) {
	world := withParityWorld(t)
	srv := parityServers(t)
	reg := tools.New(nil)
	commands := commandsByTool()
	for _, c := range []struct {
		name, tool string
		args       map[string]any
		setup      func(*testing.T, parityWorld) (map[string]any, func())
		engine     []string
		want       string
	}{
		{name: "evo vm delete of a VM this folder does not record", tool: "destroy", args: parityArgs["destroy"], want: "unknown"},
		{name: "evo destroy of the recorded deployment", tool: "destroy", args: map[string]any{}, setup: recordDeployment,
			engine: []string{"destroy"}, want: "yes"},
		{name: "evo pg delete (a kind the state does not record)", tool: "pg_delete", args: parityArgs["pg_delete"], want: "unknown"},
		{name: "evo vpc fip-release of an address no record names", tool: "release_floating_ip", args: parityArgs["release_floating_ip"], want: "unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tl, ok := reg.Lookup(c.tool)
			if !ok {
				t.Fatalf("no tool %s", c.tool)
			}
			done := func() {}
			if c.setup != nil {
				_, done = c.setup(t, world)
			}
			defer done()
			cmd := commands[c.tool]
			pc := parityCommandOf(t, cmd.group, cmd.cmd, tl, c.args)
			if c.engine != nil {
				pc.tree, pc.engine = c.engine, true
			}
			if why := comparePreviews(t, c.name, pc, srv, map[string]string{}); why != "" {
				t.Fatal(why)
			}
			newProcess()
			_, out, _ := runCLI(t, "", append([]string{"--dry-run", "--json"}, pc.tree...)...)
			details, ok := dryRunDetails(out)
			if !ok {
				t.Fatalf("no preview: %s", out)
			}
			prov, _ := details["provenance"].(map[string]any)
			if prov["created_by_evo"] != c.want || prov["note"] == "" {
				t.Fatalf("provenance %v, want created_by_evo %s with a note", details["provenance"], c.want)
			}
			newProcess()
			_, out, errOut := runCLI(t, "", append([]string{"--dry-run"}, pc.tree...)...)
			if human := out + errOut; !strings.Contains(human, "created by evo: "+c.want+" — "+prov["note"].(string)) {
				t.Errorf("the human preview does not say the provenance:\n%s", human)
			}
		})
	}
}

// The server reads the receipt written by the CLI and establishes the target's
// project itself. An ID in the journal alone cannot establish this provenance.
func TestContract_StdioDeletePreviewAfterCLICreate(t *testing.T) {
	for _, tc := range []struct {
		tool, field, host, path string
		create                  []string
	}{
		{"redis_delete", "cluster_id", "redis.api.cloud.ru", "/v1/clusters", []string{"redis", "create", "cache", "--cpu", "2", "--ram", "4"}},
		{"subnet_delete", "subnet_id", "compute.api.cloud.ru", "/api/v1/subnets", []string{"vpc", "subnet-create", "private", "--cidr", "10.10.0.0/24", "--zone", "ru.AZ-1"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			newProcess()
			code, out, stderr := runCLI(t, "", append([]string{"--yes", "--json"}, tc.create...)...)
			id, _ := parseObject(out)[tc.field].(string)
			if code != 0 || id == "" {
				t.Fatalf("create: exit %d: %s%s", code, out, stderr)
			}
			w.rt.takeWrites()
			old := w.rt.extra
			member := true
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.Host != tc.host || r.URL.Path != tc.path {
					return old(r)
				}
				if r.URL.Query().Get("project_id") != pvProject {
					t.Errorf("membership query: %s", r.URL)
				}
				items := "[]"
				total := 0
				if member {
					items = fmt.Sprintf(`[{"id":%q}]`, id)
					total = 1
				}
				body := fmt.Sprintf(`{"clusters":%s}`, items)
				if tc.tool == "subnet_delete" {
					body = fmt.Sprintf(`{"items":%s,"total":%d}`, items, total)
				}
				return contractRT{status: 200, body: body}.RoundTrip(r)
			}
			defer i18n.SetLang(i18n.Lang())
			i18n.SetLang(i18n.RU)
			for _, want := range []string{"yes", "unknown"} {
				member = want == "yes"
				a := callStdioTool(t, tc.tool, map[string]any{tc.field: id})
				var preview struct {
					Status     string
					Provenance struct {
						Created string `json:"created_by_evo"`
						Note    string
					}
				}
				if !a.isErr || json.Unmarshal([]byte(a.text), &preview) != nil || preview.Status != "confirmation_required" || preview.Provenance.Created != want || !strings.Contains(preview.Provenance.Note, "this folder's journal") {
					t.Fatalf("want confirmation with %s: %s", want, a.text)
				}
				if writes := w.rt.takeWrites(); len(writes) != 0 {
					t.Fatalf("preview sent %v", writes)
				}
			}
		})
	}
}
