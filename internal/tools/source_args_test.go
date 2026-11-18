// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"strings"
	"testing"
)

// notSources are the arguments of format cidr that name no source of a
// rule: the network of a subnet is its address range, required.
var notSources = map[string]string{
	"subnet_create cidr":      "the network of the subnet, not whom a rule lets in",
	"vpc_route_create subnet": "the destination of a route, not whom a rule lets in",
}

// An empty value of an argument that names whom a rule of a security
// group lets in — a network (cidr) or another group (remote_group) — is
// refused USAGE before the call, blank too: it was read as «not given»,
// and the rule opened the port to 0.0.0.0/0, with --yes and exit 0
// («Риск», 408; owner's decision of #2560). Every such argument of a
// changing tool is declared (Decl.Source).
func TestSourceArgs_EmptyRefusedAndDeclared(t *testing.T) {
	r := New(nil)
	checked := 0
	for name, d := range decls {
		tl, ok := r.Lookup(name)
		for _, arg := range d.Source {
			if !ok {
				t.Fatalf("%s: declared, not a tool", name)
			}
			checked++
			for _, v := range []string{"", "  "} {
				err := CheckArgs(tl, map[string]any{arg: v})
				if err == nil || !strings.Contains(err.Error(), arg) {
					t.Errorf("%s %s=%q: not refused USAGE before the call: %v", name, arg, v, err)
				}
			}
		}
	}
	if checked < 7 {
		t.Fatalf("only %d sources checked: the declarations lost them", checked)
	}
	for _, tl := range r.Tools() {
		if !tl.Mutating {
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for name, p := range props {
			pm, _ := p.(map[string]any)
			if pm["format"] != "cidr" && name != "remote_group" || notSources[tl.Name+" "+name] != "" {
				continue
			}
			if !slices.Contains(decls[tl.Name].Source, name) {
				t.Errorf("%s %s: names whom a rule lets in, not declared in Decl.Source", tl.Name, name)
			}
		}
	}
	for k := range notSources {
		tool, arg, _ := strings.Cut(k, " ")
		if tl, ok := r.Lookup(tool); !ok || tl.InputSchema["properties"].(map[string]any)[arg] == nil {
			t.Errorf("notSources names %s, no longer an argument: remove the line", k)
		}
	}
}
