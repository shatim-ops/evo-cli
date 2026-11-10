// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// signInAgain are the words of «start the sign-in again»: while this login
// waits, a second evo login does not start (STATE_LOCKED), so a refusal
// that sends there leads to another refusal («Риск», conclusion 58, № 22).
var signInAgain = []string{"Sign in again", "Запустите вход еще раз", "запустите вход еще раз"}

func callbackOf(l *loopback, query, host string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://"+host+"/callback?"+query, nil)
	r.Host = host
	l.ServeHTTP(w, r)
	return w
}

// Finding 22 of 1.8, «Риск», conclusion 58 (1)–(2): a callback with another
// login's state says the link is of another or earlier login and the way
// back to the one that waits; the other refusals say «not of this login»
// with no «sign in again» either.
func TestRefusals_DoNotSendToASecondLogin(t *testing.T) {
	prev := i18n.Lang()
	defer i18n.SetLang(prev)
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		l := &loopback{s: &session{state: "own"}, host: "127.0.0.1:1", done: make(chan outcome, 1)}
		foreign := html.UnescapeString(callbackOf(l, "state=another&code=c", l.host).Body.String())
		other := callbackOf(l, "state=own&code=c", "evil.example:1").Body.String()
		if !strings.Contains(foreign, tForeignLink()) {
			t.Errorf("%s: a callback of another login: %q", lang, foreign)
		}
		if !strings.Contains(other, tForeign()) {
			t.Errorf("%s: a request of another host: %q", lang, other)
		}
		for _, again := range signInAgain {
			if strings.Contains(foreign, again) || strings.Contains(other, again) {
				t.Errorf("%s: a refusal sends to a second login (%q):\n%s\n%s", lang, again, foreign, other)
			}
		}
	}
}

// Finding 23 of 1.8, «Риск», conclusion 58: a known error of OAuth reads
// as words, its code in brackets for the support; an unknown one is as the
// provider sent it.
func TestProviderError_KnownCodesInWords(t *testing.T) {
	for _, code := range []string{"invalid_request", "unauthorized_client", "invalid_scope", "server_error",
		"temporarily_unavailable", "login_required", "interaction_required", "consent_required"} {
		got := eProvider(code, "details")
		if !strings.Contains(got, "("+code+": "+oneLine("details")+")") || strings.Contains(got, oneLine(code+": details")) {
			t.Errorf("%s: %q, want words and the code in brackets", code, got)
		}
		if got := eProvider(code, ""); !strings.Contains(got, "("+code+")") {
			t.Errorf("%s with no description: %q", code, got)
		}
	}
	if got := eProvider("vendor_specific", "why"); !strings.Contains(got, oneLine("vendor_specific: why")) {
		t.Errorf("an unknown code: %q", got)
	}
	// The callback says it: the page and the error of the login.
	l := &loopback{s: &session{state: "own"}, host: "127.0.0.1:1", done: make(chan outcome, 1)}
	page := html.UnescapeString(callbackOf(l, "state=own&error=temporarily_unavailable", l.host).Body.String())
	o := <-l.done
	if want := eProvider("temporarily_unavailable", ""); !strings.Contains(page, want) || o.err == nil || !strings.Contains(o.err.Error(), want) {
		t.Errorf("the callback: page %q, error %v; want %q", page, o.err, want)
	}
}
