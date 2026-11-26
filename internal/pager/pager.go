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
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"

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

// AllUntilEmpty reads as All does, at most max pages, and a page without
// items ends the list whatever token it names — the first page too: an
// empty list. Only for the Organization API (/v3/customers, /v3/projects),
// which past its last item answers empty pages, each with a new token
// (acceptance B-5, 06.11; IAM-10). Another API's empty page may stand
// before the rest of its list, and ending there would cut it with exit 0,
// so All does not stop at one (TestPager_AllUntilEmptyOnlyForTheOrganizationAPI
// holds every call to the Organization API). fetch returns the number of
// items of its page too.
func AllUntilEmpty(what string, max int, fetch func(token string) (next string, items int, err error)) error {
	more, err := Upto(max, func(token string) (string, error) {
		next, items, err := fetch(token)
		if err != nil || items == 0 {
			return "", err
		}
		return next, nil
	})
	if err != nil {
		return Named(what, err)
	}
	if more {
		return Named(what, TooManyPages(max))
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

// UnpagedObjects reads an endpoint whose API returns all rows in one
// response. An unexpected continuation means the response is not the list.
func UnpagedObjects[T any](data []byte, field string) ([]T, error) {
	items, next, err := ObjectPage[T](data, field, "nextPageToken", PageFields{Token: "next_page_token"})
	if err != nil {
		return nil, err
	}
	if next != "" {
		return nil, Incomplete(i18n.T("the endpoint returned a continuation for a single-page list", "API вернул продолжение для списка без поддержки страниц"))
	}
	return items, nil
}

// ItemProgress records the stable identities encountered on successive pages.
type ItemProgress struct {
	seen      map[string]bool
	lastToken string
}

// CheckProgress rejects a nonempty page consisting entirely of identities
// already seen. A row with no known identity cannot prove a repeat.
func CheckProgress[T any](p *ItemProgress, items []T, next string, key func(T) string) error {
	if next != "" && next == p.lastToken {
		return RepeatedToken(next)
	}
	p.lastToken = next
	if p.seen == nil {
		p.seen = map[string]bool{}
	}
	fresh := false
	for _, item := range items {
		id := key(item)
		if id == "" || !p.seen[id] {
			fresh = true
		}
		if id != "" {
			p.seen[id] = true
		}
	}
	if len(items) != 0 && !fresh {
		return NoNewItems(len(p.seen), -1)
	}
	return nil
}

// ObjectID identifies a service resource by its string id on a list page.
func ObjectID(row map[string]any) string {
	id, _ := row["id"].(string)
	return id
}

// Named puts the name of the list in front of an error of this package
// ("vpc: subnets: "), keeping its code and hint. Any other error (the
// page's own: an API status, a body that does not parse) is returned as it
// is.
func Named(what string, err error) error {
	if what == "" || !stderrors.Is(err, errIncomplete) {
		return err
	}
	if _, direct := err.(*clierrors.CLIError); direct {
		return clierrors.Prefix(what+": ", err)
	}
	// Prefix on the inner CLIError would discard the service's parse context.
	return fmt.Errorf("%s: %w", what, err)
}

// errIncomplete is the cause of every error of this package: Named knows
// them by it.
var errIncomplete = stderrors.New("the list is not complete")

// Incomplete is the error of a list read only in part: API_ERROR, as
// billing answered a broken page sequence before (QA-2).
func Incomplete(why string) *clierrors.CLIError {
	return clierrors.Wrap(clierrors.CodeAPIError,
		i18n.Tf("the list is not complete: %s", "список не полон: %s", why),
		i18n.T("Cloud.ru answered a broken page sequence, and evo does not act on a part of a list. Retry later; if it repeats, report it to Cloud.ru support.",
			"Cloud.ru вернул сломанную последовательность страниц, а по части списка evo не действует. Повторите позже; если повторится — сообщите в поддержку Cloud.ru."),
		errIncomplete)
}

// RepeatedToken: the answer names a page token already followed.
func RepeatedToken(token string) *clierrors.CLIError {
	return Incomplete(i18n.Tf("the answer repeats page token %q", "ответ повторяет токен страницы %q", token))
}

// TooManyPages: max pages were read and the list still goes on.
func TooManyPages(max int) *clierrors.CLIError {
	e := Incomplete(i18n.Tf("more than %d pages", "больше %d страниц", max))
	e.Hint = i18n.T("Narrow the list (another project, a filter, a shorter period); if the list cannot be that long, Cloud.ru answers a page sequence without an end — report it to Cloud.ru support.",
		"Сузьте список (другой проект, фильтр, период короче); если список не может быть таким длинным, Cloud.ru отдаёт страницы без конца — сообщите в поддержку Cloud.ru.")
	return e
}

// EmptyPage: an empty page while read is still below the total the
// answer gives.
func EmptyPage(read, total int) *clierrors.CLIError {
	return Incomplete(i18n.Tf("an empty page after %d of %d items", "пустая страница после %d из %d объектов", read, total))
}

// NoNewItems: a page holds only items already read, the server ignores
// the offset or the page token. total < 0 is a list without a total.
func NoNewItems(read, total int) *clierrors.CLIError {
	if total < 0 {
		return Incomplete(i18n.Tf("a page repeats items already read (%d), the server ignores the offset or the page token",
			"страница повторяет уже прочитанные объекты (%d): сервер игнорирует offset или токен страницы", read))
	}
	return Incomplete(i18n.Tf("a page repeats items already read (%d of %d), the server ignores the offset or the page token",
		"страница повторяет уже прочитанные объекты (%d из %d): сервер игнорирует offset или токен страницы", read, total))
}

// PageStatus accepts a complete page only at HTTP 200. On a continuation,
// 404 is a broken page sequence rather than evidence of resource absence.
// The service keeps its own error classification for all other refusals.
func PageStatus(status int, data []byte, continuation bool, serviceError func(int, []byte) error) error {
	if status == http.StatusOK {
		return nil
	}
	if (status >= 200 && status < 300) || (continuation && status == http.StatusNotFound) {
		return Incomplete(i18n.Tf("expected a complete page (status 200), got status %d", "ожидалась полная страница (код 200), получен код %d", status))
	}
	return serviceError(status, data)
}

// PageFields names alternative spellings accepted by a service.
type PageFields struct {
	Items string
	Token string
}

// ObjectPage decodes a protobuf JSON list with an optional array of objects
// and optional string cursor. A field that is present must have its declared
// type. Pass an empty tokenField for an endpoint without a cursor.
func ObjectPage[T any](data []byte, field, tokenField string, aliases ...PageFields) ([]T, string, error) {
	var page map[string]json.RawMessage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, "", Incomplete(i18n.T("expected a JSON page object", "ожидался JSON-объект страницы"))
	}
	if page == nil {
		return nil, "", Incomplete(i18n.T("null page", "страница равна null"))
	}
	for _, alias := range aliases {
		var err error
		if field, err = pageField(page, field, alias.Items); err != nil {
			return nil, "", err
		}
		if tokenField, err = pageField(page, tokenField, alias.Token); err != nil {
			return nil, "", err
		}
	}
	var items []T
	if raw, ok := page[field]; ok {
		var err error
		if items, err = ObjectRows[T](raw, field); err != nil {
			return nil, "", err
		}
	}
	var next string
	if tokenField != "" {
		if raw, ok := page[tokenField]; ok {
			if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &next) != nil {
				return nil, "", Incomplete(i18n.Tf("%s must be a string", "%s должен быть строкой", tokenField))
			}
		}
	}
	return items, next, nil
}

func pageField(page map[string]json.RawMessage, primary, alias string) (string, error) {
	if alias == "" || alias == primary || page[alias] == nil {
		return primary, nil
	}
	if page[primary] != nil {
		return "", Incomplete(i18n.Tf("the page contains both %s and %s", "страница содержит одновременно %s и %s", primary, alias))
	}
	return alias, nil
}

// ObjectRows decodes an actual array and validates every item before
// returning it. It also serves endpoints that document a bare array.
func ObjectRows[T any](raw []byte, field string) ([]T, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
		return nil, Incomplete(i18n.Tf("%s must be an array of objects", "%s должен быть массивом объектов", field))
	}
	items := make([]T, len(rows))
	for i, row := range rows {
		if len(row) == 0 || row[0] != '{' {
			return nil, Incomplete(i18n.Tf("null %s entry or non-object item", "элемент %s равен null либо не является объектом", field))
		}
		if err := json.Unmarshal(row, &items[i]); err != nil {
			return nil, Incomplete(i18n.Tf("an item in %s has an invalid field type", "у элемента %s неверный тип поля", field))
		}
	}
	return items, nil
}
