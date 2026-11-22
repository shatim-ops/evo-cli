// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func TestVPCCreateIntentAtSend(t *testing.T) {
	for _, mode := range []string{"occupied", "created", "unwritable"} {
		t.Run(mode, func(t *testing.T) {
			const name = "vpc-test"
			var root string
			var gets, cards, posts int
			var sentID string
			f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host != "vpc.api.cloud.ru" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
				}
				pending := intent.Pending(intent.Dir(root))
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/vpcs/"+auditUUID && r.URL.RawQuery == "" && mode == "created":
					cards++
					if posts != 1 || len(pending) != 1 || pending[0].ID != sentID || pending[0].ProjectID != dbaasProject || !pending[0].Sent {
						t.Fatalf("resource read outside the accepted create: posts=%d pending=%+v", posts, pending)
					}
					fmt.Fprintf(w, `{"id":%q,"name":%q,"projectId":%q}`, auditUUID, name, dbaasProject)
				case r.Method == http.MethodGet && r.URL.Path == "/v1/vpcs" && r.URL.Query().Get("project_id") == dbaasProject:
					gets++
					// A second process must not pass planning before the first
					// has opened its intent. Observe the real shared lock at GET.
					if mode == "created" {
						unlock, err := state.FileLock(filepath.Join(config.DataDir(root), "vpc_create.lock"), 10*time.Millisecond)
						if err == nil {
							unlock()
							t.Fatal("unserialized VPC name lookup")
						}
						if !errors.Is(err, state.ErrFileLocked) {
							t.Fatal(err)
						}
					}
					if len(pending) != 0 {
						t.Errorf("name lookup already has an intent: %+v", pending)
					}
					if mode == "occupied" {
						fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":%q}]}`, auditUUID, name)
					} else {
						fmt.Fprint(w, `{"vpcs":[]}`)
					}
				case r.Method == http.MethodPost && r.URL.Path == "/v1/vpcs" && r.URL.RawQuery == "":
					var body struct{ Name, ProjectID string }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name != name || body.ProjectID != dbaasProject {
						t.Fatalf("unexpected VPC create body: %+v err=%v", body, err)
					}
					posts++
					if len(pending) != 1 || pending[0].Tool != "vpc_create" || pending[0].Name != name || !pending[0].Sent {
						t.Fatalf("POST without exactly one sent VPC intent: %+v", pending)
					}
					sentID = pending[0].ID
					fmt.Fprintf(w, `{"id":"op-1","resourceId":%q}`, auditUUID)
				default:
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
				}
				return true
			}}
			r := apiFakeRegistry(t, f)
			root, _ = r.stateDir()
			if mode == "unwritable" {
				dir := intent.Dir(root)
				if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dir, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			answer, raw, fail := callNamed(t, r, "vpc_create", map[string]any{"name": name, "confirmed": true})
			lines, _ := historyOf(t, root)
			if mode == "created" {
				if fail || gets != 1 || cards != 1 || posts != 1 || len(lines) != 1 || lines[0].IntentID != sentID || lines[0].ProjectID != dbaasProject || lines[0].Outcome != history.Created || lines[0].ResourceID != auditUUID || lines[0].OperationID != "op-1" {
					t.Fatalf("list=%d card=%d POST=%d history=%+v answer=%s", gets, cards, posts, lines, raw)
				}
			} else {
				want := "USAGE"
				if mode == "unwritable" {
					want = "CONFIG_INVALID"
				}
				if !fail || answer["reason"] != want || gets < 1 || cards != 0 || posts != 0 || len(lines) != 0 {
					t.Fatalf("list=%d card=%d POST=%d history=%+v answer=%s", gets, cards, posts, lines, raw)
				}
			}
			if pending := intent.Pending(intent.Dir(root)); len(pending) != 0 {
				t.Fatalf("pending after definite outcome: %+v", pending)
			}
		})
	}
}

func TestVPCDeletePreviewReadsOnceAndConfirmationReadsAgain(t *testing.T) {
	var cards int
	f := &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
		if req.Method != "GET" || req.URL.Path != "/v1/vpcs/"+auditUUID {
			t.Fatalf("unexpected write or read: %s %s", req.Method, req.URL)
		}
		cards++
		status := "ACTIVE"
		if cards > 1 {
			status = "DELETED"
		}
		fmt.Fprintf(w, `{"id":%q,"name":"net","projectId":%q,"status":%q}`, auditUUID, ownForeign, status)
		return true
	}}
	r := apiFakeRegistry(t, f)
	preview, text, refused := callNamed(t, r, "vpc_delete", map[string]any{"id": auditUUID})
	if !refused || preview["status"] != "confirmation_required" || preview["project_id"] != ownForeign || cards != 1 {
		t.Fatalf("card=%d preview=%s", cards, text)
	}
	after, text, failed := callNamed(t, r, "vpc_delete", map[string]any{"id": auditUUID, "confirmed": true})
	if failed || after["status"] != "already_deleted" || cards != 2 {
		t.Fatalf("card=%d outcome=%s", cards, text)
	}
}
