// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"testing"
)

// cert_operation passes the operation on whole, and its marker names the
// service's fields only: the time, the hint and the next step evo adds are
// evo's words («Риск» 293-1).
func TestCertOperation_OwnFieldsAreNotUntrusted(t *testing.T) {
	answer := map[string]any{"id": "op-1", "done": false, "error": map[string]any{"message": "x"}}
	for _, f := range opOwnFields {
		answer[f] = "evo"
	}
	got := decls["cert_operation"].Untrusted.in(answer)
	for _, f := range opOwnFields {
		if slices.Contains(got, f) {
			t.Errorf("_untrusted names %s, a field evo writes: %v", f, got)
		}
	}
	for _, f := range []string{"id", "done", "error"} {
		if !slices.Contains(got, f) {
			t.Errorf("_untrusted leaves out the service's %s: %v", f, got)
		}
	}
}
