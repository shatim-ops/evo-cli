// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pager_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/artifact"
	"github.com/shatim-ops/evo-cli/internal/billing"
	"github.com/shatim-ops/evo-cli/internal/dns"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/nlb"
	"github.com/shatim-ops/evo-cli/internal/postgresql"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/vpc"
)

func listLength[T any](rows []T, err error) (int, error) { return len(rows), err }

// A corrupt later page must not turn a partial catalog or a bill into a
// complete result. Exercise each client's HTTP response decoder.
func TestResourceClients_RejectNullPagesAndRows(t *testing.T) {
	ctx := context.Background()
	clients := []struct {
		name, field, continuation string
		read                      func(string) (int, error)
	}{
		{"postgresql", "clusters", `"next_page_token":"two"`, func(base string) (int, error) {
			return listLength(postgresql.New("token", "project", base).ListClusters(ctx))
		}},
		{"dns", "zones", `"nextPageToken":"two"`, func(base string) (int, error) {
			return listLength(dns.New("token", "project", base).ListZones(ctx, "public"))
		}},
		{"nlb", "balancers", `"next_page_token":"two"`, func(base string) (int, error) { return listLength(nlb.New("token", "project", base).ListNLB(ctx)) }},
		{"billing", "consumptions", `"next_page_token":"two"`, func(base string) (int, error) {
			return listLength(billing.New("token", "agreement", base, base).Consumption(ctx, "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"))
		}},
		{"mk8s", "clusters", `"nextPageToken":"two"`, func(base string) (int, error) {
			return listLength(mk8s.New("token", "project", base).ListClusters(ctx))
		}},
		{"artifact", "registries", `"nextPageToken":"two"`, func(base string) (int, error) {
			return listLength(artifact.New("token", "project", base).ListRegistries(ctx))
		}},
		{"vpc", "vpcs", `"nextPageToken":"two"`, func(base string) (int, error) { return listLength(vpc.New("token", "project", base).ListVPCs(ctx)) }},
		{"compute", "items", `"total":2`, func(base string) (int, error) {
			return listLength(provision.NewCloudRuAdapter(nil, base).ListVMsInProject(ctx, "project"))
		}},
		{"sg-rules", "items", `"total":2`, func(base string) (int, error) {
			return listLength(provision.NewCloudRuAdapter(nil, base).ListSecurityGroupRules(ctx, "group"))
		}},
	}
	for _, client := range clients {
		t.Run(client.name, func(t *testing.T) {
			first := fmt.Sprintf(`{%q:[{"id":"one","name":"one"}],%s}`, client.field, client.continuation)
			nullRow := fmt.Sprintf(`{%q:[null]}`, client.field)
			for _, tc := range []struct {
				name    string
				pages   []string
				invalid bool
			}{
				{"empty", []string{fmt.Sprintf(`{%q:[]}`, client.field)}, false},
				{"null-page", []string{"null"}, true}, {"null-row", []string{nullRow}, true},
				{"null-page-after-row", []string{first, "null"}, true},
				{"null-row-after-row", []string{first, nullRow}, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					calls := 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodGet || calls >= len(tc.pages) {
							http.Error(w, "unexpected request", http.StatusBadRequest)
							return
						}
						io.WriteString(w, tc.pages[calls])
						calls++
					}))
					defer server.Close()
					rows, err := client.read(server.URL)
					var apiErr *clierrors.CLIError
					if tc.invalid {
						if !errors.As(err, &apiErr) || apiErr.Code != clierrors.CodeAPIError {
							t.Errorf("expected API_ERROR, got %v", err)
						}
					} else if err != nil {
						t.Errorf("empty collection: %v", err)
					}
					if calls != len(tc.pages) || rows != 0 {
						t.Errorf("got calls=%d rows=%d; expected calls=%d rows=0", calls, rows, len(tc.pages))
					}
				})
			}
		})
	}
}
