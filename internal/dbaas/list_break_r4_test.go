// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package dbaas_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/dbaas"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/postgresql"
)

// Уникальные токены после maxListPages: список не полон, ошибки нет.
func TestBreakR4_R4_8_ListClustersMaxPagesIsError(t *testing.T) {
	const maxListPages = 50 // Public client limit, independent of the implementation.
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		k := n
		mu.Unlock()
		fmt.Fprintf(w, `{"clusters":[{"id":"00000000-0000-4000-8000-%012d"}],"next_page_token":"t%d"}`, k, k)
	}))
	defer srv.Close()

	all, err := dbaas.New("tok", "00000000-0000-4000-8000-0000000000aa", srv.URL).ListClusters(context.Background(), nil)
	if err == nil {
		t.Fatalf("past the page cap: %d clusters and a nil error after %d requests", len(all), n)
	}
	if n != maxListPages {
		t.Fatalf("requests = %d, want %d", n, maxListPages)
	}
}

func TestClusterListRequiresStringCursorWhenPresent(t *testing.T) {
	for _, service := range []string{"kafka", "redis"} {
		for _, tc := range []struct {
			name, body string
			valid      bool
		}{
			{"omitted", `{"clusters":[]}`, true},
			{"empty string", `{"clusters":[],"next_page_token":""}`, true},
			{"null", `{"clusters":[],"next_page_token":null}`, false},
			{"number", `{"clusters":[],"next_page_token":12}`, false},
		} {
			t.Run(service+"/"+tc.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("page_token") == "" {
						io.WriteString(w, `{"clusters":[{"id":"one","name":"first"}],"next_page_token":"next"}`)
						return
					}
					io.WriteString(w, tc.body)
				}))
				defer srv.Close()
				q := url.Values{}
				if service == "kafka" {
					q.Set("product_type", "kafka")
				}
				items, pages, err := dbaas.New("token", "project", srv.URL).ListClustersWithin(context.Background(), q, 3)
				if pages != 2 {
					t.Errorf("got %d pages, want 2", pages)
				}
				if tc.valid {
					if err != nil || len(items) != 1 {
						t.Errorf("valid final page: %v %v", items, err)
					}
				} else if items != nil || clierrors.Reason(err) != clierrors.CodeAPIError || clierrors.ExitCode(err) != 5 {
					t.Errorf("incomplete final page: %v %v", items, err)
				}
			})
		}
	}
}

// A fresh cursor does not make a page of previously seen IDs new. A
// missing ID, on the other hand, does not prove a duplicate.
func TestManagedClusterPageNeedsProgress(t *testing.T) {
	for _, service := range []string{"kafka", "redis", "postgresql"} {
		for _, tc := range []struct {
			name, second string
			valid        bool
		}{
			{"repeats", `{"clusters":[{"id":"one","name":"first"}]}`, false},
			{"new ID", `{"clusters":[{"id":"two","name":"second"}]}`, true},
			{"unknown identity", `{"clusters":[{"name":"first"}]}`, true},
			{"empty intermediate", `{"clusters":[],"next_page_token":"third"}`, true},
		} {
			t.Run(service+"/"+tc.name, func(t *testing.T) {
				var reads int
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					reads++
					switch r.URL.Query().Get("page_token") {
					case "":
						io.WriteString(w, `{"clusters":[{"id":"one","name":"first"}],"next_page_token":"second"}`)
					case "second":
						io.WriteString(w, tc.second)
					case "third":
						io.WriteString(w, `{"clusters":[{"id":"two","name":"second"}]}`)
					default:
						t.Errorf("unexpected cursor")
					}
				}))
				defer srv.Close()
				var items []map[string]any
				var err error
				if service == "postgresql" {
					items, err = postgresql.New("token", "project", srv.URL).ListClusters(context.Background())
				} else {
					q := url.Values{}
					if service == "kafka" {
						q.Set("product_type", "kafka")
					}
					items, err = dbaas.New("token", "project", srv.URL).ListClusters(context.Background(), q)
				}
				if tc.valid {
					if err != nil || len(items) != 2 {
						t.Errorf("valid pages: %v, %v", items, err)
					}
				} else if items != nil || clierrors.Reason(err) != clierrors.CodeAPIError || reads != 2 {
					t.Errorf("repeated page: %v, %v (%d reads)", items, err, reads)
				}
			})
		}
	}
}

