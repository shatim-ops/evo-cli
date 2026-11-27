// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/shatim-ops/evo-cli/internal/dbaas"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/postgresql"
)

// nameFree (D49): a free name lets the call go on; a taken one is USAGE with
// the existing id; a list that cannot be read is an error, not a free name —
// a create that cannot check the name does not ask for consent.
func TestNameFree(t *testing.T) {
	ctx := context.Background()
	listOf := func(items []map[string]any, err error) func(context.Context) ([]map[string]any, error) {
		return func(context.Context) ([]map[string]any, error) { return items, err }
	}
	taken := []map[string]any{{"id": "00000000-0000-4000-8000-0000000000d1", "name": "data", "status": "available"}}

	if ans, refused := nameFree(ctx, listOf(taken, nil), "disk", "other", "disk_list"); refused || ans != "" {
		t.Errorf("a free name refused: %s", ans)
	}
	ans, refused := nameFree(ctx, listOf(taken, nil), "disk", "data", "disk_list")
	var m map[string]any
	_ = json.Unmarshal([]byte(ans), &m)
	if !refused || m["reason"] != clierrors.CodeUsage || m["existing_id"] != "00000000-0000-4000-8000-0000000000d1" {
		t.Errorf("a taken name: want USAGE with existing_id: %s", ans)
	}
	ans, refused = nameFree(ctx, listOf(nil, errors.New("list down")), "disk", "data", "disk_list")
	if !refused || !strings.Contains(ans, "list down") || strings.Contains(ans, "confirmation_required") {
		t.Errorf("an unread list: want an error naming the cause: %s", ans)
	}
}

// A name cannot be declared free, or recovered after a lost reply, from
// a list whose members cannot all be compared with the requested name.
func TestManagedClusterNameChecksRequireReadableNames(t *testing.T) {
	for _, service := range []string{"redis", "kafka"} {
		for _, phase := range []string{"preview", "confirmed", "recovery", "refused POST"} {
			t.Run(service+"/"+phase, func(t *testing.T) {
				reads, posts, recoveryReads := 0, 0, 0
				fake := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
					if r.URL.Path != "/v1/clusters" {
						return false
					}
					if r.Method == http.MethodPost {
						posts++
						const body = `{}`
						if phase == "refused POST" {
							w.Header().Set("Content-Length", strconv.Itoa(len(body)+3))
							w.WriteHeader(http.StatusForbidden)
						}
						io.WriteString(w, body)
						return true
					}
					if r.Method != http.MethodGet {
						t.Errorf("unexpected %s", r.Method)
						return true
					}
					reads++
					switch {
					case posts > 0:
						recoveryReads++
						io.WriteString(w, `{"clusters":[{"id":"one","name":"requested"},{"id":"two"}]}`)
					case phase == "recovery" || phase == "refused POST":
						io.WriteString(w, `{"clusters":[]}`)
					case r.URL.Query().Get("page_token") == "":
						io.WriteString(w, `{"clusters":[{"name":"other","id":"one"}],"next_page_token":"next"}`)
					default:
						io.WriteString(w, `{"clusters":[{"id":"two"}]}`)
					}
					return true
				}}
				args := map[string]any{"name": "requested", "cpu": 2, "ram": 4, "confirmed": phase != "preview"}
				m, raw, isErr := callNamed(t, apiFakeRegistry(t, fake), service+"_create", args)
				if !isErr || m["status"] == "creating" || m["will_create"] != nil {
					t.Errorf("unsafe name decision: %s", raw)
				}
				switch phase {
				case "preview", "confirmed":
					if reads != 2 || posts != 0 || m["reason"] != "API_ERROR" {
						t.Errorf("pre-write: reads=%d posts=%d reason=%v", reads, posts, m["reason"])
					}
				case "recovery":
					if posts != 1 || recoveryReads != 1 || m["warning"] == nil {
						t.Errorf("ambiguous receipt: posts=%d recovery reads=%d result=%s", posts, recoveryReads, raw)
					}
				case "refused POST":
					if posts != 1 || recoveryReads != 0 {
						t.Errorf("a rejected POST triggered recovery: posts=%d reads=%d result=%s", posts, recoveryReads, raw)
					}
				}
			})
		}
	}
}

