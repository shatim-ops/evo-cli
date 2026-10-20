// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package artifact

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// The live API answers a fresh nextPageToken on every page (live run
// 18.10): ListRegistries must end the list on a page without new
// registries, not after pager.MaxPages.

func TestListRegistries_EndlessTokenOnLastPage(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		switch r.URL.Query().Get("pageToken") {
		case "":
			fmt.Fprintf(w, `{"registries":[{"id":"r1","name":"one"}],"nextPageToken":"t%d"}`, n)
		case "t1":
			fmt.Fprintf(w, `{"registries":[{"id":"r2","name":"two"}],"nextPageToken":"t%d"}`, n)
		default:
			fmt.Fprintf(w, `{"registries":[],"nextPageToken":"t%d"}`, n)
		}
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).ListRegistries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0]["id"] != "r1" || got[1]["id"] != "r2" {
		t.Fatalf("registries = %v, want r1 and r2", got)
	}
	if c := calls.Load(); c != 3 {
		t.Fatalf("%d requests, want 3: the empty page ends the list", c)
	}
}

// The token ignored: the first page again is not the end of the list but
// a list whose end is not known — an error after two requests (break r4;
// it was the end of the list before).
func TestListRegistries_TokenIgnoredIsError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		fmt.Fprintf(w, `{"registries":[{"id":"r1","name":"one"},{"id":"r2","name":"two"}],"nextPageToken":"t%d"}`, n)
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).ListRegistries(context.Background())
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError {
		t.Fatalf("registries = %v, err = %v; want an incomplete-list error", got, err)
	}
	if c := calls.Load(); c != 2 {
		t.Fatalf("%d requests, want 2", c)
	}
}
