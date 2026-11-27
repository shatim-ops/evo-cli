// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// D74 (the owner's decision 03.11): each of the ten deletes that read their
// outcome, and dns_zone_delete (D66) on the same point, takes --wait=false, from the schema's argument wait, and answers
// at once as 'evo tools call' with wait=false does: deleting with the
// operation_id, exit 0.
func TestDeleteWaitFalse_TreeEqualsToolsCall(t *testing.T) {
	w := withParityWorld(t)
	reg := tools.New(nil)
	deletes := map[string]bool{"ar_delete": true, "ca_delete": true, "dns_record_delete": true, "k8s_delete": true, "k8s_nodepool_delete": true,
		"kafka_delete": true, "lb_delete": true, "tg_delete": true, "pg_delete": true, "redis_delete": true, "vpc_delete": true, "dns_zone_delete": true}
	seen := 0
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if !deletes[c.Tool] {
				continue
			}
			seen++
			tl, _ := reg.Lookup(c.Tool)
			args := maps.Clone(parityArgs[c.Tool])
			args["wait"] = false
			pc := parityCommandOf(t, g, c, tl, args)
			extra := []string{"--json", "--yes"}
			newProcess()
			treeCode, treeOut, treeErr := runCLI(t, "", append(append([]string{}, pc.tree...), extra...)...)
			newProcess()
			w.rt.newCall()
			callCode, callOut, _ := runCLI(t, "", append(append([]string{}, pc.call...), extra...)...)
			w.rt.newCall()
			if treeCode != 0 || treeCode != callCode || sameVolatile(treeOut) != sameVolatile(callOut) {
				t.Errorf("evo %s ≠ evo tools call %s: exit %d/%d\n  tree: %s %s\n  call: %s",
					strings.Join(pc.tree, " "), c.Tool, treeCode, callCode, clipLine(treeOut), clipLine(treeErr), clipLine(callOut))
				continue
			}
			if obj := parseObject(treeOut); obj["status"] != "deleting" || obj["operation_id"] == nil {
				t.Errorf("evo %s: want deleting with operation_id at once, got %s", strings.Join(pc.tree, " "), clipLine(treeOut))
			}
			// FB170-4: --wait=false of evo tools call is the tool's wait, as
			// --wait=false of the tree, not a flag the call drops.
			var flag []string
			for i := 0; i < len(pc.call); i++ {
				if pc.call[i] == "--arg" && pc.call[i+1] == "wait=false" {
					i++
					continue
				}
				flag = append(flag, pc.call[i])
			}
			newProcess()
			flagCode, flagOut, flagErr := runCLI(t, "", append(append(flag, "--wait=false"), extra...)...)
			w.rt.newCall()
			if flagCode != treeCode || sameVolatile(flagOut) != sameVolatile(treeOut) {
				t.Errorf("evo tools call %s --wait=false ≠ evo %s: exit %d/%d\n  call: %s %s",
					c.Tool, strings.Join(pc.tree, " "), flagCode, treeCode, clipLine(flagOut), clipLine(flagErr))
			}
		}
	}
	if seen != len(deletes) {
		t.Errorf("commands of the deletes: %d of %d", seen, len(deletes))
	}
}

func runDeleteCLIContext(t *testing.T, ctx context.Context, args ...string) (string, string, int) {
	t.Helper()
	root := newRootCmd()
	root.SetArgs(args)
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	_, err := root.ExecuteContextC(ctx)
	if err != nil {
		handleError(output.NewWithWriters(&out, &errOut, output.ModeJSON, false), err)
	}
	return out.String(), errOut.String(), clerrors.ExitCode(err)
}

