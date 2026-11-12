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

// evo's own next step never carries an empty scope argument: an agent that
// follows next_args to the letter would get USAGE from evo's own hint
// (conclusion 117 of «Риск», У1). The one point of every next step leaves
// such an argument out; any other argument goes as it is.
func TestScopeArgs_NextStepCarriesNoEmptyScope(t *testing.T) {
	answer := map[string]any{}
	nextStep(answer, "provision", map[string]any{
		"name": "web", "zone": "", "region": "  ", "project_id": "", "availability_zone_id": "",
		"image": "", "confirmed": true,
	})
	next, _ := answer["next_args"].(map[string]any)
	for _, a := range scopeArgs {
		if _, ok := next[a]; ok {
			t.Errorf("next_args carries the empty %s: %v", a, next)
		}
	}
	if next["name"] != "web" || next["confirmed"] != true || answer["next_tool"] != "provision" {
		t.Errorf("the next step lost an argument: %v", answer)
	}
	if _, ok := next["image"]; !ok {
		t.Errorf("an empty argument that is no scope was dropped: %v", next)
	}
	tl, _ := New(nil).Lookup("provision")
	if err := CheckArgs(tl, map[string]any{"name": "web", "confirmed": true}); err != nil {
		t.Errorf("the next step is not a call the tool takes: %v", err)
	}
}

// A composite call refuses an empty scope at its plan, before the question
// and before anything is made — not in the middle of stack_apply, with
// half a stack live (conclusion 117, У2): a manifest with an empty zone
// of one resource is USAGE of stack_plan naming the field, with no
// request; deploy_plan refuses its own empty zone by the schema.
func TestScopeArgs_CompositeRefusesAtThePlan(t *testing.T) {
	f := &apiFake{}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": map[string]any{
		"name": "shop", "vm": map[string]any{"name": "web", "zone": ""},
	}})
	if !isErr || m["reason"] != clierrors.CodeUsage || !strings.Contains(text, "manifest.vm.zone") {
		t.Errorf("stack_plan with an empty zone: %s", text)
	}
	if n := f.count(); n != 0 {
		t.Errorf("stack_plan sent %d requests before refusing", n)
	}
	m, text, isErr = callNamed(t, r, "deploy_plan", map[string]any{"product": "shop", "zone": ""})
	if !isErr || m["reason"] != clierrors.CodeUsage || m["argument"] != "zone" {
		t.Errorf("deploy_plan with an empty zone: %s", text)
	}
	if n := f.count(); n != 0 {
		t.Errorf("deploy_plan sent %d requests before refusing", n)
	}
}
