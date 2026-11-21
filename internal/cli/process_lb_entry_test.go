// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// SPDX-License-Identifier: Apache-2.0
package cli

import (
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"strings"
	"testing"
)

func TestProcessLBFirstConsent(t *testing.T) {
	const pvSubnet = "00000000-0000-0000-0000-0000000000d1"
	for _, surface := range []string{"CLI-yes", "stdio", "HTTP-session", "HTTP-no-session", "stdio-linked", "HTTP-session-linked"} {
		t.Run(surface, func(t *testing.T) {
			_, f := withJSONWorld(t)
			cases, _, bad := jsonCases(t)
			if len(bad) > 0 {
				t.Fatal(bad)
			}
			for _, c := range cases {
				if c.tool.Name != "lb_create" {
					continue
				}
				args := copyArgs(c.args)
				f.reset()
				newProcess()
				if surface == "CLI-yes" {
					cmd := commandsByTool()[c.tool.Name]
					line := parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
					code, out, errout := runCLI(t, "", append([]string{"--json", "--yes"}, line...)...)
					writes := f.takeWrites()
					t.Logf("CLI first --yes code=%d writes=%v", code, writes)
					if code != 0 || len(writes) != 1 || !strings.Contains(writes[0], "/v2/networkLoadBalancers") || parseObject(out)["status"] != "creating" {
						t.Fatalf("%s %s", out, errout)
					}
				} else {
					args["confirmed"] = true
					params := map[string]any{"name": "lb_create", "arguments": args}
					srv := newROServers(t)
					var rpc func(map[string]any) roAnswer
					if strings.HasPrefix(surface, "stdio") {
						rpc = processUpgradeStdio(t, srv.plain)
					} else {
						h := mcp.NewHTTPHandler(srv.plain, mcp.HTTPOptions{})
						t.Cleanup(h.Close)
						id := ""
						if strings.HasPrefix(surface, "HTTP-session") {
							_, id = retryRPC(t, h, "", "initialize", map[string]any{"protocolVersion": "2025-03-26"})
							if id == "" {
								t.Fatal("missing initialized session")
							}
						}
						rpc = func(p map[string]any) roAnswer { a, _ := retryRPC(t, h, id, "tools/call", p); return a }
					}
					if strings.HasSuffix(surface, "-linked") {
						delete(args, "confirmed")
						first := rpc(params)
						question := parseObject(first.text)
						gotPlacement, _ := question["will_create"].(map[string]any)
						if question["status"] != "confirmation_required" || len(f.takeWrites()) != 0 || gotPlacement["subnet_id"] != pvSubnet || gotPlacement["zone"] != "ru.AZ-1" {
							t.Fatalf("initial question: %s", first.text)
						}
						args["confirmed"] = true
						if token, ok := question["preview_id"]; ok {
							args["preview_id"] = token
						}
						ans := rpc(params)
						writes := f.takeWrites()
						if len(writes) != 1 || parseObject(ans.text)["status"] != "creating" || !strings.Contains(writes[0], `"subnet_ids":["`+pvSubnet+`"]`) {
							t.Fatalf("stable consent: %s %v", ans.text, writes)
						}
						t.Logf("shown placement, then stable consent: POST=%d", len(writes))
						return
					}
					ans := rpc(params)
					writes := f.takeWrites()
					t.Logf("first confirmed status=%v writes=%v rpcError=%s", parseObject(ans.text)["status"], writes, ans.rpcErr)
					if len(writes) != 0 || parseObject(ans.text)["status"] == "creating" {
						t.Fatalf("created without actual preview: %s", ans.text)
					}
				}
				return
			}
			t.Fatal("no registered LB case")
		})
	}
}
