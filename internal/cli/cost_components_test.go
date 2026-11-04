// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// A cost that names its parts (cost.by_component) says what each one is
// and adds up only what has a price (release plan 1.7, «Класс-инвариант
// цены превью»; #855, D75): a line of class priced has its monthly_rub
// above zero, a line of another class has no monthly_rub — a «0» there
// reads as money a model need not count — and has a note that says why;
// total_monthly_rub is the sum of the priced lines. Every tool that prices
// a call before it, on the parity world: the preview of a write and the
// answer of a plan alike — stack_plan creates nothing and its cost is not a
// preview's, so TestContract_EveryPreviewPricedOrListed does not see it.
func TestContract_EveryCostByComponentHoldsItsClasses(t *testing.T) {
	world := withParityWorld(t)
	fakeKubeBinaries(t)
	srv := parityServers(t)
	var bad []string
	found := map[string]bool{}
	for _, tl := range tools.New(nil).Tools() {
		// What prices a call before it: the preview of a write, and the
		// answer of a plan (CLI verb plan: stack_plan, deploy_plan,
		// k8s_plan) — a plan writes nothing, so it has no preview.
		if !tl.Mutating && tl.CLI.Verb != "plan" {
			continue
		}
		args, ok := parityArgs[tl.Name]
		if !ok {
			bad = append(bad, tl.Name+": no parityArgs — its cost is not checked")
			continue
		}
		a := map[string]any{}
		for k, v := range args {
			a[k] = v
		}
		done := func() {}
		if setup := paritySetup[tl.Name]; setup != nil {
			extra, d := setup(t, world)
			for k, v := range extra {
				a[k] = v
			}
			done = d
		}
		// An answer that is no object (logs print lines) has no cost.
		got, err := toolTextObject(stdioRaw(t, srv.stdio, tl.Name, a))
		done()
		if err != nil {
			continue
		}
		for _, c := range costsWithComponents(got) {
			found[tl.Name] = true
			bad = append(bad, componentProblems(tl.Name, c)...)
		}
	}
	if !found["stack_plan"] {
		t.Error("stack_plan: no cost.by_component on the parity world — the answer moved, update the test")
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// costsWithComponents are the maps under any key "cost" of v that have
// by_component, at any depth (an answer, its preview, a step of it).
func costsWithComponents(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				if c, ok := e.(map[string]any); ok && k == "cost" {
					if _, ok := c["by_component"]; ok {
						out = append(out, c)
					}
				}
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// componentProblems is how cost c of tool name breaks the classes of its
// lines.
func componentProblems(name string, c map[string]any) []string {
	var bad []string
	lines, ok := c["by_component"].([]any)
	if !ok {
		return []string{fmt.Sprintf("%s: by_component is not a list: %v", name, c["by_component"])}
	}
	var sum float64
	for _, l := range lines {
		m, _ := l.(map[string]any)
		rub, hasRub := m["monthly_rub"].(float64)
		_, rubKey := m["monthly_rub"]
		note, _ := m["note"].(string)
		switch m["class"] {
		case "priced":
			if !hasRub || rub <= 0 {
				bad = append(bad, fmt.Sprintf("%s: %v is priced without its monthly_rub above zero", name, m))
			}
			sum += rub
		case "zero_at_create", "paid_unpriced":
			if rubKey {
				bad = append(bad, fmt.Sprintf("%s: %v is %v, and has monthly_rub — a number reads as its price", name, m, m["class"]))
			}
			if strings.TrimSpace(note) == "" {
				bad = append(bad, fmt.Sprintf("%s: %v is %v without a note that says why", name, m, m["class"]))
			}
		default:
			bad = append(bad, fmt.Sprintf("%s: %v has no class of its price (priced, zero_at_create, paid_unpriced)", name, m))
		}
	}
	total, ok := c["total_monthly_rub"].(float64)
	if !ok {
		return append(bad, fmt.Sprintf("%s: cost without total_monthly_rub: %v", name, c))
	}
	if math.Abs(total-math.Round(sum*100)/100) > 0.011 {
		bad = append(bad, fmt.Sprintf("%s: total_monthly_rub %v, the priced lines sum to %.2f", name, total, sum))
	}
	return bad
}
