// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Астра, full audit 14.11, finding 08 (the regression pair of finding 04):
// a delete of one secret with force and without sent two different
// DELETEs after one and the same preview — the person agreed to one
// question, and which of the two went was not in it. A value the preview
// names is checked by TestPreview_NamesWhatTheWriteSends; a value a write
// leaves out is not a value to name, so this test compares the calls
// themselves. Every mutating tool of the world is called with its base
// arguments and with each optional boolean or enum argument given each of
// its values — the same target, one argument other: two calls whose
// confirmed writes differ have previews that differ. A value of another
// type is named by TestPreview_NamesEveryOptionalArgument, so its preview
// differs from the one that leaves it out.
func TestPreview_AnotherWriteIsAnotherPreview(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, _ := jsonCases(t)
	reg := newRegistry()
	done := map[string]bool{}
	var bad []string
	pairs := 0
	needed := map[string]bool{}
	for _, c := range cases {
		if !c.tool.Mutating || done[c.tool.Name] || c.state != "" {
			continue
		}
		done[c.tool.Name] = true
		tl, _ := reg.Lookup(c.tool.Name)
		call := func(args map[string]any, confirmed bool) (string, string) {
			args = copyArgs(args)
			end := func() {}
			if c.setup != nil {
				extra, d := c.setup(t, w)
				for k, v := range extra {
					args[k] = v
				}
				end = d
			}
			if confirmed {
				args["confirmed"] = true
			}
			fake.reset()
			newProcess()
			flagJSON, flagDryRun, flagYes = false, false, false
			text, _ := tl.Handler(context.Background(), args)
			writes := fake.takeWrites()
			for i := range writes {
				writes[i] = sameFIPReceipt(writes[i])
			}
			sort.Strings(writes)
			end()
			forgetRun(w)
			return text, strings.Join(writes, "\n")
		}
		type variant struct{ label, preview, writes string }
		run := func(label string, args map[string]any) variant {
			preview, _ := call(args, false)
			_, writes := call(args, true)
			return variant{label, preview, writes}
		}
		base := run("left out", c.args)
		props := schemaProps(c.tool)
		for _, a := range sortedKeys(props) {
			if _, given := c.args[a]; given || a == "confirmed" {
				continue
			}
			key := c.tool.Name + " " + a
			if _, out := optionalLeftOut[key]; out {
				continue
			}
			values := pairValues(props[a])
			if len(values) == 0 {
				continue
			}
			vs := []variant{base}
			for _, v := range values {
				args := copyArgs(c.args)
				args[a] = v
				vs = append(vs, run(fmt.Sprintf("%v", v), args))
			}
			for i := range vs {
				for j := i + 1; j < len(vs); j++ {
					x, y := vs[i], vs[j]
					if x.writes == "" || y.writes == "" || x.writes == y.writes {
						continue
					}
					pairs++
					if _, ok := pairSameConsent[key]; ok {
						needed[key] = true
						continue
					}
					if x.preview == y.preview {
						bad = append(bad, fmt.Sprintf("%s %s: %s and %s send different writes after the same preview — name the value the write takes in the preview", c.tool.Name, a, x.label, y.label))
					}
				}
			}
		}
	}
	for key, why := range pairSameConsent {
		if !needed[key] || why == "" {
			bad = append(bad, key+": in pairSameConsent, but its values send the same writes (or no why): remove the line")
		}
	}
	t.Logf("mutating tools: %d; pairs of one target with other writes: %d", len(done), pairs)
	if pairs == 0 {
		bad = append(bad, "no pair of other writes: the world does not reach the writes")
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
	w.rt.takeUnknown()
}

// pairSameConsent are the optional arguments whose values split the
// writes of one consent between calls, not change them: "tool argument"
// → why. A line whose values send the same writes fails the test.
var pairSameConsent = map[string]string{
	"provision wait": "false answers once the cloud took the create; the floating IP is attached by the repeat of next_args, which goes on with the same plan (D102)",
}

// pairValues are the values of an optional argument to call a tool with:
// both of a boolean, every one of an enum; none of another type.
func pairValues(raw any) []any {
	p, _ := raw.(map[string]any)
	if e, ok := p["enum"].([]any); ok && len(e) > 0 {
		return e
	}
	if p["type"] == "boolean" {
		return []any{true, false}
	}
	return nil
}
