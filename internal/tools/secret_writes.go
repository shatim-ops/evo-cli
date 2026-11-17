// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// fieldSet are the keys of an object a surface may show; a key's own set,
// when not nil, the keys of the object (or of each object of the list) it
// holds. A key with no set keeps a scalar or a list of scalars only.
type fieldSet map[string]fieldSet

// anyKey in a set is any key of the object: the object is a free map of
// what the call itself set (labels), shown as its scalar values only — a
// value that is an object or a list is dropped. A list under such a key
// keeps its scalar elements only (freeScalars).
const anyKey = "*"

// scalarMap is the set of such a map.
var scalarMap = fieldSet{anyKey: nil}

// publicObject is what of the answer of a write with a secret input is
// public: the object the service answered, under field, with keys.
type publicObject struct {
	field string
	keys  fieldSet
}

// secretWrites are the tools with a secret input (CLISpec.SecretFiles):
// the service that took the secret may quote it back, in the text of a
// refusal or in a field of what it made. Their answer shows of the
// service's only the fields the specification of the object declares (the
// requests of scripts/api-contract-fields.json), and a refusal of the
// service by its class, not its text (Астра, full audit of 14.11, findings
// 02 and 03). TestSecretWrites_EveryToolWithASecretInput keeps the list
// closed.
var secretWrites = map[string]publicObject{
	"secret_create":      {field: "secret", keys: secretKeys},
	"secret_add_version": {field: "version", keys: secretVersionKeys},
	"pg_user_create":     {field: "user", keys: pgUserKeys},
	"cert_import":        {field: "certificate", keys: certificateKeys},
}

var (
	secretVersionKeys = fieldSet{"id": nil, "createdAt": nil, "state": nil}
	secretKeys        = fieldSet{"id": nil, "path": nil, "name": nil, "description": nil, "projectId": nil, "folderId": nil,
		"serviceInstanceId": nil, "kmsKeyId": nil, "labels": scalarMap, "createdAt": nil, "primary": secretVersionKeys, "versions": secretVersionKeys}
	pgRoleKeys = fieldSet{"name": nil}
	pgUserKeys = fieldSet{"id": nil, "name": nil, "cluster_id": nil, "status": nil, "created_at": nil,
		"granted_roles": pgRoleKeys, "supported_roles": pgRoleKeys}
	certificateVersionKeys = fieldSet{"id": nil, "certificateId": nil, "authorityKeyId": nil, "commonName": nil, "dnsNames": nil,
		"issuer": nil, "status": nil, "enabled": nil, "errorCode": nil, "privateKeyAlgorithm": nil, "createdAt": nil, "expiresAt": nil,
		"serverCertificate": nil, "intermediateCertificate": nil}
	certificateKeys = fieldSet{"id": nil, "name": nil, "description": nil, "type": nil, "status": nil, "errorCode": nil,
		"projectId": nil, "serviceInstanceId": nil, "kmsKeyId": nil, "labels": scalarMap, "createdAt": nil, "updatedAt": nil, "expiresAt": nil,
		"primary": certificateVersionKeys, "issueState": fieldSet{"current": certificateVersionKeys, "processing": certificateVersionKeys}}
)

// refusalKeys are the fields of a refusal of a secret write that are evo's,
// not the service's: its class, its status, what to do, which argument.
// hint and warning are kept as texts of evo only: a hint or a warning that
// quotes the service is a defect of the code that writes it, not of this
// list («Риск», conclusion 266).
var refusalKeys = map[string]bool{
	"status": true, "reason": true, "hint": true, "warning": true, "argument": true, "format": true,
	"access_denied": true, "http_status": true, apiStatusField: true,
}

// untrustedError reports an error answer whose text is a service's: its
// error or refusals marked untrusted (errAnswer, serviceText).
func untrustedError(m map[string]any) bool {
	marked, _ := m["_untrusted"].([]any)
	for _, f := range marked {
		if f == "error" || f == "refusals" {
			return true
		}
	}
	return false
}

// secretWriteAnswer is the answer of tool, a write with a secret input,
// as a surface shows it (outer): a refusal that may carry the service's
// text by its class and status, the CLI's error alike; a success with the
// public fields of its object. Any other tool's answer is as it is.
func secretWriteAnswer(ctx context.Context, tool, text string, isErr bool) string {
	o, ok := secretWrites[tool]
	if !ok {
		return text
	}
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return text
	}
	if !isErr {
		// The fields beside the object are evo's (status, and the id, the
		// state and the notes of a create found after a lost answer,
		// lostCreate): only the object is the service's whole answer.
		if obj, ok := m[o.field].(map[string]any); ok {
			m[o.field] = publicFields(obj, o.keys)
		}
		return toJSON(m)
	}
	reason, _ := m["reason"].(string)
	if _, answered := m[apiStatusField]; !answered && !untrustedError(m) {
		return text
	}
	status := 0
	if st, ok := m[apiStatusField].(float64); ok {
		status = int(st)
	}
	msg := i18n.Tf("%s refused (%s): the service's text is not shown, the call carried a secret",
		"Отказ %s (%s): текст сервиса не показан — вызов нёс секрет", tool, reason)
	if status != 0 {
		msg = i18n.Tf("%s refused (%s, HTTP %d): the service's text is not shown, the call carried a secret",
			"Отказ %s (%s, HTTP %d): текст сервиса не показан — вызов нёс секрет", tool, reason, status)
	}
	out := map[string]any{"error": msg}
	for k, v := range m {
		if refusalKeys[k] {
			out[k] = v
		}
	}
	if c := callerOf(ctx); c != nil && c.Err != nil {
		hint, _ := out["hint"].(string)
		c.Err = clierrors.New(reason, msg, hint)
	}
	return toJSON(out)
}

