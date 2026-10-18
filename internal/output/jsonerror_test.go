// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"testing"
)

func TestJSONError_GoesToErrWriter(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewWithWriters(&stdout, &stderr, ModeJSON, false)

	if err := p.JSONError(2, "AUTH_REQUIRED", "credentials not set", "set CLOUDRU_KEY_ID"); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	want := `{"error":{"code":2,"reason":"AUTH_REQUIRED","message":"credentials not set","hint":"set CLOUDRU_KEY_ID"}}` + "\n"
	if stderr.String() != want {
		t.Errorf("got %q, want %q", stderr.String(), want)
	}
}

// An error without a hint has no "hint" key, not "hint":"" (QA r4 Q10).
func TestJSONError_NoHintOmitted(t *testing.T) {
	var stderr bytes.Buffer
	p := NewWithWriters(&bytes.Buffer{}, &stderr, ModeJSON, false)

	if err := p.JSONError(4, "NOT_FOUND", "dns_zones: not found", ""); err != nil {
		t.Fatal(err)
	}
	want := `{"error":{"code":4,"reason":"NOT_FOUND","message":"dns_zones: not found"}}` + "\n"
	if stderr.String() != want {
		t.Errorf("got %q, want %q", stderr.String(), want)
	}
}
