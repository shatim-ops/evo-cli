// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// receiptWorld answers the create of a database, a user or a secret's
// version with the receipt it is given, and its lists as empty.
type receiptWorld struct {
	mu      sync.Mutex
	receipt string
	posts   int
}

func (w *receiptWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec := httptest.NewRecorder()
	switch {
	case r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/databases") || strings.HasSuffix(r.URL.Path, "/users") || r.URL.Path == "/v2/version"):
		w.posts++
		_, _ = io.WriteString(rec, w.receipt)
	case strings.HasSuffix(r.URL.Path, "/databases"):
		_, _ = io.WriteString(rec, `{"databases":[]}`)
	case strings.HasSuffix(r.URL.Path, "/users"):
		_, _ = io.WriteString(rec, `{"users":[]}`)
	case strings.HasPrefix(r.URL.Path, "/v2/secret/"):
		_, _ = io.WriteString(rec, `{"secret":{"id":"s-1","name":"db","path":"app/db"}}`)
	default:
		_, _ = io.WriteString(rec, `{"id":"00000000-0000-4000-8000-0000000000e1","name":"pg-main","status":"READY","project_id":"p"}`)
	}
	return rec.Result(), nil
}

// A 200 is a success only with a receipt that names what was made — the
// name of a database or a user, the id of a secret's version, as the
// specifications give them (scripts/api-contract.json, reads). A 200 with
// an empty object or a cut body is no receipt: the outcome is unknown, the
// answer is API_ERROR (the record of the call stays: intent.OutcomeUnknown)
// and names the read to make before any repeat — never «created» (Астра,
// the full audit of 14.11, findings 06 and 07).
func TestCreate_NoReceiptIsNoSuccess(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	for _, c := range []struct {
		tool, good, read string
		args             map[string]any
	}{
		{"pg_db_create", `{"name":"app","owner":"app"}`, "pg_databases",
			map[string]any{"cluster_id": cluster, "name": "app", "owner": "app", "confirmed": true}},
		{"pg_user_create", `{"name":"app"}`, "pg_users",
			map[string]any{"cluster_id": cluster, "name": "app", "password": "Qwerty123456!", "confirmed": true}},
		{"secret_add_version", `{"version":{"id":"v-1"}}`, "secret_get",
			map[string]any{"path": "app/db", "value": "s3cret-value", "confirmed": true}},
	} {
		for _, receipt := range []string{`{}`, `{"name":`, ``, c.good} {
			w := &receiptWorld{receipt: receipt}
			var r *Registry
			if c.tool == "secret_add_version" {
				r = apiFakeRegistry(t, &apiFake{own: func(rw http.ResponseWriter, req *http.Request) bool {
					if req.URL.Host != "secretmanager.api.cloud.ru" {
						return false
					}
					resp, _ := w.RoundTrip(req)
					body, _ := io.ReadAll(resp.Body)
					_, _ = rw.Write(body)
					return true
				}})
			} else {
				r = dbaasRegistry(t, &fakeDbaas{}, "")
				bare := httpretry.DefaultBase
				httpretry.DefaultBase = w
				t.Cleanup(func() { httpretry.DefaultBase = bare })
			}
			m, text, isErr := callNamed(t, r, c.tool, c.args)
			if w.posts != 1 {
				t.Fatalf("%s, receipt %q: %d creates sent; want one: %s", c.tool, receipt, w.posts, text)
			}
			if strings.Contains(text, "s3cret-value") || strings.Contains(text, "Qwerty123456!") {
				t.Errorf("%s: a secret value in the answer", c.tool)
			}
			if receipt == c.good {
				if isErr {
					t.Errorf("%s, a receipt %s: want success: %s", c.tool, receipt, text)
				}
				continue
			}
			reason, _ := m["reason"].(string)
			hint, _ := m["hint"].(string)
			if !isErr || m["status"] == "created" || m["status"] == "version_added" || !intent.OutcomeUnknown(reason, 0) || reason != clierrors.CodeAPIError || !strings.Contains(hint, c.read) {
				t.Errorf("%s, receipt %q: want API_ERROR (outcome unknown) whose hint names %s, not a success: %s", c.tool, receipt, c.read, text)
			}
		}
	}
}
