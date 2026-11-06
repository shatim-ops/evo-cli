// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package loginworld is the parity world of evo login (release-plan 1.8,
// B-2): fakes of the Cloud.ru OIDC provider, Organization API and
// Administration API on one httptest server, with no network, after the
// contract B-1 («Fable — авторизация», 05.11, section 6). Only tests import
// it (scripts/fake-servers.json, TestFakeServers_OnlyTestsImport).
//
// A test points evo's discovery at Issuer and its Organization and
// Administration bases at URL, drives the browser with Browser, and reads
// what happened back: Calls, the objects left in the project (Accounts,
// Permissions, Keys) and Secrets — every code, token and key secret the
// world handed out, which no channel of evo may show (invariant 4).
package loginworld

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
)

// Constants of the contract B-1, section 1, from the login table of evo
// (internal/auth, A0-4): the world checks evo against the same values.
const (
	ClientID = auth.LoginClientID
	Scope    = auth.LoginScope
	RoleName = auth.LoginRoleName
	// Customer and Project are the organization and project the human sees.
	Customer = "c0000000-0000-0000-0000-000000000001"
	Project  = "p0000000-0000-0000-0000-000000000001"
	// UserID is the human's id in the ID token (sub) and in evaluate.
	UserID = "u0000000-0000-0000-0000-000000000001"
)

// IssuerPath is the path of the issuer, served on the world's host.
var IssuerPath = func() string {
	u, err := url.Parse(auth.OIDCIssuer)
	if err != nil {
		panic(err)
	}
	return u.Path
}()

// RedirectURIs are the three registered redirect_uri of evo_cmcp.
var RedirectURIs = func() []string {
	var out []string
	for _, p := range auth.LoginRedirectPorts {
		out = append(out, fmt.Sprintf("http://127.0.0.1:%d/callback", p))
	}
	return out
}()

// Step names a write of the flow a Variant can break (contract, steps 14–18).
type Step string

// The steps after which the flow creates something.
const (
	StepAccount    Step = "account"    // POST serviceAccounts (14)
	StepPermission Step = "permission" // POST or PUT permissions (15)
	StepKey        Step = "key"        // POST accessKeys (16)
	StepKeyCheck   Step = "key-check"  // POST auth/token with the new key (17)
)

// Perm is what the project holds for the service account before the flow.
type Perm int

// The three cases of step 15.
const (
	PermNone    Perm = iota // no permission: POST
	PermShorter             // expires before the new key: PUT
	PermLonger              // expires after the new key: no write
)

// Variant is a world of the contract, section 6.
type Variant struct {
	Cancel        bool   // the human cancels: callback with error=access_denied
	ForeignState  bool   // the browser first calls back with another state
	TwiceCallback bool   // the browser calls back twice with the code
	ExpiredCode   bool   // token: 400 invalid_grant
	BadIDToken    string // "iss", "aud", "nonce", "sig", "exp": the ID token fails that check
	Denied        bool   // evaluate and bulk-evaluate: accessGranted false
	AccountExists bool   // the project already has the account evo (step 13)
	Conflict409   bool   // POST serviceAccounts: 409, the account appears in the list
	Perm          Perm   // the account evo exists and holds the role (step 15)
	PermForbidden bool   // permissions: 403
	RoleCollision bool   // POST permissions by roleName: 400, by roleId: created
	AccountLimit  bool   // the project has 10 service accounts
	KeyLimit      bool   // the account has 50 keys
	FailAt        Step   // the write of this step answers 500
	// AnyLoopbackPort takes a redirect_uri at any port of 127.0.0.1, not
	// only the registered ones: the tests of evo listen on port 0, so that
	// they share no fixed port with each other or the probe script.
	AnyLoopbackPort bool
	// EvaluateUnknown: evaluate and bulk-evaluate answer 400 «unknown
	// action» (the names of the actions are a hypothesis until the probe;
	// «Риск», U-9).
	EvaluateUnknown bool
	// UnknownActionFalse: the catalogue of actions is the one the probe A1
	// read (05.11, 2 of 2), and evaluate and bulk-evaluate answer
	// accessGranted false — not an error — for an action the catalogue
	// lacks, as an IAM may for a name it does not know («Риск», R-49).
	// The person may do everything the catalogue names.
	UnknownActionFalse bool
	// CatalogFails: the catalog of actions answers 500 («Риск», R-49: a
	// catalog evo could not read is no refusal).
	CatalogFails bool
	// AccountForbidden: POST serviceAccounts answers 403 («Риск», R-49: a
	// refusal of the platform at the first write is AUTH_FAILED).
	AccountForbidden bool
	// EmptyPagesWithToken: the lists of the Organization API answer their
	// items with a page token, then empty pages, each with a new token —
	// the list never says it ended (acceptance B-5, 06.11; IAM-10).
	EmptyPagesWithToken bool
}

