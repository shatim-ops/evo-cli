// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"testing"
)

// A DELETE followed by a 404 can produce the same CLI result as a read
// that established absence. Count requests as well as checking the answer.
func TestDeleteReadsAbsenceBeforeWriting(t *testing.T) {
	for name, target := range allDeletes() {
		t.Run(name, func(t *testing.T) {
			stackHome(t)
			fake := &apiFake{own: goneWorld}
			reg := apiFakeS3Registry(t, fake)
			args := goneArgs(name, target)
			args["confirmed"] = true
			result, text, failed := callNamed(t, reg, name, args)
			if failed || result["status"] != "already_deleted" {
				t.Fatalf("missing target: %s", text)
			}
			if len(fake.calls) == 0 || contractWrites(fake.calls) != 0 {
				t.Errorf("missing target must be read before any write: %v", fake.calls)
			}
		})
	}
}

// Where the specification has a terminal or deleting state, absence alone
// does not exercise D61. Keep the target present. The healthy control must
// reach the write; stop at that boundary without polling an operation.
// Plan 2.3 (docs/internal/audits/1.10-phase0/plan.md): pending is the
// known extra DELETE on main. Flip it to false when the zero-write control
// passes; keep both the healthy and the terminal case.
func TestDeleteTerminalStateOnlyShrinks(t *testing.T) {
	for _, tc := range []struct {
		tool, healthy, terminal, deletePath string
		pending                             bool
	}{
		{"destroy", "running", "deleting", "/api/v1/vms/" + liveID, false},
		{"pg_db_delete", "READY", "ERASING", "/v1/clusters/" + liveID + "/databases/" + liveName, true},
		{"pg_user_delete", "READY", "ERASING", "/v1/clusters/" + liveID + "/users/" + liveName, true},
		{"secret_delete", "ENABLED", "DESTROYED", "/v2/secret/" + liveName, true},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			for _, state := range []string{tc.healthy, tc.terminal} {
				stackHome(t)
				issued := false
				fake := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
					w.Header().Set("Content-Type", "application/json")
					if r.Method != "GET" && r.Method != "HEAD" {
						if r.Method != "DELETE" || r.URL.Path != tc.deletePath {
							t.Errorf("unexpected write %s %s", r.Method, r.URL.Path)
						}
						issued = true
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"message":"test stopped at DELETE"}`))
						return true
					}
					// The VM list reports deletion in progress. Its next card
					// read completes the wait without a wall-clock delay.
					if tc.tool == "destroy" && state == tc.terminal && r.URL.Path == "/api/v1/vms/"+liveID {
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"message":"not found"}`))
						return true
					}

					item := map[string]any{"id": liveID, "name": liveName, "state": state, "status": state, "project_id": dbaasProject, "projectId": dbaasProject, "path": liveName}
					card := maps.Clone(item)
					for _, key := range []string{"items", "databases", "users"} {
						card[key] = []any{item}
					}
					card["total"] = 1
					_ = json.NewEncoder(w).Encode(card)
					return true
				}}
				reg := apiFakeS3Registry(t, fake)
				target := decls[tc.tool].Delete
				args := map[string]any{target.arg: liveID, "confirmed": true}
				if target.arg == "name" || target.arg == "path" {
					args[target.arg] = liveName
				}
				if target.parent != "" {
					args[target.parent] = liveID
				}
				out, text, failed := callNamed(t, reg, tc.tool, args)
				if state == tc.healthy {
					if !issued {
						t.Fatalf("healthy control never attempted deletion: %v %s", fake.calls, text)
					}
					continue
				}
				stateRefusal := failed && out["reason"] == "USAGE" && strings.Contains(strings.ToUpper(text), strings.ToUpper(state))
				if !issued && (len(fake.calls) == 0 || !(stateRefusal || !failed && deleteSettledInTest(out))) {
					t.Fatalf("terminal control inconclusive: %v %s", fake.calls, text)
				}
				if issued != tc.pending {
					t.Errorf("state %s: DELETE=%t, recorded exception=%t; update shrinking baseline after fixing the delete: %s", state, issued, tc.pending, strings.Join(fake.calls, ", "))
				}
			}
		})
	}
}

// When a delete no longer sends, it must also give a terminal or ongoing
// outcome. A malformed fixture or an unrelated refusal is inconclusive.
func deleteSettledInTest(out map[string]any) bool {
	switch out["status"] {
	case "deleted", "already_deleted", "deleting", "already_deleting":
		return true
	case "ok":
		return out["deleted_vm"] == liveID
	}
	return false
}

// stack_destroy delegates its cloud writes. Observe the delegate at the
// registry boundary as well as the state read from the stored job.
func TestStackDeleteReadsJobBeforeDelegating(t *testing.T) {
	for _, status := range []string{"done", "destroyed"} {
		t.Run(status, func(t *testing.T) {
			stackHome(t)
			r := newTestServer(t)
			job := planJob(t, `{"name":"fixture","secrets":[{"path":"fixture","value":"example"}]}`)
			step := job.Steps[0].Key
			job.Steps[0].Status = "done"
			job.Status = status
			job.Outputs[step] = "ok"
			if err := job.Save(); err != nil {
				t.Fatal(err)
			}
			calls := 0
			r.handlers["secret_delete"] = func(context.Context, map[string]any) (string, bool) {
				calls++
				return toJSON(map[string]any{"status": "deleted"}), false
			}
			out, text, failed := callNamed(t, r, "stack_destroy", map[string]any{"job_id": job.ID, "confirmed": true})
			wantCalls, wantStatus := 1, "destroyed"
			if status == "destroyed" {
				wantCalls, wantStatus = 0, "already_destroyed"
			}
			if failed || out["status"] != wantStatus || calls != wantCalls {
				t.Fatalf("job %s: %d child deletes, want %d; %s", status, calls, wantCalls, text)
			}
		})
	}
}
