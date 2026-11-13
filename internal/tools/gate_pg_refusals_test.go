// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"maps"
	"strings"
	"testing"
)

// The refusals of pg_backup_create and pg_db_create are the planner's: a
// call refused without consent is refused with confirmed=true by the same
// answer, and neither writes (docs/internal/risk/1.9.md, conclusion 119).
func TestGatePg_RefusalWithoutConsentIsTheRefusalWithIt(t *testing.T) {
	for _, c := range []struct {
		name, tool string
		args       map[string]any
		reason     string
	}{
		{"no name", "pg_backup_create", map[string]any{"cluster_id": resMine}, "USAGE"},
		{"no cluster_id", "pg_backup_create", map[string]any{"name": "nightly"}, "USAGE"},
		{"a cluster that is gone", "pg_backup_create", map[string]any{"cluster_id": resGone, "name": "nightly"}, ""},
		{"no owner", "pg_db_create", map[string]any{"cluster_id": resMine, "name": "app"}, "USAGE"},
		{"no name", "pg_db_create", map[string]any{"cluster_id": resMine, "owner": "app"}, "USAGE"},
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
			answers[i] = withoutRequestIDs(text)
		}
		if answers[0] != answers[1] {
			t.Errorf("%s, %s: without consent\n%s\nwith confirmed=true\n%s", c.tool, c.name, answers[0], answers[1])
		}
	}
}
