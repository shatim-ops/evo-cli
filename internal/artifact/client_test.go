// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package artifact

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
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

func TestListRegistries_TokenIgnored(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		fmt.Fprintf(w, `{"registries":[{"id":"r1","name":"one"},{"id":"r2","name":"two"}],"nextPageToken":"t%d"}`, n)
	}))
	defer srv.Close()

	got, err := New("tok", "proj", srv.URL).ListRegistries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("registries = %v, want each once", got)
	}
	if c := calls.Load(); c != 2 {
		t.Fatalf("%d requests, want 2: the repeated first page ends the list", c)
	}
}
