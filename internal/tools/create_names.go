// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The name of a new resource has one rule (owner's decision 23.10, D30): a
// create whose service states a rule checks that one in its handler; every
// other create gets plainName before its handler — a name blank once
// trimmed, with a space at either end, a control character, or over 255
// characters is USAGE before the cloud, confirmed or not. "web " passed by
// a taken "web" and made a second resource (Cursor 1.2.3: lb_create; the
// creates without a rule of their own alike).
// TestCreateNames_EveryCreateDeclared keeps the list closed.

// createName is what names the resource a create makes.
type createName struct {
	args []string // the arguments that name the new resource
	// rule is the service's own rule for the name, which the handler
	// checks before the cloud (stricter than plainName); "" for
	// plainName, which the registry checks before the handler.
	rule       string
	what, whRU string
}

// checkedCreateName refuses, before the handler, a name of a new resource
// that plainName refuses, for a create without a rule of its own (D30). An
// argument not given is the handler's: required, or a default name.
func checkedCreateName(name string, next Handler) Handler {
	c, ok := createNameOf(name)
	if !ok || c.rule != "" {
		return next
	}
	return func(ctx context.Context, args map[string]any) (string, bool) {
		for _, a := range c.args {
			if v, ok := args[a].(string); ok && v != "" {
				if err := plainName(i18n.T(c.what, c.whRU), a, v); err != nil {
					return errResult(err), true
				}
			}
		}
		return next(ctx, args)
	}
}
