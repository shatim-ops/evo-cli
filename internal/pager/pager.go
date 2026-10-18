// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package pager reads every page of a Cloud.ru list. A list answers a page
// and a next-page token (next_page_token or nextPageToken, as the
// service's specification names it); the next request sends the token back
// (page_token or pageToken). Reading only the first page lost resources
// from the list tools and let a create tool miss a taken name (#98).
package pager

import "fmt"

// MaxPages bounds a list that never ends. Past it the read fails rather
// than return a list cut short without saying so.
const MaxPages = 1000

// All calls fetch with an empty token, then with each token fetch returns,
// until the token is empty or one already seen (a server that repeats a
// token would loop forever). fetch reads one page, keeps its items and
// returns the next-page token. what names the list in the error past
// MaxPages.
func All(what string, fetch func(token string) (next string, err error)) error {
	seen := map[string]bool{}
	token := ""
	for page := 0; page < MaxPages; page++ {
		next, err := fetch(token)
		if err != nil {
			return err
		}
		if next == "" || seen[next] {
			return nil
		}
		seen[next] = true
		token = next
	}
	return fmt.Errorf("%s: more than %d pages, the list is not complete", what, MaxPages)
}
