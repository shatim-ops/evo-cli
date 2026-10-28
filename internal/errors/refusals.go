// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	stderrors "errors"
	"strings"
)

// Refusals are the refusals of one call found before anything was written:
// all of them come in one answer, not one per run (dogfood 28.10, axis 1
// of CLAUDE.md §6). The first, in the order of the checks, is the call's
// error — its code, exit code and hint, as when it was the only one —
// because errors.As finds it first.
type Refusals []error

func (r Refusals) Error() string {
	msgs := make([]string, len(r))
	for i, e := range r {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

func (r Refusals) Unwrap() []error { return r }

// Join is the refusals errs, nil ones dropped: nil for none, the error
// itself for one, Refusals for more.
func Join(errs ...error) error {
	var r Refusals
	for _, e := range errs {
		var more Refusals
		switch {
		case e == nil:
		case stderrors.As(e, &more):
			r = append(r, more...)
		default:
			r = append(r, e)
		}
	}
	switch len(r) {
	case 0:
		return nil
	case 1:
		return r[0]
	}
	return r
}

// All are the refusals err stands for: each of Refusals, else err alone.
func All(err error) []error {
	var r Refusals
	if stderrors.As(err, &r) {
		return r
	}
	return []error{err}
}
