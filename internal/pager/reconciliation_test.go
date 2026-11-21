// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pager

import (
	"encoding/json"
	"testing"
)

func TestEvidencePages_OnlyCompleteScopedRowsCanSettleAReceipt(t *testing.T) {
	scope := map[string]string{"projectId": "p", "vpcId": "v"}
	row := map[string]any{"id": "first", "subnet": "192.0.2.0/24"}
	first := map[string]json.RawMessage{"nextPageToken": json.RawMessage(`"next"`), "total": json.RawMessage(`2`)}
	for _, tc := range []struct {
		name     string
		next     json.RawMessage
		row      map[string]any
		complete bool
	}{
		{"complete", nil, map[string]any{"id": "second", "subnet": "198.51.100.0/24", "projectId": "p"}, true},
		{"broken token", json.RawMessage(`{}`), map[string]any{"id": "second", "subnet": "198.51.100.0/24"}, false},
		{"missing promised row", nil, nil, false},
		{"overlapping row", nil, row, false},
		{"wrong parent", nil, map[string]any{"id": "second", "subnet": "198.51.100.0/24", "vpcId": "another"}, false},
		{"no id", nil, map[string]any{"subnet": "198.51.100.0/24"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pages EvidencePages
			if err := pages.Check(first, []map[string]any{row}, scope, "subnet"); err != nil {
				t.Fatal(err)
			}
			envelope := map[string]json.RawMessage{}
			if tc.next != nil {
				envelope["nextPageToken"] = tc.next
			}
			rows := []map[string]any{}
			if tc.row != nil {
				rows = append(rows, tc.row)
			}
			if err := pages.Check(envelope, rows, scope, "subnet"); (err == nil) != tc.complete {
				t.Fatalf("complete=%v: %v", tc.complete, err)
			}
		})
	}
}
