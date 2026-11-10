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
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
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
var checkedProject = regexp.MustCompile(`<option id="p\d+" value="([^"]+)" selected>`)
var firstProject = regexp.MustCompile(`<option id="p\d+" value="([^"]+)">`)

// pickProject is the project a person sends from the consent page: the one
// picked beforehand, or the first the list lets choose.
func pickProject(page string) string {
	for _, re := range []*regexp.Regexp{checkedProject, firstProject} {
		if p := re.FindStringSubmatch(page); p != nil {
			return p[1]
		}
	}
	return ""
}

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
		if p := pickProject(b.page); p != "" {
			f.Set("project", p)
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
	// The lock of the one login: the world's own, shared by the logins of
	// one world (TestLogin_SecondLoginRefusedWhileOneWaits).
	c.LockPath = filepath.Join(os.TempDir(), fmt.Sprintf("evo-login-%p.lock", w))
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

// login runs evo login on the world with the browser b.
func login(t *testing.T, w *loginworld.World, b *browser, ch Choice, prev *Previous, saveErr error) run {
	t.Helper()
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	r := run{b: b}
	st := &memStore{prev: prev, saveErr: saveErr}
	cfg := worldConfig(w, &out, b.open)
	cfg.Tell = func(line string) { fmt.Fprintln(&out, line) }
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
		// R-55 («Риск», conclusion 33): the term moved forward for this key
		// is moved back when the key is not made — not deleted, it was there.
		{"broken at the key after a PUT: the term goes back", loginworld.Variant{Perm: loginworld.PermShorter, FailAt: loginworld.StepKey}, clierrors.CodeAPIError, 1, 0, []string{"!DELETE /api/v3/permissions/", "!DELETE /api/v2/serviceAccounts/"}},
		{"role longer: no write", loginworld.Variant{Perm: loginworld.PermLonger}, "", 1, 1, []string{"!PUT /api/v3/permissions/", "!POST /api/v3/permissions"}},
		{"403 on permissions", loginworld.Variant{PermForbidden: true}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"403 on the account", loginworld.Variant{AccountForbidden: true}, clierrors.CodeAuthFailed, 0, 0, nil},
		{"role name collision", loginworld.Variant{RoleCollision: true}, "", 1, 1, []string{"GET /api/v1/roles"}},
		// Acceptance B-5, 06.11: a project of 20 accounts, where IAM creates
		// one more; evo refused before any write by a limit of its own.
		{"many accounts, IAM creates one more", loginworld.Variant{OtherAccounts: 20}, "", 1, 1, []string{"POST /api/v2/serviceAccounts", "POST /api/v3/permissions"}},
		// Acceptance B-5, 06.11: 8 accounts in the project, evo counted 16 —
		// the list of the project shows the organization's accounts too.
		{"accounts of the organization are not the project's", loginworld.Variant{OtherAccounts: 8, OrgAccounts: 8}, "", 1, 1, []string{"POST /api/v2/serviceAccounts", "POST /api/v3/permissions"}},
		// No limit of IAM is known: IAM's refusal at the write is the
		// answer, and what this login made is undone (R-44, R-55).
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

// The page grants what it chooses: the terminal asks nothing, and the
// consent is the page's button, not a [y/N] (the owner's decision of 06.11).
func TestLogin_PageGrantsWhatItChooses(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	b := &browser{form: func(f url.Values) { f.Set("audience", "agent"); f.Set("ttl", "90") }}
	r := login(t, w, b, Choice{}, nil, nil)
	if r.err != nil || len(r.saved) != 1 || r.res.Profile != "agent" {
		t.Fatalf("err %v; saved %d; result %+v\n%s", r.err, len(r.saved), r.res, r.out)
	}
	if strings.Contains(r.out, "[y/N]") {
		t.Errorf("the terminal asks for the consent:\n%s", r.out)
	}
}

// The page offers the flags' choice checked («Риск», addition to
// conclusion 34: the default «Мне» does not override --for).
func TestLogin_PageOffersTheFlagsChoice(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	b := &browser{cancel: true}
	r := login(t, w, b, Choice{For: ForAgent, Days: 90}, nil, nil)
	if reasonOf(r.err) != clierrors.CodeUsage || len(w.Accounts()) != 0 {
		t.Fatalf("err %v; accounts %+v", r.err, w.Accounts())
	}
	if !strings.Contains(b.consent, `value="agent" checked`) || !strings.Contains(b.consent, `value="90" checked`) {
		t.Errorf("the page does not offer the flags' choice")
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
	note := tUnchecked()
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
	note := tUnchecked()
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
	// The reason is the answer's, not the terminal's (conclusion 35.4).
	if r.err != nil || !strings.Contains(r.res.Unchecked, tActionUnknown(auth.LoginActionCreateAccount)) || strings.Contains(r.out, tActionUnknown(auth.LoginActionCreateAccount)) {
		t.Errorf("a false on an unknown action: %v\n%s", r.err, r.out)
	}

	w = newWorld(t, loginworld.Variant{UnknownActionFalse: true, CatalogFails: true})
	r = login(t, w, &browser{}, Choice{}, nil, nil)
	if r.err != nil || !strings.Contains(r.res.Unchecked, "каталог действий IAM не прочитан") && !strings.Contains(r.res.Unchecked, "catalog of actions was not read") {
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

// R-55 («Риск», conclusion 33): a login that moved the role's term forward
// and was refused the key — a limit of the platform or any other refusal —
// moves the term back: the account is left with the role it had, not with
// a longer one and no key.
func TestLogin_KeyRefusedAfterThePUTMovesTheTermBack(t *testing.T) {
	for name, v := range map[string]loginworld.Variant{
		"key broken":  {Perm: loginworld.PermShorter, FailAt: loginworld.StepKey},
		"key refused": {Perm: loginworld.PermShorter, KeyLimit: true},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, v)
			before := w.Permissions()
			r := login(t, w, &browser{}, Choice{}, nil, nil)
			if r.err == nil {
				t.Fatal("the login went through")
			}
			after := w.Permissions()
			// The term travels as RFC 3339: seconds are what comes back.
			if len(before) != 1 || len(after) != 1 || !after[0].ExpiresAt.Equal(before[0].ExpiresAt.Truncate(time.Second)) {
				t.Errorf("the role's term: before %v, after %v — want it moved back", before, after)
			}
		})
	}
}

// A refusal of the platform at a write (R-55): 403 is AUTH_FAILED at the
// key as at the account; another 4xx keeps the platform's own words and
// does not ask to retry what will be refused again.
func TestLogin_RefusalAtTheWriteSaysWhatToDo(t *testing.T) {
	for _, c := range []struct {
		name string
		v    loginworld.Variant
		code string
		says string
	}{
		{"account 403", loginworld.Variant{AccountForbidden: true}, clierrors.CodeAuthFailed, ""},
		{"key 403", loginworld.Variant{KeyForbidden: true}, clierrors.CodeAuthFailed, ""},
		{"account 400", loginworld.Variant{AccountLimit: true}, clierrors.CodeAPIError, "service account limit reached"},
		{"key 400", loginworld.Variant{KeyLimit: true}, clierrors.CodeAPIError, "access key limit reached"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := login(t, newWorld(t, c.v), &browser{}, Choice{}, nil, nil)
			var ce *clierrors.CLIError
			if !errors.As(r.err, &ce) || ce.Code != c.code {
				t.Fatalf("got %v, want %s", r.err, c.code)
			}
			if !strings.Contains(ce.Message, c.says) {
				t.Errorf("message %q lost the platform's words %q", ce.Message, c.says)
			}
			if ce.Hint == eRetry() {
				t.Errorf("hint %q asks to retry a refusal", ce.Hint)
			}
			// R-56: the owner, told to delete an unused account, nearly
			// deleted the service accounts of Kubernetes clusters.
			if regexp.MustCompile(`(?i)delet|remov|удал`).MatchString(ce.Hint) {
				t.Errorf("hint %q advises a delete of what evo did not make", ce.Hint)
			}
		})
	}
}

// A grant without a project — what required keeps the browser from
// sending — is refused by evo before any write (plan 1.8, #1084).
func TestLogin_GrantWithoutAProjectIsRefused(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	b := &browser{form: func(f url.Values) { f.Set("project", "") }}
	r := login(t, w, b, Choice{}, nil, nil)
	if reasonOf(r.err) != clierrors.CodeUsage || len(w.Accounts()) != 0 || len(w.Keys()) != 0 {
		t.Fatalf("err %v; accounts %+v; keys %+v", r.err, w.Accounts(), w.Keys())
	}
}

// Plan 1.8, #1087, texts (audit of 06.11; «Риск», conclusion 35.3): a
// refusal at a write says first, in plain words, what Cloud.ru did not do,
// and the platform's words follow in the same line; «a retry alone meets
// the same refusal» is said only of a 4xx.
func TestLogin_RefusalSaysWhatWasNotDoneFirst(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	for _, c := range []struct {
		name, first, says string
		v                 loginworld.Variant
		retry             bool
	}{
		{"account 400", "Cloud.ru не создал сервисный аккаунт", "service account limit reached", loginworld.Variant{AccountLimit: true}, false},
		{"account 500", "Cloud.ru не создал сервисный аккаунт", "internal", loginworld.Variant{FailAt: loginworld.StepAccount}, true},
		{"role 500", "Cloud.ru не выдал роль «Администратор проекта»", "internal", loginworld.Variant{FailAt: loginworld.StepPermission}, true},
		{"key 400", "Cloud.ru не выдал ключ доступа", "access key limit reached", loginworld.Variant{KeyLimit: true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := login(t, newWorld(t, c.v), &browser{}, Choice{}, nil, nil)
			var ce *clierrors.CLIError
			if !errors.As(r.err, &ce) {
				t.Fatalf("got %v", r.err)
			}
			why := ce.Message
			if i := strings.Index(why, ": "); strings.HasPrefix(why, "доступ не выдан") && i > 0 {
				why = why[i+2:]
			}
			if !strings.HasPrefix(why, c.first+": ") || !strings.Contains(why, c.says) {
				t.Errorf("message %q: want %q first, the platform's %q after", ce.Message, c.first, c.says)
			}
			if got := strings.Contains(ce.Hint, "повтор получит тот же отказ"); got == c.retry {
				t.Errorf("hint %q: «the same refusal» said of a %s", ce.Hint, c.name)
			}
		})
	}
}

// «Риск», conclusion 35.4: the note of a right evaluate did not confirm
// carries no code of the platform; its reason is in the details.
func TestLogin_UncheckedNoteHasNoPlatformCode(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	w := newWorld(t, loginworld.Variant{EvaluateUnknown: true, PermForbidden: true})
	r := login(t, w, &browser{}, Choice{}, nil, nil)
	want := "Заранее проверить право не удалось. Если доступа нет, Cloud.ru откажет при создании аккаунта, и ничего не останется"
	if !strings.Contains(r.out, want) || !strings.Contains(r.b.consent, want) {
		t.Errorf("no plain note in the terminal or on the page:\n%s", r.out)
	}
	for _, code := range []string{"iam API", "evaluate"} {
		if strings.Contains(r.out, code) || strings.Contains(tag.ReplaceAllString(r.b.consent, " "), code) {
			t.Errorf("the note shows %q", code)
		}
	}
	var ce *clierrors.CLIError
	errors.As(r.err, &ce)
	if d := detailsOf(ce); d == nil || d["unchecked"] == nil {
		t.Errorf("the reason is not in the details: %v", d)
	}
}

// Plan 1.8, #1087 (audit of 06.11): the end of the wait is not a Ctrl-C —
// its own code LOGIN_TIMEOUT, exit 1, «not completed within N min», nothing
// made; a Ctrl-C stays INTERRUPTED, 130.
func TestLogin_TimeoutIsNotAnInterrupt(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	var out bytes.Buffer
	cfg := worldConfig(w, &out, func(string) error { return nil }) // the person never comes
	cfg.Wait = 50 * time.Millisecond                               // wall-clock: a share of the production wait, to end the test
	_, err := Login(context.Background(), cfg, Choice{}, &memStore{})
	if clierrors.Reason(err) != clierrors.CodeLoginTimeout || clierrors.ExitCode(err) != clierrors.ExitGeneral || len(w.Accounts()) != 0 {
		t.Fatalf("err %v (reason %s, exit %d); accounts %d", err, clierrors.Reason(err), clierrors.ExitCode(err), len(w.Accounts()))
	}
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Hint == "" || !strings.Contains(ce.Hint, "evo login") {
		t.Errorf("no hint to run evo login again: %v", err)
	}
}

// After the wait the loopback is gone — a reload of the page meets no
// server — and no page of evo was kept by the browser: every answer is
// no-store (plan 1.8, #1087: after F5 the tab showed the form again).
func TestLogin_AfterTheWaitThePageIsGone(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	var out bytes.Buffer
	pageURL := make(chan string, 1)
	cfg := worldConfig(w, &out, func(u string) error {
		resp, err := http.Get(u) // the person signs in and sees the consent page
		if err == nil {
			if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
				t.Errorf("the consent page is %q, want no-store", cc)
			}
			pageURL <- resp.Request.URL.String()
			resp.Body.Close()
		}
		return nil
	})
	cfg.Wait = 200 * time.Millisecond // wall-clock: a share of the production wait, to end the test
	if _, err := Login(context.Background(), cfg, Choice{}, &memStore{}); clierrors.Reason(err) != clierrors.CodeLoginTimeout {
		t.Fatalf("err %v, want LOGIN_TIMEOUT", err)
	}
	u := <-pageURL
	if resp, err := http.Get(u); err == nil {
		resp.Body.Close()
		t.Errorf("after the wait %s still answers %d", u, resp.StatusCode)
	}
	// «Риск», conclusion 35.2: a grant after the wait writes nothing.
	pu, _ := url.Parse(u)
	if resp, err := http.PostForm(pu.Scheme+"://"+pu.Host+"/grant", url.Values{"project": {"p"}}); err == nil {
		resp.Body.Close()
		t.Errorf("after the wait /grant answers %d", resp.StatusCode)
	}
	if len(w.Accounts()) != 0 || len(w.Keys()) != 0 {
		t.Errorf("after the wait something was made")
	}
}

