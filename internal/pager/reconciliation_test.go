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

func TestEvidencePages_TotalMustAgreeWithRowsAndEarlierPages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		totals []string
		valid  bool
	}{
		{"omitted", []string{""}, true},
		{"zero with one row", []string{"0"}, false},
		{"stable total", []string{"2", "2"}, true},
		{"implied total from first page", []string{"2", ""}, true},
		{"total increases", []string{"1", "2"}, false},
		{"total decreases", []string{"2", "1"}, false},
		{"extra row", []string{"1", ""}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pages EvidencePages
			var err error
			for i, total := range tc.totals {
				envelope := map[string]json.RawMessage{}
				if i+1 < len(tc.totals) {
					envelope["nextPageToken"] = json.RawMessage(`"next"`)
				}
				if total != "" {
					envelope["total"] = json.RawMessage(total)
				}
				row := map[string]any{"id": []string{"a", "b"}[i]}
				if err = pages.Check(envelope, []map[string]any{row}, nil); err != nil {
					break
				}
			}
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
		})
	}
}
