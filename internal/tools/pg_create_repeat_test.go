// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// pgRepeatWorld is a PostgreSQL cluster that takes the first create of a
// database or a user and loses its answer: the name is in the list from
// then on. To a second create of the name it answers second — a 409, a
// 400 or a 200. Which of them Managed PostgreSQL gives is a hypothesis:
// no live run has it and the specification declares no refusal (release
// plan 1.9, «Повтор pg_db_create и pg_user_create после потерянного
// ответа»), so the rule of the repeat stands on none of the three.
type pgRepeatWorld struct {
	mu     sync.Mutex
	kind   string // "databases" or "users"
	second int
	names  []string
	posts  int
}

func (w *pgRepeatWorld) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/"+w.kind):
		w.posts++
		if w.posts == 1 {
			w.names = append(w.names, "app")
			return nil, errors.New("read tcp: connection reset by peer")
		}
		if w.second == http.StatusOK {
			_, _ = io.WriteString(rec, `{}`)
			break
		}
		http.Error(rec, `{"code":6,"message":"already exists"}`, w.second)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/"+w.kind):
		items := make([]string, 0, len(w.names))
		for _, n := range w.names {
			items = append(items, `{"name":"`+n+`"}`)
		}
		_, _ = io.WriteString(rec, `{"`+w.kind+`":[`+strings.Join(items, ",")+`]}`)
	default:
		_, _ = io.WriteString(rec, `{"id":"00000000-0000-4000-8000-0000000000e1","name":"pg-main","status":"READY","project_id":"p"}`)
	}
	return rec.Result(), nil
}

// Release plan 1.9, «Повтор pg_db_create и pg_user_create после
// потерянного ответа» (#1598, conclusion 177 of «Риск»): the answer of
// these creates names nothing, so a repeat cannot be matched by an id.
// The first request is taken and its answer is lost; the same call with
// the same consent comes again. Either evo sends no second request (the
// name is in the cluster, and evo can read that before it sends), or it
// sends one and reads the platform's refusal as «already exists, nothing
// was created». What it must not do is stand on the refusal: a platform
// that takes the second request gets a second create from one consent —
// for a user, with a password sent again — and evo answers «created» for
// what was there before the call.
func TestPgCreate_RepeatAfterALostAnswerMakesNoSecond(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	const password = "Qwerty123456!"
	for _, tool := range []struct {
		name, kind string
		args       map[string]any
	}{
		{"pg_db_create", "databases", map[string]any{"cluster_id": cluster, "name": "app", "owner": "app", "confirmed": true}},
		{"pg_user_create", "users", map[string]any{"cluster_id": cluster, "name": "app", "password": password, "confirmed": true}},
	} {
		for _, second := range []int{http.StatusConflict, http.StatusBadRequest, http.StatusOK} {
			w := &pgRepeatWorld{kind: tool.kind, second: second}
			r := dbaasRegistry(t, &fakeDbaas{}, "")
			bare := httpretry.DefaultBase
			httpretry.DefaultBase = w
			_, first, isErr := callNamed(t, r, tool.name, tool.args)
			if !isErr || w.posts != 1 {
				httpretry.DefaultBase = bare
				t.Fatalf("%s: the world did not lose the answer of the first create (requests %d): %s", tool.name, w.posts, first)
			}
			m, text, isErr := callNamed(t, r, tool.name, tool.args)
			httpretry.DefaultBase = bare
			if strings.Contains(first+text, password) {
				t.Errorf("%s, a second create answered %d: the password is in an answer", tool.name, second)
			}
			hint, _ := m["hint"].(string)
			if w.posts == 1 {
				// No second request: the answer is the one a refusal of
				// the platform gets — the reason and the hint of a repeat
				// stay (no breaking change).
				if !isErr || m["reason"] != "API_ERROR" || !strings.Contains(hint, "already exists") || !strings.Contains(hint, "nothing was created") {
					t.Errorf("%s, no second create: want API_ERROR with «already exists, nothing was created»: %s", tool.name, text)
				}
				continue
			}
			if !isErr || !strings.Contains(hint, "already exists") || !strings.Contains(hint, "nothing was created") {
				t.Errorf("%s, a second create answered %d: the repeat of a taken request sent a second one (requests %d) and did not read a refusal as «already exists, nothing was created»: %s",
					tool.name, second, w.posts, text)
			}
		}
	}
}

// A record of an interrupted pg_db_create or pg_user_create closes when
// doctor (checkByList) reads the cluster's list and finds the name: these
// lists give no id, so the record is closed by the name alone, with no
// «(id …)» in the words (found by name, with no id (databases and users of a PostgreSQL cluster); «Риск», 191, У2; declared with
// «разработка C»). A name the list does not have keeps the record.
func TestPgCreate_InterruptedRecordClosesByNameWithoutAnID(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	for _, c := range []struct {
		tool, list, field string
	}{
		{"pg_db_create", "pg_databases", "databases"},
		{"pg_user_create", "pg_users", "users"},
	} {
		r := New(nil)
		// The list of the cluster, with no id (as Managed PostgreSQL gives
		// it): "app" is there, "gone" is not.
		list := `{"` + c.field + `":[{"name":"app"}]}`
		if !r.Wrap(c.list, func(Handler) Handler {
			return func(context.Context, map[string]any) (string, bool) { return list, false }
		}) {
			t.Fatalf("no tool %s", c.list)
		}
		here := intent.Intent{Tool: c.tool, Name: "app", Target: "cluster_id=" + cluster}
		gone := intent.Intent{Tool: c.tool, Name: "gone", Target: "cluster_id=" + cluster}
		found, settled := r.checkByList(context.Background(), here, "")
		if !settled || strings.Contains(found, "(id ") {
			t.Errorf("%s: a name in the list closes the record without an id: settled=%v %q", c.tool, settled, found)
		}
		if _, settled := r.checkByList(context.Background(), gone, ""); settled {
			t.Errorf("%s: a name not in the list closed the record", c.tool)
		}
	}
}

