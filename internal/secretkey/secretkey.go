// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package secretkey is the one rule of which key names a credential: the
// roots of a key (Roots), the closed list of keys that hold a root but no
// credential (Exceptions), the words that make the name of a name/value
// pair a credential, and an address with a password in it. The masking of
// results over HTTP (internal/mcp) and the masking of what evo passed to
// helm (k8s_helm, #2465) ask it, so the two never disagree.
package secretkey

import (
	"net/url"
	"slices"
	"strings"
	"unicode"
)

// Root is a root of a credential field name, masked in tool results
// over HTTP without --allow-secret-export (H7). The match ignores case and
// the word separators (Sep): password, Password and PASSWORD are one
// root, as are private_key, private.key and privateKey.
type Root struct {
	// Name is the root, as docs/mcp.md names it.
	Name string
	// Exact: the key is the root itself and nothing more (token: not
	// next_page_token). Otherwise a key that contains the root anywhere
	// (new_password_confirm, passwords, private_key_2) is a credential,
	// unless Exceptions says it is not.
	Exact bool
	// Where says which results carry it, or why a cloud object might.
	Where string
}

// Roots are the roots. Masking holds for every tool's result:
// about thirty tools (pg_get, redis_get, kafka_get, k8s_get, secret_get,
// cert_get, …) return the cloud's object as the API sent it, and a field the
// API adds tomorrow must not leave the host. A key with a root anywhere is
// masked; on a shared server a leak costs more than a mask too many, so the
// exceptions are a closed list of their own (Exceptions).
var Roots = []Root{
	{Name: "password", Where: "deploy_apply and provision (the generated VM password, shown once), passwords{step} of stack_apply and stack_status; users and clusters from pg_*, redis_*, kafka_* if the API sends them; image metadata (admin_password, linux_password)"},
	{Name: "passwd", Where: "the same, spelled passwd"},
	{Name: "secret", Where: "an access key's secret, a shared secret in a cloud object"},
	{Name: "client_secret", Where: "the OIDC client secret (mk8s token exchange)"},
	{Name: "secret_access_key", Where: "object storage (S3) keys in a cloud object"},
	{Name: "private_key", Where: "key material: privateKey / private_key of Certificate Manager, ssh_private_key"},
	{Name: "api_key", Where: "API keys in a cloud object"},
	{Name: "access_token", Where: "IAM / OIDC tokens (access_token, accessToken)"},
	{Name: "refresh_token", Where: "OIDC refresh tokens"},
	{Name: "id_token", Where: "OIDC ID tokens (the mk8s cluster token)"},
	{Name: "bearer_token", Where: "bearer tokens"},
	{Name: "token", Exact: true, Where: "a bearer token under its bare name (a kubeconfig user's token); page and verification tokens are not credentials"},
	{Name: "tls.key", Exact: true, Where: "the private key of a kubernetes.io/tls Secret's data; tls.crt is the certificate and stays"},
	{Name: "id_rsa", Exact: true, Where: "an SSH private key under the name of its file (the identity files ssh(1) reads by default) in a Secret's data or a cloud object; id_rsa.pub is the public key and stays"},
	{Name: "id_dsa", Exact: true, Where: "the same, for a DSA key"},
	{Name: "id_ecdsa", Exact: true, Where: "the same, for an ECDSA key"},
	{Name: "id_ecdsa_sk", Exact: true, Where: "the same, for an ECDSA security key"},
	{Name: "id_ed25519", Exact: true, Where: "the same, for an Ed25519 key"},
	{Name: "id_ed25519_sk", Exact: true, Where: "the same, for an Ed25519 security key"},
}

// Exception is one entry of the closed list of keys that hold a root of
// Roots but not a credential.
type Exception struct {
	// Name is the key or, with Suffix, the key's last words, as docs/mcp.md
	// names it ("_expires_at").
	Name string
	// Suffix: the key ends in these words, at a word boundary (a separator
	// of Sep or a capital letter): password_expires_at and
	// passwordExpiresAt, not password_expires_at_raw and not
	// passwordexpiresat. Otherwise the whole key, compared without case and
	// separators.
	Suffix bool
	// Tools limits the entry to these tools' results; nil: every tool.
	// Container: the key is not a credential while it holds an object or a
	// list of objects (whose own keys are checked as usual); a string under
	// it is still masked.
	Tools     []string
	Container bool
	Why       string
}

