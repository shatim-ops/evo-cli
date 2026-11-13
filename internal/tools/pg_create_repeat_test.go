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
			if w.posts == 1 {
				continue
			}
			hint, _ := m["hint"].(string)
			if !isErr || !strings.Contains(hint, "already exists") || !strings.Contains(hint, "nothing was created") {
				t.Errorf("%s, a second create answered %d: the repeat of a taken request sent a second one (requests %d) and did not read a refusal as «already exists, nothing was created»: %s",
					tool.name, second, w.posts, text)
			}
		}
	}
}
