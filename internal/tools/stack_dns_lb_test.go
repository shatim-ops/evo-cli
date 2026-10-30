// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// D64 (#624, option B; FB15-47): a public DNS record on the stack's
// balancer (dns.target lb, the default) with no external address on it
// points at an address the internet does not reach — stack_plan refuses it
// before the question (USAGE, no job) and names nlb.external: true. The
// default of nlb.external stays: with external set, or a DNS target that is
// an address, the D64 refusal does not apply.
func TestStackPlan_DNSOnAnInternalBalancerRefused(t *testing.T) {
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil)
	manifest := func(external bool, target string) map[string]any {
		dns := map[string]any{"zone_id": "z-1", "name": "app"}
		if target != "" {
			dns["target"] = target
		}
		return map[string]any{
			"name": "s1",
			"vm":   map[string]any{"name": "web", "private": true},
			"nlb":  map[string]any{"name": "lb1", "port": float64(80), "target": "vm", "external": external},
			"dns":  dns,
		}
	}
	for _, c := range []struct {
		label    string
		external bool
		target   string
		refused  bool
	}{
		{"dns on the default target, internal balancer", false, "", true},
		{"dns.target=lb, internal balancer", false, "lb", true},
		{"dns.target=lb, external balancer", true, "lb", false},
		{"dns.target an address, internal balancer", false, "203.0.113.10", false},
	} {
		m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": manifest(c.external, c.target)})
		refused := isErr && strings.Contains(text, "nlb.external")
		if refused != c.refused {
			t.Errorf("%s: refused by D64 = %v, want %v: %s", c.label, refused, c.refused, text)
			continue
		}
		if refused {
			wantReason(t, c.label, m, text, "USAGE")
			if m["job_id"] != nil {
				t.Errorf("%s: job created: %s", c.label, text)
			}
			if hint, _ := m["hint"].(string); !strings.Contains(hint, "nlb.external: true") {
				t.Errorf("%s: the hint does not name nlb.external: true: %q", c.label, hint)
			}
		}
	}
}
