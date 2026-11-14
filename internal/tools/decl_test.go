// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "testing"

// Every declaration is of a tool the registry has: a declaration left
// after its tool was renamed or removed would say nothing of any tool,
// and the rule it carries would be lost without a failing test.
func TestDecl_NamesATool(t *testing.T) {
	r := New(nil)
	for name := range decls {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("decls names %q, which is not a tool", name)
		}
	}
}

// New hands every tool its declaration: the registry, the server and the
// invariants read Tool.Decl, not decls.
func TestDecl_EveryToolCarriesItsOwn(t *testing.T) {
	for _, tl := range New(nil).Tools() {
		d := decls[tl.Name]
		if tl.Decl.Group != d.Group || tl.Decl.Project != d.Project || tl.Decl.AlwaysListed != d.AlwaysListed || len(tl.Decl.RequiredOneOf) != len(d.RequiredOneOf) {
			t.Errorf("%s: Tool.Decl %+v is not its declaration %+v", tl.Name, tl.Decl, d)
		}
	}
}
