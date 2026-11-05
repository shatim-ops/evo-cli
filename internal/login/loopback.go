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
)

// Login signs the person in and makes the access they grant (contract
// B-1, steps 1–19). store names the profile of the chosen audience, holds
// its key now and keeps the new one. Access is granted only by «Grant
// access» on the consent page («Риск», conclusion 24): a terminal, when
// there is one, only gathers the choice before the browser.
func Login(ctx context.Context, cfg Config, choice Choice, store Store) (Result, error) {
	s := &session{cfg: cfg, choice: choice, store: store,
		state: random(16), nonce: random(16), verifier: random(32)}
	if cfg.Ask != nil {
		c, err := s.wizard(ctx)
		if err != nil {
			return Result{}, err
		}
		s.choice = c
	}
	d, err := cfg.discover(ctx)
	if err != nil {
		return Result{}, at(stepDiscovery, err)
	}
	s.disc = d
	ln, port, err := listen(cfg.Ports)
	if err != nil {
		return Result{}, at(stepListen, err)
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

	if s.choice.For == ForAgent {
		var term time.Time
		if s.choice.Days > 0 {
			term = s.cfg.Now().Add(time.Duration(s.choice.Days) * 24 * time.Hour)
		}
		s.warnAgent(term)
	}
	s.tell(tOpenPage())
	cfg.Say(tOpen(), authURL)
	if cfg.Open != nil {
		_ = cfg.Open(authURL) // the address is printed: a browser that did not open is no failure
	} else {
		s.tell(tNoBrowser())
	}
	s.tell(tWaiting(cfg.Wait))

	timer := time.NewTimer(cfg.Wait)
	defer timer.Stop()
	select {
	case o := <-lb.done:
		return o.res, o.err
	case <-timer.C:
		lb.close()
		return Result{}, fmt.Errorf("%s%s: %w", eTimeout(cfg.Wait), tAt(stepWait, 0), context.Canceled)
	case <-ctx.Done():
		lb.close()
		return Result{}, ctx.Err()
	}
}

// warnAgent says in the terminal, once, what a key for an agent can do
// and the role's term, when it is known (zero: not chosen yet).
func (s *session) warnAgent(term time.Time) {
	if !s.agentWarned {
		s.agentWarned = true
		until := ""
		if !term.IsZero() {
			until = day(term)
		}
		s.tell(tAgentWarnLine(until))
		s.tell(tAgentRevokeLine())
	}
}

// tell writes a line in the terminal, when there is one to write to.
// The steps of the grant come from the loopback's goroutine while Login
// may still be writing: one line at a time.
func (s *session) tell(line string) {
	if s.cfg.Tell != nil {
		s.tellMu.Lock()
		defer s.tellMu.Unlock()
		s.cfg.Tell(line)
	}
}

// listen takes the first free port of ports on 127.0.0.1 only.
func listen(ports []int) (net.Listener, int, error) {
	for _, p := range ports {
		if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p))); err == nil {
			// Port 0 (the tests) is the one the system chose.
			return ln, ln.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return nil, 0, clierrors.New(clierrors.CodeConfigInvalid, ePorts(ports), ePortsHint())
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
	fmt.Fprintln(w, tForeign())
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
	// The fonts of the pages: static, without the lock a grant may hold.
	if serveFont(w, r) {
		return
	}
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

// ownConsent is the token of the consent page; there is none before the
// page is served.
func (l *loopback) ownConsent(got string) bool {
	return l.s.consent != "" && subtle.ConstantTimeCompare([]byte(got), []byte(l.s.consent)) == 1
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
		err := at(stepCallback, clierrors.New(clierrors.CodeAuthFailed, eProvider(oneLine(e+": "+q.Get("error_description"))),
			eProviderHint()))
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
	// R-47: the consent is this page's own token, apart from the state the
	// terminal shows; it is in this answer only, which is served once.
	l.s.consent = random(16)
	l.s.tellPreselect()
	l.phase = phaseConsent
	writePage(w, http.StatusOK, consentPage(l.s))
}

// consent takes the person's choice once: the grant or the cancel.
func (l *loopback) consent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || l.phase != phaseConsent || !l.ownState(r.PostForm.Get("state")) ||
		!l.ownConsent(r.PostForm.Get("consent")) {
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
		err = at(stepChoice, err)
		writePage(w, http.StatusOK, errorPage(err))
		l.end(outcome{err: err})
		return
	}
	pl, err := l.s.planFor(r.Context(), p, aud, days)
	if err != nil {
		writePage(w, http.StatusOK, errorPage(err))
		l.end(outcome{err: err})
		return
	}
	res, err := l.s.grant(r.Context(), pl, l.s.tell)
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
		return project{}, "", 0, clierrors.New(clierrors.CodeUsage, eChoice(what), eRetry())
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
	return clierrors.New(clierrors.CodeUsage, eCancelled(), eCancelledHint())
}

// oneLine is a provider's text as evo's own text quotes it: on one line,
// at most 200 characters (process audit 1.2.1, П9).
func oneLine(s string) string {
	if len(s) > 200 {
		s = s[:200]
	}
	return strconv.Quote(s)
}
