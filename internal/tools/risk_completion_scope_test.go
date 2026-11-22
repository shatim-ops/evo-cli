// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

func TestRiskCompletionCertificateScope(t *testing.T) {
	const userProject = "project-for-tab"
	row := fmt.Sprintf(`{"id":%q,"projectId":%q}`, resMine, userProject)
	for _, tc := range []struct {
		name, project, parent, list string
		status, lists, versions     int
	}{
		{"own", userProject, resMine, `{"certificates":[` + row + `]}`, 200, 1, 1},
		{"other-project-in-row", userProject, resMine, `{"certificates":[{"id":"` + resMine + `","projectId":"other-project"}]}`, 200, 1, 0},
		{"forbidden", userProject, resMine, `{}`, 403, 1, 0},
		{"valid-empty", userProject, resMine, `{"certificates":[]}`, 200, 1, 0},
		{"null-row", userProject, resMine, `{"certificates":[` + row + `,null]}`, 200, 1, 0},
		{"no-project", "", resMine, `{}`, 200, 0, 0},
		{"no-parent", userProject, "", `{}`, 200, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := completionCfg
			cfg.ProjectID = tc.project
			reg := completionRegistry(t, &zonesCloud{}, cfg)
			original := httpretry.DefaultBase
			t.Cleanup(func() { httpretry.DefaultBase = original })
			lists, versions := 0, 0
			httpretry.DefaultBase = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Host != "certificatemanager.api.cloud.ru" || req.Header.Get("Authorization") == "" {
					return nil, fmt.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
				}
				status, body := 200, ""
				switch req.URL.Path {
				case "/v1/certificate":
					lists++
					if req.URL.Query().Get("projectId") != userProject {
						t.Errorf("unexpected list project %q", req.URL.Query().Get("projectId"))
					}
					status, body = tc.status, tc.list
				case "/v1/certificate/" + resMine + "/versions":
					versions++
					body = `{"versions":[{"id":12}]}`
				default:
					t.Errorf("unexpected read: %s", req.URL.Path)
					return nil, fmt.Errorf("unexpected read")
				}
				return &http.Response{Request: req, StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			values := reg.CompleteFor(context.Background(), "cert_private_key", "version_id", "", tc.parent)
			var want []Candidate
			if tc.versions == 1 {
				want = []Candidate{{Value: "12"}}
			}
			if !slices.Equal(values, want) || lists != tc.lists || versions != tc.versions {
				t.Fatalf("suggestions %v; list GET=%d, versions GET=%d", values, lists, versions)
			}
		})
	}
}

func TestRiskCompletionS3ObjectsOnly(t *testing.T) {
	cfg := completionCfg
	cfg.S3.Endpoint = "https://objects.example.invalid"
	cfg.S3.Region = "us-east-1"
	cfg.S3.AccessKey = "synthetic-access"
	cfg.S3.SecretKey = "synthetic-signing-fixture"
	reg := completionRegistry(t, &zonesCloud{}, cfg)
	original := httpretry.DefaultBase
	t.Cleanup(func() { httpretry.DefaultBase = original })
	calls := 0
	httpretry.DefaultBase = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || strings.Trim(req.URL.Path, "/") != "own-bucket" || req.URL.Query().Get("prefix") != "notes/" || req.URL.Query().Get("list-type") != "2" || req.Header.Get("Authorization") == "" {
			t.Errorf("unexpected resource operation %s %s", req.Method, req.URL.Path)
			return nil, fmt.Errorf("unexpected operation")
		}
		calls++
		body := `<ListBucketResult><Name>own-bucket</Name><IsTruncated>false</IsTruncated><Contents><Key>notes/a</Key><Size>1</Size></Contents></ListBucketResult>`
		return &http.Response{Request: req, StatusCode: 200, Header: http.Header{"Content-Type": {"application/xml"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got := reg.CompleteFor(context.Background(), "s3_get", "key", "notes/", "own-bucket")
	if !slices.Equal(got, []Candidate{{Value: "notes/a"}}) || calls != 1 {
		t.Fatalf("suggestions %v, list requests %d", got, calls)
	}
}
