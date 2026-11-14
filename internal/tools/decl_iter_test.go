// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "iter"

// The iterators over decls that only the invariants read: the product reads
// a declaration by its tool (createdOf and the like).

// allCreateNames are the creates and the rule of the name of what they
// make.
func allCreateNames() iter.Seq2[string, createName] {
	return func(yield func(string, createName) bool) {
		for name, d := range decls {
			if len(d.CreateName.args) > 0 && !yield(name, d.CreateName) {
				return
			}
		}
	}
}

// allNameless are the creates that make a resource whose name the call
// does not give, and why.
func allNameless() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for name, d := range decls {
			if d.Nameless != "" && !yield(name, d.Nameless) {
				return
			}
		}
	}
}
