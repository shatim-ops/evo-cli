// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"maps"
	"strings"
	"testing"
)

// The refusals the planners of secret_*, cert_*, k8s_add_addon, ca_start
// and ca_stop make are the same with and without consent, and neither
// call writes (docs/internal/risk/1.9.md, conclusion 119). What these
// tools check after the question — the client of the service, and for a
// certificate its taken name — is not here: it stays in the execution, as
// it was (the inventory of the gate, variant А).
func TestGateMisc_RefusalWithoutConsentIsTheRefusalWithIt(t *testing.T) {
	for _, c := range []struct {
		name, tool string
		args       map[string]any
		reason     string
	}{
		{"no path", "secret_create", map[string]any{"value": "v"}, "USAGE"},
		{"no path", "secret_value", map[string]any{}, "USAGE"},
		{"no value", "secret_add_version", map[string]any{"path": "app/db"}, "USAGE"},
		{"no dns_names", "cert_issue_le", map[string]any{"name": "site"}, "USAGE"},
		{"no private_key", "cert_import", map[string]any{"name": "byo", "server_certificate": "Y2VydA=="}, "USAGE"},
		{"no certificate_id", "cert_renew", map[string]any{}, "USAGE"},
		{"no name", "k8s_add_addon", map[string]any{"cluster_id": resMine}, "USAGE"},
		{"no name", "ca_start", map[string]any{}, "USAGE"},
		{"no name", "ca_stop", map[string]any{}, "USAGE"},
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