// Plan 1.8, #1095, rounds 1–2 (D3): a Ctrl-C while the grant is under way
// does not end the login as «interrupted» while the key goes on being made
// and saved: the login waits for the grant and answers its result.
func TestLogin_InterruptDuringTheGrantAnswersItsResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newWorld(t, loginworld.Variant{BeforeKey: cancel})
	b := &browser{}
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	st := &memStore{}
	res, err := Login(ctx, worldConfig(w, &out, b.open), Choice{}, st)
	<-b.ended
	if err != nil || len(st.saved) != 1 || res.KeyID == "" {
		t.Fatalf("a Ctrl-C in the grant: err %v; saved %d; key %q", err, len(st.saved), res.KeyID)
	}
}

// Plan 1.8, #1095, round 4 (б): a second «Grant access» while the first
// is under way (a double click) makes nothing twice and does not break the
// login: it shows the same result.
func TestLogin_SecondGrantShowsTheSameResult(t *testing.T) {
	var form url.Values
	var host string
	second := make(chan string, 1)
	v := loginworld.Variant{}
	v.BeforeKey = func() {
		go func() {
			resp, err := http.PostForm("http://"+host+"/grant", form)
			if err != nil {
				second <- "error: " + err.Error()
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			second <- fmt.Sprintf("%d %s", resp.StatusCode, body)
		}()
		time.Sleep(50 * time.Millisecond) // wall-clock: lets the second POST reach evo while the first grants
	}
	w := newWorld(t, v)
	b := &browser{form: func(f url.Values) { form = f }}
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	st := &memStore{}
	cfg := worldConfig(w, &out, func(u string) error {
		if p, err := url.Parse(u); err == nil {
			if cb, err := url.Parse(p.Query().Get("redirect_uri")); err == nil {
				host = cb.Host
			}
		}
		return b.open(u)
	})
	_, err := Login(context.Background(), cfg, Choice{}, st)
	<-b.ended
	got := <-second
	if err != nil || len(st.saved) != 1 || len(w.Keys()) != 1 {
		t.Fatalf("err %v; saved %d; keys %d", err, len(st.saved), len(w.Keys()))
	}
	if !strings.HasPrefix(got, "200 ") || !strings.Contains(got, tDone()) {
		t.Errorf("the second «Grant access» shows %.200q, want the same result", got)
	}
}

// refusingStore refuses the person's key, as the agent's profile does (C24).
type refusingStore struct{ memStore }

func (refusingStore) Refuses(a Audience) error {
	if a == ForMe {
		return clierrors.New(clierrors.CodeUsage, "the agent's profile", "")
	}
	return nil
}

// Plan 1.8, #1095 (C24): «Мне» chosen on the page for the agent's profile
// is refused before any write.
func TestLogin_StoreRefusesTheAudienceBeforeAnyWrite(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	b := &browser{}
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	st := &refusingStore{}
	_, err := Login(context.Background(), worldConfig(w, &out, b.open), Choice{}, st)
	<-b.ended
	if reasonOf(err) != clierrors.CodeUsage || len(w.Accounts()) != 0 || len(st.saved) != 0 {
		t.Fatalf("err %v; accounts %d; saved %d", err, len(w.Accounts()), len(st.saved))
	}
}

// «Риск», conclusion 39 (plan 1.8, #1095, round 4): the browser drops the
// connection of the first «Grant access» (a double click, F5) while the
// grant is under way. The grant goes on to its end — one key, nothing
// left — and the second «Grant access» shows its result.
func TestLogin_DroppedFirstGrantStillEnds(t *testing.T) {
	var form url.Values
	var host string
	dropFirst := make(chan struct{})
	second := make(chan string, 1)
	v := loginworld.Variant{}
	v.BeforeKey = func() {
		close(dropFirst) // the browser drops the first POST
		// The second POST goes out now, while the key is not made yet: it
		// waits for the grant under way and must see its result. Started
		// after the sleep, under load it came after the loopback closed.
		defer time.Sleep(200 * time.Millisecond) // wall-clock: the dropped connection and the second POST reach the server before the key
		go func() {
			resp, err := http.PostForm("http://"+host+"/grant", form)
			if err != nil {
				second <- "error: " + err.Error()
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			second <- fmt.Sprintf("%d %s", resp.StatusCode, body)
		}()
	}
	w := newWorld(t, v)
	var out bytes.Buffer
	st := &memStore{}
	ended := make(chan struct{})
	cfg := worldConfig(w, &out, func(authURL string) error {
		p, _ := url.Parse(authURL)
		cb, _ := url.Parse(p.Query().Get("redirect_uri"))
		host = cb.Host
		go func() {
			defer close(ended)
			c := w.Client()
			resp, err := c.Get(authURL)
			if err != nil {
				return
			}
			resp.Body.Close()
			resp, err = c.Get(resp.Header.Get("Location"))
			if err != nil {
				return
			}
			page, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			form = url.Values{"state": {stateField.FindStringSubmatch(string(page))[1]},
				"consent": {consentField.FindStringSubmatch(string(page))[1]},
				"project": {pickProject(string(page))}, "audience": {"me"}, "ttl": {"30"}}
			ctx, cancel := context.WithCancel(context.Background())
			go func() { <-dropFirst; cancel() }()
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+host+"/grant", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if r, err := http.DefaultClient.Do(req); err == nil {
				r.Body.Close()
			}
		}()
		return nil
	})
	_, err := Login(context.Background(), cfg, Choice{}, st)
	<-ended
	got := <-second
	if err != nil || len(st.saved) != 1 || len(w.Keys()) != 1 {
		t.Fatalf("the first POST dropped: err %v; saved %d; keys %d", err, len(st.saved), len(w.Keys()))
	}
	if !strings.HasPrefix(got, "200 ") || !strings.Contains(got, tDone()) {
		t.Errorf("the second «Grant access» shows %.200q, want the result", got)
	}
}
