// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
	"unicode"
)

// redacted replaces a credential in a tool result over HTTP.
const redacted = "[redacted over HTTP: start the server with --allow-secret-export]"

// redactedField is the field a masked result gets: the paths of the masked
// fields (names, never values), so the agent knows it does not see all.
const redactedField = "redacted"

// SecretField is a root of a credential field name, masked in tool results
// over HTTP without --allow-secret-export (H7). The match ignores case, "_"
// and "-": password, Password and PASSWORD are one root, as are private_key
// and privateKey.
type SecretField struct {
	// Name is the root, as docs/mcp.md names it.
	Name string
	// Exact: the key is the root itself and nothing more (token: not
	// next_page_token). Otherwise a key that contains the root anywhere
	// (new_password_confirm, passwords, private_key_2) is a credential,
	// unless HTTPNonSecretFields says it is not.
	Exact bool
	// Where says which results carry it, or why a cloud object might.
	Where string
}

// HTTPSecretFields are the roots. Masking holds for every tool's result:
// about thirty tools (pg_get, redis_get, kafka_get, k8s_get, secret_get,
// cert_get, …) return the cloud's object as the API sent it, and a field the
// API adds tomorrow must not leave the host. A key with a root anywhere is
// masked; on a shared server a leak costs more than a mask too many, so the
// exceptions are a closed list of their own (HTTPNonSecretFields).
var HTTPSecretFields = []SecretField{
	{Name: "password", Where: "deploy_apply (the generated VM password, shown once); users and clusters from pg_*, redis_*, kafka_* if the API sends them; image metadata (admin_password, linux_password)"},
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
}

// NonSecretField is one entry of the closed list of keys that hold a root of
// HTTPSecretFields but not a credential.
type NonSecretField struct {
	// Name is the key or, with Suffix, the key's last words, as docs/mcp.md
	// names it ("_expires_at").
	Name string
	// Suffix: the key ends in these words, at a word boundary ("_", "-" or
	// a capital letter): password_expires_at and passwordExpiresAt, not
	// password_expires_at_raw and not passwordexpiresat. Otherwise the whole
	// key, compared without case, "_" and "-".
	Suffix bool
	// Tools limits the entry to these tools' results; nil: every tool.
	// Container: the key is not a credential while it holds an object or a
	// list of objects (whose own keys are checked as usual); a string under
	// it is still masked.
	Tools     []string
	Container bool
	Why       string
}

// HTTPNonSecretFields is the closed list of exceptions.
var HTTPNonSecretFields = []NonSecretField{
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

// normKey lowercases a key and drops "_" and "-".
func normKey(k string) string {
	return strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(k))
}

// keyWords splits a key into lowercase words at "_", "-" and a capital
// letter after a lowercase one or a digit: passwordExpiresAt and
// password_expires_at are both password, expires, at.
func keyWords(k string) []string {
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
		case r == '_' || r == '-':
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

// nonSecret reports whether HTTPNonSecretFields excepts key k (holding v) in
// the result of tool.
func nonSecret(tool, k string, v any) bool {
	words := keyWords(k)
	for _, f := range HTTPNonSecretFields {
		if f.Tools != nil && !slices.Contains(f.Tools, tool) {
			continue
		}
		if f.Container && !holdsObjects(v) {
			continue
		}
		if !f.Suffix {
			if normKey(k) == normKey(f.Name) {
				return true
			}
			continue
		}
		suf := keyWords(f.Name)
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

// hasRoot reports whether key k holds a root of HTTPSecretFields.
func hasRoot(k string) bool {
	n := normKey(k)
	for _, f := range HTTPSecretFields {
		root := normKey(f.Name)
		if f.Exact && n == root || !f.Exact && strings.Contains(n, root) {
			return true
		}
	}
	return false
}

// redactSecrets masks the credentials anywhere in a JSON result of tool: a
// non-empty string under a credential key, and every string inside a list or
// an object under one (passwords: ["a", "b"]). When the result is an
// object, it lists the masked fields in its "redacted" field (sorted, array
// elements as "[]": "users[].password"). Text that is not JSON is returned
// as is, and so is a result with nothing to mask.
func redactSecrets(tool, text string) string {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber() // keep large IDs and amounts as the tool wrote them
	var v any
	if dec.Decode(&v) != nil {
		return text
	}
	if _, err := dec.Token(); err != io.EOF {
		return text // not a single JSON value
	}
	paths := map[string]bool{}
	redactValue(tool, v, "", paths)
	if len(paths) == 0 {
		return text
	}
	if m, ok := v.(map[string]any); ok {
		m[redactedField] = redactedList(m[redactedField], paths)
	}
	out, err := json.MarshalIndent(v, "", "  ") // as the tools write results
	if err != nil {
		return text
	}
	return string(out)
}

// redactedList merges the masked paths with a "redacted" list the result
// already has (a tool answer that embeds another tool's masked answer, as
// stack_apply's in-process calls do). Any other value of the field is
// replaced.
func redactedList(prev any, paths map[string]bool) []string {
	if list, ok := prev.([]any); ok {
		for _, x := range list {
			if s, ok := x.(string); ok {
				paths[s] = true
			}
		}
	}
	out := make([]string, 0, len(paths))
	for p := range paths {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// redactValue masks in place and records the path of each masked field.
func redactValue(tool string, v any, path string, paths map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if hasRoot(k) && !nonSecret(tool, k, x) {
				t[k] = maskAll(x, p, paths)
				continue
			}
			redactValue(tool, x, p, paths)
		}
	case []any:
		for _, x := range t {
			redactValue(tool, x, path+"[]", paths)
		}
	}
}

// maskAll masks every non-empty string in v, the value of a credential key.
func maskAll(v any, path string, paths map[string]bool) any {
	switch t := v.(type) {
	case string:
		if t != "" {
			paths[path] = true
			return redacted
		}
	case map[string]any:
		for k, x := range t {
			t[k] = maskAll(x, path+"."+k, paths)
		}
	case []any:
		for i, x := range t {
			t[i] = maskAll(x, path+"[]", paths)
		}
	}
	return v
}