func TestClusterListRequiresCompleteHTTPPages(t *testing.T) {
	for _, service := range []string{"kafka", "redis", "postgresql"} {
		for _, fault := range []string{"complete", "short declared body", "206", "content range", "403"} {
			for _, badPage := range []int{1, 2} {
				t.Run(fmt.Sprintf("%s/%s/page%d", service, fault, badPage), func(t *testing.T) {
					reads := 0
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						reads++
						if r.Method != "GET" || r.URL.Path != "/v1/clusters" || r.URL.Query().Get("project_id") != "project" {
							t.Errorf("unexpected request %s %s", r.Method, r.URL)
						}
						if reads == 1 && badPage == 2 {
							io.WriteString(w, `{"clusters":[{"id":"one","name":"first"}],"next_page_token":"second"}`)
							return
						}
						if reads == 2 && r.URL.Query().Get("page_token") != "second" {
							t.Error("continuing without the cursor")
						}
						const body = `{"clusters":[]}`
						switch fault {
						case "short declared body":
							w.Header().Set("Content-Length", strconv.Itoa(len(body)+12))
						case "206":
							w.WriteHeader(http.StatusPartialContent)
						case "content range":
							w.Header().Set("Content-Range", "bytes 0-14/30")
						case "403":
							w.WriteHeader(http.StatusForbidden)
						}
						io.WriteString(w, body)
					}))
					defer srv.Close()
					var got []map[string]any
					var err error
					if service == "postgresql" {
						got, err = postgresql.New("token", "project", srv.URL).ListClusters(context.Background())
					} else {
						q := url.Values{}
						if service == "kafka" {
							q.Set("product_type", "kafka")
						}
						var pages int
						got, pages, err = dbaas.New("token", "project", srv.URL).ListClustersWithin(context.Background(), q, 4)
						if pages != badPage {
							t.Errorf("pages=%d, want %d", pages, badPage)
						}
					}
					if reads != badPage {
						t.Errorf("GETs=%d, want %d", reads, badPage)
					}
					switch fault {
					case "complete":
						if err != nil || len(got) != badPage-1 {
							t.Fatalf("valid list: %v %v", got, err)
						}
					case "403":
						if err == nil || got != nil {
							t.Fatalf("accepted denied page: %v %v", got, err)
						}
					default:
						if clierrors.Reason(err) != clierrors.CodeAPIError || got != nil {
							t.Fatalf("accepted partial list or wrong reason: %v %v", got, err)
						}
					}
				})
			}
		}
	}
}

// Even valid JSON in a truncated POST response must not be acknowledged as an operation receipt.
// A recovery read such as GetCluster must also be complete before its result can be used.
func TestManagedDBRejectsShortOperationAndCardBodies(t *testing.T) {
	for _, service := range []string{"dbaas", "postgresql"} {
		t.Run(service, func(t *testing.T) {
			gets, posts := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "GET":
					gets++
				case "POST":
					posts++
				default:
					t.Errorf("unexpected request %s", r.Method)
				}
				body := `{"id":"op-or-card"}`
				if strings.HasSuffix(r.URL.Path, "/backups") {
					body = `{"backups":[]}`
				}
				w.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
				io.WriteString(w, body)
			}))
			defer srv.Close()
			ctx := context.Background()
			wantGets := 1
			if service == "postgresql" {
				wantGets++
				c := postgresql.New("token", "project", srv.URL)
				if op, err := c.CreateCluster(ctx, map[string]any{"name": "test"}); err == nil || op != nil {
					t.Errorf("incomplete operation: %v %v", op, err)
				}
				if card, err := c.GetCluster(ctx, "test"); err == nil || card != nil {
					t.Errorf("incomplete card: %v %v", card, err)
				}
				if backups, err := c.ListBackups(ctx, "test"); err == nil || backups != nil {
					t.Errorf("incomplete backup inventory: %v %v", backups, err)
				}
			} else {
				c := dbaas.New("token", "project", srv.URL)
				if op, err := c.CreateCluster(ctx, map[string]any{"name": "test"}); err == nil || op != nil {
					t.Errorf("incomplete operation: %v %v", op, err)
				}
				if card, err := c.GetCluster(ctx, "test"); err == nil || card != nil {
					t.Errorf("incomplete card: %v %v", card, err)
				}
			}
			if gets != wantGets || posts != 1 {
				t.Fatalf("GET=%d POST=%d", gets, posts)
			}
		})
	}
}

