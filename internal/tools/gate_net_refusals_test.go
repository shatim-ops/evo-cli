// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"maps"
	"strings"
	"testing"
)

// The refusals of vpc_create, vpc_route_create, ar_create, snat_create,
// dns_zone_create and tg_create are the planner's: a call refused without
// consent is refused with confirmed=true by the same answer, and neither
// writes (docs/internal/risk/1.9.md, conclusion 119).
func TestGateNet_RefusalWithoutConsentIsTheRefusalWithIt(t *testing.T) {
	for _, c := range []struct {
		name, tool string
		args       map[string]any
		reason     string
	}{
		{"no name", "vpc_create", map[string]any{}, "USAGE"},
		{"the name is taken", "vpc_create", map[string]any{"name": "res-a1"}, "USAGE"},
		{"no destination", "vpc_route_create", map[string]any{"vpc_id": resMine}, "USAGE"},
		{"no next hop", "vpc_route_create", map[string]any{"vpc_id": resMine, "subnet": "192.0.2.0/24"}, "USAGE"},
		{"no name", "ar_create", map[string]any{}, "USAGE"},
		{"no name", "snat_create", map[string]any{}, "USAGE"},
		{"no domain", "dns_zone_create", map[string]any{}, "USAGE"},
		{"vpc_id of a public zone", "dns_zone_create", map[string]any{"domain": "example.com.", "vpc_id": resMine}, "USAGE"},
		{"no name", "tg_create", map[string]any{}, "USAGE"},
	} {
		var answers [2]string
		for i, confirmed := range []bool{false, true} {
			f := &ownerFake{}
			r := ownerRegistry(t, f, false, ownProject)
			m, text, isErr := callOwned(t, r, c.tool, maps.Clone(c.args), confirmed)
			if !isErr || m["status"] != "error" || (c.reason != "" && m["reason"] != c.reason) {
				t.Errorf("%s, %s (confirmed %v): %s, want a refusal %s", c.tool, c.name, confirmed, text, c.reason)
			}
			for _, q := range f.log() {
				if !strings.HasPrefix(q, "GET ") {
					t.Errorf("%s, %s (confirmed %v): the refused call sent %s", c.tool, c.name, confirmed, q)
				}
			}
			answers[i] = text
		}
		if answers[0] != answers[1] {
			t.Errorf("%s, %s: without consent\n%s\nwith confirmed=true\n%s", c.tool, c.name, answers[0], answers[1])
		}
	}
}