// probeActions are the actions of IAM about accounts, keys and permissions
// in the catalogue the probe A1 read on 05.11 (2 of 2): the names are
// lower case and singular, and there is no create of an account.
var probeActions = []string{
	"iam.permission.add", "iam.permission.edit", "iam.permission.read",
	"iam.serviceaccount.accesskey.edit", "iam.serviceaccount.accesskey.read",
	"iam.serviceaccount.edit", "iam.serviceaccount.read",
}

// catalogue is the catalogue of actions the world serves.
func (w *World) catalogue() []string {
	if w.v.UnknownActionFalse {
		return probeActions
	}
	return []string{auth.LoginActionCreateAccount, auth.LoginActionGrantRole}
}

// granted answers evaluate for one action.
func (w *World) granted(action string) bool {
	if w.v.Denied {
		return false
	}
	if !w.v.UnknownActionFalse {
		return true
	}
	for _, a := range w.catalogue() {
		if a == action {
			return true
		}
	}
	return false
}

// Account, Permission and Key are the objects of the project.
type Account struct{ ID, Name, Description string }

// Permission is a role binding of an account in the project.
type Permission struct {
	ID, Subject, RoleName, RoleID string
	ExpiresAt                     time.Time
}

// Key is an access key of an account.
type Key struct {
	ID, KeyID, Secret, Account, Description string
	ExpiredAt                               time.Time
}

// World is a running fake.
type World struct {
	// URL is the base of every service; Issuer is URL + IssuerPath.
	URL, Issuer string

	t   testing.TB
	v   Variant
	srv *httptest.Server
	key *rsa.PrivateKey

	mu       sync.Mutex
	calls    []string
	secrets  []string
	codes    map[string]authz // code → its authorization request
	human    string           // the human's access token
	accounts map[string]*Account
	perms    map[string]*Permission
	keys     map[string]*Key
	saTokens map[string]string // token → account id
	seq      int
}

// authz is what an authorization request bound to its code.
type authz struct{ redirect, challenge, nonce string }

var (
	keyOnce sync.Once
	rsaKey  *rsa.PrivateKey
)

// New starts the world of v; the server closes with the test.
func New(t testing.TB, v Variant) *World {
	t.Helper()
	keyOnce.Do(func() {
		var err error
		if rsaKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	w := &World{t: t, v: v, key: rsaKey, codes: map[string]authz{},
		accounts: map[string]*Account{}, perms: map[string]*Permission{},
		keys: map[string]*Key{}, saTokens: map[string]string{}}
	// Everything the handler reads is written before the server starts:
	// its requests may come from another process (the probe script), whose
	// order the race detector does not see.
	w.srv = httptest.NewUnstartedServer(http.HandlerFunc(w.serve))
	w.URL = "http://" + w.srv.Listener.Addr().String()
	w.Issuer = w.URL + IssuerPath
	n := 0
	if v.AccountLimit {
		n = 10
	}
	for i := 0; i < n; i++ {
		w.addAccount(fmt.Sprintf("other-%d", i), "")
	}
	if v.AccountExists || v.Conflict409 || v.KeyLimit || v.Perm != PermNone {
		a := w.addAccount("evo", "evo login, earlier")
		if v.Conflict409 {
			a.Name = "" // hidden from the first list, shown after the 409
		}
		// The account holds the role from an earlier login: a day left, or
		// a year — shorter or longer than any key of 7, 30 or 90 days.
		switch v.Perm {
		case PermShorter:
			w.seedPermission(a.ID, time.Now().Add(24*time.Hour))
		case PermLonger:
			w.seedPermission(a.ID, time.Now().Add(365*24*time.Hour))
		}
	}
	w.srv.Start()
	t.Cleanup(w.srv.Close)
	return w
}

// Client is an HTTP client that does not follow redirects, as a browser
// hands the redirect to the loopback itself.
func (w *World) Client() *http.Client {
	c := *w.srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// Calls are "METHOD path" of every request, in order.
func (w *World) Calls() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.calls...)
}

// Secrets are every code, token and key secret the world handed out: none
// may reach a channel of evo but the key file (contract, invariant 4).
func (w *World) Secrets() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.secrets...)
}

