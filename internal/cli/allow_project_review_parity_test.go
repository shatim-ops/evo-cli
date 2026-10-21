// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// consumptionTrip records billing consumption queries and answers one row.
// Every other request goes to the transport that was already installed
// (IAM in these tests).
type consumptionTrip struct {
	base http.RoundTripper
	mu   sync.Mutex
	qs   []url.Values
}

func (c *consumptionTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "organization.api.cloud.ru" && r.URL.Path == "/v2/consumption" {
		c.mu.Lock()
		c.qs = append(c.qs, r.URL.Query())
		c.mu.Unlock()
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		_, _ = rec.WriteString(`{"consumptions":[{"servname":"Compute","resource_name":"vm-other","amount":10,"amount_nds":12}]}`)
		return rec.Result(), nil
	}
	return c.base.RoundTrip(r)
}

// TestReviewParity_RP_1_BillingAgreementBypassesAllowProject: with
// --allow-project the only documented reads of another project are
// org_quotas and iam_service_accounts (docs/mcp.md, H9). billing_consumption
// and billing_finops still query the whole agreement when
// CLOUDRU_AGREEMENT_ID is set and project_id is omitted: the factory sees
// the server's allowed project and lets the call through, and the request
// has agreement_id and no project_ids.
func TestReviewParity_RP_1_BillingAgreementBypassesAllowProject(t *testing.T) {
	setupCLI(t)
	const allowed = "00000000-0000-4000-8000-000000000001"
	saveProfile(t, "default", allowed)
	t.Setenv("CLOUDRU_AGREEMENT_ID", "agr-review")

	trip := &consumptionTrip{base: http.DefaultTransport}
	prev := http.DefaultTransport
	http.DefaultTransport = trip
	t.Cleanup(func() { http.DefaultTransport = prev })

	reg := tools.New(mcpEngineFactory([]string{allowed}))
	for _, name := range []string{"billing_consumption", "billing_finops"} {
		tl, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("no tool %s", name)
		}
		before := len(trip.qs)
		text, isErr := tl.Handler(context.Background(), map[string]any{
			"start": "2026-10-01", "end": "2026-10-01",
		})
		if isErr {
			t.Fatalf("%s: %s", name, text)
		}
		if strings.Contains(text, "not allowed") {
			t.Fatalf("%s refused the allowed project: %s", name, text)
		}
		got := trip.qs[before:]
		if len(got) == 0 {
			t.Fatalf("%s made no consumption request: %s", name, text)
		}
		for _, q := range got {
			if q.Get("project_ids") != allowed {
				t.Errorf("%s query %v: project_ids=%q, want %s (agreement-wide read is outside the closed --allow-project exceptions)",
					name, q, q.Get("project_ids"), allowed)
			}
		}
	}
}
