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

// pgUsersWorld is a PostgreSQL cluster that refuses to create a user:
// POST …/users answers status with body, GET …/users lists users (500 when
// listFails). The specification of Managed PostgreSQL declares only 200
// and «default» for UserService_Create: the status and the text of the
// refusal of a taken name are a hypothesis, no live run has them — the
// world answers a 409 and a 400 so that the rule does not stand on either.
type pgUsersWorld struct {
	mu        sync.Mutex
	status    int
	body      string
	users     string
	listFails bool
	// drop: the create is sent and its answer is lost.
	drop  bool
	posts int
}

func (w *pgUsersWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/users"):
		w.posts++
		if w.drop {
			return nil, errors.New("read tcp: connection reset by peer")
		}
		http.Error(rec, w.body, w.status)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users") && w.listFails:
		http.Error(rec, `{"message":"internal"}`, 500)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users"):
		_, _ = io.WriteString(rec, `{"users":[`+w.users+`]}`)
	default:
		_, _ = io.WriteString(rec, `{"id":"00000000-0000-4000-8000-0000000000e1","name":"pg-main","status":"READY","project_id":"p"}`)
	}
	return rec.Result(), nil
}

// Release plan 1.9, «Повтор у выпущенных инструментов» (owner's decision
// 12.11, conclusion 120 of «Риск»): pg_user_create of a user the cluster
// has was refused by the platform with nothing but its text, and the way
// on an agent finds by itself — delete the user and create it again —
// changes the password under the applications that use it. evo reads the
// users after the refusal: the user is there — the answer says so and
// that deleting and creating anew is not the way, ask the person. The
// reason and the exit code stay what the refusal alone gives; a refusal
// for another cause, and one evo cannot tell (the list is not read), stay
// as they were.
func TestPgUserCreate_RefusedForAUserThatExistsSaysSo(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	const password = "Qwerty123456!"
	call := func(w *pgUsersWorld) (map[string]any, string) {
		t.Helper()
		r := dbaasRegistry(t, &fakeDbaas{}, "")
		bare := httpretry.DefaultBase
		httpretry.DefaultBase = w
		t.Cleanup(func() { httpretry.DefaultBase = bare })
		m, text, isErr := callNamed(t, r, "pg_user_create", map[string]any{"cluster_id": cluster, "name": "app", "password": password, "confirmed": true})
		if !isErr {
			t.Fatalf("a refused create answers no error: %s", text)
		}
		if strings.Contains(text, password) {
			t.Errorf("the answer holds the password: %s", text)
		}
		if w.posts != 1 && !w.drop && w.status < 500 {
			t.Errorf("sent %d creates, want 1", w.posts)
		}
		return m, text
	}
	for _, refusal := range []struct {
		status int
		body   string
	}{
		{409, `{"code":6,"message":"user already exists"}`},
		{400, `{"code":3,"message":"validate user: name is taken"}`},
	} {
		// The refusal alone: the user is not in the cluster.
		plain, plainText := call(&pgUsersWorld{status: refusal.status, body: refusal.body, users: `{"name":"other"}`})
		if hint, _ := plain["hint"].(string); strings.Contains(hint, "already exists") {
			t.Errorf("%d: a refusal for another cause says the user exists: %s", refusal.status, plainText)
		}
		// The user is in the cluster.
		m, text := call(&pgUsersWorld{status: refusal.status, body: refusal.body, users: `{"name":"other"},{"name":"app"}`})
		hint, _ := m["hint"].(string)
		if !strings.Contains(hint, "already exists") || !strings.Contains(hint, "ask the person") || !strings.Contains(hint, "password") {
			t.Errorf("%d: the hint does not say the user exists, what a delete and a new create break, and to ask the person: %s", refusal.status, text)
		}
		if m["reason"] != plain["reason"] || m["status"] != plain["status"] {
			t.Errorf("%d: reason %v, status %v — changed from the refusal alone (%v, %v): %s", refusal.status, m["reason"], m["status"], plain["reason"], plain["status"], text)
		}
		if msg, _ := m["error"].(string); !strings.Contains(msg, strings.TrimSuffix(strings.SplitAfter(refusal.body, `"message":"`)[1], `"}`)) {
			t.Errorf("%d: the answer lost the platform's text: %s", refusal.status, text)
		}
		for _, next := range []string{"next_tool", "next"} {
			if m[next] != nil {
				t.Errorf("%d: the answer names a next step (%s): the person decides: %s", refusal.status, next, text)
			}
		}
		// The users are not read: the answer is the refusal alone.
		unread, unreadText := call(&pgUsersWorld{status: refusal.status, body: refusal.body, listFails: true})
		if unread["hint"] != plain["hint"] || unread["reason"] != plain["reason"] || unread["error"] != plain["error"] {
			t.Errorf("%d: users not read: the answer differs from the refusal alone:\n%s\n%s", refusal.status, unreadText, plainText)
		}
	}
	// An outcome evo does not know — the answer to the create is lost, or
	// the service failed: the create may have reached the cluster, and the
	// user in the list may be this call's own. The answer does not say
	// «nothing was created» (conclusion 121 of «Риск»): an agent that
	// repeats after a lost answer would take its own password for another's.
	for name, w := range map[string]func() *pgUsersWorld{
		"lost answer": func() *pgUsersWorld { return &pgUsersWorld{drop: true} },
		"500":         func() *pgUsersWorld { return &pgUsersWorld{status: 500, body: `{"message":"internal"}`} },
	} {
		there := w()
		there.users = `{"name":"app"}`
		m, text := call(there)
		hint, _ := m["hint"].(string)
		if !strings.Contains(hint, "may have created it") || !strings.Contains(hint, "ask the person") || !strings.Contains(hint, "password") {
			t.Errorf("%s, the user is in the cluster: the hint does not say this call may have created it: %s", name, text)
		}
		if strings.Contains(hint, "nothing was created") {
			t.Errorf("%s: the hint says nothing was created of an outcome evo does not know: %s", name, text)
		}
		plain, plainText := call(w())
		if h, _ := plain["hint"].(string); strings.Contains(h, "is in the cluster") || strings.Contains(h, "already exists") {
			t.Errorf("%s, no such user: the answer says the user is there: %s", name, plainText)
		}
		if m["reason"] != plain["reason"] || m["status"] != plain["status"] {
			t.Errorf("%s: reason %v, status %v — changed from the failure alone (%v, %v)", name, m["reason"], m["status"], plain["reason"], plain["status"])
		}
	}
}