// Interrupt the preflight, the DELETE itself, or the poll after its acceptance.
func TestCADeleteCLI_InterruptionOnlyContinuesAStartedDelete(t *testing.T) {
	for _, point := range []string{"before DELETE", "lost DELETE reply", "after DELETE"} {
		t.Run(point, func(t *testing.T) {
			withParityWorld(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base := httpretry.DefaultBase
			writes, reads := 0, 0
			httpretry.DefaultBase = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/v2/containers/web" {
					return base.RoundTrip(r)
				}
				switch r.Method {
				case http.MethodGet:
					reads++
					if ctx.Err() == nil && (point == "before DELETE" || writes == 1) {
						cancel()
						return nil, r.Context().Err()
					}
					return contractRT{200, `{"name":"web","status":"DELETING"}`}.RoundTrip(r)
				case http.MethodDelete:
					writes++
					if point == "lost DELETE reply" {
						cancel()
						return nil, r.Context().Err()
					}
					return contractRT{200, `{"id":"op-web","done":false}`}.RoundTrip(r)
				default:
					t.Fatalf("unexpected %s", r.Method)
					return nil, nil
				}
			})
			t.Cleanup(func() { httpretry.DefaultBase = base })
			out, stderr, code := runDeleteCLIContext(t, ctx, "ca", "delete", "web", "--wait", "--yes", "--json")
			envelope, found := errorEnvelope(stderr)
			if !found || code == 0 || envelope.Code != code || out != "" {
				t.Fatalf("code %d stdout %q stderr %q", code, out, stderr)
			}
			details := envelope.Details
			root, err := stateDir()
			if err != nil {
				t.Fatal(err)
			}
			if point == "before DELETE" {
				if writes != 0 || reads != 1 || len(intentFiles(root)) != 0 || details["operation_id"] != nil || details["next_tool"] != nil || details["next_args"] != nil {
					t.Fatalf("preflight: writes=%d reads=%d stderr=%s", writes, reads, stderr)
				}
				return
			}

			name := "web"
			if point == "lost DELETE reply" {
				if details["operation_id"] != nil || details["next_tool"] != nil || details["next_args"] != nil {
					t.Fatalf("lost reply supplied an unconfirmed operation: %s", stderr)
				}
			} else {
				next, ok := details["next_args"].(map[string]any)
				if code != 130 || envelope.Reason != "INTERRUPTED" || details["operation_id"] != "op-web" || details["name"] != name || details["next_tool"] != "ca_get" || !ok || len(next) != 1 || next["name"] != name {
					t.Fatalf("started delete: code=%d stderr=%s", code, stderr)
				}
				name = next["name"].(string)
			}
			files := intentFiles(root)
			if len(files) != 1 {
				t.Fatalf("sent delete left %d records", len(files))
			}
			for _, machine := range []bool{true, false} {
				newProcess()
				get := []string{"ca", "get", name}
				if machine {
					get = append(get, "--json")
				} else {
					russian(t)
				}
				count := reads
				code, out, stderr = runCLI(t, "", get...)
				if code != 0 || strings.TrimSpace(out) == "" || reads != count+1 || writes != 1 {
					t.Fatalf("read-only continuation: code=%d stdout=%s stderr=%s reads=%d writes=%d", code, out, stderr, reads, writes)
				}
				want := "deletion has not been confirmed"
				if machine {
					var card map[string]any
					if json.Unmarshal([]byte(out), &card) != nil || card["status"] != "DELETING" {
						t.Fatalf("unexpected card: %s", out)
					}
				} else {
					want = "удаление не подтверждено"
				}
				if !strings.Contains(stderr, want) || !strings.Contains(stderr, "ca_get") || !strings.Contains(stderr, "evo ca get") ||
					strings.Contains(stderr, "settles it") || strings.Contains(stderr, "закроет запись") {
					t.Fatalf("wrong advice for the saved delete: %s", stderr)
				}
				if now := intentFiles(root); len(now) != 1 || now[0] != files[0] {
					t.Fatalf("read replaced or removed the delete record: %v", now)
				}
			}
		})
	}
}

