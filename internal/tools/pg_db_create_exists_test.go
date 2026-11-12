// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// pgDatabasesWorld is a PostgreSQL cluster that refuses to create a
// database: POST …/databases answers status with body (or loses its answer,
// drop), GET …/databases lists databases (500 when listFails). The
// specification of Managed PostgreSQL declares only 200 and «default» for
// the create of a database: the status and the text of the refusal of a
// taken name are a hypothesis — the world answers a 409 and a 400 so that
// the rule stands on neither (as pgUsersWorld).
type pgDatabasesWorld struct {
	mu        sync.Mutex
	status    int
	body      string
	databases string
	listFails bool
	drop      bool
	posts     int
}

func (w *pgDatabasesWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/databases"):
		w.posts++
		if w.drop {
			return nil, errors.New("read tcp: connection reset by peer")
		}
		http.Error(rec, w.body, w.status)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/databases") && w.listFails:
		http.Error(rec, `{"message":"internal"}`, 500)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/databases"):
		_, _ = io.WriteString(rec, `{"databases":[`+w.databases+`]}`)
	default:
		_, _ = io.WriteString(rec, `{"id":"00000000-0000-4000-8000-0000000000e1","name":"pg-main","status":"READY","project_id":"p"}`)
	}
	return rec.Result(), nil
}

// Release plan 1.9, «Повтор у выпущенных инструментов» (owner's decision
// 12.11, conclusion 120 of «Риск»): pg_db_create of a database the cluster
// has was refused by the platform with nothing but its text, and the way
// on an agent finds by itself — delete the database and create it again —
// destroys its data. evo reads the databases after the refusal
// (pgCreateOfExisting, the rule of pg_user_create): the database is there
// — the answer says so, and that deleting it is not the way, ask the
// person. The reason and the exit code stay what the refusal alone gives;
// a refusal for another cause, and one evo cannot tell, stay as they were.
// An outcome evo does not know (a lost answer, a 5xx) does not say
// «nothing was created»: the database in the list may be this call's own
// (conclusion 121).
func TestPgDbCreate_RefusedForADatabaseThatExistsSaysSo(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	call := func(w *pgDatabasesWorld) (map[string]any, string) {
		t.Helper()
		r := dbaasRegistry(t, &fakeDbaas{}, "")
		bare := httpretry.DefaultBase
		httpretry.DefaultBase = w
		t.Cleanup(func() { httpretry.DefaultBase = bare })
		m, text, isErr := callNamed(t, r, "pg_db_create", map[string]any{"cluster_id": cluster, "name": "app", "owner": "app", "confirmed": true})
		if !isErr {
			t.Fatalf("a refused create answers no error: %s", text)
		}
		return m, text
	}
	for _, refusal := range []struct {
		status int
		body   string
	}{
		{409, `{"code":6,"message":"database already exists"}`},
		{400, `{"code":3,"message":"validate database: name is taken"}`},
	} {
		plain, plainText := call(&pgDatabasesWorld{status: refusal.status, body: refusal.body, databases: `{"name":"other"}`})
		if hint, _ := plain["hint"].(string); strings.Contains(hint, "already exists") {
			t.Errorf("%d: a refusal for another cause says the database exists: %s", refusal.status, plainText)
		}
		m, text := call(&pgDatabasesWorld{status: refusal.status, body: refusal.body, databases: `{"name":"other"},{"name":"app"}`})
		hint, _ := m["hint"].(string)
		if !strings.Contains(hint, "already exists") || !strings.Contains(hint, "nothing was created") ||
			!strings.Contains(hint, "ask the person") || !strings.Contains(hint, "data") {
			t.Errorf("%d: the hint does not say the database exists, what a delete destroys, and to ask the person: %s", refusal.status, text)
		}
		if m["reason"] != plain["reason"] || m["status"] != plain["status"] {
			t.Errorf("%d: reason %v, status %v — changed from the refusal alone (%v, %v): %s", refusal.status, m["reason"], m["status"], plain["reason"], plain["status"], text)
		}
		for _, next := range []string{"next_tool", "next"} {
			if m[next] != nil {
				t.Errorf("%d: the answer names a next step (%s): the person decides: %s", refusal.status, next, text)
			}
		}
		unread, unreadText := call(&pgDatabasesWorld{status: refusal.status, body: refusal.body, listFails: true})
		if unread["hint"] != plain["hint"] || unread["reason"] != plain["reason"] || unread["error"] != plain["error"] {
			t.Errorf("%d: databases not read: the answer differs from the refusal alone:\n%s\n%s", refusal.status, unreadText, plainText)
		}
	}
	for name, w := range map[string]func() *pgDatabasesWorld{
		"lost answer": func() *pgDatabasesWorld { return &pgDatabasesWorld{drop: true} },
		"500":         func() *pgDatabasesWorld { return &pgDatabasesWorld{status: 500, body: `{"message":"internal"}`} },
	} {
		there := w()
		there.databases = `{"name":"app"}`
		m, text := call(there)
		hint, _ := m["hint"].(string)
		if !strings.Contains(hint, "may have created it") || !strings.Contains(hint, "ask the person") || strings.Contains(hint, "nothing was created") {
			t.Errorf("%s, the database is in the cluster: the hint does not say this call may have created it: %s", name, text)
		}
	}
}