// Accounts, Permissions and Keys are what the project holds now.
func (w *World) Accounts() []Account {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []Account
	for _, a := range w.accounts {
		out = append(out, *a)
	}
	return out
}

// Permissions of the role RoleName.
func (w *World) Permissions() []Permission {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []Permission
	for _, p := range w.perms {
		out = append(out, *p)
	}
	return out
}

// Keys of every account.
func (w *World) Keys() []Key {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []Key
	for _, k := range w.keys {
		out = append(out, *k)
	}
	return out
}

func (w *World) seedPermission(account string, exp time.Time) {
	id := w.next("perm")
	w.perms[id] = &Permission{ID: id, Subject: account, RoleName: RoleName, RoleID: "role-admin", ExpiresAt: exp}
}

// Browser plays the human at authURL: it signs in, and the provider sends
// it to the redirect_uri of the request, which it calls as a browser would
// — with ForeignState first, twice with TwiceCallback. It returns the
// statuses of the callbacks.
func (w *World) Browser(authURL string) ([]int, error) {
	resp, err := w.Client().Get(authURL)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		return nil, fmt.Errorf("authorize: %d", resp.StatusCode)
	}
	cb := resp.Header.Get("Location")
	var targets []string
	if w.v.ForeignState {
		u, _ := url.Parse(cb)
		q := u.Query()
		q.Set("state", "foreign")
		u.RawQuery = q.Encode()
		targets = append(targets, u.String())
	}
	targets = append(targets, cb)
	if w.v.TwiceCallback {
		targets = append(targets, cb)
	}
	var codes []int
	for _, t := range targets {
		r, err := w.Client().Get(t) // the loopback of evo
		if err != nil {
			return codes, err
		}
		r.Body.Close()
		codes = append(codes, r.StatusCode)
	}
	return codes, nil
}

func (w *World) next(kind string) string {
	w.seq++
	return fmt.Sprintf("%s-%04d", kind, w.seq)
}

func (w *World) secret(kind string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	s := kind + "-" + hex.EncodeToString(b)
	w.secrets = append(w.secrets, s)
	return s
}

func (w *World) addAccount(name, desc string) *Account {
	a := &Account{ID: w.next("sa"), Name: name, Description: desc}
	w.accounts[a.ID] = a
	return a
}

func writeJSON(rw http.ResponseWriter, code int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(code)
	_ = json.NewEncoder(rw).Encode(v)
}

func fail(rw http.ResponseWriter, code int, msg string) {
	writeJSON(rw, code, map[string]any{"code": code, "message": msg})
}

