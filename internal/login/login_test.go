// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// The flow of evo login on the world of B-2 (internal/loginworld): every
// variant of the contract's section 6 ends with the reason the contract
// names, and a failure after a write leaves nothing this login made
// (invariant 8; «Риск», R-44). The world listens on no fixed port; evo's
// loopback takes the registered ones, so these tests do not run in
// parallel.

// person plays the human in a browser: signs in, reads the consent page
// and grants (or cancels) — or does what the variant of the test says.
type browser struct {
	t      *testing.T
	w      *loginworld.World
	cancel bool             // press «Cancel» on the consent page
	form   func(url.Values) // change the form before it is sent
	// noToken first sends «Grant access» without the consent token, then
	// cancels; again asks for the page a second time.
	noToken, again bool
	consent        string // the consent page
	page           string // the last page evo served
	status         []int
	ended          chan struct{} // closed when the browser is done
}

var stateField = regexp.MustCompile(`name="state" value="([^"]+)"`)
var consentField = regexp.MustCompile(`name="consent" value="([^"]+)"`)
var checkedProject = regexp.MustCompile(`name="project" id="p\d+" value="([^"]+)" required checked`)

func (b *browser) open(authURL string) error {
	go func() {
		defer close(b.ended)
		c := b.w.Client()
		resp, err := c.Get(authURL)
		if err != nil {
			return
		}
		resp.Body.Close()
		cb := resp.Header.Get("Location")
		resp, err = c.Get(cb) // evo's loopback: the consent page, or the end
		if err != nil {
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		b.page = string(body)
		b.status = append(b.status, resp.StatusCode)
		b.consent = b.page
		m := stateField.FindStringSubmatch(b.page)
		if m == nil {
			return // no consent: the login ended at the callback
		}
		if b.again {
			if r, err := c.Get(cb); err == nil {
				r.Body.Close()
				b.status = append(b.status, r.StatusCode)
			}
		}
		f := url.Values{"state": {m[1]}, "audience": {"me"}, "ttl": {"30"}}
		u, _ := url.Parse(cb)
		if b.noToken {
			if r, err := c.PostForm("http://"+u.Host+"/grant", f); err == nil {
				r.Body.Close()
				b.status = append(b.status, r.StatusCode)
			}
			b.cancel = true
		}
		if t := consentField.FindStringSubmatch(b.page); t != nil {
			f.Set("consent", t[1])
		}
		if p := checkedProject.FindStringSubmatch(b.page); p != nil {
			f.Set("project", p[1])
		}
		if b.form != nil {
			b.form(f)
		}
		action := "/grant"
		if b.cancel {
			action = "/cancel"
		}
		resp, err = c.PostForm("http://"+u.Host+action, f)
		if err != nil {
			return
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		b.page = string(body)
		b.status = append(b.status, resp.StatusCode)
	}()
	return nil
}

func worldConfig(w *loginworld.World, out io.Writer, open func(string) error) Config {
	c := Default(func(m, u string) { fmt.Fprintln(out, m); fmt.Fprintln(out, u) }, open)
	c.Issuer, c.OrgBase, c.AdminBase, c.KeyToken = w.Issuer, w.URL, w.URL, w.URL+"/api/v1/auth/token"
	// The world is plain http on 127.0.0.1: its own address is the one
	// trusted; anything else is not (CheckDiscovery is held to the
	// production rule in TestCheckDiscovery).
	c.Trusted = func(u string) bool { return strings.HasPrefix(u, w.URL+"/") }
	c.HTTP = w.Client()
	c.Wait = 20 * time.Second
	// Port 0: the tests share no fixed port (the world takes any loopback
	// port); the registered ones are held by TestLogin_RegisteredPorts.
	c.Ports = []int{0}
	return c
}

// newWorld is the world of B-2 that takes the loopback at any port.
func newWorld(t *testing.T, v loginworld.Variant) *loginworld.World {
	v.AnyLoopbackPort = true
	return loginworld.New(t, v)
}

type run struct {
	res   Result
	err   error
	saved []Saved
	auds  []Audience
	out   string
	b     *browser
}

// memStore is a profile in memory: «default» for the person, «agent» for an
// agent, as the CLI names them.
type memStore struct {
	prev    *Previous
	saveErr error
	saved   []Saved
	auds    []Audience
}

func (m *memStore) Profile(a Audience) string {
	if a == ForAgent {
		return "agent"
	}
	return "default"
}

func (m *memStore) Previous(Audience) *Previous { return m.prev }

func (m *memStore) Save(a Audience, s Saved) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved, m.auds = append(m.saved, s), append(m.auds, a)
	return nil
}

func login(t *testing.T, w *loginworld.World, b *browser, ch Choice, prev *Previous, saveErr error) run {
	t.Helper()
	return loginWith(t, w, b, ch, prev, saveErr, nil)
}

// loginWith is login with the terminal's answers: nil is no terminal.
func loginWith(t *testing.T, w *loginworld.World, b *browser, ch Choice, prev *Previous, saveErr error, answers []string) run {
	t.Helper()
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	r := run{b: b}
	st := &memStore{prev: prev, saveErr: saveErr}
	cfg := worldConfig(w, &out, b.open)
	cfg.Tell = func(line string) { fmt.Fprintln(&out, line) }
	if answers != nil {
		cfg.Ask = func(_ context.Context, prompt string) (string, error) {
			fmt.Fprint(&out, prompt)
			if len(answers) == 0 {
				t.Errorf("an unexpected question: %q", prompt)
				return "", io.EOF
			}
			a := answers[0]
			answers = answers[1:]
			fmt.Fprintln(&out, a)
			return a, nil
		}
	}
	res, err := Login(context.Background(), cfg, ch, st)
	r.saved, r.auds = st.saved, st.auds
	r.res, r.err, r.out = res, err, out.String()
	<-b.ended
	// Nothing of the person's or the key's secrets in the answer, the
	// terminal or the pages (contract, invariant 4); the secret of the new
	// key only in what is saved.
	answer, _ := json.Marshal(r.res)
	shown := string(answer) + r.out + b.page
	if r.err != nil {
		shown += r.err.Error()
	}
	for _, s := range w.Secrets() {
		if strings.Contains(shown, s) {
			t.Fatalf("a secret of the world reached a channel of evo: %q", s)
		}
	}
	return r
}

func reasonOf(err error) string {
	if err == nil {
		return ""
	}
	return clierrors.Reason(err)
}

func TestLogin_Variants(t *testing.T) {
	cases := []struct {
		name     string
		v        loginworld.Variant
		reason   string
		accounts int // service accounts named evo left
		keys     int
		calls    []string // must (or, with "!", must not) be made; a trailing "/" is a prefix
	}{
		{"success", loginworld.Variant{}, "", 1, 1, []string{"POST /api/v2/serviceAccounts", "POST /api/v3/permissions"}},
		{"cancel at the provider", loginworld.Variant{Cancel: true}, clierrors.CodeUsage, 0, 0, []string{"!POST " + loginworld.IssuerPath + "/openid/token"}},
		{"expired code", loginworld.Variant{ExpiredCode: true}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"id token iss", loginworld.Variant{BadIDToken: "iss"}, clierrors.CodeAuthFailed, 0, 0, []string{"!GET /v3/customers"}},
		{"id token aud", loginworld.Variant{BadIDToken: "aud"}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"id token nonce", loginworld.Variant{BadIDToken: "nonce"}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"id token signature", loginworld.Variant{BadIDToken: "sig"}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"id token expired", loginworld.Variant{BadIDToken: "exp"}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"no right", loginworld.Variant{Denied: true}, clierrors.CodeAuthFailed, 0, 0, []string{"!POST /api/v2/serviceAccounts"}},
		{"account exists", loginworld.Variant{AccountExists: true}, "", 1, 1, []string{"!POST /api/v2/serviceAccounts"}},
		{"409 on the account", loginworld.Variant{Conflict409: true}, "", 1, 1, []string{"POST /api/v2/serviceAccounts"}},
		{"role shorter: PUT", loginworld.Variant{Perm: loginworld.PermShorter}, "", 1, 1, []string{"PUT /api/v3/permissions/", "!POST /api/v3/permissions"}},
		{"broken at the key after a PUT: the term stays", loginworld.Variant{Perm: loginworld.PermShorter, FailAt: loginworld.StepKey}, clierrors.CodeAPIError, 1, 0, []string{"!DELETE /api/v3/permissions/", "!DELETE /api/v2/serviceAccounts/"}},
		{"role longer: no write", loginworld.Variant{Perm: loginworld.PermLonger}, "", 1, 1, []string{"!PUT /api/v3/permissions/", "!POST /api/v3/permissions"}},
		{"403 on permissions", loginworld.Variant{PermForbidden: true}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"403 on the account", loginworld.Variant{AccountForbidden: true}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"role name collision", loginworld.Variant{RoleCollision: true}, "", 1, 1, []string{"GET /api/v1/roles"}},
		// No limit of IAM is known (acceptance C2.1): IAM's refusal at the
		// write is the answer, and what this login made is undone (R-44).
		// Acceptance B-5, 06.11: 8 accounts in the project, evo counted 16 —
		// the list of the project shows the organization's accounts too.
		{"accounts of the organization in the list", loginworld.Variant{OtherAccounts: 8, OrgAccounts: 8}, "", 1, 1, []string{"POST /api/v2/serviceAccounts", "POST /api/v3/permissions"}},
		{"account limit", loginworld.Variant{AccountLimit: true}, clierrors.CodeAPIError, 0, 0, []string{"POST /api/v2/serviceAccounts"}},
		{"key limit", loginworld.Variant{KeyLimit: true}, clierrors.CodeAPIError, 1, 0, []string{"POST /api/v3/permissions", "DELETE /api/v3/permissions/"}},
		{"broken at the account", loginworld.Variant{FailAt: loginworld.StepAccount}, clierrors.CodeAPIError, 0, 0, nil},
		{"broken at the permission", loginworld.Variant{FailAt: loginworld.StepPermission}, clierrors.CodeAPIError, 0, 0, nil},
		{"broken at the key", loginworld.Variant{FailAt: loginworld.StepKey}, clierrors.CodeAPIError, 0, 0, nil},
		{"broken at the key check", loginworld.Variant{FailAt: loginworld.StepKeyCheck}, clierrors.CodeAuthFailed, 0, 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, c.v)
			r := login(t, w, &browser{}, Choice{}, nil, nil)
			if got := reasonOf(r.err); got != c.reason {
				t.Fatalf("ended %q (%v), want %q", got, r.err, c.reason)
			}
			evo := 0
			for _, a := range w.Accounts() {
				if a.Name == "evo" {
					evo++
				}
			}
			if evo != c.accounts || len(w.Keys()) != c.keys {
				t.Errorf("left %d accounts evo and %d keys, want %d and %d", evo, len(w.Keys()), c.accounts, c.keys)
			}
			if c.reason == "" {
				if len(r.saved) != 1 || r.saved[0].Secret == "" || r.saved[0].AccessKeyID == "" || r.saved[0].ServiceAccountID == "" ||
					r.saved[0].ProjectID != loginworld.Project || r.res.KeyExpiresAt.IsZero() {
					t.Errorf("saved %+v, result %+v", r.saved, r.res)
				}
				p := w.Permissions()
				want := time.Now().Add(30 * 24 * time.Hour)
				if c.v.Perm == loginworld.PermLonger {
					want = time.Now().Add(365 * 24 * time.Hour)
				}
				if len(p) != 1 || p[0].ExpiresAt.Sub(want).Abs() > time.Minute {
					t.Errorf("permissions %+v, want one until %s", p, want)
				}
			} else if len(r.saved) != 0 {
				t.Errorf("a failed login saved %+v", r.saved)
			}
			calls := w.Calls()
			for _, wr := range c.calls {
				not := strings.HasPrefix(wr, "!")
				wr = strings.TrimPrefix(wr, "!")
				has := false
				for _, l := range calls {
					has = has || l == wr || (strings.HasSuffix(wr, "/") && strings.HasPrefix(l, wr))
				}
				if has == not {
					t.Errorf("call %q: made %v\n%s", wr, has, strings.Join(calls, "\n"))
				}
			}
		})
	}
}

