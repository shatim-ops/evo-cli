// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	stderrors "errors"
	"slices"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// An argument that names where a call works (scopeArgs: the project, the
// region, the zone) takes no empty value (D100): zone: "" moved the call to
// the zone of the profile, as an empty --zone did before D89 — the agent
// that built the argument from an empty variable wrote where it did not
// mean to. The rule is the schema's (minLength, D36), so every surface
// refuses alike, before the handler and any request; and the handler's own
// filters of a list (listSpec.own) follow the registry's (FB182-3): an
// empty one is not «no filter».
func TestScopeArgs_EmptyIsUsage(t *testing.T) {
	r := New(nil)
	scoped, own := 0, 0
	for _, tl := range r.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		names := map[string]string{}
		for _, a := range scopeArgs {
			names[a] = "scope"
		}
		for _, a := range listSpecs[tl.Name].own {
			names[a] = "own filter"
		}
		for arg, what := range names {
			p, ok := props[arg].(map[string]any)
			if !ok || p["type"] != "string" {
				continue
			}
			if what == "scope" {
				scoped++
			} else {
				own++
			}
			if p["minLength"] != 1 {
				t.Errorf("%s.%s (%s): the schema declares no minLength 1", tl.Name, arg, what)
			}
			for _, empty := range []string{"", "  "} {
				err := CheckArgs(tl, map[string]any{arg: empty})
				var re *ruleError
				var ce *clierrors.CLIError
				if !stderrors.As(err, &re) || re.Arg != arg || !stderrors.As(err, &ce) || ce.Code != clierrors.CodeUsage {
					t.Errorf("%s.%s = %q (%s): %v, want USAGE naming the argument", tl.Name, arg, empty, what, err)
					continue
				}
				if strings.Contains(ce.Hint, "filter") && what == "scope" && !slices.Contains(ListFilters(tl), arg) {
					t.Errorf("%s.%s: the hint speaks of a filter: %s", tl.Name, arg, ce.Hint)
				}
			}
		}
	}
	if scoped < 20 || own < 3 {
		t.Fatalf("checked %d scope arguments and %d own filters", scoped, own)
	}
}
