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

// allIDArgs are the tools that declare arguments naming a resource by id,
// and those arguments.
func allIDArgs() iter.Seq2[string, []idArg] {
	return func(yield func(string, []idArg) bool) {
		for name, d := range decls {
			if d.IDArgs != nil && !yield(name, d.IDArgs) {
				return
			}
		}
	}
}

// allRefArgs are the tools that take a name for a reference, and those
// arguments.
func allRefArgs() iter.Seq2[string, []nameRef] {
	return func(yield func(string, []nameRef) bool) {
		for name, d := range decls {
			if d.RefArgs != nil && !yield(name, d.RefArgs) {
				return
			}
		}
	}
}

// allDeletes are the deletes and what they delete.
func allDeletes() iter.Seq2[string, deleteTarget] {
	return func(yield func(string, deleteTarget) bool) {
		for name, d := range decls {
			if d.Delete.arg != "" && !yield(name, d.Delete) {
				return
			}
		}
	}
}

// allCompletes are the tools that declare the list of an argument, and
// those lists.
func allCompletes() iter.Seq2[string, map[string]completion] {
	return func(yield func(string, map[string]completion) bool) {
		for name, d := range decls {
			if d.Completes != nil && !yield(name, d.Completes) {
				return
			}
		}
	}
}

// allNoCompletion are the tools whose arguments named as in valueRefs
// name something else, and why.
func allNoCompletion() iter.Seq2[string, map[string]string] {
	return func(yield func(string, map[string]string) bool) {
		for name, d := range decls {
			if d.NoCompletion != nil && !yield(name, d.NoCompletion) {
				return
			}
		}
	}
}
