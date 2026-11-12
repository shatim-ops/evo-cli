// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"maps"
	"strings"
	"testing"
)

// The refusals of disk_create and image_create are the planner's: a call
// refused without consent is refused with confirmed=true by the same
// answer, and neither writes (docs/internal/risk/1.9.md, conclusion 119).
func TestGateCompute_RefusalWithoutConsentIsTheRefusalWithIt(t *testing.T) {
	for _, c := range []struct {
		name, tool string
		args       map[string]any
		reason     string
	}{
		{"no name", "disk_create", map[string]any{"size_gb": float64(10), "zone": "ru.AZ-1"}, "USAGE"},
		{"no size", "disk_create", map[string]any{"name": "data", "zone": "ru.AZ-1"}, "USAGE"},
		{"a zone the catalogue does not have", "disk_create", map[string]any{"name": "data", "size_gb": float64(10), "zone": "ru.AZ-99"}, ""},
		{"no disk", "image_create", map[string]any{"name": "img"}, "USAGE"},
		{"no name", "image_create", map[string]any{"disk_id": resMine}, "USAGE"},
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
