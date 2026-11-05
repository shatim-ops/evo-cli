// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Login signs the person in and makes the access they choose (contract
// B-1, steps 1–19). previous is the key of the profile now, replaced by the
// new one; save keeps the new one in the profile.
func Login(ctx context.Context, cfg Config, choice Choice, previous *Previous, save func(Saved) error) (Result, error) {
	s := &session{cfg: cfg, choice: choice, previous: previous, save: save,
		state: random(16), nonce: random(16), verifier: random(32)}
	d, err := cfg.discover(ctx)
	if err != nil {
		return Result{}, err
	}
	s.disc = d
	ln, port, err := listen(cfg.Ports)
	if err != nil {
		return Result{}, err
	}
	s.redirect = fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	authURL := d.AuthorizationEndpoint + "?" + url.Values{
		"response_type": {"code"}, "client_id": {auth.LoginClientID}, "redirect_uri": {s.redirect},
		"scope": {auth.LoginScope}, "state": {s.state}, "nonce": {s.nonce},
		"code_challenge": {challenge(s.verifier)}, "code_challenge_method": {"S256"},
	}.Encode()

	lb := &loopback{s: s, host: "127.0.0.1:" + strconv.Itoa(port), done: make(chan outcome, 1)}
	srv := &http.Server{Handler: lb, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	cfg.Say(i18n.T("Open this address in a browser to sign in to Cloud.ru:", "Откройте этот адрес в браузере и войдите в Cloud.ru:"), authURL)
	if cfg.Open != nil {
		_ = cfg.Open(authURL) // the address is printed: a browser that did not open is no failure
	}

	timer := time.NewTimer(cfg.Wait)
	defer timer.Stop()
	select {
	case o := <-lb.done:
		return o.res, o.err
	case <-timer.C:
		lb.close()
		return Result{}, fmt.Errorf(i18n.T("the sign-in was not completed in %s; nothing was created: %w",
			"вход не завершён за %s; ничего не создано: %w"), cfg.Wait, context.Canceled)
	case <-ctx.Done():
		lb.close()
		return Result{}, ctx.Err()
	}
}

// listen takes the first free port of ports on 127.0.0.1 only.
func listen(ports []int) (net.Listener, int, error) {
	for _, p := range ports {
		if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p))); err == nil {
			return ln, p, nil
		}
	}
	return nil, 0, clierrors.New(clierrors.CodeConfigInvalid,
		i18n.Tf("every port of the sign-in is taken (%v); nothing was sent", "все порты входа заняты (%v); ничего не отправлено", ports),
		i18n.T("Free one of them (another 'evo login' may hold it) and run 'evo login' again.",
			"Освободите один из них (его может держать другой 'evo login') и повторите 'evo login'."))
}

// outcome is how the login ended.
type outcome struct {
	res Result
	err error
}

// loopback serves the redirect and the consent on 127.0.0.1: one code, one
// choice, of this login's state only (contract B-1, invariant 3).
type loopback struct {
	s    *session
	host string
	done chan outcome

	mu    sync.Mutex
	phase int // 0: waiting for the code; 1: the consent; 2: ended
}

const (
	phaseCode = iota
	phaseConsent
	phaseEnded
)

func (l *loopback) close() {
	l.mu.Lock()
	l.phase = phaseEnded
	l.mu.Unlock()
}

func (l *loopback) end(o outcome) {
	l.phase = phaseEnded
	l.done <- o
}

// refuse answers a request that is not of this login: no page, no secret.
func refuse(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintln(w, i18n.T("This request is not of this sign-in; run 'evo login' again.",
		"Запрос не от этого входа; повторите 'evo login'."))
}

func (l *loopback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A page of another site cannot drive the loopback by a name that
	// resolves to it (DNS rebinding): the Host is ours or nothing.
	if r.Host != l.host {
		refuse(w, http.StatusForbidden)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/callback":
		l.callback(w, r)
	case r.Method == http.MethodPost && (r.URL.Path == "/grant" || r.URL.Path == "/cancel"):
		l.consent(w, r)
	default:
		refuse(w, http.StatusNotFound)
	}
}