// Exceptions is the closed list of exceptions.
var Exceptions = []Exception{
	{Name: "_id", Suffix: true, Why: "an identifier of the secret, not the secret (secret_id, private_key_id)"},
	{Name: "_ids", Suffix: true, Why: "identifiers"},
	{Name: "_at", Suffix: true, Why: "a timestamp"},
	{Name: "_expires_at", Suffix: true, Why: "a timestamp (password_expires_at)"},
	{Name: "_updated_at", Suffix: true, Why: "a timestamp"},
	{Name: "_created_at", Suffix: true, Why: "a timestamp"},
	{Name: "_policy", Suffix: true, Why: "the rules for a password, not a password"},
	{Name: "_algorithm", Suffix: true, Why: "the key's algorithm (privateKeyAlgorithm)"},
	{Name: "_alg", Suffix: true, Why: "the key's algorithm"},
	{Name: "_type", Suffix: true, Why: "a kind of credential (access_token_type)"},
	{Name: "_kind", Suffix: true, Why: "a kind of credential"},
	{Name: "_name", Suffix: true, Why: "a name (secret_name)"},
	{Name: "_ref", Suffix: true, Why: "a reference to where the secret is stored"},
	{Name: "_version", Suffix: true, Why: "a version number"},
	{Name: "_length", Suffix: true, Why: "a length"},
	{Name: "_status", Suffix: true, Why: "a state"},
	{Name: "_enabled", Suffix: true, Why: "a flag"},
	{Name: "_required", Suffix: true, Why: "a flag"},
	{Name: "_set", Suffix: true, Why: "a flag (password_set)"},
	{Name: "next_page_token", Why: "a page cursor of the list APIs, not a credential"},
	{Name: "page_token", Why: "a page cursor of the list APIs, not a credential"},
	{Name: "secret", Tools: secretManagerTools, Container: true, Why: "secret_create's answer: the secret's metadata (id, path, description; secret_create drops the payload)"},
	{Name: "secrets", Tools: secretManagerTools, Container: true, Why: "secret_list's answer: the secrets' metadata (id, name, path, description; values only from secret_value, H5)"},
}

// secretManagerTools answer Secret Manager metadata under "secret" and
// "secrets".
var secretManagerTools = []string{"secret_list", "secret_get", "secret_create", "secret_add_version", "secret_delete"}

// Sep reports whether r separates the words of a key: "_", "-", "." and
// a space (private_key, private-key, private.key, "private key"; Cursor
// review 21.10, RP-4).
func Sep(r rune) bool { return r == '_' || r == '-' || r == '.' || r == ' ' }

// Norm lowercases a key and drops its separators (Sep).
func Norm(k string) string {
	return strings.Map(func(r rune) rune {
		if Sep(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, k)
}

// Words splits a key into lowercase words at a separator (Sep) and a
// capital letter after a lowercase one or a digit: passwordExpiresAt and
// password_expires_at are both password, expires, at.
func Words(k string) []string {
	var words []string
	var cur []rune
	var prev rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	for _, r := range k {
		switch {
		case Sep(r):
			flush()
		case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
		prev = r
	}
	flush()
	return words
}

// Excepted reports whether Exceptions excepts key k (holding v) in
// the result of tool.
func Excepted(tool, k string, v any) bool {
	words := Words(k)
	for _, f := range Exceptions {
		if f.Tools != nil && !slices.Contains(f.Tools, tool) {
			continue
		}
		if f.Container && !holdsObjects(v) {
			continue
		}
		if !f.Suffix {
			if Norm(k) == Norm(f.Name) {
				return true
			}
			continue
		}
		suf := Words(f.Name)
		if len(words) > len(suf) && slices.Equal(words[len(words)-len(suf):], suf) {
			return true
		}
	}
	return false
}

// holdsObjects: v is an object or a non-empty list of objects only.
func holdsObjects(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		return true
	case []any:
		for _, x := range t {
			if _, ok := x.(map[string]any); !ok {
				return false
			}
		}
		return len(t) > 0
	}
	return false
}

// HasRoot reports whether key k holds a root of Roots.
func HasRoot(k string) bool {
	n := Norm(k)
	for _, f := range Roots {
		root := Norm(f.Name)
		if f.Exact && n == root || !f.Exact && strings.Contains(n, root) {
			return true
		}
	}
	return false
}

// pairWords are the last words that make the name of a name/value
// pair a credential beyond the roots of a key: an environment variable
// names its secret in words a JSON key does not (GITHUB_TOKEN, DB_PASS,
// MYSQL_PWD, AWS_ACCESS_KEY; Fable audit 1.5, FB15-44). Only the last
// word counts — TOKEN_URL is where a token is asked for — and a public key
// is none: only a name that ends in PUBLIC_KEY, so MY_PUBLIC_API_KEY is
// masked (an API key named public is not known to be one; D24, the safe
// side).
var pairWords = []string{"token", "tokens", "pass", "passes", "pwd", "pwds", "credential", "credentials", "key", "keys"}

// Pair reports whether a pair named name holds a credential in value, in
// the result of tool:
// the name holds a root of a key (HasRoot) or ends in pairWords, not
// excepted by Exceptions; or the value is a URL with a password in
// it (postgres://app:…@db), whatever the name (DATABASE_URL) — a URL with a
// password is masked before the exceptions of Exceptions are
// asked, so no exception keeps it.
func Pair(tool, name string, value any) bool {
	if s, ok := value.(string); ok {
		if _, has := URLPassword(s); has {
			return true
		}
	}
	if Excepted(tool, name, value) {
		return false
	}
	if HasRoot(name) {
		return true
	}
	words := Words(name)
	if len(words) == 0 || !slices.Contains(pairWords, words[len(words)-1]) {
		return false
	}
	return len(words) < 2 || words[len(words)-2] != "public"
}

// URLPassword is the password in the userinfo of s, when s is an address
// with one (postgres://app:…@db): the part of the address that is a
// credential, whatever key holds it.
func URLPassword(s string) (string, bool) {
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return "", false
	}
	return u.User.Password()
}
