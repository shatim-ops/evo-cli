// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The terminal card must belong to the same project even when an old
// deletion entry has no project of its own. An earlier entry for that
// resource inherits only a terminal result whose scope was verified.
func TestHistory_LegacyClusterDeletionScope(t *testing.T) {
	for _, tool := range []string{"pg_delete", "redis_delete"} {
		for _, tc := range []struct {
			name, recorded, card                   string
			bound                                  bool
			entries, wantLive, wantGone, wantReads int
		}{
			{"legacy-own", "", ownProject, false, 1, 0, 1, 1},
			{"legacy-other", "", ownOther, false, 1, 1, 0, 1},
			{"legacy-unknown", "", "", false, 1, 1, 0, 1},
			{"legacy-other-supersedes", "", ownOther, false, 2, 2, 0, 1},
			{"recorded-own", ownProject, ownProject, false, 1, 0, 1, 1},
			{"recorded-other", ownProject, ownOther, false, 1, 1, 0, 1},
			{"bounded-legacy", "", ownOther, true, 1, 0, 0, 0},
		} {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				checkAt(t)
				r := ownerRegistry(t, &ownerFake{}, tc.bound, ownProject)
				eng, err := r.newEngine(nil)
				if err != nil {
					t.Fatal(err)
				}
				hdir := history.DirOf(intent.Dir(eng.StateDir))
				for i := 0; i < tc.entries; i++ {
					line := history.Line{
						Time: fmt.Sprintf("2026-11-17T10:0%d:00Z", i), Tool: tool, Kind: resourceKind(tool),
						ResourceID: resMine, ProjectID: tc.recorded, Outcome: history.Unknown,
						Key: eng.Maker(), IntentID: fmt.Sprintf("%016x", i+1),
					}
					// Older PostgreSQL entries stored the ID in the target only.
					if tool == "pg_delete" && tc.recorded == "" && i == 0 {
						line.Target = "cluster_id=" + resMine
						line.ResourceID = ""
					}
					if err := history.Append(hdir, line); err != nil {
						t.Fatal(err)
					}
				}
				reads := 0
				httpretry.DefaultBase = &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
					if req.Method != http.MethodGet {
						t.Fatalf("unexpected %s", req.Method)
					}
					switch req.URL.Path {
					case "/v1/clusters/" + resMine:
						reads++
						fmt.Fprintf(w, `{"id":%q,"project_id":%q,"status":"DELETED"}`, resMine, tc.card)
					case "/v1/clusters":
						// The fallback project list for a legacy Redis card
						// contains no matching resource.
						if tool != "redis_delete" || req.URL.Query().Get("project_id") != ownProject {
							t.Fatalf("unexpected scoped list %v", req.URL)
						}
						fmt.Fprint(w, `{"clusters":[]}`)
					default:
						t.Fatalf("unexpected read %s", req.URL)
					}
					return true
				}}
				text, failed := r.handlers["history"](context.Background(), map[string]any{"check": true, "since": "2026-11-01"})
				if failed {
					t.Fatal(text)
				}
				var got struct {
					Live    []historyLine
					Checked historyChecked
				}
				if err := json.Unmarshal([]byte(text), &got); err != nil {
					t.Fatal(err)
				}
				if reads != tc.wantReads || got.Checked.Gone != tc.wantGone || got.Checked.WouldClose != 0 || len(got.Live) != tc.wantLive {
					t.Fatalf("GET by ID=%d, gone=%d, would_close=%d, live=%d; want %d, %d, 0, %d",
						reads, got.Checked.Gone, got.Checked.WouldClose, len(got.Live), tc.wantReads, tc.wantGone, tc.wantLive)
				}
				if len(got.Live) > 0 && got.Live[len(got.Live)-1].CheckResult != checkReadFailed {
					t.Fatalf("latest live result=%q; want %q", got.Live[len(got.Live)-1].CheckResult, checkReadFailed)
				}
				if len(got.Live) == 2 && got.Live[0].CheckResult != checkLater {
					t.Fatalf("earlier live result=%q; want %q", got.Live[0].CheckResult, checkLater)
				}
			})
		}
	}
}
