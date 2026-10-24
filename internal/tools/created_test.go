// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"testing"
)

// Every create says what names what it makes (createdIDs): the field of
// its answer, the argument the delete takes, or why nothing; the delete is
// a declared one and takes what the create gives. Every create of a stack
// step is found by name and deleted by stack_destroy (G13-14).
func TestCreated_EveryCreateDeclared(t *testing.T) {
	r := New(nil)
	for _, tl := range r.Tools() {
		c, declared := createdIDs[tl.Name]
		if createLike(tl) != declared {
			t.Errorf("%s: makes a resource %v, in createdIDs %v", tl.Name, createLike(tl), declared)
			continue
		}
		if !declared {
			continue
		}
		forms := 0
		for _, set := range []bool{len(c.id) > 0, c.arg != "", c.none != ""} {
			if set {
				forms++
			}
		}
		if forms != 1 {
			t.Errorf("%s: one of id, arg and none is set, not %d", tl.Name, forms)
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, a := range append([]string{c.arg}, c.listArgs...) {
			if _, ok := props[a]; a != "" && !ok {
				t.Errorf("%s: createdIDs names argument %q, which its schema has not", tl.Name, a)
			}
		}
		if c.list != "" {
			if _, ok := r.Lookup(c.list); !ok || c.field == "" || len(createNames[tl.Name].args) == 0 {
				t.Errorf("%s: list %q is not a tool, names no field, or the create no name argument (createNames)", tl.Name, c.list)
			}
		}
		if c.delete == "" {
			continue
		}
		dt, ok := deleteTargets[c.delete]
		if !ok {
			t.Errorf("%s: delete %q is not in deleteTargets", tl.Name, c.delete)
			continue
		}
		if _, ok := props[dt.parent]; dt.parent != "" && !ok {
			t.Errorf("%s: %s lives in %q, which the create does not take", tl.Name, c.delete, dt.parent)
		}
	}
	for name := range createdIDs {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("createdIDs names %s, which is not a tool", name)
		}
	}

	all := planJob(t, `{"name":"s","vpc":{"name":"v"},"postgres":{"name":"p"},"kafka":{"name":"k"},"redis":{"name":"r"},"k8s":{"name":"k8"},"registry":{"name":"reg"},"snat":{"name":"sn"},"vm":{"name":"vm"},"nlb":{"name":"lb","port":80},"dns":{"zone_id":"z","name":"www"},"secrets":[{"path":"a","value":"b"}],"s3":[{"bucket":"b","key":"k","content":"c"}]}`)
	for _, st := range all.Steps {
		c := createdIDs[st.Tool]
		if !slices.Contains(stackTeardown, st.Tool) || c.delete == "" {
			t.Errorf("step %s: %s is not in stackTeardown or declares no delete", st.Key, st.Tool)
		}
		if _, ok := stepFinder(st); c.arg == "" && !ok {
			t.Errorf("step %s: %s is not found by name (createdIDs list)", st.Key, st.Tool)
		}
	}
}
