// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/mcp"
)

// Fictitious projects of the --allow-project tests.
const (
	allowedProject = "00000000-0000-4000-8000-000000000001"
	otherProject   = "00000000-0000-4000-8000-000000000002"
)

// consumptionTrip records billing consumption queries and answers one row;
// any other request to a Cloud.ru API (the billing balance) is a 500 here,
// never the network. The rest goes to the transport that was already
// installed (the fake IAM and Organization API of setupCLI).
type consumptionTrip struct {
	base http.RoundTripper
	mu   sync.Mutex
	qs   []url.Values
}

func (c *consumptionTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(r.URL.Hostname(), ".cloud.ru") {
		return c.base.RoundTrip(r)
	}
	rec := httptest.NewRecorder()
	if r.URL.Host != "organization.api.cloud.ru" || r.URL.Path != "/v2/consumption" {
		http.Error(rec, "unexpected "+r.URL.Path, http.StatusInternalServerError)
		return rec.Result(), nil
	}
	c.mu.Lock()
	c.qs = append(c.qs, r.URL.Query())
	c.mu.Unlock()
	rec.Header().Set("Content-Type", "application/json")
	_, _ = rec.WriteString(`{"consumptions":[{"servname":"Compute","resource_name":"vm-other","amount":10,"amount_nds":12}]}`)
	return rec.Result(), nil
}

// queries returns the queries recorded since the first n.
func (c *consumptionTrip) queries(n int) []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]url.Values(nil), c.qs[n:]...)
}

func (c *consumptionTrip) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.qs)
}

// installConsumptionTrip routes the billing API of the test to a
// consumptionTrip.
func installConsumptionTrip(t *testing.T) *consumptionTrip {
	t.Helper()
	trip := &consumptionTrip{base: http.DefaultTransport}
	prev := http.DefaultTransport
	http.DefaultTransport = trip
	t.Cleanup(func() { http.DefaultTransport = prev })
	return trip
}

// toolCall calls one tool and returns the answer.
type toolCall func(name string, args map[string]any) roAnswer

// mcpTransports are evo mcp's servers on allowProjects (newMCPServer, as
// runMCP builds it), over stdio and over HTTP.
func mcpTransports(t *testing.T, allowProjects []string) map[string]toolCall {
	t.Helper()
	params := func(name string, args map[string]any) any {
		return map[string]any{"name": name, "arguments": args}
	}
	stdio := newMCPServer(allowProjects)
	h := mcp.NewHTTPHandler(newMCPServer(allowProjects), mcp.HTTPOptions{BearerToken: "review-token"})
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		h.Close()
		ts.Close()
	})
	return map[string]toolCall{
		"stdio": func(name string, args map[string]any) roAnswer {
			out := &bytes.Buffer{}
			if err := stdio.Run(bytes.NewReader(append(roRequest("tools/call", params(name, args)), '\n')), out); err != nil {
				t.Fatalf("stdio %s: %v", name, err)
			}
			a, _ := roDecode(t, out.Bytes())
			return a
		},
		"http": func(name string, args map[string]any) roAnswer {
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(roRequest("tools/call", params(name, args))))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Authorization", "Bearer review-token")
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			a, _ := roDecode(t, raw)
			return a
		},
	}
}

// billingTools are the tools that read the consumption of the agreement.
var billingTools = []string{"billing_consumption", "billing_finops"}

// TestReviewParity_RP_1_BillingAgreementBypassesAllowProject: with
// --allow-project the only documented reads of another project are
// org_quotas and iam_service_accounts (docs/mcp.md, H9). billing_consumption
// and billing_finops queried the whole agreement when CLOUDRU_AGREEMENT_ID
// was set and project_id omitted. Bound to its project, the server reads the
// agreement within the server's project, over stdio and HTTP.
func TestReviewParity_RP_1_BillingAgreementBypassesAllowProject(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", allowedProject)
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-review")
	trip := installConsumptionTrip(t)

	for transport, call := range mcpTransports(t, []string{allowedProject}) {
		for _, name := range billingTools {
			before := trip.count()
			a := call(name, map[string]any{"start": "2026-10-01", "end": "2026-10-01"})
			if a.isErr || a.rpcErr != "" {
				t.Fatalf("%s %s: %s%s", transport, name, a.text, a.rpcErr)
			}
			got := trip.queries(before)
			if len(got) == 0 {
				t.Fatalf("%s %s made no consumption request: %s", transport, name, a.text)
			}
			for _, q := range got {
				if q.Get("project_ids") != allowedProject || q.Get("agreement_id") != "agr-review" {
					t.Errorf("%s %s query %v: want project_ids=%s within the agreement (agreement-wide read is outside the closed --allow-project exceptions)",
						transport, name, q, allowedProject)
				}
			}
			if obj := parseObject(a.text); obj["scope"] != "project" || obj["project_id"] != allowedProject {
				t.Errorf("%s %s: answer %s, want scope project", transport, name, a.text)
			}
		}
	}
}