// The profile is written at once after the key; when it cannot be, the key
// goes too (R-44): no key without a profile.
func TestLogin_ProfileFailsKeyGoes(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{}, Choice{}, nil, fmt.Errorf("disk full"))
	if r.err == nil || len(w.Keys()) != 0 || len(w.Permissions()) != 0 {
		t.Fatalf("err %v; left keys %d, permissions %d", r.err, len(w.Keys()), len(w.Permissions()))
	}
	if !strings.Contains(r.err.Error(), "disk full") {
		t.Errorf("the cause is not named: %v", r.err)
	}
}

// Cancel on the consent page: USAGE, nothing made; the page says so.
func TestLogin_CancelOnThePage(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{cancel: true}, Choice{}, nil, nil)
	if reasonOf(r.err) != clierrors.CodeUsage || len(w.Accounts()) != 0 || !strings.Contains(r.b.page, tCancelled()) {
		t.Fatalf("err %v; accounts %d; page %q", r.err, len(w.Accounts()), r.b.page)
	}
}

// A form the page did not offer is refused: another project, a term not
// among 7/30/90, another audience.
func TestLogin_ChoiceHeldToThePage(t *testing.T) {
	for name, f := range map[string]func(url.Values){
		"project":  func(f url.Values) { f.Set("project", "p-deleted") },
		"term":     func(f url.Values) { f.Set("ttl", "365") },
		"audience": func(f url.Values) { f.Set("audience", "root") },
	} {
		w := newWorld(t, loginworld.Variant{})
		r := login(t, w, &browser{form: f}, Choice{}, nil, nil)
		if reasonOf(r.err) != clierrors.CodeUsage || len(w.Accounts()) != 0 {
			t.Errorf("%s: err %v; accounts %d", name, r.err, len(w.Accounts()))
		}
	}
}

