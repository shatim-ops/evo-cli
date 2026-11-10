// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Finding 10 of 1.8 (R3-F5): a callback with this login's state but no
// code and no error is no answer of the provider — a reload cut short, a
// probe that read the state. It is refused, and the login waits on for
// the provider's redirect: it does not end with a failed exchange.
func TestCallback_OwnStateWithoutCodeDoesNotEndTheLogin(t *testing.T) {
	l := &loopback{s: &session{state: "own"}, host: "127.0.0.1:1", done: make(chan outcome, 1)}
	for _, q := range []string{"state=own", "state=own&code="} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "http://"+l.host+"/callback?"+q, nil)
		r.Host = l.host
		l.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: answered %d, want 403", q, w.Code)
		}
		select {
		case o := <-l.done:
			t.Fatalf("%s: the login ended: %v", q, o.err)
		default:
		}
		if l.phase != phaseCode {
			t.Errorf("%s: phase %d, want the wait for the code", q, l.phase)
		}
	}
}
