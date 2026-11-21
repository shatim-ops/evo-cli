// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

func TestProcessD122Consumers(t *testing.T) {
	cases := []struct{ tool, endpoint, field, token, query string }{
		{"pg_create", "postgresql.api.cloud.ru/v1/clusters", "clusters", "next_page_token", "page_token"},
		{"lb_create", "nlb.api.cloud.ru/v2/networkLoadBalancers", "balancers", "next_page_token", "page_token"},
		{"ar_create", "ar.api.cloud.ru/v1/registries", "registries", "nextPageToken", "pageToken"},
		{"iam_service_accounts", "iam.api.cloud.ru/api/v2/serviceAccounts", "service_accounts", "next_page_token", "pageToken"},
		{"org_projects", "organization.api.cloud.ru/v3/projects", "projects", "next_page_token", "page_token"},
		{"billing_finops", "organization.api.cloud.ru/v2/consumption", "consumptions", "next_page_token", "page_filter.page_token"},
	}
	for _, c := range cases {
		for _, surface := range []string{"cli", "stdio"} {
			t.Run(c.tool+"/"+surface, func(t *testing.T) {
				w, _ := withJSONWorld(t)
				t.Setenv("CLOUDRU_AGREEMENT_ID", "")
				old := w.rt.extra
				reads := 0
				w.rt.extra = func(req *http.Request) (*http.Response, error) {
					if req.Method != "GET" {
						return contractRT{status: 409, body: `{"message":"unexpected write in synthetic world"}`}.RoundTrip(req)
					}
					if req.URL.Host == "compute.api.cloud.ru" && req.URL.Path == "/api/v1/subnets" {
						return contractRT{status: 200, body: `{"items":[],"total":0}`}.RoundTrip(req)
					}
					if req.URL.Host+req.URL.Path != c.endpoint {
						return old(req)
					}
					reads++
					token := req.URL.Query().Get(c.query)
					if token == "" && reads < 8 {
						body := fmt.Sprintf(`{%q:[{"id":%q,"name":"first-unrelated-row","status":"READY","amount":73,"servname":"fixture","cost":73,"organization_unit_id":"ou"}],%q:"next"}`, c.field, parityVM, c.token)
						return contractRT{status: 200, body: body}.RoundTrip(req)
					}
					if token != "next" || reads > 8 {
						t.Fatalf("unexpected list request %s (#%d)", req.URL, reads)
					}
					return contractRT{status: 200, body: "null"}.RoundTrip(req)
				}
				args := copyArgs(parityArgs[c.tool])
				raw := ""
				failed := false
				code := -1
				if surface == "stdio" {
					if strings.HasSuffix(c.tool, "_create") {
						args["confirmed"] = true
					}
					a := callStdioTool(t, c.tool, args)
					raw = a.text
					failed = a.isErr
				} else {
					tl, _ := newRegistry().Lookup(c.tool)
					cmd := commandsByTool()[c.tool]
					line := parityCommandOf(t, cmd.group, cmd.cmd, tl, args).tree
					newProcess()
					var out, errOut string
					code, out, errOut = runCLI(t, "", append([]string{"--json", "--yes"}, line...)...)
					raw = out + errOut
					failed = code != 0
				}
				writes := w.rt.takeWrites()
				files := 0
				for _, dir := range intent.Dirs(w.dir) {
					matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
					if err != nil {
						t.Fatal(err)
					}
					files += len(matches)
				}
				t.Logf("reads=%d writes=%v receipts=%d code=%d error=%t: %s", reads, writes, files, code, failed, raw)
				if !failed || !strings.Contains(raw, "API_ERROR") || (surface == "cli" && code != 5) || reads != 2 || len(writes) != 0 || files != 0 || strings.Contains(raw, "first-unrelated-row") || strings.Contains(raw, "\"total_cost\"") || strings.Contains(raw, "\"requires_confirmation\"") {
					t.Error("corrupt later page did not stop the consumer cleanly")
				}
			})
		}
	}
}