// doctor itself closes the record of an interrupted pg_db_create whose
// database is now in the cluster: it reads pg_databases (no id in the
// list) and closes the record by the name, naming it (FB19I-24; «Риск»,
// 191, У2). A record whose name the list does not have stays.
func TestDoctor_ClosesInterruptedPgDbCreateByName(t *testing.T) {
	r, dir := targetRegistry(t, &disksCloud{})
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	oldIAM, oldCompute := auth.IAMEndpoint, ComputeBaseURL
	t.Cleanup(func() { auth.IAMEndpoint, ComputeBaseURL = oldIAM, oldCompute })
	auth.IAMEndpoint, ComputeBaseURL = srv.URL+"/token", srv.URL
	if !r.Wrap("pg_databases", func(Handler) Handler {
		return func(context.Context, map[string]any) (string, bool) {
			return `{"databases":[{"name":"made"}]}`, false
		}
	}) {
		t.Fatal("no tool pg_databases")
	}
	cluster := "cluster_id=00000000-0000-4000-8000-0000000000e1"
	for _, in := range []intent.Intent{
		{Tool: "pg_db_create", Name: "made", Target: cluster, ProjectID: "p"},
		{Tool: "pg_db_create", Name: "not-yet", Target: cluster, ProjectID: "p"},
	} {
		if _, err := intent.Begin(intent.Dir(dir), in); err != nil {
			t.Fatal(err)
		}
	}
	text, _ := r.handlers["doctor"](context.Background(), map[string]any{})
	left := map[string]bool{}
	for _, in := range intent.Pending(intent.Dir(dir)) {
		left[in.Name] = true
	}
	if left["made"] || !left["not-yet"] {
		t.Errorf("doctor left %v (want «made» closed, «not-yet» kept):\n%s", left, text)
	}
	if !strings.Contains(text, "the create took effect") || strings.Contains(text, "(id )") {
		t.Errorf("doctor does not close «made» by name without an id:\n%s", text)
	}
}

// «Риск», 191, У2: a lost answer leaves one record of the call; the same
// call repeated three times, each refused before its create (the name is
// in the cluster, nothing sent), adds none — a refusal that sent no write
// closes its own record (#1675), and pending_operations does not grow
// with calls that sent nothing.
func TestPgCreate_RepeatsAfterALostAnswerAddNoRecords(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	for _, tool := range []struct {
		name, kind string
		args       map[string]any
	}{
		{"pg_db_create", "databases", map[string]any{"cluster_id": cluster, "name": "app", "owner": "app", "confirmed": true}},
		{"pg_user_create", "users", map[string]any{"cluster_id": cluster, "name": "app", "password": "Qwerty123456!", "confirmed": true}},
	} {
		w := &pgRepeatWorld{kind: tool.kind, second: http.StatusOK}
		r := dbaasRegistry(t, &fakeDbaas{}, "")
		root, ok := r.stateDir()
		if !ok {
			t.Fatal("no state directory")
		}
		bare := httpretry.DefaultBase
		httpretry.DefaultBase = w
		if _, text, isErr := callNamed(t, r, tool.name, tool.args); !isErr || w.posts != 1 {
			httpretry.DefaultBase = bare
			t.Fatalf("%s: the world did not lose the first answer: %s", tool.name, text)
		}
		after := len(pendingIntents(root))
		if after == 0 {
			t.Fatalf("%s: the cut call left no record: the test would hold nothing", tool.name)
		}
		for range 3 {
			callNamed(t, r, tool.name, tool.args)
		}
		httpretry.DefaultBase = bare
		if got := len(pendingIntents(root)); got > after || w.posts != 1 {
			t.Errorf("%s: records %d after the cut call, %d after three repeats (creates sent %d): the repeats that sent nothing left records", tool.name, after, got, w.posts)
		}
	}
}

// D109 («Риск», 191, У1; the owner's «да» 13.11): a name the cluster has is
// refused before the question — a call without consent gets the refusal,
// not confirmation_required, and nothing is sent. The answer is the one
// the check before the create gives (API_ERROR, «already exists, nothing
// was created»).
func TestPgCreate_TakenNameRefusedBeforeTheQuestion(t *testing.T) {
	const cluster = "00000000-0000-4000-8000-0000000000e1"
	for _, tool := range []struct {
		name, kind string
		args       map[string]any
	}{
		{"pg_db_create", "databases", map[string]any{"cluster_id": cluster, "name": "app", "owner": "app"}},
		{"pg_user_create", "users", map[string]any{"cluster_id": cluster, "name": "app", "password": "Qwerty123456!"}},
	} {
		w := &pgRepeatWorld{kind: tool.kind, names: []string{"app"}}
		r := dbaasRegistry(t, &fakeDbaas{}, "")
		bare := httpretry.DefaultBase
		httpretry.DefaultBase = w
		m, text, isErr := callNamed(t, r, tool.name, tool.args)
		httpretry.DefaultBase = bare
		hint, _ := m["hint"].(string)
		if !isErr || m["reason"] != "API_ERROR" || m["status"] == "confirmation_required" || !strings.Contains(hint, "already exists") || w.posts != 0 {
			t.Errorf("%s without consent, the name taken: want the refusal before the question, nothing sent (%d): %s", tool.name, w.posts, text)
		}
	}
}
