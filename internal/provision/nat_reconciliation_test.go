// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/pager"
)

func TestNATReconciliation_OffsetTotals(t *testing.T) {
	first := `{"items":[{"id":"a"}],"total":2}`
	for _, tc := range []struct {
		name, last string
		valid      bool
	}{
		{"same total", `{"items":[{"id":"b"}],"total":2}`, true},
		{"increased", `{"items":[{"id":"b"}],"total":3}`, false},
		{"decreased", `{"items":[{"id":"b"}],"total":1}`, false},
		{"omitted", `{"items":[{"id":"b"}]}`, true},
		{"complete array", `[{"id":"b"}]`, true},
		{"extra array row", `[{"id":"b"},{"id":"c"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var evidence pager.EvidencePages
			if err := validateNATPage([]byte(first), "p", nil, 0, &evidence); err != nil {
				t.Fatal(err)
			}
			err := validateNATPage([]byte(tc.last), "p", map[string]bool{"a": true}, 2, &evidence)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
		})
	}
}
