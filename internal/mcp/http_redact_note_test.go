// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// Agent run of 1.7.0 (h1; findings 2.1, №16): over HTTP the VM password of
// provision was masked (H7), while the answer said «The VM password is in
// this answer only». The tool cannot tell its transport: H7 masks, so H7
// says it (decision of «1.8 — релиз», variant (а)) — every answer it masks
// a field of says the values in redacted are hidden by the server's policy,
// whatever the rest of the answer says, and how to get them.
func TestRedact_EveryMaskedFieldIsSaid(t *testing.T) {
	for _, w := range []wording{httpWording, readOnlyWording} {
		for _, f := range HTTPSecretFields {
			text := `{"message":"The VM password is in this answer only (password).","` + f.Name + `":"s3cr3t-value-123"}`
			out := redactSecrets("provision", text, w)
			var m map[string]any
			if err := json.Unmarshal([]byte(out), &m); err != nil {
				t.Fatalf("%s: %v: %s", f.Name, err, out)
			}
			if _, masked := m["redacted"]; !masked {
				continue // a root this answer does not hold as a credential
			}
			note, _ := m["redacted_note"].(string)
			if !strings.Contains(note, "redacted") || !strings.Contains(note, w.restart) {
				t.Errorf("%s (%s): the masked answer does not say its values are hidden and how to get them: note %q", f.Name, w.where, note)
			}
			// «Риск», R-39: a value shown once — a new VM's password — no
			// call shows again (provision's repeat is refused by name,
			// D73): the note does not promise a repeat gets it, and names
			// the way — a reset in the console.
			if strings.Contains(note, "same call") || !strings.Contains(note, "shown only once") || !strings.Contains(note, "console") {
				t.Errorf("%s (%s): the note promises a repeat, or does not say a value shown once is not shown again: %q", f.Name, w.where, note)
			}
		}
	}
	// An answer with nothing masked has no note.
	if out := redactSecrets("vm_list", `{"vms":[]}`, httpWording); strings.Contains(out, "redacted_note") {
		t.Errorf("a note without a mask: %s", out)
	}
}
