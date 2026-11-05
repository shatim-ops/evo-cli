// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package loginworld

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// walk is the reference client of the contract B-1, steps 2–18 without the
// loopback page: what evo login must do, as the world sees it. It answers
// the reason evo would end with ("" — success) and the step it ended at.
type walk struct {
	t       *testing.T
	w       *World
	c       *http.Client
	ttl     time.Duration
	created []string // DELETE paths of what this walk made, in order
}

type outcome struct{ reason, at string }

func (k *walk) do(method, path string, body any, tok string) (int, map[string]any) {
	k.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, k.w.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.c.Do(req)
	if err != nil {
		k.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

// rollback deletes what the walk made, newest first, with the human's token.
func (k *walk) rollback(tok string) {
	for i := len(k.created) - 1; i >= 0; i-- {
		if code, _ := k.do(http.MethodDelete, k.created[i], nil, tok); code != http.StatusNoContent {
			k.t.Errorf("rollback %s: %d", k.created[i], code)
		}
	}
}

func (k *walk) run() outcome {
	t, w := k.t, k.w
	// 2. Discovery: the issuer is the expected one; endpoints only from here.
	resp, err := k.c.Get(w.Issuer + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&d)
	resp.Body.Close()
	if d["issuer"] != w.Issuer {
		return outcome{"AUTH_FAILED", "discovery"}
	}
	verifier := strings.Repeat("v", 64)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state, nonce, redirect := "state-1", "nonce-1", RedirectURIs[0]
	// 4–5. Authorization: the provider redirects to the loopback.
	q := url.Values{"response_type": {"code"}, "client_id": {ClientID}, "redirect_uri": {redirect}, "scope": {Scope},
		"state": {state}, "nonce": {nonce}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	resp, err = k.c.Get(d["authorization_endpoint"].(string) + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	cb := loc.Query()
	if cb.Get("state") != state {
		return outcome{"AUTH_FAILED", "callback"}
	}
	if cb.Get("error") == "access_denied" {
		return outcome{"USAGE", "callback"}
	}
	// 6. Token.
	form := url.Values{"grant_type": {"authorization_code"}, "code": {cb.Get("code")}, "redirect_uri": {redirect},
		"client_id": {ClientID}, "code_verifier": {verifier}}
	resp, err = k.c.PostForm(d["token_endpoint"].(string), form)
	if err != nil {
		t.Fatal(err)
	}
	var tok map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return outcome{"AUTH_FAILED", "token"}
	}
	// 7. ID token: signature by JWKS, iss, aud, nonce, exp.
	if err := k.checkIDToken(d["jwks_uri"].(string), tok["id_token"].(string), nonce); err != nil {
		return outcome{"AUTH_FAILED", "id_token: " + err.Error()}
	}
	human := tok["access_token"].(string)
	// 8–9. Organization and project.
	if code, _ := k.do(http.MethodGet, "/v3/customers", nil, human); code != 200 {
		return outcome{"API_ERROR", "customers"}
	}
	if code, m := k.do(http.MethodGet, "/v3/projects?customer_ids="+Customer, nil, human); code != 200 || len(m["projects"].([]any)) != 2 {
		return outcome{"API_ERROR", "projects"}
	}
	// 10, 12. The human may create the account and grant the role.
	_, m := k.do(http.MethodPost, "/api/v1/permissions/bulk-evaluate", map[string]any{"requests": []map[string]string{
		{"action": "iam.serviceAccounts.create", "resourceId": Project, "userId": UserID}}}, human)
	if m["results"].([]any)[0].(map[string]any)["accessGranted"] != true {
		return outcome{"AUTH_FAILED", "bulk-evaluate"}
	}
	if _, m := k.do(http.MethodGet, "/api/v1/permissions/evaluate?resourceId="+Project, nil, human); m["accessGranted"] != true {
		return outcome{"AUTH_FAILED", "evaluate"}
	}
	// Limits are refused before any write.
	_, all := k.do(http.MethodGet, "/api/v2/serviceAccounts?target.projectId="+Project, nil, human)
	accounts, _ := all["serviceAccounts"].([]any)
	// 13. The account by name.
	_, m = k.do(http.MethodGet, "/api/v2/serviceAccounts?target.projectId="+Project+"&filter=name=evo", nil, human)
	sa := ""
	if l, _ := m["serviceAccounts"].([]any); len(l) == 1 {
		sa = l[0].(map[string]any)["id"].(string)
	}
	if sa == "" && len(accounts) >= 10 {
		return outcome{"USAGE", "account limit"}
	}
	if sa != "" { // its keys: the limit before any write
		if _, m := k.do(http.MethodGet, "/api/v2/serviceAccounts/"+sa+"/accessKeys", nil, human); len(m["accessKeys"].([]any)) >= 50 {
			return outcome{"USAGE", "key limit"}
		}
	}
	// 14. Create it; 409 — list again and reuse.
	if sa == "" {
		code, m := k.do(http.MethodPost, "/api/v2/serviceAccounts", map[string]any{"name": "evo", "description": "evo login, user, 2026-11-05",
			"target": map[string]string{"projectId": Project}}, human)
		switch code {
		case 200:
			sa = m["id"].(string)
			k.created = append(k.created, "/api/v2/serviceAccounts/"+sa)
		case 409:
			_, m = k.do(http.MethodGet, "/api/v2/serviceAccounts?target.projectId="+Project+"&filter=name=evo", nil, human)
			sa = m["serviceAccounts"].([]any)[0].(map[string]any)["id"].(string)
		default:
			k.rollback(human)
			return outcome{"API_ERROR", "account"}
		}
	}
	// 15. The role until the key's expiry: PUT a shorter one, keep a longer one.
	exp := time.Now().Add(k.ttl).UTC().Truncate(time.Second)
	code, m := k.do(http.MethodGet, "/api/v3/permissions?filter="+url.QueryEscape("resource_id="+Project+" AND subject_id="+sa+" AND role_name="+RoleName), nil, human)
	if code != 200 {
		k.rollback(human)
		return outcome{"API_ERROR", "permission"}
	}
	if l, _ := m["permissions"].([]any); len(l) == 1 {
		p := l[0].(map[string]any)
		have, _ := time.Parse(time.RFC3339, p["expiresAt"].(string))
		if have.Before(exp) {
			if code, _ := k.do(http.MethodPut, "/api/v3/permissions/"+p["id"].(string), map[string]any{"expiresAt": exp}, human); code != 200 {
				k.rollback(human)
				return outcome{"API_ERROR", "permission"}
			}
		}
	} else {
		body := map[string]any{"subjectId": sa, "resourceId": Project, "roleIdentifier": map[string]string{"roleName": RoleName}, "expiresAt": exp}
		code, m := k.do(http.MethodPost, "/api/v3/permissions", body, human)
		if code == 400 { // a collision of role names: by roleId
			_, r := k.do(http.MethodGet, "/api/v1/roles?customerId="+Customer, nil, human)
			body["roleIdentifier"] = map[string]string{"roleId": r["roles"].([]any)[0].(map[string]any)["id"].(string)}
			code, m = k.do(http.MethodPost, "/api/v3/permissions", body, human)
		}
		if code != 200 {
			k.rollback(human)
			return outcome{"API_ERROR", "permission"}
		}
		k.created = append(k.created, "/api/v3/permissions/"+m["id"].(string))
	}
	// 16. The key.
	code, key := k.do(http.MethodPost, "/api/v2/serviceAccounts/"+sa+"/accessKeys",
		map[string]string{"description": "evo login, user, 2026-11-05", "ttl": fmt.Sprintf("%dh", int(k.ttl.Hours()))}, human)
	if code != 200 {
		k.rollback(human)
		return outcome{"API_ERROR", "key"}
	}
	k.created = append(k.created, "/api/v2/serviceAccounts/"+sa+"/accessKeys/"+key["id"].(string))
	// 17. The key works.
	code, _ = k.do(http.MethodPost, "/api/v1/auth/token", map[string]string{"keyId": key["keyId"].(string), "secret": key["secret"].(string)}, "")
	if code != 200 {
		k.rollback(human)
		return outcome{"AUTH_FAILED", "key-check"}
	}
	return outcome{}
}

func (k *walk) checkIDToken(jwks, id, nonce string) error {
	resp, err := k.c.Get(jwks)
	if err != nil {
		return err
	}
	var set struct{ Keys []struct{ N, E string } }
	_ = json.NewDecoder(resp.Body).Decode(&set)
	resp.Body.Close()
	parts := strings.Split(id, ".")
	n, _ := base64.RawURLEncoding.DecodeString(set.Keys[0].N)
	e, _ := base64.RawURLEncoding.DecodeString(set.Keys[0].E)
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig) != nil {
		return errors.New("signature")
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Iss, Nonce string
		Aud        []string
		Exp        int64
	}
	_ = json.Unmarshal(payload, &c)
	switch {
	case c.Iss != k.w.Issuer:
		return errors.New("iss")
	case len(c.Aud) != 1 || c.Aud[0] != ClientID:
		return errors.New("aud")
	case c.Nonce != nonce:
		return errors.New("nonce")
	case time.Unix(c.Exp, 0).Before(time.Now()):
		return errors.New("exp")
	}
	return nil
}

// Every variant of section 6 ends as the contract says, and a failure
// after a write leaves nothing this login made (invariant 8).
func TestLoginWorld_ReferenceWalk(t *testing.T) {
	const ttl = 30 * 24 * time.Hour
	cases := []struct {
		name     string
		v        Variant
		want     outcome
		accounts int // service accounts named evo left
		keys     int
		writes   []string // calls that must (or, with "!", must not) be made
	}{
		{"success", Variant{}, outcome{}, 1, 1, []string{"POST /api/v2/serviceAccounts", "POST /api/v3/permissions"}},
		{"cancel", Variant{Cancel: true}, outcome{"USAGE", "callback"}, 0, 0, []string{"!POST " + IssuerPath + "/openid/token"}},
		{"expired code", Variant{ExpiredCode: true}, outcome{"AUTH_FAILED", "token"}, 0, 0, nil},
		{"id token iss", Variant{BadIDToken: "iss"}, outcome{"AUTH_FAILED", "id_token: iss"}, 0, 0, []string{"!GET /v3/customers"}},
		{"id token aud", Variant{BadIDToken: "aud"}, outcome{"AUTH_FAILED", "id_token: aud"}, 0, 0, nil},
		{"id token nonce", Variant{BadIDToken: "nonce"}, outcome{"AUTH_FAILED", "id_token: nonce"}, 0, 0, nil},
		{"id token signature", Variant{BadIDToken: "sig"}, outcome{"AUTH_FAILED", "id_token: signature"}, 0, 0, nil},
		{"id token expired", Variant{BadIDToken: "exp"}, outcome{"AUTH_FAILED", "id_token: exp"}, 0, 0, nil},
		{"no right", Variant{Denied: true}, outcome{"AUTH_FAILED", "bulk-evaluate"}, 0, 0, []string{"!POST /api/v2/serviceAccounts"}},
		{"account exists", Variant{AccountExists: true}, outcome{}, 1, 1, []string{"!POST /api/v2/serviceAccounts"}},
		{"409 on the account", Variant{Conflict409: true}, outcome{}, 1, 1, []string{"POST /api/v2/serviceAccounts"}},
		{"role shorter: PUT", Variant{Perm: PermShorter}, outcome{}, 1, 1, []string{"PUT /api/v3/permissions/", "!POST /api/v3/permissions"}},
		{"broken at the key after a PUT: the term stays", Variant{Perm: PermShorter, FailAt: StepKey}, outcome{"API_ERROR", "key"}, 1, 0, []string{"!DELETE /api/v3/permissions/", "!DELETE /api/v2/serviceAccounts/"}},
		{"role longer: no write", Variant{Perm: PermLonger}, outcome{}, 1, 1, []string{"!PUT /api/v3/permissions/", "!POST /api/v3/permissions"}},
		{"403 on permissions", Variant{PermForbidden: true}, outcome{"API_ERROR", "permission"}, 0, 0, nil},
		{"role name collision", Variant{RoleCollision: true}, outcome{}, 1, 1, []string{"GET /api/v1/roles"}},
		{"account limit", Variant{AccountLimit: true}, outcome{"USAGE", "account limit"}, 0, 0, []string{"!POST /api/v2/serviceAccounts"}},
		{"key limit", Variant{KeyLimit: true}, outcome{"USAGE", "key limit"}, 1, 0, []string{"!POST /api/v2/serviceAccounts/", "!POST /api/v3/permissions"}},
		{"broken at the account", Variant{FailAt: StepAccount}, outcome{"API_ERROR", "account"}, 0, 0, nil},
		{"broken at the permission", Variant{FailAt: StepPermission}, outcome{"API_ERROR", "permission"}, 0, 0, nil},
		{"broken at the key", Variant{FailAt: StepKey}, outcome{"API_ERROR", "key"}, 0, 0, nil},
		{"broken at the key check", Variant{FailAt: StepKeyCheck}, outcome{"AUTH_FAILED", "key-check"}, 0, 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := New(t, c.v)
			k := &walk{t: t, w: w, c: w.Client(), ttl: ttl}
			if got := k.run(); got != c.want {
				t.Fatalf("ended %+v, want %+v", got, c.want)
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
			if c.keys == 1 {
				p := w.Permissions()
				want := time.Now().Add(ttl)
				if c.v.Perm == PermLonger {
					want = time.Now().Add(365 * 24 * time.Hour)
				}
				if len(p) != 1 || p[0].ExpiresAt.Sub(want).Abs() > time.Minute {
					t.Errorf("permissions %+v, want one until %s", p, want)
				}
			}
			// A call is a whole line; one that ends with "/" is a prefix.
			calls := w.Calls()
			for _, wr := range c.writes {
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

// The provider holds the request to its contract: an unregistered
// redirect_uri, plain PKCE, a second use of a code, another redirect_uri at
// the exchange and a wrong verifier are refused (invariants 2, 10).
func TestLoginWorld_ProviderRefuses(t *testing.T) {
	w := New(t, Variant{})
	c := w.Client()
	verifier := strings.Repeat("v", 64)
	sum := sha256.Sum256([]byte(verifier))
	base := url.Values{"response_type": {"code"}, "client_id": {ClientID}, "redirect_uri": {RedirectURIs[1]}, "scope": {Scope},
		"state": {"s"}, "nonce": {"n"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	auth := func(change func(url.Values)) (int, url.Values) {
		q := url.Values{}
		for k, v := range base {
			q[k] = v
		}
		change(q)
		resp, err := c.Get(w.Issuer + "/openid/auth?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		loc, _ := url.Parse(resp.Header.Get("Location"))
		return resp.StatusCode, loc.Query()
	}
	for name, change := range map[string]func(url.Values){
		"redirect_uri on another port": func(q url.Values) { q.Set("redirect_uri", "http://127.0.0.1:18387/callback") },
		"redirect_uri on localhost":    func(q url.Values) { q.Set("redirect_uri", "http://localhost:18386/callback") },
		"plain PKCE":                   func(q url.Values) { q.Set("code_challenge_method", "plain") },
		"no nonce":                     func(q url.Values) { q.Del("nonce") },
		"another client":               func(q url.Values) { q.Set("client_id", "other") },
	} {
		if code, _ := auth(change); code != 400 {
			t.Errorf("authorize with %s: %d, want 400", name, code)
		}
	}
	exchange := func(code, redirect, verifier string) int {
		resp, err := c.PostForm(w.Issuer+"/openid/token", url.Values{"grant_type": {"authorization_code"}, "code": {code},
			"redirect_uri": {redirect}, "client_id": {ClientID}, "code_verifier": {verifier}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	_, cb := auth(func(url.Values) {})
	if got := exchange(cb.Get("code"), RedirectURIs[0], verifier); got != 400 {
		t.Errorf("exchange with another redirect_uri: %d, want 400", got)
	}
	_, cb = auth(func(url.Values) {})
	if got := exchange(cb.Get("code"), RedirectURIs[1], "w"+verifier[1:]); got != 400 {
		t.Errorf("exchange with a wrong verifier: %d, want 400", got)
	}
	_, cb = auth(func(url.Values) {})
	if got := exchange(cb.Get("code"), RedirectURIs[1], verifier); got != 200 {
		t.Errorf("exchange: %d, want 200", got)
	}
	// The person's access token and ID token are among Secrets: a channel
	// walk that checks Secrets catches either (A0-5).
	secrets := strings.Join(w.Secrets(), "\n")
	if w.human == "" || !strings.Contains(secrets, w.human) || strings.Count(secrets, ".") < 2 {
		t.Error("the access token or the ID token of the exchange is not among Secrets")
	}
	if got := exchange(cb.Get("code"), RedirectURIs[1], verifier); got != 400 {
		t.Errorf("second exchange of a code: %d, want 400", got)
	}
	// Organization API takes only the human's token; a key is deleted only
	// by its own account or the human.
	req, _ := http.NewRequest(http.MethodGet, w.URL+"/v3/customers", nil)
	req.Header.Set("Authorization", "Bearer forged")
	if resp, _ := c.Do(req); resp.StatusCode != 401 {
		t.Errorf("customers with a forged token: %d, want 401", resp.StatusCode)
	}
}

// Browser calls back the loopback as the variant says; the code and the
// tokens are among Secrets.
func TestLoginWorld_Browser(t *testing.T) {
	ln, port := listen(t)
	got := make(chan url.Values, 4)
	srv := &http.Server{Handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		got <- r.URL.Query()
		if r.URL.Query().Get("state") != "s" {
			rw.WriteHeader(http.StatusForbidden)
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	w := New(t, Variant{ForeignState: true, TwiceCallback: true})
	sum := sha256.Sum256([]byte(strings.Repeat("v", 64)))
	q := url.Values{"response_type": {"code"}, "client_id": {ClientID}, "redirect_uri": {RedirectURIs[port]}, "scope": {Scope},
		"state": {"s"}, "nonce": {"n"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	codes, err := w.Browser(w.Issuer + "/openid/auth?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(codes) != "[403 200 200]" {
		t.Errorf("callbacks answered %v, want [403 200 200] (foreign state, the code, the code again)", codes)
	}
	code := (<-got).Get("code")
	if (<-got).Get("code") != code || code == "" {
		t.Error("the foreign state did not carry the same code")
	}
	found := false
	for _, s := range w.Secrets() {
		found = found || s == code
	}
	if !found {
		t.Error("the code is not among Secrets")
	}
}

// listen takes the first free of the registered ports, as evo does (step 3).
func listen(t *testing.T) (net.Listener, int) {
	t.Helper()
	for i, u := range RedirectURIs {
		p, _ := url.Parse(u)
		if ln, err := net.Listen("tcp", p.Host); err == nil {
			return ln, i
		}
	}
	t.Skip("all three registered ports are busy on this machine")
	return nil, 0
}
