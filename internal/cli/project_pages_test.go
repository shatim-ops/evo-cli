// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
