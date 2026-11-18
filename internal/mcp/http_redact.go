// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/secretkey"
)

// redacted replaces a credential in a tool result over HTTP (httpWording);
// a --read-only server has its own (readOnlyWording).
const redacted = "[redacted over HTTP: start the server with --allow-secret-export]"

// redactedNoteField is the field a masked result gets beside redactedField:
// what the mask means and how to get the values (redactedNote).
const redactedNoteField = "redacted_note"

// redactedField is the field a masked result gets: the paths of the masked
// fields (names, never values), so the agent knows it does not see all.
const redactedField = "redacted"

// HTTPSecretFields are the roots of a credential key (secretkey.Roots) by
// the name docs/mcp.md gives them; HTTPNonSecretFields, the exceptions
// (secretkey.Exceptions).
var (
	HTTPSecretFields    = secretkey.Roots
	HTTPNonSecretFields = secretkey.Exceptions
)

// pairNames are the fields that name the value of a name/value pair, the
// form in which a cloud object carries a list of settings: the env of a
// container ({"name": "DB_PASSWORD", "value": …}: ca_get, ca_list, a pod
// spec), the parameters of a create preview, the metadata of a cloud
// object ({"key": …, "value": …}). The name is the value's key: a
// credential name makes the value a credential (Fable audit, REL12-2).
// The fields are compared as keys are (normKey), so a Go API's
// {"Name": …, "Value": …} is the same pair (B12-2); both are written
// normalised.
var pairNames = []string{"name", "key"}

// pairValue is the field that holds the value of a name/value pair.
const pairValue = "value"

// minEchoLen is the length, in characters, from which echoes looks for a
// masked value elsewhere in the result. A shorter value is a common word
// ("ok", "true", "debug", "admin") that unrelated fields hold too; it is
// still masked under its own key. Eight is the shortest password the usual
// policies accept (NIST SP 800-63B for a password a person chooses), and a
// generated password, a key or a token is longer (Fable audit, REL12-10).
const minEchoLen = 8

// redactSecrets masks the credentials anywhere in a JSON result of tool: a
// non-empty string or a number under a credential key, and every one inside
// a list or an object under one (passwords: ["a", "b"]); true and false stay,
// as a flag (password set or not) is no credential. The value of a
// name/value pair whose name is a credential key (pairNames) is masked the
// same way. A string that is itself a JSON object or array (nested) is
// masked as a part of the result. A string or a number anywhere else equal
// to a masked value of minEchoLen characters or more is masked too, so no
// value is left under a key without a root (Cursor review 21.10, RP-2,
// RP-3; B12-4). When the result is an object, its "redacted" field lists
// the masked paths (sorted, array elements as "[]": "users[].password").
// Text that is not JSON is returned as is, and so is a result with nothing
// to mask. w gives the mask.
func redactSecrets(tool, text string, w wording) string {
	v, ok := jsonValue(text)
	if !ok {
		return text
	}
	m := masking{tool: tool, mask: w.mask, paths: map[string]bool{}, values: map[string]bool{}}
	v = m.walk(v, "")
	if len(m.paths) == 0 {
		return text
	}
	v = m.echoes(v, "")
	if obj, ok := v.(map[string]any); ok {
		obj[redactedField] = m.list()
		obj[redactedNoteField] = w.redactedNote()
	}
	out, err := json.MarshalIndent(v, "", "  ") // as the tools write results
	if err != nil {
		return text
	}
	return string(out)
}

// jsonValue decodes text that is a single JSON value, numbers as
// json.Number (large IDs and amounts as the tool wrote them).
func jsonValue(text string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false // not a single JSON value
	}
	return v, true
}

// masking is one result's redaction: the mask, the paths of the masked
// fields, the values they held (for echoes) and the number of masks put
// (for nested).
type masking struct {
	tool, mask string
	paths      map[string]bool
	values     map[string]bool
	count      int
}

// nested is a string of a result that is itself a JSON object or array: a
// VM's user_data, an annotation (last-applied-configuration), a setting
// that holds JSON. Masking walks the JSON inside as a part of the result,
// its paths going on from the string's ("user_data.admin_password"), and
// writes it back as a string (MarshalJSON): as it was if nothing in it is
// masked (B12-3). Only JSON is looked into: base64 is not decoded.
type nested struct {
	text    string
	v       any
	changed bool
}

// nestedJSON returns s as nested if it is a JSON object or array.
func nestedJSON(s string) (*nested, bool) {
	// A first character other than { or [ is no object or array: most
	// strings stop here, before a decoder.
	if t := strings.TrimLeft(s, " \t\r\n"); t == "" || t[0] != '{' && t[0] != '[' {
		return nil, false
	}
	v, ok := jsonValue(s)
	if !ok {
		return nil, false
	}
	return &nested{text: s, v: v}, true
}