// For an agent: the account evo-agent.
func TestLogin_ForAnAgent(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{form: func(f url.Values) { f.Set("audience", "agent") }}, Choice{}, nil, nil)
	if r.err != nil || r.res.Account != "evo-agent" {
		t.Fatalf("err %v; result %+v", r.err, r.res)
	}
	found := false
	for _, a := range w.Accounts() {
		found = found || a.Name == "evo-agent"
	}
	if !found {
		t.Errorf("no account evo-agent: %+v", w.Accounts())
	}
}

// A second login replaces the key of the profile and deletes the previous
// one by its id (R-25).
func TestLogin_PreviousKeyRevokedByID(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	first := login(t, w, &browser{}, Choice{}, nil, nil)
	if first.err != nil {
		t.Fatal(first.err)
	}
	prev := &Previous{ServiceAccountID: first.saved[0].ServiceAccountID, AccessKeyID: first.saved[0].AccessKeyID, KeyID: first.saved[0].KeyID}
	second := login(t, w, &browser{}, Choice{}, prev, nil)
	if second.err != nil || second.res.RevokedKeyID != prev.KeyID {
		t.Fatalf("err %v; result %+v", second.err, second.res)
	}
	keys := w.Keys()
	if len(keys) != 1 || keys[0].ID != second.saved[0].AccessKeyID {
		t.Errorf("keys %+v, want only the new one", keys)
	}
}

