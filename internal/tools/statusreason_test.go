// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/minio/minio-go/v7"

	"github.com/shatim-ops/evo-cli/internal/artifact"
	"github.com/shatim-ops/evo-cli/internal/audit"
	"github.com/shatim-ops/evo-cli/internal/billing"
	"github.com/shatim-ops/evo-cli/internal/certmanager"
	"github.com/shatim-ops/evo-cli/internal/containerapp"
	"github.com/shatim-ops/evo-cli/internal/dbaas"
	"github.com/shatim-ops/evo-cli/internal/dns"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/iamadmin"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/monitoring"
	"github.com/shatim-ops/evo-cli/internal/nlb"
	"github.com/shatim-ops/evo-cli/internal/postgresql"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/secret"
	"github.com/shatim-ops/evo-cli/internal/vpc"
)

// Статус раньше текста. У отказа в правах, конфликта, лимита и сбоя
// сервера (403, 409, 429, 5xx) причину задаёт статус: 429 —
// API_RATE_LIMITED, остальные — API_ERROR. Слова «not found» или «не
// найден» в теле ответа её не меняют. Иначе 429 ResourceExhausted от
// PostgreSQL с «not found» в тексте стал NOT_FOUND с кодом 4 (cloud-e2e,
// run 40123320696): агент читает «ресурса нет», а при уборке — «уже
// удалено» (CLAUDE.md §6, ось 3).
//
// У 400, 422 и 499 текст решает: так платформа отвечает на ресурс,
// которого нет (реестр расхождений: DB-13 — 400 «service instance not
// found», Container Apps — 499 «… is not found», compute — 422 «Image …
// not found»). Повторный delete там — already_deleted (D26), и правка,
// которая отдаст причину одному статусу, его сломает.
//
// Клиенты сервисов проверяются целиком: настоящий клиент против
// httptest-сервера, тот же путь, что у инструмента. Retry-After: 0 —
// повторы на 429 и 5xx идут без продовых пауз (§5).

// statusClients — клиенты с ошибкой вида «<service> API <код>: …», по
// одному вызову на каждый. Новый клиент без строки здесь роняет
// TestStatusBeforeText_EveryClientListed.
var statusClients = map[string]func(ctx context.Context, base string) error{
	"artifact": func(ctx context.Context, base string) error {
		_, err := artifact.New("tok", "proj", base).ListRegistries(ctx)
		return err
	},
	"audit": func(ctx context.Context, base string) error {
		_, err := audit.New("tok", "proj", base).List(ctx, audit.ListOptions{})
		return err
	},
	"billing": func(ctx context.Context, base string) error {
		_, err := billing.New("tok", "agr", base, base).Balance(ctx)
		return err
	},
	"certmanager": func(ctx context.Context, base string) error {
		_, err := certmanager.New("tok", "proj", base).List(ctx, certmanager.ListOptions{})
		return err
	},
	"containerapp": func(ctx context.Context, base string) error {
		_, err := containerapp.New("tok", "proj", base).List(ctx)
		return err
	},
	"dbaas": func(ctx context.Context, base string) error {
		_, err := dbaas.New("tok", "proj", base).ListClusters(ctx, nil)
		return err
	},
	"dns": func(ctx context.Context, base string) error {
		_, err := dns.New("tok", "proj", base).ListZones(ctx, "public")
		return err
	},
	"iamadmin": func(ctx context.Context, base string) error {
		_, err := iamadmin.New(staticToken{}, base).Permissions(ctx, "proj", "")
		return err
	},
	"mk8s": func(ctx context.Context, base string) error {
		_, err := mk8s.New("tok", "proj", base).ListClusters(ctx)
		return err
	},
	"monitoring": func(ctx context.Context, base string) error {
		_, err := monitoring.New("tok", "proj", base).ScrapeMetrics(ctx, "up")
		return err
	},
	"nlb": func(ctx context.Context, base string) error {
		_, err := nlb.New("tok", "proj", base).ListNLB(ctx)
		return err
	},
	"postgresql": func(ctx context.Context, base string) error {
		_, err := postgresql.New("tok", "proj", base).ListClusters(ctx)
		return err
	},
	"secret": func(ctx context.Context, base string) error {
		_, err := secret.New("tok", "proj", base).List(ctx)
		return err
	},
	"vpc": func(ctx context.Context, base string) error {
		_, err := vpc.New("tok", "proj", base).ListVPCs(ctx)
		return err
	},
}

// statusTexts — тела ответа: со словами «не найдено» на обоих языках и без
// них.
var statusTexts = []string{"resource not found", "кластер не найден", "slow down"}

// statusCodes — классы ответа, причину которых задаёт статус, и 404 как
// контрольная строка.
var statusCodes = []int{403, 404, 409, 429, 500, 502, 503}

// textCodes — статусы, которыми платформа отвечает на ресурс, которого нет:
// «not found» в тексте даёт NOT_FOUND.
var textCodes = []int{400, 422, 499}

// reasonWant — причина ответа со статусом st и телом text.
func reasonWant(st int, text string) string {
	if slices.Contains(textCodes, st) && (strings.Contains(text, "not found") || strings.Contains(text, "не найден")) {
		return clierrors.CodeNotFound
	}
	switch st {
	case http.StatusNotFound:
		return clierrors.CodeNotFound
	case http.StatusTooManyRequests:
		return clierrors.CodeAPIRateLimited
	}
	return clierrors.CodeAPIError
}

