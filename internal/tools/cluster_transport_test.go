// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"testing"
	"testing/iotest"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

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
