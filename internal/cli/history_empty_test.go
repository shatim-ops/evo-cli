// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
)

// An empty since is not «the last 30 days»: it is refused USAGE, as an
// empty project_id beside it is (D100), over MCP and in the CLI — an agent
// that built the argument from an empty variable reads another window than
// it meant (A19-33, #2435).
func TestHistory_EmptySinceIsUsage(t *testing.T) {
	w, _ := withJSONWorld(t)
	srv := parityServers(t)
	for _, arg := range []string{"since", "project_id"} {
		m := stdioDetails(t, srv.stdio, "history", map[string]any{arg: ""})
		if m["reason"] != "USAGE" || m["argument"] != arg {
			t.Errorf("mcp %s: \"\": want USAGE naming it: %v", arg, m)
		}
		forgetRun(w)
		code, out, errOut := runCLI(t, "", "history", "--"+strings.ReplaceAll(arg, "_", "-"), "", "--json")
		if code == 0 || !strings.Contains(out+errOut, "USAGE") {
			t.Errorf("cli --%s \"\": want USAGE, code %d: %s%s", arg, code, out, errOut)
		}
	}
}

func TestContract_HistoryAfterPartialComposeCleanup(t *testing.T) {
	w, _ := withJSONWorld(t)
	_, done := paritySetup["deploy"](t, w)
	defer done()
	old := w.rt.extra
	refuseRelease := true
	releases := 0
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == "DELETE" && strings.Contains(r.URL.Path, "/security-groups/") && strings.Contains(r.URL.Path, "/rules/") {
			return contractRT{status: 200, body: "{}"}.RoundTrip(r)
		}
		if r.Method == "DELETE" && r.URL.Path == "/api/v1/floating-ips/"+parityVM {
			releases++
			if refuseRelease {
				return contractRT{status: 403, body: `{"message":"release refused"}`}.RoundTrip(r)
			}
		}
		return old(r)
	}
	code, stdout, stderr := runCLI(t, "", "--json", "deploy", "--yes")
	if code != 0 {
		t.Fatalf("deploy: %s%s", stdout, stderr)
	}
	if code, _, _ := runCLI(t, "", "--json", "destroy", "--yes"); code == 0 || releases == 0 {
		t.Fatalf("partial destroy: exit %d, releases %d", code, releases)
	}
	later := clock.Now().Add(time.Hour)
	clock.Now = func() time.Time { return later }
	check := func() roAnswer {
		return callStdioTool(t, "history", map[string]any{"check": true}, "--read-only", "--allow-project", pvProject)
	}
	a := check()
	var v struct {
		Live []struct {
			Tool, Outcome string
			Result        string `json:"check_result"`
			ID            string `json:"resource_id"`
		}
		Would []struct{ Tool string } `json:"would_close"`
	}
	if a.isErr || json.Unmarshal([]byte(a.text), &v) != nil {
		t.Fatal(a.text)
	}
	found := false
	for _, line := range v.Live {
		if line.Tool == "destroy" && line.ID == parityVM && line.Outcome == "unknown" && line.Result == "cleanup_unconfirmed" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing pending destroy: %s", a.text)
	}
	for _, line := range v.Would {
		if line.Tool == "destroy" {
			t.Errorf("would close a destroy with an outstanding address: %s", a.text)
		}
	}
	failedAttempts := releases
	refuseRelease = false
	newProcess()
	code, stdout, stderr = runCLI(t, "", "--json", "destroy", "--yes")
	if code != 0 || releases <= failedAttempts {
		t.Fatalf("retry: exit %d releases %d: %s%s", code, releases, stdout, stderr)
	}
	a = check()
	if a.isErr || json.Unmarshal([]byte(a.text), &v) != nil || len(v.Live) != 0 {
		t.Fatalf("after retry: %s", a.text)
	}
}

// The two services return different evidence of the project. Start with
// the lines an async CLI delete actually records, including its projectless line.
func TestContract_HistoryAfterAsyncClusterDelete(t *testing.T) {
	for _, tc := range []struct {
		service, host string
		size          []string
	}{
		{"pg", "postgresql.api.cloud.ru", []string{"--specification-id", "std"}},
		{"redis", "redis.api.cloud.ru", []string{"--cpu", "2", "--ram", "4"}},
	} {
		t.Run(tc.service, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			old := w.rt.extra
			phase := "READY"
			created := false
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if !created || r.Method != "GET" || r.URL.Host != tc.host {
					return old(r)
				}
				switch r.URL.Path {
				case "/v1/clusters/" + parityVM:
					card := map[string]string{"id": parityVM, "status": phase}
					if tc.service == "pg" {
						card["project_id"] = pvProject
					}
					body, err := json.Marshal(card)
					if err != nil {
						t.Fatal(err)
					}
					return contractRT{status: 200, body: string(body)}.RoundTrip(r)
				case "/v1/clusters":
					if r.URL.Query().Get("project_id") != pvProject {
						t.Errorf("unexpected list query: %s", r.URL)
					}
					return contractRT{status: 200, body: fmt.Sprintf(`{"clusters":[{"id":%q}]}`, parityVM)}.RoundTrip(r)
				}
				return old(r)
			}
			newProcess()
			args := append([]string{"--json", "--yes", tc.service, "create", "sample"}, tc.size...)
			code, out, stderr := runCLI(t, "", args...)
			id, _ := parseObject(out)["cluster_id"].(string)
			if code != 0 || id != parityVM {
				t.Fatalf("create: %s%s", out, stderr)
			}
			created = true
			newProcess()
			code, out, stderr = runCLI(t, "", "--json", "--yes", tc.service, "delete", id, "--wait=false")
			if code != 0 || parseObject(out)["status"] != "deleting" {
				t.Fatalf("async delete: %s%s", out, stderr)
			}
			later := clock.Now().Add(time.Hour)
			clock.Now = func() time.Time { return later }
			for _, status := range []string{"ERASING", "ERASED"} {
				phase = status
				a := callStdioTool(t, "history", map[string]any{"check": true}, "--read-only")
				var view struct {
					Live []struct {
						ID string `json:"resource_id"`
					}
					Checked struct{ Gone int }
				}
				if a.isErr || json.Unmarshal([]byte(a.text), &view) != nil {
					t.Fatal(a.text)
				}
				if status == "ERASING" {
					if len(view.Live) != 2 || view.Live[0].ID != id || view.Live[1].ID != id || view.Checked.Gone != 0 {
						t.Fatalf("in progress: %s", a.text)
					}
				} else if len(view.Live) != 0 || view.Checked.Gone == 0 {
					t.Fatalf("after deletion: %s", a.text)
				}
			}
		})
	}
}
