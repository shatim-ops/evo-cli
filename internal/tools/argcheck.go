// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// checkedArgs refuses, before the handler, the arguments CheckArgs refuses.
func checkedArgs(name string, schema map[string]any, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		if err := checkArgs(name, schema, args); err != nil {
			return errResult(err), true
		}
		return next(ctx, args)
	}
}

// CheckArgs checks args against the schema of tool t, as every call of the
// tool does before its handler: the one check of the arguments of a tool,
// for MCP, 'evo tools call' and the commands of the CLI alike (spec.md
// REQ-8). It refuses an argument the schema does not declare, a scalar of
// another JSON type than the schema's (a handler read it as absent:
// confirmed:"true" made a preview, size_gb:"20" answered "size_gb is
// required" — release audit, R-09, A-013), and an enum value outside the
// schema's list (matched case-insensitively; args gets it in the schema's
// spelling). Unknown arguments were silently ignored: destroy
// {"vm": "orphan-1"} — the name disk_attach uses — fell through to "delete
// the current deployment" (release QA, D-mcp 4); dns_zone_create
// {"type": "Private"} made a public zone (D-mcp 7). confirmed is taken by
// every tool. Arrays and objects are not checked by type (docs/mcp.md): a
// handler takes them in more than one form ("80,443" for ports). The error
// is USAGE, naming the argument.
func CheckArgs(t Tool, args map[string]any) error {
	return checkArgs(t.Name, t.InputSchema, args)
}

func checkArgs(name string, schema map[string]any, args map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	var unknown []string
	for k := range args {
		// confirmed on a read-only tool is harmless: agents send it
		// after an approval they got for another call.
		if _, ok := props[k]; !ok && k != "confirmed" {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		known := make([]string, 0, len(props))
		for k := range props {
			known = append(known, k)
		}
		sort.Strings(known)
		hint := i18n.Tf("%s takes: no arguments.", "%s не принимает аргументов.", name)
		if len(known) > 0 {
			hint = i18n.Tf("%s takes: %s.", "Аргументы %s: %s.", name, strings.Join(known, ", "))
		}
		return clierrors.New(clierrors.CodeUsage,
			i18n.Tf("unknown argument(s) %s", "неизвестные аргументы: %s", strings.Join(unknown, ", ")),
			hint)
	}
	for k, v := range args {
		p, _ := props[k].(map[string]any)
		typ, _ := p["type"].(string)
		if v != nil && !scalarOfType(typ, v) {
			return clierrors.New(clierrors.CodeUsage,
				i18n.Tf("%s must be %s, got %s", "%s должен быть %s, получено: %s", k, typeName(typ), describeValue(v)),
				i18n.Tf("Pass %s as a JSON %s (see the tool's inputSchema).",
					"Передайте %s как JSON %s (см. inputSchema инструмента).", k, typ))
		}
	}
	for k, v := range args {
		p, _ := props[k].(map[string]any)
		enum, _ := p["enum"].([]any)
		s, isStr := v.(string)
		if len(enum) == 0 || !isStr || s == "" {
			continue
		}
		canon := ""
		var allowed []string
		for _, e := range enum {
			es, _ := e.(string)
			allowed = append(allowed, es)
			// Spaces around the value are not part of it (" public ").
			if strings.EqualFold(es, strings.TrimSpace(s)) {
				canon = es
			}
		}
		if canon == "" {
			return clierrors.New(clierrors.CodeUsage,
				i18n.Tf("%s %q is not one of %s", "%s %q не входит в список: %s", k, s, strings.Join(allowed, ", ")),
				i18n.Tf("Pass %s as one of: %s.", "Допустимые значения %s: %s.", k, strings.Join(allowed, ", ")))
		}
		args[k] = canon
	}
	return nil
}

// scalarOfType reports whether v fits a scalar schema type: boolean,
// integer, number or string. Arrays and objects are not checked: handlers
// take them in more than one form (a comma-separated string for an array,
// a JSON string for k8s_add_addon's config). Numbers come as float64 or
// json.Number from JSON, as Go integers from the stack orchestrator.
func scalarOfType(typ string, v any) bool {
	switch typ {
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "integer", "number":
		var f float64
		switch n := v.(type) {
		case float64:
			f = n
		case float32:
			f = float64(n)
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		case json.Number:
			var err error
			if f, err = n.Float64(); err != nil {
				return false
			}
		default:
			return false
		}
		return typ == "number" || f == math.Trunc(f)
	}
	return true
}

func typeName(typ string) string {
	switch typ {
	case "integer":
		return i18n.T("an integer", "целым числом")
	case "number":
		return i18n.T("a number", "числом")
	case "string":
		return i18n.T("a string", "строкой")
	case "boolean":
		return i18n.T("a boolean (true or false)", "логическим значением (true или false)")
	}
	return "a " + typ
}

// describeValue names the JSON type of v with the value, shortened.
func describeValue(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 40 {
		s = s[:40] + "…"
	}
	kind := i18n.T("a number", "число")
	switch v.(type) {
	case string:
		kind = i18n.T("a string", "строка")
	case bool:
		kind = i18n.T("a boolean", "логическое значение")
	case []any:
		kind = i18n.T("an array", "массив")
	case map[string]any:
		kind = i18n.T("an object", "объект")
	}
	return kind + " " + s
}
