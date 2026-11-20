// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

func TestHistoryRedisProjectMembership(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lastPage string
		status   int
		pages    int
		gone     bool
	}{
		{"found", "{}", 200, 2, true},
		{"missing", "{}", 200, 2, false},
		{"late-null", "null", 200, 2, false},
		{"forbidden", "{}", 403, 2, false},
		{"list-404", "{}", 404, 2, false},
		{"budget", "{}", 200, historyCheckMax - 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkAt(t)
			r := ownerRegistry(t, &ownerFake{}, false, ownProject)
			eng, err := r.newEngine(nil)
			if err != nil {
				t.Fatal(err)
			}
			l := history.Line{Format: history.Format, Time: "2026-11-17T10:00:00Z", Tool: "redis_create", Kind: resourceKind("redis_create"), ResourceID: resMine, ProjectID: ownProject, Key: eng.Maker(), Outcome: history.Created}
			pages := 0
			httpretry.DefaultBase = &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
				if req.Method != "GET" {
					t.Fatalf("unexpected %s", req.Method)
				}
				switch req.URL.Path {
				case "/v1/clusters/" + resMine:
					fmt.Fprintf(w, `{"id":%q,"status":"ERASED"}`, resMine)
				case "/v1/clusters":
					if req.URL.Query().Get("project_id") != ownProject {
						t.Fatal(req.URL)
					}
					pages++
					if pages == 1 || tc.name == "budget" {
						id := resMine
						if tc.name == "missing" {
							id = resOther
						}
						fmt.Fprintf(w, `{"clusters":[{"id":%q}],"next_page_token":"%d"}`, id, pages)
					} else {
						w.WriteHeader(tc.status)
						fmt.Fprint(w, tc.lastPage)
					}
				default:
					t.Fatalf("unexpected path %s", req.URL)
				}
				return true
			}}
			out, results := r.checkLive(context.Background(), []history.Line{l}, []int{0}, eng)
			want := checkReadFailed
			if tc.gone {
				want = checkGone
			}
			if pages != tc.pages || out.Read != 1 || results[0] != want || out.WouldClose != 0 {
				t.Fatalf("pages=%d checked=%+v results=%v", pages, out, results)
			}
		})
	}
}
