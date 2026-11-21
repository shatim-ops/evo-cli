// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package vpc

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

func TestRouteReconciliationTotals(t *testing.T) {
	for _, tc := range []struct {
		name, first, last string
		valid             bool
	}{
		{"consistent", `"totalCount":2`, `"totalCount":2`, true},
		{"omitted", "", "", true},
		{"first_page_only", `"totalCount":2`, "", true},
		{"last_page_only", "", `"totalCount":2`, true},
		{"too_few_rows", `"totalCount":3`, "", false},
		{"too_many_rows", "", `"totalCount":0`, false},
		{"changed", `"totalCount":3`, `"totalCount":2`, false},
		{"legacy_conflict", "", `"totalCount":3,"total":2`, false},
		{"legacy_agrees", "", `"totalCount":2,"total":2`, true},
		{"legacy_conflict_across_pages", `"total":3`, `"totalCount":2`, false},
		{"null", "", `"totalCount":null`, false},
		{"noninteger", "", `"totalCount":2.5`, false},
		{"negative", "", `"totalCount":-1`, false},
		{"string", "", `"totalCount":"2"`, false},
		{"uint32_overflow", "", `"totalCount":4294967296`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/vpcs/parent/routes/static" {
					t.Errorf("unexpected %s %s", r.Method, r.URL)
					http.NotFound(w, r)
					return
				}
				count, id, next := tc.first, "one", `,"nextPageToken":"two"`
				switch r.URL.Query().Get("pageToken") {
				case "":
				case "two":
					count, id, next = tc.last, "two", ""
				default:
					t.Errorf("unexpected page token: %s", r.URL)
					http.NotFound(w, r)
					return
				}
				if count != "" {
					count = "," + count
				}
				fmt.Fprintf(w, `{"routes":[{"id":%q,"subnet":"192.0.2.0/24","vpcId":"parent","projectId":"project"}]%s%s}`, id, next, count)
			}))
			defer srv.Close()
			rows, err := New("token", "project", srv.URL).ListRoutes(pager.ForReconciliation(t.Context()), "parent")
			if tc.valid {
				if err != nil || len(rows) != 2 {
					t.Fatalf("rows=%v err=%v", rows, err)
				}
			} else if !errors.Is(err, &clierrors.CLIError{Code: clierrors.CodeAPIError}) || rows != nil {
				t.Fatalf("incomplete evidence returned rows=%v err=%v", rows, err)
			}
		})
	}
}
