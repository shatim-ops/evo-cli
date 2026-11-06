// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// 'evo project list', configure and doctor read every page of
// GET /v3/projects (api.op.v3.yaml: next_page_token in the answer,
// page_token in the next request), not only the first (#98).
func TestListProjects_FollowsNextPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_token") == "" {
			_, _ = io.WriteString(w, `{"projects":[{"id":"p1","name":"one"}],"next_page_token":"n2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"projects":[{"id":"p2","name":"two"}]}`)
	}))
	defer srv.Close()
	old := orgProjectsURL
	orgProjectsURL = srv.URL + "/v3/projects"
	defer func() { orgProjectsURL = old }()

	got, err := listProjects(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ID != "p2" {
		t.Fatalf("projects = %+v", got)
	}
}

// cloud-e2e readonly, 18.10.2026: one dropped connection made
// 'evo project list' fail at once with API_UNREACHABLE. The GET is now
// repeated, as the service clients repeat theirs.
func TestListProjects_RetriesDroppedConnection(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close() // the connection drops before any answer
			}
			return
		}
		_, _ = io.WriteString(w, `{"projects":[{"id":"p1","name":"one"}]}`)
	}))
	defer srv.Close()
	oldURL, oldClient := orgProjectsURL, orgClient
	orgProjectsURL = srv.URL + "/v3/projects"
	orgClient = &http.Client{Timeout: 5 * time.Second, Transport: &httpretry.Transport{Backoff: time.Millisecond}}
	defer func() { orgProjectsURL, orgClient = oldURL, oldClient }()

	got, err := listProjects(context.Background(), "tok")
	if err != nil {
		t.Fatalf("dropped connection not repeated: %v", err)
	}
	if len(got) != 1 || calls.Load() != 2 {
		t.Fatalf("projects = %+v after %d calls", got, calls.Load())
	}
}

// FB160-3 (audit of Fable 1.6): the Organization API answers 404 to a key
// that cannot list projects, and D63 keeps that 404 as it is for this API
// (tools.OrgClient, Keep404). configure read the same list through a
// client of its own without it, and said «the route is not there». One
// client, one policy: the 404 of configure is a 404.
func TestListProjects_404IsKept(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	old := orgProjectsURL
	orgProjectsURL = srv.URL + "/v3/projects"
	defer func() { orgProjectsURL = old }()

	_, err := listProjects(context.Background(), "tok")
	if err == nil || strings.Contains(err.Error(), "without the service's answer") || !strings.Contains(err.Error(), "404") {
		t.Fatalf("a 404 of the Organization API: %v", err)
	}
}

// IAM-10 (acceptance B-5, 06.11): an empty page with a new token is the
// end of the list of the Organization API, for configure and doctor too.
func TestListProjects_EmptyPagesWithATokenEndTheList(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		tok := r.URL.Query().Get("page_token")
		if tok == "" {
			_, _ = io.WriteString(w, `{"projects":[{"id":"p1","name":"one"}],"next_page_token":"p"}`)
			return
		}
		_, _ = io.WriteString(w, `{"projects":[],"next_page_token":"`+tok+`p"}`)
	}))
	defer srv.Close()
	old := orgProjectsURL
	orgProjectsURL = srv.URL + "/v3/projects"
	defer func() { orgProjectsURL = old }()

	got, err := listProjects(context.Background(), "tok")
	if err != nil || len(got) != 1 || calls.Load() != 2 {
		t.Fatalf("projects %+v after %d requests, err %v; want p1 after 2", got, calls.Load(), err)
	}
}
