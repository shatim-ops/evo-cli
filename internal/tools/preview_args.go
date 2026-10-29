// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// echoedArgs are the optional arguments a create sends as the call gives
// them and its preview names as they are (CLAUDE.md §6, axis 4: the
// preview names everything evo sends; release plan 1.6, «Превью называет
// заданные необязательные аргументы»). One point for them all, as
// namedProject is for the project, not a line in each handler. A secret
// never goes here: the preview shows the value as the call gives it.
var echoedArgs = map[string][]string{
	"cert_issue_le":    {"description"},
	"cert_import":      {"description"},
	"dns_zone_create":  {"description"},
	"image_create":     {"description", "display_name"},
	"kafka_create":     {"description"},
	"lb_create":        {"target_port"},
	"pg_create":        {"description"},
	"pg_db_create":     {"lc_collate", "lc_ctype"},
	"redis_create":     {"description"},
	"vpc_create":       {"description"},
	"vpc_route_create": {"description"},
}

// namedArgs is the handler of a tool of echoedArgs whose preview names
// the arguments of the list the call gives: in the params of the preview
// object where there is one (engine.CreatePreview), else beside the
// message, where the CLI shows every field. An argument the preview names
// already, under its own key, is left as it is. confirmation_required is
// the one status of a preview (gated): any other answer is not one.
func namedArgs(keys []string, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(ctx, args)
		var m map[string]any
		if !isErr || json.Unmarshal([]byte(text), &m) != nil || m["status"] != "confirmation_required" {
			return text, isErr
		}
		p, isPreview := m["preview"].(map[string]any)
		params, _ := p["params"].([]any)
		named := map[string]bool{}
		for _, prm := range params {
			if pm, ok := prm.(map[string]any); ok {
				named[fmt.Sprint(pm["key"])] = true
			}
		}
		changed := false
		for _, k := range keys {
			v, ok := args[k]
			if !ok || v == nil || v == "" || named[k] {
				continue
			}
			if isPreview {
				if _, has := p[k]; has {
					continue
				}
				params = append(params, map[string]any{"key": k, "value": v})
			} else {
				if _, has := m[k]; has {
					continue
				}
				m[k] = v
			}
			changed = true
		}
		if !changed {
			return text, isErr
		}
		if isPreview {
			p["params"] = params
		}
		return toJSON(m), isErr
	}
}
