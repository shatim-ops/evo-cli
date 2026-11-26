// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"testing"
)

// pg_list and the pg_create name check depend on the same collection.
// A bad second page must also invalidate every row read on the first one.
func TestPGListAndCreateRequireCompleteClusterPages(t *testing.T) {
	const cluster = `{"id":"existing-id","name":"existing"}`
	for _, tc := range []struct {
		name     string
		pages    []string
		wantRows int // -1 is an invalid response
	}{
		{"empty", []string{`{"clusters":[]}`}, 0},
		{"nonempty", []string{`{"clusters":[` + cluster + `]}`}, 1},
		{"null root", []string{`null`}, -1},
		{"null collection", []string{`{"clusters":null}`}, -1},
		{"string as collection", []string{`{"clusters":"not a collection"}`}, -1},
		{"object as collection", []string{`{"clusters":{}}`}, -1},
		{"null row", []string{`{"clusters":[null]}`}, -1},
		{"invalid continuation", []string{`{"clusters":[` + cluster + `],"next_page_token":12}`}, -1},
		{"invalid second page", []string{`{"clusters":[` + cluster + `],"next_page_token":"next"}`, `{"clusters":false}`}, -1},
	} {
		for _, name := range []string{"pg_list", "pg_create"} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				var pageReads, laterReads, writes int
				catalogue := pgCatalogue(`{"versions":[{"id":"v17","name":"17"}]}`)
				fake := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
					if r.URL.Host != "postgresql.api.cloud.ru" {
						return false
					}
					switch {
					case r.Method != http.MethodGet:
						writes++
						http.Error(w, "unexpected write", http.StatusMethodNotAllowed)
					case r.URL.Path == "/v1/clusters":
						pageReads++
						if pageReads > len(tc.pages) {
							http.Error(w, "unexpected page", http.StatusBadRequest)
							break
						}
						if pageReads == 2 && r.URL.Query().Get("page_token") != "next" {
							t.Errorf("second read used page token %q", r.URL.Query().Get("page_token"))
						}
						_, _ = io.WriteString(w, tc.pages[pageReads-1])
					default:
						laterReads++
						return catalogue(w, r)
					}
					return true
				}}
				args := map[string]any{}
				if name == "pg_create" {
					args = map[string]any{"name": "requested", "specification_id": "std", "version": "17", "subnet_id": "sn-1"}
				}
				got, raw, isError := callNamed(t, apiFakeRegistry(t, fake), name, args)
				if writes != 0 || pageReads != len(tc.pages) {
					t.Errorf("writes = %d, pages = %d, want 0 and %d", writes, pageReads, len(tc.pages))
				}
				if tc.wantRows < 0 {
					if !isError || got["reason"] != "API_ERROR" || got["clusters"] != nil || laterReads != 0 {
						t.Errorf("incomplete read: isError=%v reason=%v clusters=%v laterReads=%d", isError, got["reason"], got["clusters"], laterReads)
					}
					return
				}
				if name == "pg_create" {
					if got["status"] != "confirmation_required" {
						t.Errorf("valid cluster list did not reach preview: %s", raw)
					}
					return
				}
				rows, ok := got["clusters"].([]any)
				if isError || !ok || len(rows) != tc.wantRows {
					t.Fatalf("valid cluster list: %s", raw)
				}
				if tc.wantRows == 1 {
					row, _ := rows[0].(map[string]any)
					if row["id"] != "existing-id" {
						t.Errorf("returned cluster = %v", row)
					}
				}
			})
		}
	}
}
