// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pager

import (
	"errors"
	"net/http"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Повтор токена обрывает список. Это не конец выборки: ошибка, и запросов
// конечное число.
func TestBreakR4_R4_6_RepeatedTokenIsError(t *testing.T) {
	n := 0
	more, err := Upto(10, func(token string) (string, error) {
		n++
		return "page-a", nil
	})
	if err == nil {
		t.Fatalf("repeated token ended the list: more=%v after %d fetches", more, n)
	}
	if n > 3 {
		t.Fatalf("fetches = %d, the walk is not bounded", n)
	}
}

func TestObjectPageOptionalFieldsAndRawTypes(t *testing.T) {
	type row struct {
		ID string `json:"id"`
	}
	for _, tc := range []struct {
		name, body string
		rows       int
		next       string
	}{
		{"omitted", `{}`, 0, ""}, {"empty", `{"items":[],"cursor":""}`, 0, ""},
		{"intermediate", `{"items":[],"cursor":"more"}`, 0, "more"},
		{"typed rows", `{"items":[{"id":"one"}],"cursor":"more"}`, 1, "more"},
		{"empty body", "", -1, ""}, {"null page", `null`, -1, ""}, {"bare array", `[]`, -1, ""},
		{"null list", `{"items":null}`, -1, ""}, {"non-array", `{"items":{}}`, -1, ""},
		{"null row", `{"items":[{"id":"one"},null]}`, -1, ""}, {"scalar row", `{"items":[42]}`, -1, ""},
		{"invalid row field", `{"items":[{"id":9}]}`, -1, ""},
		{"null cursor", `{"items":[{"id":"one"}],"cursor":null}`, -1, ""}, {"numeric cursor", `{"cursor":1}`, -1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, cursor, err := ObjectPage[row]([]byte(tc.body), "items", "cursor")
			if tc.rows < 0 {
				if rows != nil || cursor != "" || clierrors.Reason(err) != clierrors.CodeAPIError {
					t.Fatalf("partial page: %v %q %v", rows, cursor, err)
				}
			} else if err != nil || len(rows) != tc.rows || cursor != tc.next || len(rows) > 0 && rows[0].ID != "one" {
				t.Fatalf("valid page: %v %q %v", rows, cursor, err)
			}
		})
	}
}

func TestPageStatusPreservesScopeOfNotFound(t *testing.T) {
	serviceErr := errors.New("service refusal")
	for _, tc := range []struct {
		status       int
		continuation bool
		outcome      string
	}{
		{http.StatusOK, false, "complete"}, {http.StatusOK, true, "complete"},
		{http.StatusPartialContent, false, "incomplete"}, {http.StatusNoContent, true, "incomplete"},
		{http.StatusNotFound, false, "service"}, {http.StatusNotFound, true, "incomplete"},
		{http.StatusForbidden, false, "service"}, {http.StatusForbidden, true, "service"},
	} {
		err := PageStatus(tc.status, []byte("body"), tc.continuation, func(status int, data []byte) error {
			if status != tc.status || string(data) != "body" {
				t.Fatal("service lost status or body")
			}
			return serviceErr
		})
		if tc.outcome == "complete" && err != nil || tc.outcome == "service" && !errors.Is(err, serviceErr) || tc.outcome == "incomplete" && clierrors.Reason(err) != clierrors.CodeAPIError {
			t.Errorf("HTTP %d continuation=%t: %v", tc.status, tc.continuation, err)
		}
	}
}

func TestObjectPageAliases(t *testing.T) {
	alias := PageFields{Items: "service_accounts", Token: "next_page_token"}
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"service_accounts":[{"id":"one"}],"next_page_token":"more"}`, true},
		{`{"serviceAccounts":[{"id":"one"}],"next_page_token":"more"}`, true},
		{`{"serviceAccounts":[],"service_accounts":[{"id":"one"}]}`, false},
		{`{"nextPageToken":"","next_page_token":"more"}`, false},
	} {
		rows, next, err := ObjectPage[map[string]any]([]byte(tc.body), "serviceAccounts", "nextPageToken", alias)
		if tc.valid {
			if err != nil || len(rows) != 1 || rows[0]["id"] != "one" || next != "more" {
				t.Fatalf("valid aliases: %v %q %v", rows, next, err)
			}
		} else if err == nil || rows != nil || next != "" {
			t.Fatalf("ambiguous aliases: %v %q %v", rows, next, err)
		}
	}
}
