// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// D57: a reference argument takes an unambiguous name. The name is matched
// without regard to case; two of the name — USAGE naming their ids; an id
// of the list, or a value no item is named, stays as it is (the tool's own
// checks refuse it, as before D57); a value of the UUID form is always an
// id and is not looked up (a subnet named like a UUID does not shadow one).
func TestRefByName(t *testing.T) {
	subs := []*provision.Subnet{
		{ID: "00000000-0000-4000-8000-0000000000a1", Name: "web"},
		{ID: "00000000-0000-4000-8000-0000000000a2", Name: "Dup"},
		{ID: "00000000-0000-4000-8000-0000000000a3", Name: "dup"},
		{ID: "00000000-0000-4000-8000-0000000000a4", Name: "00000000-0000-4000-8000-0000000000a1"},
	}
	id := func(s *provision.Subnet) string { return s.ID }
	name := func(s *provision.Subnet) string { return s.Name }
	for _, c := range []struct {
		ref, want, reason string
	}{
		{"WEB", "00000000-0000-4000-8000-0000000000a1", ""},
		{"00000000-0000-4000-8000-0000000000a1", "00000000-0000-4000-8000-0000000000a1", ""},
		{"00000000-0000-4000-8000-0000000000ff", "00000000-0000-4000-8000-0000000000ff", ""},
		{"dup", "", clierrors.CodeUsage},
		{"nope", "nope", ""},
		{"web-id-like", "web-id-like", ""},
		{"", "", ""},
	} {
		got, err := refByName(subs, c.ref, id, name, refSubnet)
		reason := ""
		if err != nil {
			reason, _, _ = errReason(err, nil)
		}
		if got != c.want || reason != c.reason {
			t.Errorf("%q: %q, %v; want %q, %s", c.ref, got, err, c.want, c.reason)
		}
	}
}

// D57: refArgs is the one declaration of an argument that takes a name —
// the tool has the argument, its description says so in both languages,
// and Tab offers names from the same list the name is looked up in.
func TestRefArgs_DeclarationsHold(t *testing.T) {
	r := New(nil)
	for tool, refs := range refArgs {
		tl, ok := r.Lookup(tool)
		if !ok {
			t.Errorf("refArgs names %s, which is no tool", tool)
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, ra := range refs {
			p, _ := props[ra.arg].(map[string]any)
			if p == nil {
				t.Errorf("refArgs[%s]: no argument %s", tool, ra.arg)
				continue
			}
			if d, _ := p["description"].(string); !strings.Contains(d, "UUID or name") {
				t.Errorf("%s.%s: the description does not say it takes a name: %q", tool, ra.arg, d)
			}
			if d := tl.ArgDescriptionsRU[ra.arg]; !strings.Contains(d, "UUID или имя") {
				t.Errorf("%s.%s: the RU description does not say it takes a name: %q", tool, ra.arg, d)
			}
			if _, uuid := p["format"]; uuid {
				t.Errorf("%s.%s: format %v refuses a name before the registry resolves it", tool, ra.arg, p["format"])
			}
			c, ok := r.completionOf(tool, ra.arg)
			if !ok || c.list != ra.kind.list || !c.byName {
				t.Errorf("%s.%s: completed from %+v (%v), want names and ids of %s", tool, ra.arg, c, ok, ra.kind.list)
			}
		}
	}
}