// The terminal gets the address to open; a browser that does not open is
// no failure.
func TestLogin_AddressPrinted(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || !strings.Contains(r.out, w.Issuer+"/openid/auth?") || !strings.Contains(r.out, "code_challenge_method=S256") {
		t.Fatalf("err %v; out %q", r.err, r.out)
	}
}

// The production rule of the discovery: the issuer byte for byte, every
// endpoint on a trusted host, S256 offered.
func TestCheckDiscovery(t *testing.T) {
	good := Discovery{Issuer: auth.OIDCIssuer, AuthorizationEndpoint: auth.OIDCIssuer + "/openid/auth",
		TokenEndpoint: auth.OIDCTokenURL, JWKSURI: auth.OIDCIssuer + "/openid/certs", CodeChallengeMethods: []string{"plain", "S256"}}
	if err := CheckDiscovery(good, auth.OIDCIssuer, auth.TrustedOIDCURL); err != nil {
		t.Fatalf("a good document refused: %v", err)
	}
	for name, spoil := range map[string]func(d *Discovery){
		"issuer with a trailing slash": func(d *Discovery) { d.Issuer += "/" },
		"another issuer":               func(d *Discovery) { d.Issuer = "https://auth.iam.cloud.ru/auth/system" },
		"authorize elsewhere":          func(d *Discovery) { d.AuthorizationEndpoint = "https://example.com/auth" },
		"token over http":              func(d *Discovery) { d.TokenEndpoint = "http://id.cloud.ru/auth/system/openid/token" },
		"jwks elsewhere":               func(d *Discovery) { d.JWKSURI = "https://example.com/certs" },
		"no S256":                      func(d *Discovery) { d.CodeChallengeMethods = []string{"plain"} },
	} {
		d := good
		d.CodeChallengeMethods = append([]string(nil), good.CodeChallengeMethods...)
		spoil(&d)
		if err := CheckDiscovery(d, auth.OIDCIssuer, auth.TrustedOIDCURL); reasonOf(err) != clierrors.CodeAuthFailed {
			t.Errorf("%s: %v, want AUTH_FAILED", name, err)
		}
	}
}