func statusReason(t *testing.T, err error) string {
	t.Helper()
	var got map[string]any
	if jerr := json.Unmarshal([]byte(errResult(err)), &got); jerr != nil {
		t.Fatal(jerr)
	}
	r, _ := got["reason"].(string)
	return r
}

func TestStatusBeforeText_ServiceClients(t *testing.T) {
	names := make([]string, 0, len(statusClients))
	for n := range statusClients {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, st := range slices.Concat(statusCodes, textCodes) {
		for _, text := range statusTexts {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(st)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": text, "error": text, "detail": text, "code": 5})
			}))
			for _, n := range names {
				err := statusClients[n](context.Background(), srv.URL)
				if err == nil {
					t.Errorf("%s, %d %q: no error", n, st, text)
					continue
				}
				if got, want := statusReason(t, err), reasonWant(st, text); got != want {
					t.Errorf("%s, %d %q: reason %s, want %s (%v)", n, st, text, got, want, err)
				}
			}
			srv.Close()
		}
	}
}

// Ошибки со статусом в типе, а не в тексте: compute (provision), Organization
// API, S3.
func TestStatusBeforeText_TypedErrors(t *testing.T) {
	for _, st := range slices.Concat(statusCodes, textCodes) {
		for _, text := range statusTexts {
			for name, err := range map[string]error{
				"compute":          fmt.Errorf("get vm: %w", &provision.StatusError{Status: st, Body: text}),
				"Organization API": &adminHTTPError{Status: st, Msg: text},
				"S3":               fmt.Errorf("list buckets: %w", minio.ErrorResponse{Code: "X", Message: text, StatusCode: st}),
			} {
				if got, want := statusReason(t, err), reasonWant(st, text); got != want {
					t.Errorf("%s, %d %q: reason %s, want %s (%v)", name, st, text, got, want, err)
				}
			}
		}
	}
}

// Каждый пакет, который пишет ошибку «<service> API %d: …», есть в
// statusClients.
func TestStatusBeforeText_EveryClientListed(t *testing.T) {
	format := regexp.MustCompile(`"[a-z-]+ API %d: `)
	root := filepath.Join("..")
	var missing []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pkg := filepath.Base(filepath.Dir(path))
		if format.Match(b) {
			if _, ok := statusClients[pkg]; !ok {
				missing = append(missing, pkg)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Errorf("клиенты без строки в statusClients: %v", missing)
	}
}

func TestExportMetrics_AllSeriesOrAPIError(t *testing.T) {
	const row = `{"metric":{"__name__":"up"},"values":[0,1],"timestamps":[11,12]}`
	want := monitoring.Series{Metric: map[string]string{"__name__": "up"}, Values: []float64{0, 1}, Timestamps: []int64{11, 12}}
	for _, tc := range []struct {
		name, body    string
		status, count int
	}{
		{"array", "[" + row + "," + row + "]", 200, 2},
		{"single series", row, 200, 1},
		{"ndjson", row + "\n\n" + row + "\n", 200, 2},
		{"wrapper series", `{"series":[` + row + "]}", 200, 1},
		{"wrapper data", `{"data":[` + row + "]}", 200, 1},
		{"wrapper result", `{"result":[` + row + "]}", 200, 1},
		{"empty array", "[]", 200, 0}, {"empty wrapper", `{"data":[]}`, 200, 0},
		{"no body", "", 200, -1}, {"no page", "null", 200, -1}, {"missing", "{}", 200, -1},
		{"empty HTTP", "", 204, -1}, {"partial HTTP", "[" + row + "]", 206, -1},
		{"array null", "[" + row + ",null]", 200, -1}, {"wrapper null", `{"series":null}`, 200, -1},
		{"wrong wrapper", `{"series":{}}`, 200, -1}, {"bad last line", row + "\n{", 200, -1},
		{"null point", `{"metric":{},"values":[null],"timestamps":[1]}`, 200, -1},
		{"unpaired", `{"metric":{},"values":[0],"timestamps":[]}`, 200, -1},
		{"no timestamps", `{"metric":{},"values":[0]}`, 200, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			got, err := monitoring.New("tok", "project", srv.URL).ExportMetrics(t.Context(), "up", 1, 2)
			if tc.count < 0 {
				var ce *clierrors.CLIError
				if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError || got != nil {
					t.Fatalf("series=%v, err=%v", got, err)
				}
				return
			}
			if err != nil || len(got) != tc.count {
				t.Fatalf("series=%v, err=%v", got, err)
			}
			for _, s := range got {
				if !reflect.DeepEqual(s, want) {
					t.Errorf("series=%+v, want %+v", s, want)
				}
			}
		})
	}
}

func TestScrapeMetrics_RawTextOnlyAtOK(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{200, "up 0\n"}, {200, ""}, {204, ""}, {206, "up 0\n"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			io.WriteString(w, tc.body)
		}))
		got, err := monitoring.New("tok", "project", srv.URL).ScrapeMetrics(t.Context(), "up")
		srv.Close()
		if tc.status == 200 {
			if err != nil || got != tc.body {
				t.Errorf("HTTP 200: got %q, err %v", got, err)
			}
		} else {
			var ce *clierrors.CLIError
			if got != "" || !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError {
				t.Errorf("HTTP %d: got %q, err %v", tc.status, got, err)
			}
		}
	}
}

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
