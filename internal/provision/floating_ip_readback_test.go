// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func TestGetFloatingIP_RequiresCompleteCardOrServiceAbsence(t *testing.T) {
	const id = "fip-1"
	const card = `{"id":"fip-1"}`
	const absent = `[{"message":"floating IP not found"}]`
	fullAtLimit := absent + strings.Repeat(" ", maxErrorBody-len(absent))
	for _, tc := range []struct {
		name         string
		status       int
		body         string
		contentRange string
		truncated    bool
		wantReason   string
	}{
		{name: "complete card", status: 200, body: card},
		{name: "two JSON values", status: 200, body: card + `{}`, wantReason: clierrors.CodeAPIError},
		{name: "partial content", status: 206, body: card, wantReason: clierrors.CodeAPIError},
		{name: "accepted without final card", status: 202, body: card, wantReason: clierrors.CodeAPIError},
		{name: "other address", status: 200, body: `{"id":"fip-2"}`, wantReason: clierrors.CodeAPIError},
		{name: "connection ends after valid card", status: 200, body: card, truncated: true, wantReason: clierrors.CodeAPIError},
		{name: "complete service error at limit", status: 404, body: fullAtLimit, wantReason: clierrors.CodeNotFound},
		{name: "service error past limit", status: 404, body: fullAtLimit + " ", wantReason: clierrors.CodeAPIError},
		{name: "partial service error", status: 404, body: absent, contentRange: "bytes 0-" + strconv.Itoa(len(absent)-1) + "/100", wantReason: clierrors.CodeAPIError},
		{name: "connection ends after valid service error", status: 404, body: absent, truncated: true, wantReason: clierrors.CodeAPIError},
		{name: "service resource not found", status: 404, body: `{"code":5,"message":"resource not found"}`, wantReason: clierrors.CodeNotFound},
		{name: "gateway not found", status: 404, body: `{"code":5,"message":"Not Found"}`, wantReason: clierrors.CodeAPIError},
		{name: "conflicting codes", status: 404, body: `{"code":"project_not_found","code":"floating_ip_not_found","message":"floating IP not found"}`, wantReason: clierrors.CodeAPIError},
		{name: "conflicting messages", status: 404, body: `[{"message":"route not found","message":"gone"}]`, wantReason: clierrors.CodeAPIError},
		{name: "mixed gateway and resource", status: 404, body: `{"error_msg":"Route not found","message":"gone"}`, wantReason: clierrors.CodeAPIError},
		{name: "gateway route not found", status: 404, body: `{"code":5,"message":"route not found"}`, wantReason: clierrors.CodeAPIError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/floating-ips/"+id {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.contentRange != "" {
					w.Header().Set("Content-Range", tc.contentRange)
				}
				if tc.truncated {
					// The JSON value fits in the body. The HTTP response does not.
					w.Header().Set("Content-Length", strconv.Itoa(len(tc.body)+1))
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			defer srv.Close()

			got, err := adapter.GetFloatingIP(t.Context(), id)
			if reason := clierrors.Reason(err); reason != tc.wantReason {
				t.Fatalf("GetFloatingIP: reason %q, want %q (err %v)", reason, tc.wantReason, err)
			}
			if err != nil && got != nil || err == nil && (got == nil || got.ID != id) {
				t.Errorf("GetFloatingIP: card %+v, err %v", got, err)
			}
		})
	}
}