// The loopback takes one callback of this login: another state and a
// second callback are 403 with no page (contract, invariant 3).
func TestLogin_ForeignStateAndSecondCallback(t *testing.T) {
	for _, v := range []loginworld.Variant{{ForeignState: true}, {TwiceCallback: true}} {
		w := newWorld(t, v)
		statuses := make(chan []int, 1)
		ctx, cancel := context.WithCancel(context.Background())
		var out bytes.Buffer
		cfg := worldConfig(w, &out, func(u string) error {
			go func() {
				s, _ := w.Browser(u)
				statuses <- s
				cancel() // no grant follows in this world's browser
			}()
			return nil
		})
		_, err := Login(ctx, cfg, Choice{}, &memStore{})
		s := <-statuses
		switch {
		case v.ForeignState && (len(s) != 2 || s[0] != http.StatusForbidden || s[1] != http.StatusOK):
			t.Errorf("foreign state: callbacks %v, want 403 then 200", s)
		case v.TwiceCallback && (len(s) != 2 || s[0] != http.StatusOK || s[1] != http.StatusForbidden):
			t.Errorf("twice: callbacks %v, want 200 then 403", s)
		}
		// No grant follows: the login ends interrupted, nothing made.
		if clierrors.ExitCode(err) != clierrors.ExitInterrupted || len(w.Accounts()) != 0 {
			t.Errorf("err %v (exit %d); accounts %d", err, clierrors.ExitCode(err), len(w.Accounts()))
		}
	}
}

