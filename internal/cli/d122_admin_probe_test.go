// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestProcessD122IAMForms(t *testing.T) {
	for _, c := range []struct {
		name, body string
		status     int
		fail       bool
	}{
		{"empty", `{"service_accounts":[]}`, 200, false}, {"absent", `{}`, 200, false}, {"field-null", `{"service_accounts":null}`, 200, true}, {"invalid-row", `{"service_accounts":["diagnostic-private-row"]}`, 200, true}, {"auth", `{"message":"request refused"}`, 401, true}, {"role", `{"message":"request refused"}`, 403, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			old := w.rt.extra
			reads := 0
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.URL.Host+r.URL.Path == "iam.api.cloud.ru/api/v2/serviceAccounts" && r.Method == "GET" {
					reads++
					return contractRT{status: c.status, body: c.body}.RoundTrip(r)
				}
				if r.URL.String() == "https://iam.api.cloud.ru/api/v1/auth/token" && r.Method == "POST" {
					return contractRT{status: 200, body: `{"access_token":"test-token","expires_in":3600}`}.RoundTrip(r)
				}
				return old(r)
			}
			a := callStdioTool(t, "iam_service_accounts", map[string]any{"project_id": pvProject})
			m := parseObject(a.text)
			t.Logf("HTTP %d reads=%d isError=%t reason=%v hint=%v writes=%v", c.status, reads, a.isErr, m["reason"], m["hint"], w.rt.takeWrites())
			if c.fail != a.isErr || strings.Contains(a.text, "diagnostic-private-row") {
				t.Fatal(a.text)
			}
			if !c.fail && fmt.Sprint(m["service_accounts"]) != "[]" {
				t.Fatal(a.text)
			}
		})
	}
}
