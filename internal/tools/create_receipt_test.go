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
	"time"

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
		// The form of v2AddVersionResponse in the specification of Secret
		// Manager: version.id is an int32 (cloud-e2e of 1.9, the job of
		// secrets: a number was no receipt).
		{"secret_add_version", `{"secret":{"id":"s-1","path":"app/db"},"version":{"id":2,"state":"ENABLED","createdAt":"2026-11-19T00:00:00Z"}}`, "secret_get",
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

// D112 promises no second create after an answer without a receipt: the
// outcome is unknown, so the same call again — with the consent of the
// first, its record kept — does not send a second request. It asks, as a
// repeat of D107 does (confirmation_required with interrupted): a
// database or a user while the list does not show its name, a version of
// a secret always — a second version is a second change (Астра, #2113).
// The question sends to the read, not to the repeat.
func TestCreate_NoReceiptRepeatAsksAndSendsNothing(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	for _, c := range []struct {
		tool, read string
		args       map[string]any
	}{
		{"pg_db_create", "pg_databases", map[string]any{"cluster_id": cluster, "name": "app", "owner": "app", "confirmed": true}},
		{"pg_user_create", "pg_users", map[string]any{"cluster_id": cluster, "name": "app", "password": "Qwerty123456!", "confirmed": true}},
		{"secret_add_version", "secret_get", map[string]any{"path": "app/db", "value": "s3cret-value", "confirmed": true}},
	} {
		w := &receiptWorld{receipt: `{}`}
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
		if m, text, isErr := callNamed(t, r, c.tool, c.args); !isErr || m["reason"] != clierrors.CodeAPIError || w.posts != 1 {
			t.Fatalf("%s: the first call — want API_ERROR and one create request (%d): %s", c.tool, w.posts, text)
		}
		m, text, isErr := callNamed(t, r, c.tool, c.args)
		msg, _ := m["message"].(string)
		if w.posts != 1 || !isErr || m["status"] != "confirmation_required" || m["interrupted"] == nil || !strings.Contains(msg, c.read) {
			t.Errorf("%s: the repeat after an answer without a receipt sent %d create requests, want 1 and the question that names %s:\n%s", c.tool, w.posts, c.read, text)
		}
		// The cloud answered, with no receipt: the question does not say
		// the call was cut before it did («Риск», conclusion 288, У3).
		if strings.Contains(msg, "before the cloud answered") || !strings.Contains(msg, "did not name") {
			t.Errorf("%s: the question says the call was cut, though the cloud answered it without a receipt: %s", c.tool, msg)
		}
		// A database and a user have no price of their own — the cluster
		// is billed: the question names no second billed resource
		// (CLAUDE.md §6, axis 4; «Риск», conclusion 288, У4).
		if strings.Contains(msg, "billed") {
			t.Errorf("%s: the question names a price the resource has not: %s", c.tool, msg)
		}
		if strings.Contains(text, "s3cret-value") || strings.Contains(text, "Qwerty123456!") {
			t.Errorf("%s: a secret value in the answer", c.tool)
		}
		// The question is asked once: the next call with consent is the
		// person's yes to one more, and sends (У2).
		if _, text, _ := callNamed(t, r, c.tool, c.args); w.posts != 2 {
			t.Errorf("%s: the call with consent after the question sent %d create requests in all, want 2: %s", c.tool, w.posts, text)
		}
	}
}

// A create of the class says a yes makes a second billed resource only
// when the resource is billed: the rule declares it (notBilled), and the
// question of each tool says the price its rule declares (У4).
func TestCutCallWarning_PriceIsTheRules(t *testing.T) {
	cut := []intent.Intent{{Tool: "x", Name: "app", StartedAt: time.Now()}}
	for tool, rule := range everySendIsAChange {
		if !rule.made {
			continue
		}
		msg, _ := cutCallWarning(tool, cut)
		if billed := strings.Contains(msg, "billed"); billed == rule.notBilled {
			t.Errorf("%s: notBilled %v, and the question says «billed» %v: %s", tool, rule.notBilled, billed, msg)
		}
	}
	for _, tool := range []string{"pg_db_create", "pg_user_create"} {
		if !everySendIsAChange[tool].notBilled {
			t.Errorf("%s: a database and a user of a cluster are not billed on their own; the rule does not say so", tool)
		}
	}
	if everySendIsAChange["pg_create"].notBilled {
		t.Error("pg_create: a cluster is billed; the rule says it is not")
	}
}
