// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/artifact"
	"github.com/shatim-ops/evo-cli/internal/audit"
	"github.com/shatim-ops/evo-cli/internal/billing"
	"github.com/shatim-ops/evo-cli/internal/certmanager"
	"github.com/shatim-ops/evo-cli/internal/containerapp"
	"github.com/shatim-ops/evo-cli/internal/dns"
	"github.com/shatim-ops/evo-cli/internal/iamadmin"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/monitoring"
	"github.com/shatim-ops/evo-cli/internal/nlb"
	"github.com/shatim-ops/evo-cli/internal/postgresql"
	"github.com/shatim-ops/evo-cli/internal/secret"
	"github.com/shatim-ops/evo-cli/internal/vpc"
)

// These clients read collections used for name checks, access decisions and
// reports. A complete JSON value can arrive before the end of its HTTP body.
func TestServiceReadsRequireCompleteHTTPBody(t *testing.T) {
	ctx := context.Background()
	readers := []struct {
		name string
		body string
		read func(baseURL string) error
	}{
		{"vpc", `{"vpcs":[]}`, func(baseURL string) error {
			_, err := vpc.New("token", "project", baseURL).ListVPCs(ctx)
			return err
		}},
		{"nlb", `{"balancers":[]}`, func(baseURL string) error {
			_, err := nlb.New("token", "project", baseURL).ListNLB(ctx)
			return err
		}},
		{"mk8s", `{"clusters":[]}`, func(baseURL string) error {
			_, err := mk8s.New("token", "project", baseURL).ListClusters(ctx)
			return err
		}},
		{"dns", `{"zones":[]}`, func(baseURL string) error {
			_, err := dns.New("token", "project", baseURL).ListZones(ctx, "private")
			return err
		}},
		{"artifact", `{"registries":[]}`, func(baseURL string) error {
			_, err := artifact.New("token", "project", baseURL).ListRegistries(ctx)
			return err
		}},
		{"certmanager", `{"certificates":[]}`, func(baseURL string) error {
			_, err := certmanager.New("token", "project", baseURL).List(ctx, certmanager.ListOptions{})
			return err
		}},
		{"secret", `{"secrets":[]}`, func(baseURL string) error {
			_, err := secret.New("token", "project", baseURL).List(ctx)
			return err
		}},
		{"billing", `{"consumptions":[]}`, func(baseURL string) error {
			_, err := billing.New("token", "agreement", baseURL, baseURL).Consumption(ctx, "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z")
			return err
		}},
		{"audit", `{"events":[]}`, func(baseURL string) error {
			_, err := audit.New("token", "project", baseURL).List(ctx, audit.ListOptions{})
			return err
		}},
		{"monitoring", `[]`, func(baseURL string) error {
			_, err := monitoring.New("token", "project", baseURL).ExportMetrics(ctx, "sample", 1, 2)
			return err
		}},
		{"iamadmin", `{"permissions":[]}`, func(baseURL string) error {
			_, err := iamadmin.New(staticToken{}, baseURL).Permissions(ctx, "00000000-0000-4000-8000-000000000001", "")
			return err
		}},
		{"tools_admin", `{"projects":[]}`, func(baseURL string) error {
			_, err := tokenGET(ctx, staticToken{}, baseURL)
			return err
		}},
		{"postgresql", `{"databases":[]}`, func(baseURL string) error {
			_, err := postgresql.New("token", "project", baseURL).ListDatabases(ctx, "sample")
			return err
		}},
		{"containerapp", `{"data":[]}`, func(baseURL string) error {
			_, err := containerapp.New("token", "project", baseURL).List(ctx)
			return err
		}},
	}
	for _, reader := range readers {
		for _, ending := range []string{"complete", "short content length", "unfinished chunked body"} {
			t.Run(reader.name+"/"+ending, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					if r.Method != http.MethodGet {
						t.Errorf("unexpected %s request", r.Method)
					}
					if ending == "short content length" {
						w.Header().Set("Content-Length", strconv.Itoa(len(reader.body)+5))
					}
					io.WriteString(w, reader.body)
					if ending == "unfinished chunked body" {
						// Send the JSON as a real chunk, then drop the connection
						// before the final zero-length chunk can be delivered.
						w.(http.Flusher).Flush()
						panic(http.ErrAbortHandler)
					}
				}))
				defer server.Close()
				err := reader.read(server.URL)
				if n := requests.Load(); n != 1 {
					t.Errorf("read made %d requests, want 1", n)
				}
				if ending == "complete" && err != nil {
					t.Errorf("complete response: %v", err)
				}
				if ending != "complete" && err == nil {
					t.Error("accepted an incomplete HTTP response")
				}
			})
		}
	}
}
