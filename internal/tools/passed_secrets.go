// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/secretkey"
)

// passedHidden replaces a value evo passed on as a credential where the
// answer quotes it: the rest of the text — helm's error — stays.
const passedHidden = "[hidden: a secret value of the call]"

// hidePassed hides, in the answer of the tool and in the error it hands
// the CLI, every value of passed(args): a program evo runs (helm) may
// quote what it was given — its arguments, a values file — in its output
// and its errors (#2465).
func hidePassed(passed func(args map[string]any) []string, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(ctx, args)
		values := passed(args)
		if len(values) == 0 {
			return text, isErr
		}
		hide := func(s string) string {
			for _, v := range values {
				s = strings.ReplaceAll(s, v, passedHidden)
			}
			return s
		}
		if c := callerOf(ctx); c != nil && c.Err != nil {
			var ce *clierrors.CLIError
			switch {
			case errors.As(c.Err, &ce):
				hidden := *ce
				hidden.Message, hidden.Hint = hide(ce.Message), hide(ce.Hint)
				if ce.Details != nil {
					hidden.Details, _ = hideStrings(maps.Clone(ce.Details), hide).(map[string]any)
				}
				c.Err = &hidden
			case hide(c.Err.Error()) != c.Err.Error():
				c.Err = errors.New(hide(c.Err.Error()))
			}
		}
		var v any
		if json.Unmarshal([]byte(text), &v) != nil {
			return hide(text), isErr
		}
		return toJSON(hideStrings(v, hide)), isErr
	}
}

// hideStrings is v with hide applied to every string in it.
func hideStrings(v any, hide func(string) string) any {
	switch t := v.(type) {
	case string:
		return hide(t)
	case map[string]any:
		for k, x := range t {
			t[k] = hideStrings(x, hide)
		}
	case []any:
		for i, x := range t {
			t[i] = hideStrings(x, hide)
		}
	}
	return v
}

// helmPassedSecrets are the values of set and values that are credentials
// by the rule of internal/secretkey, the one the masking over HTTP asks:
// the password of an address (scheme://user:pass@host) under any key — the
// password only; a value under a key of a secret (auth.rootPassword,
// db.password); every value inside an object or a list under one. A secret
// under a key that names none is not known to be one, and is not hidden
// (the description of k8s_helm says so). Longest first, so a value inside
// another is not cut first.
func helmPassedSecrets(args map[string]any) []string {
	var out []string
	add := func(key, value string, underSecret bool) {
		if value == "" {
			return
		}
		if pw, ok := secretkey.URLPassword(value); ok {
			if pw != "" {
				out = append(out, pw)
			}
			return
		}
		if underSecret || secretkey.Pair("k8s_helm", key, value) {
			out = append(out, value)
		}
	}
	if sets, ok := args["set"].([]any); ok {
		for _, s := range sets {
			str, _ := s.(string)
			for _, a := range helmSetAssignments(str) {
				if k, v, ok := strings.Cut(a, "="); ok {
					add(lastKey(k), strings.ReplaceAll(v, `\,`, ","), false)
				}
			}
		}
	}
	if raw := stringArg(args, "values"); raw != "" {
		var doc any
		if yaml.Unmarshal([]byte(raw), &doc) == nil {
			walkValues("", doc, false, add)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return slices.Compact(out)
}

// walkValues hands add every scalar of a values document with the key it
// is under, and whether a key above it names a secret.
func walkValues(key string, v any, underSecret bool, add func(key, value string, underSecret bool)) {
	switch t := v.(type) {
	case map[string]any:
		secret := underSecret || key != "" && secretkey.Pair("k8s_helm", key, t)
		for k, x := range t {
			walkValues(k, x, secret, add)
		}
	case []any:
		secret := underSecret || key != "" && secretkey.Pair("k8s_helm", key, t)
		for _, x := range t {
			walkValues(key, x, secret, add)
		}
	case string:
		add(key, t, underSecret)
	case bool, nil:
		// A flag (password set or not) is no credential.
	default:
		add(key, fmt.Sprint(t), underSecret)
	}
}

// helmSetAssignments splits a --set argument into its assignments: helm
// takes a=1,b=2 as two, a comma escaped (\,) or inside braces ({x,y}, a
// list) as part of a value.
func helmSetAssignments(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth = max(depth-1, 0)
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// lastKey is the last part of a key path of --set (auth.rootPassword,
// users[0].password): the name of the value.
func lastKey(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		path = path[i+1:]
	}
	if i := strings.Index(path, "["); i >= 0 {
		path = path[:i]
	}
	return path
}
