// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

func TestVPCCreateIntentAtSend(t *testing.T) {
	for _, mode := range []string{"occupied", "created", "unwritable"} {
		t.Run(mode, func(t *testing.T) {
			const name = "vpc-test"
			var root string
			var gets, posts int
			f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host != "vpc.api.cloud.ru" || r.URL.Path != "/v1/vpcs" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
				}
				pending := intent.Pending(intent.Dir(root))
				switch r.Method {
				case http.MethodGet:
					gets++
					if len(pending) != 0 {
						t.Errorf("name lookup already has an intent: %+v", pending)
					}
					if mode == "occupied" {
						fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":%q}]}`, auditUUID, name)
					} else {
						fmt.Fprint(w, `{"vpcs":[]}`)
					}
				case http.MethodPost:
					posts++
					if len(pending) != 1 || pending[0].Tool != "vpc_create" || pending[0].Name != name || !pending[0].Sent {
						t.Errorf("POST without exactly one sent VPC intent: %+v", pending)
					}
					fmt.Fprintf(w, `{"id":"op-1","resourceId":%q}`, auditUUID)
				default:
					t.Fatalf("unexpected method %s", r.Method)
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
				if fail || gets != 1 || posts != 1 || len(lines) != 1 || lines[0].Outcome != history.Created || lines[0].ResourceID != auditUUID || lines[0].OperationID != "op-1" {
					t.Fatalf("GET=%d POST=%d history=%+v answer=%s", gets, posts, lines, raw)
				}
			} else {
				want := "USAGE"
				if mode == "unwritable" {
					want = "CONFIG_INVALID"
				}
				if !fail || answer["reason"] != want || gets < 1 || posts != 0 || len(lines) != 0 {
					t.Fatalf("GET=%d POST=%d history=%+v answer=%s", gets, posts, lines, raw)
				}
			}
			if pending := intent.Pending(intent.Dir(root)); len(pending) != 0 {
				t.Fatalf("pending after definite outcome: %+v", pending)
			}
		})
	}
}
