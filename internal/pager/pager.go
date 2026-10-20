// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package pager reads every page of a Cloud.ru list. A list answers a page
// and a next-page token (next_page_token or nextPageToken, as the
// service's specification names it); the next request sends the token back
// (page_token or pageToken). Reading only the first page lost resources
// from the list tools and let a create tool miss a taken name (#98).
//
// A list read only in part is an error, never a shorter list: evo deletes
// what is left over (destroy, cleanup), checks a name, counts quotas and
// money from it. The errors of this package say that the list is not
// complete and why; every client of a token or an offset list uses them
// (break r4, R4-2…R4-8).
package pager

import (
	stderrors "errors"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// MaxPages bounds a list that never ends. Past it the read fails rather
// than return a list cut short without saying so.
const MaxPages = 1000

// Option changes how Upto treats a page sequence.
type Option int

// StopOnRepeat makes a token already followed the end of the read instead
// of an error. Only for a read that is a tail and says itself when it is
// cut (containerapp.Logs: truncated); a list of resources never takes it.
const StopOnRepeat Option = 1

// All calls fetch with an empty token, then with each token fetch returns,
// until the token is empty. A token already followed is an error (the
// server would loop forever, and where the list ends is not known), and so
// is a list longer than MaxPages. fetch reads one page, keeps its items and
// returns the next-page token. what names the list in the error.
func All(what string, fetch func(token string) (next string, err error)) error {
	more, err := Upto(MaxPages, fetch)
	if err != nil {
		return Named(what, err)
	}
	if more {
		return Named(what, TooManyPages(MaxPages))
	}
	return nil
}

// Upto reads pages as All does, but at most max of them. more reports that
// max pages were read and the last one still named a new token: the caller
// decides whether a cut list is an error. A token already followed is
// RepeatedToken, unless StopOnRepeat is given.
func Upto(max int, fetch func(token string) (next string, err error), opts ...Option) (more bool, err error) {
	stopOnRepeat := false
	for _, o := range opts {
		if o == StopOnRepeat {
			stopOnRepeat = true
		}
	}
	seen := map[string]bool{}
	token := ""
	for page := 0; page < max; page++ {
		next, err := fetch(token)
		if err != nil {
			return false, err
		}
		if next == "" {
			return false, nil
		}
		if seen[next] {
			if stopOnRepeat {
				return false, nil
			}
			return false, RepeatedToken(next)
		}
		seen[next] = true
		token = next
	}
	return true, nil
}

// Named puts the name of the list in front of an error of this package
// ("vpc: subnets: "), keeping its code and hint. Any other error (the
// page's own: an API status, a body that does not parse) is returned as it
// is.
func Named(what string, err error) error {
	if what == "" || !stderrors.Is(err, errIncomplete) {
		return err
	}
	return clierrors.Prefix(what+": ", err)
}

// errIncomplete is the cause of every error of this package: Named knows
// them by it.
var errIncomplete = stderrors.New("the list is not complete")

// incomplete is the error of a list read only in part: API_ERROR, as
// billing answered a broken page sequence before (QA-2).
func incomplete(why string) *clierrors.CLIError {
	return clierrors.Wrap(clierrors.CodeAPIError,
		i18n.Tf("the list is not complete: %s", "список не полон: %s", why),
		i18n.T("Cloud.ru answered a broken page sequence, and evo does not act on a part of a list. Retry later; if it repeats, report it to Cloud.ru support.",
			"Cloud.ru вернул сломанную последовательность страниц, а по части списка evo не действует. Повторите позже; если повторится — сообщите в поддержку Cloud.ru."),
		errIncomplete)
}

// RepeatedToken: the answer names a page token already followed.
func RepeatedToken(token string) *clierrors.CLIError {
	return incomplete(i18n.Tf("the answer repeats page token %q", "ответ повторяет токен страницы %q", token))
}

// TooManyPages: max pages were read and the list still goes on.
func TooManyPages(max int) *clierrors.CLIError {
	e := incomplete(i18n.Tf("more than %d pages", "больше %d страниц", max))
	e.Hint = i18n.T("Narrow the list (another project, a filter, a shorter period); if the list cannot be that long, Cloud.ru answers a page sequence without an end — report it to Cloud.ru support.",
		"Сузьте список (другой проект, фильтр, период короче); если список не может быть таким длинным, Cloud.ru отдаёт страницы без конца — сообщите в поддержку Cloud.ru.")
	return e
}

// EmptyPage: an empty page while read is still below the total the
// answer gives.
func EmptyPage(read, total int) *clierrors.CLIError {
	return incomplete(i18n.Tf("an empty page after %d of %d items", "пустая страница после %d из %d объектов", read, total))
}

// NoNewItems: a page holds only items already read, the server ignores
// the offset or the page token. total < 0 is a list without a total.
func NoNewItems(read, total int) *clierrors.CLIError {
	if total < 0 {
		return incomplete(i18n.Tf("a page repeats items already read (%d), the server ignores the offset or the page token",
			"страница повторяет уже прочитанные объекты (%d): сервер игнорирует offset или токен страницы", read))
	}
	return incomplete(i18n.Tf("a page repeats items already read (%d of %d), the server ignores the offset or the page token",
		"страница повторяет уже прочитанные объекты (%d из %d): сервер игнорирует offset или токен страницы", read, total))
}
