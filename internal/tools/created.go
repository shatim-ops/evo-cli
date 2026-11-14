// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
)

// What a create made is named in one place (G13-14, Grok wave 2): where its
// answer gives the id, how to find it by name, and what deletes it. A
// synchronous stack step took the id from vm_id, id or path of the answer
// and wrote "ok" when none was there; snat_create answers nat_gateway.id,
// so stack_destroy got "ok" and the billed gateway stayed. lostCreate
// reads the id field of its answer and the list tool here as well.

// created is what names the resource a create makes. One of id, arg and
// none is set.
type created struct {
	// id is the field of the create's answer that holds the id the delete
	// takes, "a.b" for field b of object a.
	id string
	// lost is the field of lostCreate's answer that holds the id, when the
	// create's own answer was lost; "" for id.
	lost string
	// arg is the create's argument the delete takes instead of an id: a
	// secret is deleted by its path, an object by its key.
	arg string
	// none says why the create makes nothing a delete names.
	none string
	// list and field are the list tool and its array of resources, to find
	// the one named as the create's name: by stack steps (Decl.CreateName) and
	// in lostCreate's answer. listArgs are the create's arguments the list
	// takes too.
	list, field string
	listArgs    []string
	// delete is the tool that deletes it (deleteTargets); "" for none.
	delete string
}

// declared reports whether c is a declaration: one of id, arg and none
// is set.
func (c created) declared() bool { return c.id != "" || c.arg != "" || c.none != "" }

// lostField is the field of lostCreate's answer that holds the id.
func (c created) lostField() string {
	if c.lost != "" {
		return c.lost
	}
	return c.id
}

// createdID is the id of what tool made, as its answer res gives it; ""
// when the answer names none (an error, or a create with arg or none).
func createdID(tool string, res map[string]any) string {
	c := decls[tool].Created
	if c.id == "" {
		return ""
	}
	for _, path := range []string{c.id, c.lostField()} {
		v := any(res)
		for _, k := range strings.Split(path, ".") {
			m, _ := v.(map[string]any)
			v = m[k]
		}
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

// createdRef is what the delete of tool takes to delete what a successful
// call with args made: the id its answer res gives, or the argument that
// names it.
func createdRef(tool string, args, res map[string]any) string {
	if a := decls[tool].Created.arg; a != "" {
		return stringArg(args, a)
	}
	return createdID(tool, res)
}
