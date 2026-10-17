package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// checkedArgs refuses, before the handler, an argument the tool's schema
// does not declare, a scalar of another JSON type than the schema's (a
// handler read it as absent: confirmed:"true" made a preview, size_gb:"20"
// answered "size_gb is required" — release audit, R-09, A-013), and an
// enum value outside the schema's list (matched
// case-insensitively and passed on in the schema's spelling). Unknown
// arguments were silently ignored: destroy {"vm": "orphan-1"} — the name
// disk_attach uses — fell through to "delete the current deployment"
// (release QA, D-mcp 4); dns_zone_create {"type": "Private"} made a public
// zone (D-mcp 7). confirmed is taken by every tool.
func checkedArgs(name string, schema map[string]any, next Handler) Handler {
	props, _ := schema["properties"].(map[string]any)
	return func(ctx context.Context, args map[string]any) (string, bool) {
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
			takes := "no arguments"
			if len(known) > 0 {
				takes = strings.Join(known, ", ")
			}
			return errResult(clierrors.New(clierrors.CodeUsage,
				fmt.Sprintf("%s: unknown argument(s) %s", name, strings.Join(unknown, ", ")),
				fmt.Sprintf("%s takes: %s.", name, takes))), true
		}
		for k, v := range args {
			p, _ := props[k].(map[string]any)
			typ, _ := p["type"].(string)
			if v != nil && !scalarOfType(typ, v) {
				return errResult(clierrors.New(clierrors.CodeUsage,
					fmt.Sprintf("%s: %s must be %s, got %s", name, k, typeName(typ), describeValue(v)),
					fmt.Sprintf("Pass %s as a JSON %s (see the tool's inputSchema).", k, typ))), true
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
				if strings.EqualFold(es, s) {
					canon = es
				}
			}
			if canon == "" {
				return errResult(clierrors.New(clierrors.CodeUsage,
					fmt.Sprintf("%s: %s %q is not one of %s", name, k, s, strings.Join(allowed, ", ")),
					fmt.Sprintf("Pass %s as one of: %s.", k, strings.Join(allowed, ", ")))), true
			}
			args[k] = canon
		}
		return next(ctx, args)
	}
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
		return "an integer"
	case "boolean":
		return "a boolean (true or false)"
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
	kind := "a number"
	switch v.(type) {
	case string:
		kind = "a string"
	case bool:
		kind = "a boolean"
	case []any:
		kind = "an array"
	case map[string]any:
		kind = "an object"
	}
	return kind + " " + s
}