// Every refusal names its step, and the HTTP status of the answer that made
// it (#981); a rollback names what it made, undid and left.
func TestLogin_RefusalNamesStepAndStatus(t *testing.T) {
	cases := []struct {
		v      loginworld.Variant
		step   string
		status int
	}{
		{loginworld.Variant{ExpiredCode: true}, stepToken, http.StatusBadRequest},
		{loginworld.Variant{BadIDToken: "nonce"}, stepIDToken, 0},
		{loginworld.Variant{Denied: true}, stepBulk, 0},
		{loginworld.Variant{AccountLimit: true}, stepAccount, http.StatusBadRequest},
		{loginworld.Variant{FailAt: loginworld.StepPermission}, stepPermission, http.StatusInternalServerError},
		{loginworld.Variant{FailAt: loginworld.StepKey}, stepKey, http.StatusInternalServerError},
	}
	for _, c := range cases {
		w := newWorld(t, c.v)
		r := login(t, w, &browser{}, Choice{}, nil, nil)
		var ce *clierrors.CLIError
		if !errors.As(r.err, &ce) || ce.Details["step"] != c.step {
			t.Errorf("%+v: %v, details %v; want the step %q", c.v, r.err, detailsOf(ce), c.step)
			continue
		}
		if got, _ := ce.Details["http_status"].(int); got != c.status {
			t.Errorf("%+v: http_status %v, want %d", c.v, ce.Details["http_status"], c.status)
		}
		if !strings.Contains(r.err.Error(), c.step) {
			t.Errorf("%+v: the text does not name the step: %v", c.v, r.err)
		}
		if c.v.FailAt != "" {
			if _, ok := ce.Details["undone"]; !ok {
				t.Errorf("%+v: no rollback in the details %v", c.v, ce.Details)
			}
		}
	}
}

func detailsOf(ce *clierrors.CLIError) map[string]any {
	if ce == nil {
		return nil
	}
	return ce.Details
}

// A terminal only gathers the choice before the browser: the summary
// first, then the address; the page offers the choice and grants.
func TestLogin_TerminalGathersThePageGrants(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	b := &browser{form: func(f url.Values) { f.Set("audience", "agent"); f.Set("ttl", "90") }}
	r := loginWith(t, w, b, Choice{}, nil, nil, []string{"", "90"})
	if r.err != nil || len(r.saved) != 1 || r.res.Profile != "agent" {
		t.Fatalf("err %v; saved %d; result %+v\n%s", r.err, len(r.saved), r.res, r.out)
	}
	if sum, open := strings.Index(r.out, tWhatWillBeDone()), strings.Index(r.out, tOpen()); sum < 0 || open < sum {
		t.Errorf("the summary is not before the address:\n%s", r.out)
	}
	if strings.Contains(r.out, "[y/N]") {
		t.Errorf("the terminal asks for the consent:\n%s", r.out)
	}
}

// The page offers the terminal's choice checked.
func TestLogin_PageOffersTheTerminalChoice(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	b := &browser{cancel: true}
	r := loginWith(t, w, b, Choice{}, nil, nil, []string{"2", "90"})
	if reasonOf(r.err) != clierrors.CodeUsage || len(w.Accounts()) != 0 {
		t.Fatalf("err %v; accounts %+v", r.err, w.Accounts())
	}
	if !strings.Contains(b.consent, `value="agent" checked`) || !strings.Contains(b.consent, `value="90" checked`) {
		t.Errorf("the page does not offer the terminal's choice")
	}
}

// R-47: «Grant access» takes the page's consent token, not the state the
// terminal shows; the page is served once.
func TestLogin_GrantTakesTheConsentToken(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{noToken: true, again: true}, Choice{}, nil, nil)
	if reasonOf(r.err) != clierrors.CodeUsage || len(w.Accounts()) != 0 || len(w.Keys()) != 0 {
		t.Fatalf("err %v; accounts %+v; keys %+v", r.err, w.Accounts(), w.Keys())
	}
	// The page, the second GET, the grant without the token, the cancel.
	if len(r.b.status) != 4 || r.b.status[1] != http.StatusForbidden || r.b.status[2] != http.StatusForbidden || r.b.status[3] != http.StatusOK {
		t.Errorf("statuses %v, want [200 403 403 200]", r.b.status)
	}
	if tok := consentField.FindStringSubmatch(r.b.consent); tok == nil || strings.Contains(r.out, tok[1]) {
		t.Errorf("the consent token is missing from the page or printed: %v", tok)
	}
}

