// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"sort"
	"testing"
)

// Every create names in its record what it makes (gap П2 of the coverage
// of 1.9, audits/coverage-1.9.md; release plan 1.9, «Удержанная запись
// закрывается чужим ответом с той же целью»): a held record is closed by an
// answer of the call that names the same resource (settleHeld). Two calls
// of one tool with the same target and another name of what they make —
// a restore of one copy into disk-a and into disk-b — are two operations,
// and the answer of one must not close the record of the other.
// TestHeld_AnotherNameOfTheSameTargetDoesNotCloseTheRecord holds that for
// backup_restore in its world; this walk holds it for every tool that
// declares the arguments naming what it makes (Decl.CreateName): the
// resource of the record differs when only one of them differs.
//
// The other half of settleHeld — a record without a name closed by an
// answer that names its target — is the world of the point test above.
func TestHeld_EveryCreateNamesWhatItMakesInItsRecord(t *testing.T) {
	r := contractRegCfg(t, &contractRT{kind: "200"}, nil)
	ctx := context.Background()
	tools := make([]string, 0, len(decls))
	for name := range decls {
		tools = append(tools, name)
	}
	sort.Strings(tools)
	n := 0
	seen := map[string]bool{}
	defer func() {
		for name := range heldNameMisses {
			if !seen[name] {
				t.Errorf("heldNameMisses names %s, whose record names what it makes now: remove the line", name)
			}
		}
	}()
	for _, name := range tools {
		d := decls[name]
		if len(d.CreateName.args) == 0 {
			continue
		}
		base := map[string]any{}
		for _, a := range d.IDArgs {
			base[a.name] = qaID1
		}
		for _, arg := range d.CreateName.args {
			n++
			call := func(v string) map[string]any {
				args := map[string]any{}
				for k, x := range base {
					args[k] = x
				}
				args[arg] = v
				return args
			}
			ra, ta := r.named(ctx, name, call("evo-held-a"))
			rb, tb := r.named(ctx, name, call("evo-held-b"))
			if ra == rb && ta == tb {
				if heldNameMisses[name] != "" {
					seen[name] = true
					continue
				}
				t.Errorf("%s: calls with %s=evo-held-a and %s=evo-held-b name one resource (name %q, target %q): the answer of one closes the held record of the other", name, arg, arg, ra, ta)
			}
		}
	}
	if n == 0 {
		t.Fatal("no tool declares CreateName: the walk checks nothing")
	}
}

// heldNameMisses are creates whose record does not name what they make:
// tool → why. The walk found them when it was written. The list only
// shrinks: a line whose tool names its resource now fails the test.
// Closes by 1.9 (gap П2 of the coverage of 1.9, before rc.1).
var heldNameMisses = map[string]string{
	// The zone is named by domain, and intentResource reads name (or
	// Created.arg): two zones of one VPC are one record — a held record of
	// one closes on the answer of the other. The fix — the layer reads the
	// arguments of CreateName — is «разработка»'s (intent.go).
	"dns_zone_create": "the record reads name, the zone is named by domain",
}