// Once headers have arrived, the status, incomplete body and request
// cancellation convey different facts to callers. Exercise the real clients
// and the same HTTP status extraction used by the tools' absence checks.
func TestManagedClusterIncompleteResponseClassification(t *testing.T) {
	for _, service := range []string{"dbaas", "postgresql"} {
		for _, tc := range []struct {
			name         string
			status       int
			list, cancel bool
		}{
			{"partial list", 200, true, false}, {"cancelled list", 200, true, true},
			{"refused read", 403, false, false}, {"cancelled refused read", 403, false, true},
			{"unconfirmed absence", 404, false, false},
		} {
			t.Run(service+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				old := httpretry.DefaultBase
				t.Cleanup(func() { httpretry.DefaultBase = old })
				httpretry.DefaultBase = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					// Cancellation after the response has been returned and the bytes
					// consumed, just before the final read fails.
					end := iotest.ErrReader(io.ErrUnexpectedEOF)
					if tc.cancel {
						end = cancelledBodyRead{cancel}
					}
					return &http.Response{StatusCode: tc.status, Header: make(http.Header), ContentLength: -1, Body: io.NopCloser(io.MultiReader(strings.NewReader(`{"clusters":[]}`), end))}, nil
				})
				var err error
				if service == "dbaas" {
					c := dbaas.New("token", "project", "https://service.invalid")
					if tc.list {
						_, err = c.ListClusters(ctx, nil)
					} else {
						_, err = c.GetCluster(ctx, "cluster")
					}
				} else {
					c := postgresql.New("token", "project", "https://service.invalid")
					if tc.list {
						_, err = c.ListClusters(ctx)
					} else {
						_, err = c.GetCluster(ctx, "cluster")
					}
				}
				if err == nil {
					t.Fatal("incomplete response reported as success")
				}
				switch {
				case tc.cancel:
					if !errors.Is(err, context.Canceled) || apiStatus(err) != 0 {
						t.Errorf("expected request cancellation, got %v", err)
					}
				case tc.list:
					if clierrors.Reason(err) != clierrors.CodeAPIError || clierrors.ExitCode(err) != 5 {
						t.Errorf("expected API_ERROR/5, got %v", err)
					}
				case tc.status == 403:
					if apiStatus(err) != 403 {
						t.Errorf("lost the HTTP refusal: %v", err)
					}
				case tc.status == 404:
					if apiStatus(err) == 404 || !errors.Is(err, io.ErrUnexpectedEOF) {
						t.Errorf("incomplete 404 treated as absence: %v", err)
					}
				}
			})
		}
	}
}

type cancelledBodyRead struct{ cancel context.CancelFunc }

func (r cancelledBodyRead) Read([]byte) (int, error) {
	r.cancel()
	return 0, io.ErrUnexpectedEOF
}

// Some resources are addressed by their input name or path. A database
// cluster instead needs the ID returned by the service to identify it.
func TestLostCreateRequiresTheDeclaredIdentity(t *testing.T) {
	for _, tc := range []struct {
		tool      string
		inputIsID bool
	}{{"pg_create", false}, {"redis_create", false}, {"ca_deploy", true}, {"secret_create", true}} {
		t.Run(tc.tool, func(t *testing.T) {
			list := func(context.Context) ([]map[string]any, error) {
				return []map[string]any{{"name": "requested", "id": " "}}, nil
			}
			raw, isErr := lostCreate(context.Background(), tc.tool, list, "resource", "requested", nil, "read", io.ErrUnexpectedEOF)
			var result map[string]any
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				t.Fatal(err)
			}
			if tc.inputIsID {
				if isErr || result["status"] != "creating" {
					t.Errorf("input identity lost: %s", raw)
				}
			} else if !isErr || result["status"] == "creating" || result["warning"] == nil {
				t.Errorf("recovered an unidentified resource: %s", raw)
			}
		})
	}
}