// The CLI may wait longer than an MCP call. Exercise the actual command and
// its continuation on a single fake clock with the production polling intervals.
func TestCADeleteCLI_WaitForTwoMinutesOrClientDeadline(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		disappears, clientDeadline, stallFrom time.Duration
		wantStatus                            string
	}{
		{"gone at 5s", 5 * time.Second, 0, 0, "deleted"},
		{"gone at 50s", 50 * time.Second, 0, 0, "deleted"},
		{"visible at two minutes", 150 * time.Second, 0, 0, "deleting"},
		{"last GET stalls", 150 * time.Second, 0, 110 * time.Second, "deleting"},
		{"shorter client deadline", 50 * time.Second, 7 * time.Second, 0, "deleting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				withParityWorld(t)
				reads, deletes, goneReads := 0, 0, 0
				var accepted, goneAt, stallAt time.Time
				var stalledAt time.Duration
				base := httpretry.DefaultBase
				httpretry.DefaultBase = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path != "/v2/containers/web" {
						t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
					}
					switch r.Method {
					case http.MethodDelete:
						deletes++
						accepted = time.Now()
						goneAt = accepted.Add(tc.disappears)
						if tc.stallFrom > 0 {
							stallAt = accepted.Add(tc.stallFrom)
						}
						return contractRT{200, `{"id":"op-web","done":false}`}.RoundTrip(r)
					case http.MethodGet:
						reads++
						now := time.Now()
						if !stallAt.IsZero() && stalledAt == 0 && !now.Before(stallAt) {
							stalledAt = now.Sub(accepted)
							// A new GET's own HTTP timeout must not extend the original wait.
							<-r.Context().Done()
							return nil, r.Context().Err()
						}
						if !goneAt.IsZero() && !now.Before(goneAt) {
							goneReads++
							return contractRT{404, `{"code":"NOT_FOUND","message":"container not found"}`}.RoundTrip(r)
						}
						return contractRT{200, `{"name":"web","status":"DELETING"}`}.RoundTrip(r)
					default:
						t.Fatalf("unexpected method %s", r.Method)
						return nil, nil
					}
				})
				t.Cleanup(func() { httpretry.DefaultBase = base })

				ctx := t.Context()
				if tc.clientDeadline > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tc.clientDeadline)
					defer cancel()
				}
				out, stderr, code := runDeleteCLIContext(t, ctx, "ca", "delete", "web", "--wait", "--yes", "--json")
				finished := time.Now()
				elapsed := finished.Sub(accepted)
				result := parseObject(out)
				t.Logf("elapsed=%s GET=%d DELETE=%d stalledAt=%s status=%v", elapsed, reads, deletes, stalledAt, result["status"])
				if code != 0 || deletes != 1 || reads < 2 || result["name"] != "web" || result["operation_id"] != "op-web" || result["status"] != tc.wantStatus {
					t.Fatalf("elapsed=%s GET=%d DELETE=%d code=%d stdout=%s stderr=%s", elapsed, reads, deletes, code, out, stderr)
				}
				if tc.wantStatus == "deleted" {
					if goneReads != 1 || finished.Before(goneAt) || !finished.Before(accepted.Add(2*time.Minute)) {
						t.Fatalf("deleted after %s with %d reads proving disappearance", elapsed, goneReads)
					}
					return
				}
				wantElapsed := 2 * time.Minute
				if tc.clientDeadline > 0 {
					wantElapsed = tc.clientDeadline
				}
				if tc.stallFrom > 0 && (stalledAt < tc.stallFrom || stalledAt >= wantElapsed) {
					t.Fatalf("last GET stalled at %s, want between %s and %s", stalledAt, tc.stallFrom, wantElapsed)
				}
				next, ok := result["next_args"].(map[string]any)
				if elapsed != wantElapsed || goneReads != 0 || result["next_tool"] != "ca_get" || !ok || len(next) != 1 || next["name"] != "web" {
					t.Fatalf("pending after %s, want %s, gone reads=%d, answer=%s", elapsed, wantElapsed, goneReads, out)
				}
				newProcess()
				previousReads := reads
				out, stderr, code = runDeleteCLIContext(t, t.Context(), "ca", "get", next["name"].(string), "--json")
				if code != 0 || reads != previousReads+1 || deletes != 1 || parseObject(out)["status"] != "DELETING" {
					t.Fatalf("continuation: GET=%d DELETE=%d code=%d stdout=%s stderr=%s", reads-previousReads, deletes, code, out, stderr)
				}
			})
		})
	}
}

// The CLI flag's own help gives the person a bound and a read-only CLI
// command to continue once that bound is reached.
func TestCADeleteCLI_WaitHelpNamesLimitAndContinuation(t *testing.T) {
	for _, tc := range []struct{ lang, limit string }{{"en", "2 min"}, {"ru", "2 минут"}} {
		t.Run(tc.lang, func(t *testing.T) {
			if tc.lang == "ru" {
				russian(t)
			}
			code, out, stderr := runCLI(t, "", "ca", "delete", "--help")
			_, rest, found := strings.Cut(out, "--wait ")
			line, _, _ := strings.Cut(rest, "\n")
			if code != 0 || !found || !strings.Contains(line, tc.limit) || !strings.Contains(line, "evo ca get ") || !strings.Contains(line, "NOT_FOUND") {
				t.Fatalf("exit %d, --wait help %q, stderr %s", code, line, stderr)
			}
		})
	}
}