// Production listens on the registered ports only.
func TestLogin_RegisteredPorts(t *testing.T) {
	if got := Default(nil, nil).Ports; !slices.Equal(got, auth.LoginRedirectPorts[:]) {
		t.Errorf("ports %v, want %v", got, auth.LoginRedirectPorts)
	}
}

// Without a terminal the choice is the page's.
func TestLogin_NoTerminalIsThePage(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{}, Choice{For: ForMe}, nil, nil)
	if r.err != nil || !strings.Contains(r.b.page, tDone()) {
		t.Fatalf("err %v; page %q", r.err, r.b.page)
	}
}

// U-9: an error of evaluate is no refusal: the login goes on, the terminal
// and the page say the right is not checked, and the platform's answer to
// the first write decides.
func TestLogin_EvaluateErrorIsNoRefusal(t *testing.T) {
	w := newWorld(t, loginworld.Variant{EvaluateUnknown: true})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || len(r.saved) != 1 {
		t.Fatalf("err %v; saved %d", r.err, len(r.saved))
	}
	note := tUnchecked("")
	note = note[:strings.Index(note, ":")]
	if !strings.Contains(r.out, note) || !strings.Contains(r.b.consent, note) {
		t.Errorf("the note %q is not in the terminal or on the page:\n%s", note, r.out)
	}
	// A refusal of the platform at the first write creates nothing.
	w = newWorld(t, loginworld.Variant{EvaluateUnknown: true, PermForbidden: true})
	r = login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err == nil || len(w.Keys()) != 0 || len(w.Permissions()) != 0 {
		t.Fatalf("err %v; keys %+v; permissions %+v", r.err, w.Keys(), w.Permissions())
	}
}

// R-49 («Риск», A1 05.11: the actions evo asks evaluate about are not in
// IAM's catalogue, 2 of 2): when evaluate answers false — not an error —
// for an action the catalogue lacks, «false» means «no such action», not
// «no right». The administrator of the project is not refused: the login
// goes on as unchecked (U-9), as on an error of evaluate. A person without
// the right is still refused (Variant Denied, «no right»).
func TestLogin_UnknownActionIsNoRefusal(t *testing.T) {
	w := newWorld(t, loginworld.Variant{UnknownActionFalse: true})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || len(r.saved) != 1 {
		t.Fatalf("the administrator was refused for an action IAM does not know: err %v; saved %d\n%s", r.err, len(r.saved), r.out)
	}
	note := tUnchecked("")
	note = note[:strings.Index(note, ":")]
	if !strings.Contains(r.out, note) || !strings.Contains(r.b.consent, note) {
		t.Errorf("the note %q is not in the terminal or on the page:\n%s", note, r.out)
	}
}

// R-48 on the world: the terminal names the preselected project and its
// source, and the page offers it checked.
func TestLogin_PreselectNamedInTheTerminal(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{}, Choice{Preselect: loginworld.Project, PreselectFrom: "из CLOUDRU_PROJECT_ID", PreselectEnv: true}, nil, nil)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !strings.Contains(r.out, "из CLOUDRU_PROJECT_ID") || !strings.Contains(r.b.consent, tEnvProjectPicked()) {
		t.Errorf("terminal:\n%s", r.out)
	}
}

