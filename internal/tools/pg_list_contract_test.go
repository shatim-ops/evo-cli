// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"testing"
	"testing/iotest"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
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

// A complete JSON document is still an incomplete HTTP response if reading
// the body ends with an error. In particular it cannot establish that a
// cluster name is free. Check the collection and the consumer that may write.
func TestClusterListAndCreateRequireCompleteHTTPBody(t *testing.T) {
	for _, service := range []string{"kafka", "redis", "pg"} {
		for _, action := range []string{"list", "create"} {
			for _, tc := range []struct {
				name     string
				pages    int
				broken   int    // one-based; zero means clean EOF
				lastBody string // explicit wire representation of the last page
				invalid  bool
			}{
				{name: "empty first page cut short", pages: 1, broken: 1, invalid: true},
				{name: "empty final page cut short", pages: 2, broken: 2, invalid: true},
				{name: "complete two pages", pages: 2},
				{name: "omitted collection on final page", pages: 2, lastBody: `{}`},
				{name: "null collection on final page", pages: 2, lastBody: `{"clusters":null}`, invalid: true},
				{name: "null continuation on final page", pages: 2, lastBody: `{"clusters":[],"next_page_token":null}`, invalid: true},
			} {
				t.Run(service+"/"+action+"/"+tc.name, func(t *testing.T) {
					var pages, writes int
					catalogue := pgCatalogue(`{"versions":[{"id":"v17","name":"17"}]}`)
					fake := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
						if r.URL.Path != "/v1/clusters" {
							return catalogue(w, r)
						}
						if r.Method != http.MethodGet {
							writes++
							return dbaasCreateOK(w, r)
						}
						pages++
						token := r.URL.Query().Get("page_token")
						switch {
						case pages == 1 && tc.pages == 2 && token == "":
							io.WriteString(w, `{"clusters":[{"id":"existing-id","name":"existing"}],"next_page_token":"second"}`)
						case pages == 1 && tc.pages == 1 && token == "", pages == 2 && token == "second":
							body := tc.lastBody
							if body == "" {
								body = `{"clusters":[]}`
							}
							io.WriteString(w, body)
						default:
							t.Fatalf("unexpected cluster page %d, token %q", pages, token)
						}
						return true
					}}
					reg := apiFakeRegistry(t, fake)
					// Keep the ordinary service transport and its actual page parser.
					// Only the final read from the selected response is changed.
					httpretry.DefaultBase = roundTripFunc(func(r *http.Request) (*http.Response, error) {
						response, err := fake.RoundTrip(r)
						if err == nil && r.Method == http.MethodGet && r.URL.Path == "/v1/clusters" && tc.broken != 0 && tc.broken == pages {
							body := response.Body
							response.Body = struct {
								io.Reader
								io.Closer
							}{io.MultiReader(body, iotest.ErrReader(io.ErrUnexpectedEOF)), body}
						}
						return response, err
					})
					args := map[string]any{}
					if action == "create" {
						args = map[string]any{"name": "requested", "cpu": 2, "ram": 4, "confirmed": true}
						if service == "pg" {
							args = map[string]any{"name": "requested", "version": "17", "specification_id": "std", "subnet_id": "sn-1", "confirmed": true}
						}
					}
					result, raw, failed := callNamed(t, reg, service+"_"+action, args)
					if pages != tc.pages {
						t.Errorf("read %d cluster pages, want %d", pages, tc.pages)
					}
					if tc.invalid {
						if !failed || result["reason"] != "API_ERROR" || result["clusters"] != nil || writes != 0 {
							t.Errorf("incomplete HTTP page: failed=%t, writes=%d, result=%s", failed, writes, raw)
						}
						return
					}
					if failed {
						t.Fatalf("complete page: %s", raw)
					}
					if action == "create" {
						if result["status"] != "creating" || writes != 1 {
							t.Errorf("valid create: writes=%d, result=%s", writes, raw)
						}
						return
					}
					rows, ok := result["clusters"].([]any)
					if !ok || len(rows) != 1 || writes != 0 {
						t.Fatalf("valid list: writes=%d, result=%s", writes, raw)
					}
					row, _ := rows[0].(map[string]any)
					if row["id"] != "existing-id" {
						t.Errorf("list lost the initial page: %s", raw)
					}
				})
			}
		}
	}
}