// Without --allow-project the server, like the CLI, reads the whole
// agreement as documented; with the flag, project_id of an allowed project
// reads that project as before.
func TestAllowProject_BillingScopeOtherwiseUnchanged(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", allowedProject)
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-review")
	trip := installConsumptionTrip(t)

	check := func(where string, a roAnswer, before int, project string) {
		t.Helper()
		if a.isErr || a.rpcErr != "" {
			t.Fatalf("%s: %s%s", where, a.text, a.rpcErr)
		}
		got := trip.queries(before)
		if len(got) == 0 {
			t.Fatalf("%s made no consumption request: %s", where, a.text)
		}
		for _, q := range got {
			if q.Get("agreement_id") != "agr-review" || q.Get("project_ids") != project {
				t.Errorf("%s query %v: want agreement_id agr-review and project_ids %q", where, q, project)
			}
		}
		scope := "agreement"
		if project != "" {
			scope = "project"
		}
		if obj := parseObject(a.text); obj["scope"] != scope {
			t.Errorf("%s: answer %s, want scope %s", where, a.text, scope)
		}
	}
	args := map[string]any{"start": "2026-10-01"}
	for transport, call := range mcpTransports(t, nil) {
		for _, name := range billingTools {
			before := trip.count()
			check(transport+" "+name+" without --allow-project", call(name, args), before, "")
		}
	}
	for transport, call := range mcpTransports(t, []string{allowedProject, otherProject}) {
		for _, name := range billingTools {
			before := trip.count()
			check(transport+" "+name+" project_id", call(name, map[string]any{"start": "2026-10-01", "project_id": otherProject}), before, otherProject)
		}
	}
	reg := newRegistry() // the CLI's: evo billing, evo tools call
	for _, name := range billingTools {
		tl, _ := reg.Lookup(name)
		before := trip.count()
		text, isErr := tl.Handler(context.Background(), args)
		check("CLI "+name, roAnswer{text: text, isErr: isErr}, before, "")
	}
}

// Bound to its project, org_projects lists the server's project, not the
// organization's; without the flag every project the key sees.
func TestAllowProject_OrgProjectsListsItsProject(t *testing.T) {
	fc := setupCLI(t)
	fc.orgProject = `[{"id":"` + allowedProject + `","name":"mine","status":"active"},{"id":"` + otherProject + `","name":"theirs","status":"active"}]`
	saveProfile(t, "default", allowedProject)
	for _, c := range []struct {
		allow []string
		want  []string
	}{{[]string{allowedProject}, []string{allowedProject}}, {nil, []string{allowedProject, otherProject}}} {
		for transport, call := range mcpTransports(t, c.allow) {
			a := call("org_projects", map[string]any{})
			list, _ := parseObject(a.text)["projects"].([]any)
			var ids []string
			for _, p := range list {
				m, _ := p.(map[string]any)
				id, _ := m["id"].(string)
				ids = append(ids, id)
			}
			if a.isErr || strings.Join(ids, ",") != strings.Join(c.want, ",") {
				t.Errorf("%s --allow-project %v: projects %v, want %v: %s", transport, c.allow, ids, c.want, a.text)
			}
		}
	}
}

// Bound, with no project of its own: CONFIG_INVALID with the hint to pass
// project_id, and no consumption request.
func TestAllowProject_BillingWithoutServerProject(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "")
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-review")
	trip := installConsumptionTrip(t)
	for transport, call := range mcpTransports(t, []string{allowedProject}) {
		for _, name := range billingTools {
			a := call(name, map[string]any{"start": "2026-10-01"})
			obj := parseObject(a.text)
			hint, _ := obj["hint"].(string)
			if !a.isErr || obj["reason"] != "CONFIG_INVALID" || !strings.Contains(hint, "project_id") {
				t.Errorf("%s %s: %s, want CONFIG_INVALID with project_id in the hint", transport, name, a.text)
			}
		}
	}
	if n := trip.count(); n != 0 {
		t.Errorf("%d consumption requests, want none", n)
	}
}
