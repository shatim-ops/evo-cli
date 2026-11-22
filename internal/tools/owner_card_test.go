// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A kind with a read by id — a tool «…_get» that takes one id of it —
// reads its resource by id (card): the preview of a change of another
// project's resource names that project, not «not determined», and evo
// doctor checks a cut delete by the id (FB19I-20, the class of line 8 of the
// journal of 1.9: backup_delete and k8s_nodepool_delete had no card though
// backup_get and k8s_nodepool_get read by id).

func TestOwnerKinds_AReadByIDDeclaresTheCard(t *testing.T) {
	seen := 0
	for tool, args := range allIDArgs() {
		if !strings.HasSuffix(tool, "_get") || len(args) != 1 || args[0].kind == nil {
			continue
		}
		seen++
		if args[0].kind.card == nil {
			t.Errorf("%s reads a %s by id, and the kind declares no card: the preview of its change and evo doctor cannot read it by id", tool, args[0].kind.what)
		}
	}
	if seen == 0 {
		t.Fatal("no read by id in Decl.IDArgs: the test reads nothing")
	}
}

func TestOwnerCard_ReadByIDForCutDelete(t *testing.T) {
	ctx := context.Background()
	key := &state.Maker{KeyID: "test****3456"}
	for _, tc := range []struct {
		tool  string
		kind  *ownerKind
		paths []string
	}{
		{"destroy", kindVM, []string{"/api/v1/vms/"}},
		{"redis_delete", kindRedis, []string{"/v1/clusters/"}},
		{"kafka_delete", kindKafka, []string{"/v1/clusters/"}},
		{"dns_zone_delete", kindDNSZone, []string{"/v1/publicZones/", "/v1/privateZones/"}},
		{"dns_record_delete", kindDNSRecord, []string{"/v1/publicRecordsSole/", "/v1/privateRecordsSole/"}},
	} {
		for _, world := range []string{"present", "wrong_id", "forbidden", "absent", "gateway_404", "routing_404", "other_key"} {
			t.Run(tc.tool+"/"+world, func(t *testing.T) {
				f := &ownerFake{}
				r := ownerRegistry(t, f, true, ownProject)
				if tc.kind.card == nil {
					t.Fatal("no addressable card")
				}
				calls := 0
				rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if calls >= len(tc.paths) || req.Method != "GET" || req.URL.Path != tc.paths[calls]+resMine {
						t.Fatalf("unexpected request %s %s", req.Method, req.URL)
					}
					calls++
					status := http.StatusOK
					body := `{"id":"` + resMine + `","name":"same","status":"ACTIVE"}`
					if len(tc.paths) == 2 && calls == 1 {
						status = 404
						body = `{"code":5,"message":"not found"}`
					} else {
						switch world {
						case "wrong_id":
							body = `{"id":"` + resForeign + `","name":"same"}`
						case "forbidden":
							status = 403
							body = `{"message":"permission denied"}`
						case "absent":
							status = 404
							body = `{"code":5,"message":"not found"}`
							if tc.kind == kindVM {
								body = `[{"message":"not found"}]`
							}
						case "routing_404":
							status = 404
							body = `{"code":5,"message":"Not Found","details":[]}`
						case "gateway_404":
							status = 404
							body = "route not found"
						}
					}
					w := httptest.NewRecorder()
					w.WriteHeader(status)
					w.WriteString(body)
					return w.Result(), nil
				})
				httpretry.DefaultBase = rt
				factory := r.newEngine
				r.newEngine = func(over map[string]any) (*engine.Engine, error) {
					e, err := factory(over)
					if err == nil {
						e.Config.KeyID = "test-key-123456"
						e.Cloud = provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
					}
					return e, err
				}
				cur := key
				if world == "other_key" {
					cur = &state.Maker{KeyID: "other"}
				}
				eng, err := r.newEngine(nil)
				if err != nil {
					t.Fatal(err)
				}
				arg, _ := deleteOf(tc.tool)
				in := intent.Intent{Tool: tc.tool, Key: cur, ProjectID: ownProject, Target: arg.arg + "=" + resMine}
				diagnostic, settled, _ := r.checkPending(ctx, in, eng)
				if tc.kind == kindVM {
					if settled || calls != 0 {
						t.Fatal("VM disappearance alone cannot settle destroy", diagnostic, calls)
					}
					_, settled, _, _ = r.readByID(ctx, tc.kind, resMine, cur, key)
				}

				if world == "present" || world == "wrong_id" {
					calls = 0
					m, _, _, err := r.readByID(ctx, tc.kind, resMine, key, cur)
					if world == "present" && (err != nil || stringArg(m, "id") != resMine) || world == "wrong_id" && err == nil {
						t.Fatal(m, err, diagnostic)
					}
				}
				wantCalls := len(tc.paths)
				if world == "other_key" {
					wantCalls = 0
				}
				if settled != (world == "absent") || calls != wantCalls {
					t.Fatalf("settled %v calls %d, expected settled %v calls %d", settled, calls, world == "absent", wantCalls)
				}
			})
		}
	}
}

// Project membership is checked separately from a receipt's addressable GET.
// An identically named resource in another project must not pass that check.
func TestOwnerCard_BoundProjectWithSameName(t *testing.T) {
	for _, k := range []*ownerKind{kindVM, kindKafka, kindRedis, kindDNSZone, kindDNSRecord} {
		t.Run(k.what, func(t *testing.T) {
			fake := &ownerFake{}
			r := ownerRegistry(t, fake, true, ownProject)
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet {
					return nil, fmt.Errorf("unexpected %s", req.Method)
				}
				resp, err := fake.RoundTrip(req)
				if err != nil {
					return nil, err
				}
				b, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					return nil, err
				}
				body := strings.NewReplacer("res-a1", "same", "res-b1", "same").Replace(string(b))
				resp.Body = io.NopCloser(strings.NewReader(body))
				return resp, nil
			})
			httpretry.DefaultBase = rt
			mk := r.newEngine
			r.newEngine = func(over map[string]any) (*engine.Engine, error) {
				e, err := mk(over)
				if err == nil {
					e.Cloud = provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
				}
				return e, err
			}
			c := &ownerCall{r: r}
			if err := k.check(context.Background(), c, k, resMine); err != nil {
				t.Fatal(err)
			}
			if err := k.check(context.Background(), c, k, resForeign); err == nil {
				t.Fatal("foreign resource admitted")
			}
		})
	}
}