func (l *loopback) ownState(got string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(l.s.state)) == 1
}

// callback takes the provider's redirect once, with this login's state
// (steps 5–11).
func (l *loopback) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if l.phase != phaseCode || !l.ownState(q.Get("state")) {
		refuse(w, http.StatusForbidden)
		return
	}
	ctx := r.Context()
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			writePage(w, http.StatusOK, cancelledPage())
			l.end(outcome{err: cancelled()})
			return
		}
		err := clierrors.New(clierrors.CodeAuthFailed,
			i18n.Tf("the provider of Cloud.ru refused the sign-in: %s; nothing was created",
				"провайдер входа Cloud.ru отказал: %s; ничего не создано", oneLine(e+": "+q.Get("error_description"))),
			i18n.T("Run 'evo login' again; if it repeats, the account may not sign in here — ask the organization administrator.",
				"Повторите 'evo login'; если повторится — входу этой учётной записи что-то мешает, спросите администратора организации."))
		writePage(w, http.StatusOK, errorPage(err))
		l.end(outcome{err: err})
		return
	}
	if err := l.s.exchange(ctx, q.Get("code")); err != nil {
		writePage(w, http.StatusOK, errorPage(err))
		l.end(outcome{err: err})
		return
	}
	if err := l.s.prepare(ctx); err != nil {
		writePage(w, http.StatusOK, errorPage(err))
		l.end(outcome{err: err})
		return
	}
	l.phase = phaseConsent
	writePage(w, http.StatusOK, consentPage(l.s))
}

// consent takes the person's choice once: the grant or the cancel.
func (l *loopback) consent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || l.phase != phaseConsent || !l.ownState(r.PostForm.Get("state")) {
		refuse(w, http.StatusForbidden)
		return
	}
	if r.URL.Path == "/cancel" {
		writePage(w, http.StatusOK, cancelledPage())
		l.end(outcome{err: cancelled()})
		return
	}
	p, aud, days, err := l.s.chosen(r.PostForm)
	if err != nil {
		writePage(w, http.StatusOK, errorPage(err))
		l.end(outcome{err: err})
		return
	}
	res, err := l.s.grant(r.Context(), p, aud, days)
	if err != nil {
		writePage(w, http.StatusOK, errorPage(err))
		l.end(outcome{res: res, err: err})
		return
	}
	writePage(w, http.StatusOK, donePage(res))
	l.end(outcome{res: res})
}

// chosen is the choice of the form, held to what the page offered.
func (s *session) chosen(f url.Values) (project, Audience, int, error) {
	bad := func(what string) (project, Audience, int, error) {
		return project{}, "", 0, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("the choice of the page is not one it offered (%s); nothing was created", "выбор страницы не из предложенных (%s); ничего не создано", what),
			i18n.T("Run 'evo login' again.", "Повторите 'evo login'."))
	}
	var p *project
	for i := range s.projects {
		if s.projects[i].ID == f.Get("project") {
			p = &s.projects[i]
		}
	}
	if p == nil {
		return bad("project")
	}
	if !p.Allowed {
		return project{}, "", 0, noRight(p.Name)
	}
	aud := Audience(f.Get("audience"))
	if aud != ForMe && aud != ForAgent {
		return bad("audience")
	}
	days, err := strconv.Atoi(f.Get("ttl"))
	if err != nil || !containsInt(Terms, days) {
		return bad("ttl")
	}
	return *p, aud, days, nil
}

func containsInt(xs []int, x int) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// cancelled is the end of a sign-in the person cancelled: USAGE, nothing
// made.
func cancelled() error {
	return clierrors.New(clierrors.CodeUsage, i18n.T("the sign-in was cancelled; nothing was created", "вход отменён; ничего не создано"),
		i18n.T("Run 'evo login' when ready.", "Повторите 'evo login', когда будете готовы."))
}

// oneLine is a provider's text as evo's own text quotes it: on one line,
// at most 200 characters (process audit 1.2.1, П9).
func oneLine(s string) string {
	if len(s) > 200 {
		s = s[:200]
	}
	return strconv.Quote(s)
}