func TestProcessD122ProjectUse(t *testing.T) {
	for _, mode := range []string{"valid", "late-null", "late-null-item", "late-null-uuid", "late-null-item-uuid", "denied-name", "denied-uuid"} {
		t.Run(mode, func(t *testing.T) {
			setupCLI(t)
			saveProfile(t, "default", "old")
			before, err := os.ReadFile(config.SettingsPath())
			if err != nil {
				t.Fatal(err)
			}
			prev := orgClient
			t.Cleanup(func() { orgClient = prev })
			reads := 0
			orgClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/v3/projects") {
					t.Fatal(r.Method, r.URL.Path)
				}
				reads++
				if strings.HasPrefix(mode, "denied-") {
					return contractRT{status: 403, body: `{"message":"denied"}`}.RoundTrip(r)
				}
				body := `{"projects":[{"id":"new","name":"operator-selection"}],"next_page_token":"next"}`
				if reads == 2 && r.URL.Query().Get("page_token") == "next" {
					switch mode {
					case "valid":
						body = `{"projects":[]}`
					case "late-null", "late-null-uuid":
						body = "null"
					case "late-null-item", "late-null-item-uuid":
						body = `{"projects":[null]}`
					}
				} else if reads != 1 {
					t.Fatal("unexpected page", r.URL)
				}
				return contractRT{status: 200, body: body}.RoundTrip(r)
			})}
			newProcess()
			want := "operator-selection"
			if strings.HasSuffix(mode, "-uuid") {
				want = parityVM
			}
			code, out, stderr := runCLI(t, "", "--json", "project", "use", want)
			after, e := os.ReadFile(config.SettingsPath())
			if e != nil {
				t.Fatal(e)
			}
			prof, _, _, e := config.LoadProfile("default")
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("reads=%d exit=%d selected=%s settings-unchanged=%t %s%s", reads, code, prof.ProjectID, bytes.Equal(before, after), out, stderr)
			if mode == "denied-name" {
				if reads != 1 || code != 2 || !strings.Contains(stderr, "CONFIG_INVALID") || !bytes.Equal(after, before) {
					t.Fatal("name with denied catalogue did not keep legacy outcome")
				}
				return
			}
			if mode == "denied-uuid" {
				if code != 0 || reads != 1 || prof.ProjectID != parityVM {
					t.Fatal("legacy explicit UUID fallback failed")
				}
				return
			}
			if reads != 2 {
				t.Fatal("incorrect reads")
			}
			if mode == "valid" {
				if code != 0 || prof.ProjectID != "new" {
					t.Fatal("valid selection failed")
				}
				return
			}
			if code != 5 || !strings.Contains(stderr, "API_ERROR") || prof.ProjectID != "old" || !bytes.Equal(before, after) {
				t.Error("corrupt catalogue changed active project")
			}
		})
	}
}

func TestProcessD122FinOpsComparison(t *testing.T) {
	for _, mode := range []string{"valid", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			t.Setenv("CLOUDRU_AGREEMENT_ID", "")
			old := w.rt.extra
			reads := 0
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.URL.Host+r.URL.Path != "organization.api.cloud.ru/v2/consumption" {
					return old(r)
				}
				if r.Method != "GET" || r.URL.Query().Get("project_ids") != pvProject {
					t.Fatal(r.Method, r.URL)
				}
				reads++
				body := `{"consumptions":[{"servname":"Compute","amount":73}]}`
				if reads == 2 {
					body = `{"consumptions":[{"servname":"Compute","amount":39}],"next_page_token":"late"}`
				}
				if reads == 3 {
					if r.URL.Query().Get("page_filter.page_token") != "late" {
						t.Fatal(r.URL)
					}
					body = `{"consumptions":[]}`
					if mode == "corrupt" {
						body = `{"consumptions":[null]}`
					}
				}
				if reads > 3 {
					t.Fatal("unexpected read")
				}
				return contractRT{status: 200, body: body}.RoundTrip(r)
			}
			a := callStdioTool(t, "billing_finops", map[string]any{"project_id": pvProject, "start": "2026-10-01", "end": "2026-10-02"})
			m := parseObject(a.text)
			t.Logf("reads=%d error=%t reason=%v total=%v", reads, a.isErr, m["reason"], m["total_cost"])
			if reads != 3 || len(w.rt.takeWrites()) != 0 {
				t.Fatal("request count")
			}
			if mode == "valid" {
				if a.isErr || m["total_cost"] != float64(73) {
					t.Fatal(a.text)
				}
				return
			}
			if !a.isErr || m["reason"] != "API_ERROR" || m["total_cost"] != nil || m["anomalies"] != nil {
				t.Fatal(a.text)
			}
		})
	}
}