func (w *World) serve(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, r.Method+" "+r.URL.Path)
	p := r.URL.Path
	switch {
	case p == IssuerPath+"/.well-known/openid-configuration":
		writeJSON(rw, 200, map[string]any{
			"issuer":                           w.Issuer,
			"authorization_endpoint":           w.Issuer + "/openid/auth",
			"token_endpoint":                   w.Issuer + "/openid/token",
			"jwks_uri":                         w.Issuer + "/openid/certs",
			"code_challenge_methods_supported": []string{"S256"},
		})
	case p == IssuerPath+"/openid/auth":
		w.authorize(rw, r)
	case p == IssuerPath+"/openid/token":
		w.token(rw, r)
	case p == IssuerPath+"/openid/certs":
		n := base64.RawURLEncoding.EncodeToString(w.key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(w.key.E)).Bytes())
		writeJSON(rw, 200, map[string]any{"keys": []map[string]string{{"kid": "k1", "kty": "RSA", "alg": "RS256", "use": "sig", "n": n, "e": e}}})
	case p == "/api/v1/auth/token":
		w.keyToken(rw, r)
	case strings.HasPrefix(p, "/v3/"):
		if !w.bearerHuman(rw, r) {
			return
		}
		w.organization(rw, r)
	case strings.HasPrefix(p, "/api/"):
		w.administration(rw, r)
	default:
		fail(rw, 404, "no route "+p)
	}
}

// loopbackCallback is a redirect_uri on 127.0.0.1 at any port.
var loopbackCallback = regexp.MustCompile(`^http://127\.0\.0\.1:[0-9]{1,5}/callback$`)

// authorize checks the request of step 4 and redirects with a code (302).
func (w *World) authorize(rw http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	known := false
	for _, u := range RedirectURIs {
		known = known || u == redirect
	}
	if w.v.AnyLoopbackPort {
		known = known || loopbackCallback.MatchString(redirect)
	}
	switch {
	case !known:
		fail(rw, 400, "redirect_uri is not registered")
		return
	case q.Get("client_id") != ClientID, q.Get("response_type") != "code", q.Get("scope") != Scope:
		fail(rw, 400, "client_id, response_type or scope")
		return
	case q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43:
		fail(rw, 400, "PKCE S256 is required")
		return
	case q.Get("state") == "" || q.Get("nonce") == "":
		fail(rw, 400, "state and nonce are required")
		return
	}
	cb, _ := url.Parse(redirect)
	out := url.Values{"state": {q.Get("state")}}
	if w.v.Cancel {
		out.Set("error", "access_denied")
		out.Set("error_description", "the user cancelled")
	} else {
		code := w.secret("code")
		w.codes[code] = authz{redirect: redirect, challenge: q.Get("code_challenge"), nonce: q.Get("nonce")}
		out.Set("code", code)
	}
	cb.RawQuery = out.Encode()
	rw.Header().Set("Location", cb.String())
	rw.WriteHeader(http.StatusFound)
}

// token exchanges the code of step 6, once, with the verifier.
func (w *World) token(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		fail(rw, 400, "a form POST is required")
		return
	}
	_ = r.ParseForm()
	f := r.PostForm
	a, ok := w.codes[f.Get("code")]
	delete(w.codes, f.Get("code")) // a code is used once
	sum := sha256.Sum256([]byte(f.Get("code_verifier")))
	switch {
	case w.v.ExpiredCode || !ok:
		writeJSON(rw, 400, map[string]string{"error": "invalid_grant"})
		return
	case f.Get("grant_type") != "authorization_code", f.Get("client_id") != ClientID:
		writeJSON(rw, 400, map[string]string{"error": "invalid_request"})
		return
	case f.Get("redirect_uri") != a.redirect: // invariant 10: byte for byte
		writeJSON(rw, 400, map[string]string{"error": "invalid_grant", "error_description": "redirect_uri differs"})
		return
	case base64.RawURLEncoding.EncodeToString(sum[:]) != a.challenge:
		writeJSON(rw, 400, map[string]string{"error": "invalid_grant", "error_description": "code_verifier"})
		return
	}
	w.human = w.secret("access")
	id := w.idToken(a.nonce)
	w.secrets = append(w.secrets, id)
	writeJSON(rw, 200, map[string]any{"access_token": w.human, "id_token": id, "expires_in": 3600, "token_type": "Bearer"})
}