// publicFields is obj with the keys of keys only: an object or a list of
// objects under a key with a set of its own the same way, a value under a
// key with none only when it is no object.
func publicFields(obj map[string]any, keys fieldSet) map[string]any {
	out := make(map[string]any, len(keys))
	for k, sub := range keys {
		v, ok := obj[k]
		if !ok {
			continue
		}
		if kept, ok := publicValue(v, sub); ok {
			out[k] = kept
		}
	}
	return out
}

func publicValue(v any, keys fieldSet) (any, bool) {
	if _, free := keys[anyKey]; free {
		return freeScalars(v), true
	}
	switch x := v.(type) {
	case map[string]any:
		if keys == nil {
			return nil, false
		}
		return publicFields(x, keys), true
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			if kept, ok := publicValue(e, keys); ok {
				out = append(out, kept)
			}
		}
		return out, true
	default:
		return v, true
	}
}

// freeScalars is v under a key of a free map (anyKey): a map keeps its
// pairs whose value is a scalar, a list its scalar elements; an object or
// a list inside either is dropped, at any depth.
func freeScalars(v any) any {
	scalar := func(e any) bool {
		switch e.(type) {
		case map[string]any, []any:
			return false
		}
		return true
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if scalar(e) {
				out[k] = e
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			if scalar(e) {
				out = append(out, e)
			}
		}
		return out
	default:
		return v
	}
}

// secretTexts are string arguments that may hold a secret beside those of
// CLISpec.SecretFiles: a cloud-init template carries passwords and keys.
// evo shows them only as a summary (EchoedArgs), never as they are.
var secretTexts = map[string][]string{
	"image_create": {"user_data_template"},
}

// secretArgValues are the values of the arguments of the call of tool
// that hold a secret (CLISpec.SecretFiles, secretTexts).
func secretArgValues(tool string, args map[string]any) []string {
	names := append([]string(nil), secretTexts[tool]...)
	for k := range decls[tool].CLI.SecretFiles {
		names = append(names, k)
	}
	var out []string
	for _, k := range names {
		if v, _ := args[k].(string); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// publicArgText reports whether p is in the value of an argument of the
// call that is not secret: evo names those (a name, an id) in its own
// words, and the text holding p there is not the secret's echo.
func publicArgText(tool string, args map[string]any, p string) bool {
	for k, v := range args {
		s, _ := v.(string)
		if s == "" || decls[tool].CLI.SecretFiles[k] || slices.Contains(secretTexts[tool], k) {
			continue
		}
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// secretMask stands in an answer for the value of a secret argument.
const secretMask = "[the value of a secret argument]"

// withoutSecretArgs is the answer text of a call of tool without the
// values of its secret arguments: a service that echoes the body of the
// request in its refusal does not bring a password of the call back to
// the agent or the screen (Астра #2257). Each value is masked as it is,
// line by line for a text of several lines, and in the forms JSON escaping
// gives it inside the service's text (one level for each body it is
// nested in). A value too short to mask without masking evo's own words
// stays: a secret argument of a few characters is refused by its rules.
func withoutSecretArgs(tool string, args map[string]any, text string) string {
	values := secretArgValues(tool, args)
	if len(values) == 0 {
		return text
	}
	var parts []string
	for _, v := range values {
		parts = append(parts, v)
		for _, line := range strings.Split(v, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				parts = append(parts, line)
			}
		}
	}
	var pairs []string
	for _, p := range parts {
		if len(p) < secretMinMask || publicArgText(tool, args, p) {
			continue
		}
		s := p
		for level := 0; level < 4; level++ {
			pairs = append(pairs, s, secretMask)
			b, _ := json.Marshal(s)
			s = string(b[1 : len(b)-1])
		}
	}
	if len(pairs) == 0 {
		return text
	}
	var m any
	if json.Unmarshal([]byte(text), &m) != nil {
		return strings.NewReplacer(pairs...).Replace(text)
	}
	r := strings.NewReplacer(pairs...)
	return toJSON(maskStrings(m, r))
}

// secretMinMask is the shortest part of a secret masked in an answer.
const secretMinMask = 6

// maskStrings is v with every string in it passed through r.
func maskStrings(v any, r *strings.Replacer) any {
	switch x := v.(type) {
	case string:
		return r.Replace(x)
	case map[string]any:
		for k, e := range x {
			x[k] = maskStrings(e, r)
		}
	case []any:
		for i, e := range x {
			x[i] = maskStrings(e, r)
		}
	}
	return v
}