func TestDBaaSClusterCollectionForms(t *testing.T) {
	for _, service := range []string{"kafka", "redis"} {
		for _, tc := range []struct {
			name, body string
			rows       int // negative for an invalid list
		}{
			{"omitted", `{}`, 0},
			{"null collection", `{"clusters":null}`, -1},
			{"empty", `{"clusters":[]}`, 0},
			{"named cluster", `{"clusters":[{"id":"one","name":"readable"}]}`, 1},
			{"membership only", `{"clusters":[{"id":"one"}]}`, 1},
			{"blank name", `{"clusters":[{"id":"one","name":" "}]}`, 1},
			{"null page", `null`, -1},
			{"null row", `{"clusters":[null]}`, -1},
		} {
			t.Run(service+"/"+tc.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.body) }))
				defer srv.Close()
				c := dbaas.New("token", "project", srv.URL)
				q := url.Values{}
				if service == "kafka" {
					q.Set("product_type", "kafka")
				}
				items, err := c.ListClusters(context.Background(), q)
				if tc.rows < 0 {
					if err == nil || items != nil {
						t.Errorf("invalid inventory: %v %v", items, err)
					}
				} else if err != nil || len(items) != tc.rows {
					t.Errorf("valid inventory: %v %v", items, err)
				}
			})
		}
	}
}

// PostgreSQL follows collection continuations. Kafka/Redis reference
// catalogues accept only their documented single-page representation.
func TestManagedDBReferenceListsValidatePages(t *testing.T) {
	for _, service := range []string{"postgresql", "dbaas"} {
		field := "versions"
		if service == "postgresql" {
			field = "databases"
		}
		for _, tc := range []struct {
			name, body string
			status     int
			complete   bool
		}{
			{"optional collection", `{}`, 200, true}, {"empty collection", `{"%s":[]}`, 200, true},
			{"empty body", "", 200, false}, {"null page", "null", 200, false},
			{"null collection", `{"%s":null}`, 200, false}, {"wrong collection", `{"%s":{}}`, 200, false},
			{"null row", `{"%s":[null]}`, 200, false}, {"null cursor", `{"next_page_token":null}`, 200, false},
			{"invalid cursor", `{"next_page_token":11}`, 200, false},
			{"no content", "", 204, false}, {"lost page", "{}", 404, false},
		} {
			t.Run(service+"/"+tc.name, func(t *testing.T) {
				reads := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					reads++
					if reads == 1 && service == "postgresql" {
						fmt.Fprintf(w, `{"%s":[{"name":"one"}],"next_page_token":"more"}`, field)
						return
					}
					if service == "postgresql" && r.URL.Query().Get("page_token") != "more" {
						t.Error("missing continuation token")
					}
					w.WriteHeader(tc.status)
					io.WriteString(w, strings.ReplaceAll(tc.body, "%s", field))
				}))
				defer srv.Close()
				var rows []map[string]any
				var err error
				if service == "postgresql" {
					rows, err = postgresql.New("token", "project", srv.URL).ListDatabases(context.Background(), "test")
				} else {
					rows, err = dbaas.New("token", "project", srv.URL).ListVersions(context.Background(), nil)
				}
				wantReads, wantRows := 1, 0
				if service == "postgresql" {
					wantReads, wantRows = 2, 1
				}
				if reads != wantReads {
					t.Errorf("read %d pages, want %d", reads, wantReads)
				}
				if tc.complete {
					if err != nil || len(rows) != wantRows || (wantRows > 0 && rows[0]["name"] != "one") {
						t.Errorf("complete: %v %v", rows, err)
					}
				} else if tc.status == 404 && service == "dbaas" {
					if rows != nil || err == nil || !strings.Contains(err.Error(), "404") {
						t.Errorf("first-page refusal: %v %v", rows, err)
					}
				} else if rows != nil || clierrors.Reason(err) != clierrors.CodeAPIError || clierrors.ExitCode(err) != 5 {
					t.Errorf("incomplete: %v %v", rows, err)
				}
			})
		}
	}
}