// idToken signs the ID token of step 7, broken as BadIDToken says.
func (w *World) idToken(nonce string) string {
	now := time.Now()
	claims := map[string]any{"iss": w.Issuer, "aud": []string{ClientID}, "sub": UserID,
		"nonce": nonce, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()}
	switch w.v.BadIDToken {
	case "iss":
		claims["iss"] = w.URL + "/auth/other" // another issuer of the same host
	case "aud":
		claims["aud"] = []string{"another-client"}
	case "nonce":
		claims["nonce"] = "another-nonce"
	case "exp":
		claims["exp"] = now.Add(-time.Minute).Unix()
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signed := enc(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signed))
	sig, _ := rsa.SignPKCS1v15(nil, w.key, crypto.SHA256, sum[:])
	if w.v.BadIDToken == "sig" {
		sig[0] ^= 0xff
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// keyToken is the IAM token of an access key (step 17, and logout).
func (w *World) keyToken(rw http.ResponseWriter, r *http.Request) {
	var body struct{ KeyID, Secret string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if w.v.FailAt == StepKeyCheck {
		fail(rw, 500, "internal")
		return
	}
	for _, k := range w.keys {
		if k.KeyID == body.KeyID && k.Secret == body.Secret {
			tok := w.secret("satoken")
			w.saTokens[tok] = k.Account
			writeJSON(rw, 200, map[string]any{"access_token": tok, "expires_in": 3600})
			return
		}
	}
	fail(rw, 401, "invalid credentials")
}

func (w *World) bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (w *World) bearerHuman(rw http.ResponseWriter, r *http.Request) bool {
	if w.human == "" || w.bearer(r) != w.human {
		fail(rw, 401, "the token of the human is required")
		return false
	}
	return true
}

func (w *World) organization(rw http.ResponseWriter, r *http.Request) {
	var key string
	var items any
	switch r.URL.Path {
	case "/v3/customers":
		key, items = "customers", []map[string]string{{"id": Customer, "name": "org", "full_name": "Org"}}
	case "/v3/projects":
		key, items = "projects", []any{}
		if r.URL.Query().Get("customer_ids") == Customer {
			items = []map[string]string{
				{"id": Project, "name": "proj", "status": "active", "customer_id": Customer},
				{"id": "p-deleted", "name": "old", "status": "deleted", "customer_id": Customer},
			}
		}
	default:
		fail(rw, 404, "no route")
		return
	}
	page := map[string]any{key: items}
	if w.v.EmptyPagesWithToken {
		// The items with a token, then empty pages, each with a new one.
		tok := r.URL.Query().Get("page_token")
		if tok != "" {
			page[key] = []any{}
		}
		page["next_page_token"] = tok + "p"
	}
	writeJSON(rw, 200, page)
}

// administration serves IAM: evaluate, service accounts, permissions,
// roles and access keys. Every call but key deletion takes the human's
// token; key deletion takes the token of the key's own account (logout).
func (w *World) administration(rw http.ResponseWriter, r *http.Request) {
	p, m := r.URL.Path, r.Method
	if !(m == http.MethodDelete && strings.Contains(p, "/accessKeys/")) && !w.bearerHuman(rw, r) {
		return
	}
	switch {
	case p == "/api/v1/actions" && w.v.CatalogFails:
		fail(rw, 500, "internal")
	case p == "/api/v1/actions":
		list := []map[string]string{}
		for _, a := range w.catalogue() {
			list = append(list, map[string]string{"name": a, "product_code": "iam"})
		}
		writeJSON(rw, 200, map[string]any{"actions": list})
	case w.v.EvaluateUnknown && (p == "/api/v1/permissions/evaluate" || p == "/api/v1/permissions/bulk-evaluate"):
		fail(rw, 400, "unknown action")
	case p == "/api/v1/permissions/evaluate":
		writeJSON(rw, 200, map[string]bool{"accessGranted": w.granted(r.URL.Query().Get("action"))})
	case p == "/api/v1/permissions/bulk-evaluate":
		var body struct{ Requests []map[string]string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		res := make([]map[string]bool, len(body.Requests))
		for i := range res {
			res[i] = map[string]bool{"accessGranted": w.granted(body.Requests[i]["action"])}
		}
		writeJSON(rw, 200, map[string]any{"results": res})
	case p == "/api/v2/serviceAccounts" && m == http.MethodGet:
		name := strings.TrimPrefix(r.URL.Query().Get("filter"), "name=")
		list := []map[string]any{}
		for _, a := range w.accounts {
			if a.Name != "" && (name == "" || a.Name == name) {
				list = append(list, map[string]any{"id": a.ID, "name": a.Name, "enabled": true})
			}
		}
		writeJSON(rw, 200, map[string]any{"serviceAccounts": list})
	case p == "/api/v2/serviceAccounts" && m == http.MethodPost:
		w.createAccount(rw, r)
	case p == "/api/v3/permissions" && m == http.MethodGet:
		w.listPermissions(rw, r)
	case strings.HasPrefix(p, "/api/v3/permissions/") && m == http.MethodDelete:
		// Rollback of step 15: only a permission this login made by POST
		// (one extended by PUT keeps its term).
		id := strings.TrimPrefix(p, "/api/v3/permissions/")
		if w.perms[id] == nil {
			fail(rw, 404, "permission not found")
			return
		}
		delete(w.perms, id)
		rw.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(p, "/api/v2/serviceAccounts/") && !strings.Contains(p, "/accessKeys") && m == http.MethodDelete:
		// Rollback of step 14: only an account this login made (one found by
		// its name stays).
		id := strings.TrimPrefix(p, "/api/v2/serviceAccounts/")
		if w.accounts[id] == nil {
			fail(rw, 404, "service account not found")
			return
		}
		delete(w.accounts, id)
		rw.WriteHeader(http.StatusNoContent)
	case p == "/api/v3/permissions" && m == http.MethodPost:
		w.createPermission(rw, r)
	case strings.HasPrefix(p, "/api/v3/permissions/") && m == http.MethodPut:
		w.updatePermission(rw, r, strings.TrimPrefix(p, "/api/v3/permissions/"))
	case p == "/api/v1/roles":
		writeJSON(rw, 200, map[string]any{"roles": []map[string]any{{"id": "role-admin", "name": RoleName, "type": "ROLE_TYPE_PRODUCT", "enabled": true}}})
	case strings.HasPrefix(p, "/api/v2/serviceAccounts/") && strings.Contains(p, "/accessKeys"):
		w.accessKeys(rw, r)
	default:
		fail(rw, 404, "no route "+p)
	}
}

func (w *World) createAccount(rw http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, Description string
		Target            struct{ ProjectID string }
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch {
	case w.v.FailAt == StepAccount:
		fail(rw, 500, "internal")
	case w.v.AccountForbidden:
		fail(rw, 403, "forbidden")
	case w.v.AccountLimit:
		fail(rw, 400, "service account limit reached")
	case body.Target.ProjectID != Project:
		fail(rw, 404, "project not found")
	case w.v.Conflict409:
		for _, a := range w.accounts {
			if a.Name == "" {
				a.Name = body.Name // now visible: another login made it
			}
		}
		fail(rw, 409, "already exists")
	default:
		for _, a := range w.accounts {
			if a.Name == body.Name {
				fail(rw, 409, "already exists")
				return
			}
		}
		a := w.addAccount(body.Name, body.Description)
		writeJSON(rw, 200, map[string]any{"id": a.ID, "name": a.Name, "enabled": true})
	}
}

func (w *World) listPermissions(rw http.ResponseWriter, r *http.Request) {
	if w.v.PermForbidden {
		fail(rw, 403, "forbidden")
		return
	}
	f := r.URL.Query().Get("filter")
	list := []map[string]any{}
	for _, p := range w.perms {
		if strings.Contains(f, "subject_id="+p.Subject) && strings.Contains(f, "resource_id="+Project) {
			list = append(list, map[string]any{"id": p.ID, "roleName": p.RoleName, "subjectId": p.Subject,
				"resourceId": Project, "expiresAt": p.ExpiresAt.UTC().Format(time.RFC3339), "enabled": true})
		}
	}
	writeJSON(rw, 200, map[string]any{"permissions": list})
}

func (w *World) createPermission(rw http.ResponseWriter, r *http.Request) {
	var body struct {
		SubjectID, ResourceID string
		RoleIdentifier        struct{ RoleName, RoleID string }
		ExpiresAt             time.Time
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch {
	case w.v.FailAt == StepPermission:
		fail(rw, 500, "internal")
		return
	case w.v.PermForbidden:
		fail(rw, 403, "forbidden")
		return
	case w.accounts[body.SubjectID] == nil || body.ResourceID != Project:
		fail(rw, 404, "subject or resource not found")
		return
	case body.RoleIdentifier.RoleName != "" && w.v.RoleCollision:
		fail(rw, 400, "role name is ambiguous")
		return
	case body.RoleIdentifier.RoleName != RoleName && body.RoleIdentifier.RoleID != "role-admin":
		fail(rw, 400, "unknown role")
		return
	}
	id := w.next("perm")
	w.perms[id] = &Permission{ID: id, Subject: body.SubjectID, RoleName: RoleName, RoleID: "role-admin", ExpiresAt: body.ExpiresAt}
	writeJSON(rw, 200, map[string]any{"id": id, "roleName": RoleName, "expiresAt": body.ExpiresAt, "enabled": true})
}

func (w *World) updatePermission(rw http.ResponseWriter, r *http.Request, id string) {
	var body struct{ ExpiresAt time.Time }
	_ = json.NewDecoder(r.Body).Decode(&body)
	p := w.perms[id]
	switch {
	case w.v.FailAt == StepPermission:
		fail(rw, 500, "internal")
	case p == nil:
		fail(rw, 404, "permission not found")
	default:
		p.ExpiresAt = body.ExpiresAt
		writeJSON(rw, 200, map[string]any{"id": id, "roleName": p.RoleName, "expiresAt": p.ExpiresAt})
	}
}

func (w *World) accessKeys(rw http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v2/serviceAccounts/")
	account, keyID, _ := strings.Cut(strings.Replace(rest, "/accessKeys", "", 1), "/")
	if w.accounts[account] == nil {
		fail(rw, 404, "service account not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		list := []map[string]any{}
		n := 0
		if w.v.KeyLimit {
			n = 50
		}
		for i := 0; i < n; i++ {
			list = append(list, map[string]any{"id": fmt.Sprintf("other-key-%d", i)})
		}
		for _, k := range w.keys {
			if k.Account == account {
				list = append(list, map[string]any{"id": k.ID, "keyId": k.KeyID, "description": k.Description, "expiredAt": k.ExpiredAt})
			}
		}
		writeJSON(rw, 200, map[string]any{"accessKeys": list})
	case http.MethodPost:
		var body struct{ Description, TTL string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		ttl, err := time.ParseDuration(body.TTL)
		switch {
		case w.v.FailAt == StepKey:
			fail(rw, 500, "internal")
			return
		case w.v.KeyLimit:
			fail(rw, 400, "access key limit reached")
			return
		case err != nil || ttl <= 0:
			fail(rw, 400, "ttl")
			return
		}
		k := &Key{ID: w.next("key"), KeyID: w.next("keyid"), Secret: w.secret("keysecret"), Account: account,
			Description: body.Description, ExpiredAt: time.Now().Add(ttl)}
		w.keys[k.ID] = k
		writeJSON(rw, 200, map[string]any{"id": k.ID, "keyId": k.KeyID, "secret": k.Secret, "expiredAt": k.ExpiredAt, "serviceAccountId": account})
	case http.MethodDelete:
		tok := w.bearer(r)
		if w.saTokens[tok] != account && tok != w.human {
			fail(rw, 403, "only the account itself or the human deletes its key")
			return
		}
		if w.keys[keyID] == nil || w.keys[keyID].Account != account {
			fail(rw, 404, "access key not found")
			return
		}
		delete(w.keys, keyID)
		rw.WriteHeader(http.StatusNoContent)
	default:
		fail(rw, 405, "method")
	}
}
