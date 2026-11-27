// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"net/http"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
)

// Both moments are March 1 locally, but their billing dates in UTC differ.
// No start or end is passed to the command or either MCP server.
func TestBillingFinops_DefaultPeriodUsesProductClock(t *testing.T) {
	local := time.FixedZone("UTC+03", 3*60*60)
	for _, tc := range []struct {
		at                                   time.Time
		start, end, prevStart, prevEnd, zero string
	}{
		{time.Date(2024, 3, 1, 2, 59, 59, 0, local), "2024-01-31", "2024-02-29", "2024-01-01", "2024-01-30", "2024-03-10"},
		{time.Date(2024, 3, 1, 3, 0, 0, 0, local), "2024-02-01", "2024-03-01", "2024-01-02", "2024-01-31", "2024-03-11"},
	} {
		t.Run(tc.end, func(t *testing.T) {
			w := withParityWorld(t)
			clock.Now = func() time.Time { return tc.at }
			t.Setenv("CLOUDRU_AGREEMENT_ID", parityVM)
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/v2/consumption" {
					return nil, nil
				}
				start, end := r.URL.Query().Get("start_date"), r.URL.Query().Get("end_date")
				current := start == tc.start+"T00:00:00Z" && end == tc.end+"T23:59:59Z"
				previous := start == tc.prevStart+"T00:00:00Z" && end == tc.prevEnd+"T23:59:59Z"
				if !current && !previous {
					t.Errorf("unexpected consumption period %s to %s", start, end)
				}
				// 3000 for 30 days and a balance of 1000 give ten days of runway.
				return contractRT{status: 200, body: `{"consumptions":[{"amount":3000}]}`}.RoundTrip(r)
			}

			code, stdout, stderr := runCLI(t, "", "billing", "finops", "--json")
			if code != 0 {
				t.Fatalf("billing finops exited %d: %s", code, stderr)
			}
			s := parityServers(t)
			for surface, got := range map[string]map[string]any{
				"cli":   parseObject(stdout),
				"stdio": stdioDetails(t, s.stdio, "billing_finops", map[string]any{}),
				"http":  httpDetails(t, s.http, "billing_finops", map[string]any{}),
			} {
				p, _ := got["period"].(map[string]any)
				prev, _ := got["prev_period"].(map[string]any)
				if p["start"] != tc.start || p["end"] != tc.end || p["days"] != float64(30) ||
					prev["start"] != tc.prevStart || prev["end"] != tc.prevEnd || got["projected_zero_date"] != tc.zero {
					t.Errorf("%s: period=%v, previous=%v, projected zero date=%v; want %s..%s, %s..%s, %s",
						surface, p, prev, got["projected_zero_date"], tc.start, tc.end, tc.prevStart, tc.prevEnd, tc.zero)
				}
			}
		})
	}
}