// MarshalJSON writes n back as a string: the one it was, or its JSON with
// the masks if a mask was put in it.
func (n *nested) MarshalJSON() ([]byte, error) {
	if !n.changed {
		return json.Marshal(n.text)
	}
	b, err := json.Marshal(n.v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(b))
}

// into runs a pass of masking (walk, echoes) over the JSON inside n and
// notes whether the pass put a mask there.
func (m *masking) into(n *nested, pass func(any) any) {
	before := m.count
	n.v = pass(n.v)
	n.changed = n.changed || m.count > before
}

// list is the "redacted" field of a masked object: the masked paths. The
// field is the masking's: one the result already had is replaced, not
// merged — its strings are the tool's, not paths masking found, and a
// secret value in them left the host (RP-2). Paths inside the replaced
// field are not listed, as it is gone.
func (m *masking) list() []string {
	out := make([]string, 0, len(m.paths))
	for p := range m.paths {
		if p == redactedField || strings.HasPrefix(p, redactedField+".") || strings.HasPrefix(p, redactedField+"[]") {
			continue
		}
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// walk masks the value of every credential key in v, and of every
// name/value pair named so (all), in place and inside a string that is
// JSON (nested), and returns v.
func (m *masking) walk(v any, path string) any {
	switch t := v.(type) {
	case string:
		if n, ok := nestedJSON(t); ok {
			m.into(n, func(x any) any { return m.walk(x, path) })
			return n
		}
	case map[string]any:
		for _, k := range m.secretPair(t) {
			t[k] = m.all(t[k], keyPath(path, k))
		}
		for k, x := range t {
			p := keyPath(path, k)
			if secretkey.HasRoot(k) && !secretkey.Excepted(m.tool, k, x) {
				t[k] = m.all(x, p)
				continue
			}
			t[k] = m.walk(x, p)
		}
	case []any:
		for i, x := range t {
			t[i] = m.walk(x, path+"[]")
		}
	}
	return v
}

// secretPair returns the value fields (pairValue) of object o if it is a
// name/value pair (pairNames) that holds a credential (secretkey.Pair). The
// fields are matched by normKey; an object with both value and Value has
// both masked.
func (m *masking) secretPair(o map[string]any) []string {
	var names, values []string
	for k, x := range o {
		switch n := secretkey.Norm(k); {
		case n == pairValue:
			values = append(values, k)
		case slices.Contains(pairNames, n):
			if name, ok := x.(string); ok {
				names = append(names, name)
			}
		}
	}
	var out []string
	for _, k := range values {
		if slices.ContainsFunc(names, func(name string) bool { return secretkey.Pair(m.tool, name, o[k]) }) {
			out = append(out, k)
		}
	}
	return out
}

// all replaces every non-empty string and every number in v, the value of
// a credential key, with the mask, and records the path and the value.
func (m *masking) all(v any, path string) any {
	switch t := v.(type) {
	case string:
		if t != "" {
			return m.masked(t, path)
		}
	case json.Number:
		return m.masked(t.String(), path)
	case map[string]any:
		for k, x := range t {
			t[k] = m.all(x, keyPath(path, k))
		}
	case []any:
		for i, x := range t {
			t[i] = m.all(x, path+"[]")
		}
	}
	return v
}

// masked records a masked value and its path, and returns the mask. echoes
// looks only for a value of minEchoLen characters or more.
func (m *masking) masked(value, path string) string {
	m.count++
	m.paths[path] = true
	if utf8.RuneCountInString(value) >= minEchoLen {
		m.values[value] = true
	}
	return m.mask
}

// echoes masks, after walk, every string and number in v whose text is a
// recorded masked value (masked): a secret repeated under a key without a
// root, in the result or inside a string that is JSON (nested). A number is
// compared as the tool wrote it, so the password 123456789 masks the
// string "123456789" too, and the other way round (B12-4).
func (m *masking) echoes(v any, path string) any {
	switch t := v.(type) {
	case string:
		if m.values[t] {
			return m.masked(t, path)
		}
	case json.Number:
		if m.values[t.String()] {
			return m.masked(t.String(), path)
		}
	case *nested:
		if m.values[t.text] {
			return m.masked(t.text, path)
		}
		m.into(t, func(x any) any { return m.echoes(x, path) })
	case map[string]any:
		for k, x := range t {
			t[k] = m.echoes(x, keyPath(path, k))
		}
	case []any:
		for i, x := range t {
			t[i] = m.echoes(x, path+"[]")
		}
	}
	return v
}

// keyPath is the path of key k of the object at path ("" — the result).
func keyPath(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}
