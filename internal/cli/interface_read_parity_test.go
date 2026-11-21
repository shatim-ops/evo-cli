// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Exercise the actual CLI command and a read-only, project-bound stdio
// server. The free interface is only on the second page of the project.
func TestNetworkInterfaceReadSurfaces(t *testing.T) {
	world := withParityWorld(t)
	card := fmt.Sprintf(`{"id":%q,"name":"spare\ncloud text","vm":null,"security_groups":[],"primary":false}`, parityNIC)
	world.rt.extra = func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Path {
		case "/api/v1/interfaces":
			if r.URL.Query().Get("project_id") != pvProject {
				t.Errorf("project query: %s", r.URL)
			}
			switch r.URL.Query().Get("offset") {
			case "":
				body = fmt.Sprintf(`{"items":[{"id":%q}],"total":2}`, parityVM)
			case "1":
				body = `{"items":[` + card + `],"total":2}`
			default:
				t.Errorf("unexpected offset: %s", r.URL)
			}
		case "/api/v1/interfaces/" + parityNIC:
			body = card
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			return contractRT{status: 404, body: "{}"}.RoundTrip(r)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		return contractRT{status: 200, body: body}.RoundTrip(r)
	}
	servers := newROServers(t)
	servers.ro.SetProjectBound()
	for _, tc := range []struct {
		name, key string
		args      map[string]any
	}{
		{"interface_list", "interfaces", map[string]any{}},
		{"interface_get", "interface", map[string]any{"interface_id": parityNIC}},
	} {
		for _, lang := range []string{i18n.RU, i18n.EN} {
			t.Run(tc.name+"/"+lang, func(t *testing.T) {
				orig := i18n.Lang()
				t.Cleanup(func() { i18n.SetLang(orig) })
				t.Setenv("EVO_LANG", lang)
				i18n.SetLang(lang)
				mcpAnswer, _ := servers.stdio(t, servers.ro, "tools/call", map[string]any{"name": tc.name, "arguments": tc.args})
				cmd, ok := commandsByTool()[tc.name]
				if !ok || mcpAnswer.rpcErr != "" || mcpAnswer.isErr {
					t.Fatalf("CLI registered=%t, MCP error=%q, result=%s", ok, mcpAnswer.rpcErr, mcpAnswer.text)
				}
				tl, ok := newRegistry().Lookup(tc.name)
				if !ok {
					t.Fatal("missing registry entry")
				}
				line := parityCommandOf(t, cmd.group, cmd.cmd, tl, tc.args).tree
				helpCode, help, helpErr := runCLI(t, "", append(slices.Clone(line), "--help")...)
				wantHelp := tl.ShortRU
				if lang == i18n.EN {
					wantHelp = strings.Split(tl.Description, ".")[0]
				}
				if helpCode != 0 || wantHelp == "" || !strings.Contains(help, wantHelp) {
					t.Fatalf("%s help: exit=%d stdout=%s stderr=%s", lang, helpCode, help, helpErr)
				}
				code, out, errOut := runCLI(t, "", append([]string{"--json"}, line...)...)
				if code != 0 || strings.TrimSpace(out) != mcpAnswer.text {
					t.Fatalf("CLI exit=%d stdout=%s stderr=%s; MCP=%s", code, out, errOut, mcpAnswer.text)
				}
				if tc.name == "interface_list" {
					var list struct {
						Interfaces []map[string]json.RawMessage `json:"interfaces"`
					}
					if json.Unmarshal([]byte(out), &list) != nil || len(list.Interfaces) != 2 ||
						list.Interfaces[0]["vm"] != nil || list.Interfaces[0]["security_groups"] != nil ||
						string(list.Interfaces[1]["vm"]) != "null" || string(list.Interfaces[1]["security_groups"]) != "[]" {
						t.Fatalf("missing and reported binding data: %s", out)
					}
				}
				var result struct {
					Untrusted []string `json:"_untrusted"`
				}
				if json.Unmarshal([]byte(out), &result) != nil || !slices.Contains(result.Untrusted, tc.key) || !strings.Contains(out, `spare\ncloud text`) {
					t.Fatalf("untrusted cloud data: %s", out)
				}
			})
		}
	}
	t.Run("outside project", func(t *testing.T) {
		ans, _ := servers.stdio(t, servers.ro, "tools/call", map[string]any{"name": "interface_get", "arguments": map[string]any{"interface_id": parityStoppedVM}})
		var result struct {
			Reason string `json:"reason"`
		}
		if !ans.isErr || json.Unmarshal([]byte(ans.text), &result) != nil || result.Reason != "NOT_FOUND" {
			t.Fatalf("RPC error=%s, result=%s", ans.rpcErr, ans.text)
		}
	})
}
