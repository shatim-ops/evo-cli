// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// Findings ledger 1.8, № 3 and № 66 (R-60; «Риск», conclusion 36 п. 4(а)):
// a live evo mcp read config.yaml on every call, so a person editing the
// profile for their own CLI work moved the agent's session to another
// project. The project and the zone are the server's from its start; a
// changed file is named in every answer (H16 in docs/mcp.md), and the key is read anew (a
// rotated key still works without a restart).
func TestMCP_SessionKeepsItsProjectWhenConfigChanges(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-start")
	calls := mcpTransports(t, nil)
	whoami := func(transport string) map[string]any {
		t.Helper()
		a := calls[transport]("whoami", map[string]any{})
		var m map[string]any
		if a.isErr || json.Unmarshal([]byte(a.text), &m) != nil {
			t.Fatalf("%s: whoami %+v", transport, a)
		}
		return m
	}
	for tr := range calls {
		if m := whoami(tr); m["project_id"] != "p-start" || m["config_changed"] != nil {
			t.Fatalf("%s: before the change %v", tr, m)
		}
	}
	saveProfile(t, "default", "p-edited")
	for tr := range calls {
		m := whoami(tr)
		note, _ := m["config_changed"].(string)
		if m["project_id"] != "p-start" || !strings.Contains(note, "p-start") || !strings.Contains(note, "p-edited") {
			t.Errorf("%s: after the change the session moved or did not say so: %v", tr, m)
		}
	}
	// The key is read anew: a rotated key needs no restart.
	if err := config.SaveProfile("default", config.Profile{ProjectID: "p-edited"},
		config.Credentials{KeyID: "fake0000-key-id-0000-000000000002", Secret: fakeSecret}); err != nil {
		t.Fatal(err)
	}
	for tr := range calls {
		if m := whoami(tr); !strings.HasSuffix(m["key_id"].(string), "0002") {
			t.Errorf("%s: the rotated key is not read: %v", tr, m["key_id"])
		}
	}
}

// «Риск», conclusion 62: a refusal after the change carries config_changed
// too (evo login to the same profile for another project: the key of B in
// the project of A is a 403, and the agent reads why in the refusal); a
// server started before any configuration takes its targets at the first
// call that resolves one (recommendation (а)).
func TestMCP_SessionTargetsAtFirstResolveAndInARefusal(t *testing.T) {
	fc := setupCLI(t)
	calls := mcpTransports(t, nil) // no profile yet
	saveProfile(t, "default", "p-first")
	for tr, call := range calls {
		if a := call("whoami", map[string]any{}); a.isErr || !strings.Contains(a.text, "p-first") || strings.Contains(a.text, "config_changed") {
			t.Fatalf("%s: the first resolve %+v", tr, a)
		}
	}
	// evo login to the same profile for another project: a new key, which
	// IAM refuses in this world.
	if err := config.SaveProfile("default", config.Profile{ProjectID: "p-later"},
		config.Credentials{KeyID: "fake0000-key-id-0000-000000000003", Secret: fakeSecret}); err != nil {
		t.Fatal(err)
	}
	fc.iamStatus = 403
	for tr, call := range calls {
		a := call("whoami", map[string]any{})
		var m map[string]any
		_ = json.Unmarshal([]byte(a.text), &m)
		note, _ := m["config_changed"].(string)
		if !a.isErr || !strings.Contains(note, "p-first") || !strings.Contains(note, "p-later") {
			t.Errorf("%s: a refusal after the change: %+v", tr, a)
		}
	}
}
