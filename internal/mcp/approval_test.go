// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Every confirmation_required answer tells the agent that the approval is
// the user's; nothing else in the answer changes, numbers included.
func TestWithApprovalNote(t *testing.T) {
	in := `{"status":"confirmation_required","message":"call again with confirmed=true","will_create":{"disk_gb":12345678901234567}}`
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(withApprovalNote(in)), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["approval"]) != mustJSON(t, approvalNote) {
		t.Errorf("approval = %s", got["approval"])
	}
	var wc bytes.Buffer
	if err := json.Compact(&wc, got["will_create"]); err != nil || wc.String() != `{"disk_gb":12345678901234567}` {
		t.Errorf("will_create changed: %s", got["will_create"])
	}

	for _, keep := range []string{
		`{"status":"ok","note":"confirmation_required is not this"}`,
		`not json "confirmation_required"`,
		`{"status":"confirmation_required","approval":"own"}`,
	} {
		if out := withApprovalNote(keep); out != keep {
			t.Errorf("withApprovalNote(%s) = %s, want it unchanged", keep, out)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
