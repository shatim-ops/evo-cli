// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	page   string           // the last page evo served
	status []int
	ended  chan struct{} // closed when the browser is done
}

var stateField = regexp.MustCompile(`name="state" value="([^"]+)"`)
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
		m := stateField.FindStringSubmatch(b.page)
		if m == nil {
			return // no consent: the login ended at the callback
		}
		f := url.Values{"state": {m[1]}, "audience": {"me"}, "ttl": {"30"}}
		if p := checkedProject.FindStringSubmatch(b.page); p != nil {
			f.Set("project", p[1])
		}
		if b.form != nil {
			b.form(f)
		}
		u, _ := url.Parse(cb)
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
	c := Default(out, open)
	c.Issuer, c.OrgBase, c.AdminBase, c.KeyToken = w.Issuer, w.URL, w.URL, w.URL+"/api/v1/auth/token"
	// The world is plain http on 127.0.0.1: its own address is the one
	// trusted; anything else is not (CheckDiscovery is held to the
	// production rule in TestCheckDiscovery).
	c.Trusted = func(u string) bool { return strings.HasPrefix(u, w.URL+"/") }
	c.HTTP = w.Client()
	c.Wait = 20 * time.Second
	return c
}

type run struct {
	res   Result
	err   error
	saved []Saved
	out   string
	b     *browser
}

func login(t *testing.T, w *loginworld.World, b *browser, ch Choice, prev *Previous, saveErr error) run {
	t.Helper()
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	r := run{b: b}
	res, err := Login(context.Background(), worldConfig(w, &out, b.open), ch, prev, func(s Saved) error {
		if saveErr != nil {
			return saveErr
		}
		r.saved = append(r.saved, s)
		return nil
	})
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
		{"403 on permissions", loginworld.Variant{PermForbidden: true}, clierrors.CodeAPIError, 0, 0, nil},
		{"role name collision", loginworld.Variant{RoleCollision: true}, "", 1, 1, []string{"GET /api/v1/roles"}},
		{"account limit", loginworld.Variant{AccountLimit: true}, clierrors.CodeUsage, 0, 0, []string{"!POST /api/v2/serviceAccounts"}},
		{"key limit", loginworld.Variant{KeyLimit: true}, clierrors.CodeUsage, 1, 0, []string{"!POST /api/v2/serviceAccounts/", "!POST /api/v3/permissions"}},
		{"broken at the account", loginworld.Variant{FailAt: loginworld.StepAccount}, clierrors.CodeAPIError, 0, 0, nil},
		{"broken at the permission", loginworld.Variant{FailAt: loginworld.StepPermission}, clierrors.CodeAPIError, 0, 0, nil},
		{"broken at the key", loginworld.Variant{FailAt: loginworld.StepKey}, clierrors.CodeAPIError, 0, 0, nil},
		{"broken at the key check", loginworld.Variant{FailAt: loginworld.StepKeyCheck}, clierrors.CodeAuthFailed, 0, 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := loginworld.New(t, c.v)
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
	w := loginworld.New(t, loginworld.Variant{})
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
	w := loginworld.New(t, loginworld.Variant{})
	r := login(t, w, &browser{cancel: true}, Choice{}, nil, nil)
	if reasonOf(r.err) != clierrors.CodeUsage || len(w.Accounts()) != 0 || !strings.Contains(r.b.page, "cancelled") && !strings.Contains(r.b.page, "отменён") {
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
		w := loginworld.New(t, loginworld.Variant{})
		r := login(t, w, &browser{form: f}, Choice{}, nil, nil)
		if reasonOf(r.err) != clierrors.CodeUsage || len(w.Accounts()) != 0 {
			t.Errorf("%s: err %v; accounts %d", name, r.err, len(w.Accounts()))
		}
	}
}

// For an agent: the account evo-agent.
func TestLogin_ForAnAgent(t *testing.T) {
	w := loginworld.New(t, loginworld.Variant{})
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
	w := loginworld.New(t, loginworld.Variant{})
	first := login(t, w, &browser{}, Choice{}, nil, nil)
	if first.err != nil {
		t.Fatal(first.err)
	}
	prev := &Previous{ServiceAccountID: first.saved[0].ServiceAccountID, AccessKeyID: first.saved[0].AccessKeyID}
	second := login(t, w, &browser{}, Choice{}, prev, nil)
	if second.err != nil || !second.res.PreviousRevoked {
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
	w := loginworld.New(t, loginworld.Variant{})
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
		w := loginworld.New(t, v)
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
		_, err := Login(ctx, cfg, Choice{}, nil, func(Saved) error { return nil })
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