// R-49: the catalog of actions is read only after a false of evaluate; a
// false on an action the catalog names is «no right» before any write; a
// catalog evo cannot read makes the false «not confirmed»; a 403 at the
// first write is AUTH_FAILED with its step, nothing left.
func TestLogin_FalseReadsTheCatalog(t *testing.T) {
	readsCatalog := func(calls []string) bool { return slices.Contains(calls, "GET /api/v1/actions") }

	w := newWorld(t, loginworld.Variant{})
	if r := login(t, w, &browser{}, Choice{}, nil, nil); r.err != nil || readsCatalog(w.Calls()) {
		t.Errorf("an allowed login: err %v; the catalog read %v", r.err, readsCatalog(w.Calls()))
	}

	w = newWorld(t, loginworld.Variant{Denied: true})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if reasonOf(r.err) != clierrors.CodeAuthFailed || !readsCatalog(w.Calls()) || len(w.Accounts()) != 0 {
		t.Errorf("a false on a known action: %v; catalog %v; accounts %d", r.err, readsCatalog(w.Calls()), len(w.Accounts()))
	}

	w = newWorld(t, loginworld.Variant{UnknownActionFalse: true})
	r = login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || !strings.Contains(r.out, tActionUnknown(auth.LoginActionCreateAccount)) {
		t.Errorf("a false on an unknown action: %v\n%s", r.err, r.out)
	}

	w = newWorld(t, loginworld.Variant{UnknownActionFalse: true, CatalogFails: true})
	r = login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || !strings.Contains(r.out, "каталог действий IAM не прочитан") && !strings.Contains(r.out, "catalog of actions was not read") {
		t.Errorf("a catalog not read: %v\n%s", r.err, r.out)
	}

	w = newWorld(t, loginworld.Variant{UnknownActionFalse: true, AccountForbidden: true})
	r = login(t, w, &browser{}, Choice{}, nil, nil)
	var ce *clierrors.CLIError
	if !errors.As(r.err, &ce) || ce.Code != clierrors.CodeAuthFailed || ce.Details["step"] != stepAccount || len(w.Accounts()) != 0 {
		t.Errorf("a 403 at the first write: %v %v; accounts %d", r.err, detailsOf(ce), len(w.Accounts()))
	}
}

// D34, variant A: before the browser the terminal says once that the
// Evolution CMCP page opens.
func TestLogin_SaysThePageName(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || strings.Count(r.out, tOpenPage()) != 1 || strings.Index(r.out, tOpenPage()) > strings.Index(r.out, tOpen()) {
		t.Errorf("err %v:\n%s", r.err, r.out)
	}
}

// IAM-10 (acceptance B-5, 06.11, a blocker of 1.8): past its items the
// Organization API answers empty pages, each with a new token. orgList read
// them to the limit of pages and refused every person with «too many
// pages»; an empty page is the end of the list.
func TestLogin_EmptyPagesWithATokenEndTheList(t *testing.T) {
	w := newWorld(t, loginworld.Variant{EmptyPagesWithToken: true})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || len(r.saved) != 1 {
		t.Fatalf("login with empty pages after the items: err %v; saved %d\n%s", r.err, len(r.saved), r.out)
	}
}

// «Риск» to IAM-10 (the blocker of B-5): a person of no organization gets
// the empty first page of /v3/customers with a token, as past the last
// item. Its first empty page ends the list: login reads it once and
// refuses as for no project — USAGE, before any write — not «too many
// pages» and not a wait for more pages.
func TestLogin_NoCustomersFirstPageWithToken(t *testing.T) {
	w := newWorld(t, loginworld.Variant{NoCustomersWithToken: true})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if reasonOf(r.err) != clierrors.CodeUsage || len(r.saved) != 0 || len(w.Accounts()) != 0 || len(w.Keys()) != 0 {
		t.Fatalf("ended %v (%s); saved %d, accounts %d, keys %d — want USAGE before any write", r.err, reasonOf(r.err), len(r.saved), len(w.Accounts()), len(w.Keys()))
	}
	n := 0
	for _, c := range w.Calls() {
		if strings.HasPrefix(c, "GET /v3/customers") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("/v3/customers read %d times; its empty first page ends the list", n)
	}
}

// Acceptance C2.1 (06.11, a defect of 1.8): evo login refused a project of
// ten or more service accounts — «the limit of service accounts (20)» —
// before any write, while IAM created the account there (20, then 18
// accounts live). No limit of IAM is known: the login asks IAM, and its
// refusal is the answer.
func TestLogin_ManyAccountsInTheProjectIsNoLimit(t *testing.T) {
	w := newWorld(t, loginworld.Variant{OtherAccounts: 20})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || len(r.saved) != 1 {
		t.Fatalf("a project of 20 accounts: err %v; saved %d\n%s", r.err, len(r.saved), r.out)
	}
}
