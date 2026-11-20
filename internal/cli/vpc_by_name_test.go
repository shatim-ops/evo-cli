// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func TestVPCNameOnCLIAndStdio(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		label := "unique"
		if duplicate {
			label = "duplicate"
		}
		t.Run(label, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			defer i18n.SetLang(i18n.Lang())
			t.Setenv("EVO_LANG", "ru")
			i18n.SetLang(i18n.RU)
			const id = "00000000-0000-4000-8000-000000000041"
			const other = "00000000-0000-4000-8000-000000000042"
			prev := w.rt.extra
			w.rt.extra = func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/v1/vpcs" {
					rows := `[{"id":"` + id + `","name":"Private-Net"}]`
					if duplicate {
						rows = `[{"id":"` + id + `","name":"Private-Net"},{"id":"` + other + `","name":"private-net"}]`
					}
					return contractRT{status: 200, body: `{"vpcs":` + rows + `}`}.RoundTrip(req)
				}
				if req.Method == "DELETE" && req.URL.Path == "/v1/vpcs/"+id {
					return contractRT{status: 200, body: `{"id":"op-1","done":false}`}.RoundTrip(req)
				}
				if req.URL.Path == "/v1/vpcs/"+id {
					return contractRT{status: 200, body: `{"id":"` + id + `","name":"Private-Net","status":"ACTIVE"}`}.RoundTrip(req)
				}
				return prev(req)
			}
			args := []string{"vpc", "delete", "pRivate-net", "--dry-run", "--json"}
			if duplicate {
				args = []string{"vpc", "delete", "pRivate-net", "--yes", "--json"}
			}
			code, out, stderr := runCLI(t, "", args...)
			i18n.SetLang(i18n.EN)
			response, _ := roDecode(t, stdioRaw(t, parityServers(t).stdio, "vpc_delete", map[string]any{"id": "pRivate-net", "confirmed": duplicate}))
			if len(w.rt.takeWrites()) != 0 {
				t.Fatal("unexpected mutation")
			}
			if duplicate {
				if code != 1 || !strings.Contains(stderr, "USAGE") || !strings.Contains(stderr, other) || !response.isErr || !strings.Contains(response.text, other) || hasCyrillic(stderr+response.text) {
					t.Fatal(code, stderr, response)
				}
				for _, lang := range []string{i18n.RU, i18n.EN} {
					t.Setenv("EVO_LANG", lang)
					i18n.SetLang(lang)
					code, stdout, stderr := runCLI(t, "", "vpc", "delete", "pRivate-net", "--yes")
					if code != 1 || !strings.Contains(stdout+stderr, other) || hasCyrillic(stdout+stderr) != (lang == i18n.RU) || len(w.rt.takeWrites()) != 0 {
						t.Fatal(lang, code, stdout, stderr)
					}
				}
			} else if code != 0 || !json.Valid([]byte(out)) || !strings.Contains(out, id) || !strings.Contains(out, "Private-Net") || !strings.Contains(response.text, "confirmation_required") || !strings.Contains(response.text, id) || !strings.Contains(response.text, "Private-Net") {
				t.Fatal(code, out, stderr, response)
			}
			if !duplicate {
				forgetRun(w)
				code, out, stderr := runCLI(t, "", "vpc", "delete", "pRivate-net", "--yes", "--json")
				writes := w.rt.takeWrites()
				want := "DELETE vpc.api.cloud.ru/v1/vpcs/" + id
				if code != 0 || len(writes) != 1 || writes[0] != want {
					t.Fatal(code, writes, out, stderr)
				}
				forgetRun(w)
				answer, _ := roDecode(t, stdioRaw(t, parityServers(t).stdio, "vpc_delete", map[string]any{"id": "pRivate-net", "confirmed": true}))
				writes = w.rt.takeWrites()
				if answer.isErr || len(writes) != 1 || writes[0] != want {
					t.Fatal(writes, answer)
				}
			}
		})
	}
}

func TestLBVPCNamePreservesCatalogFailure(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w, _ := withJSONWorld(t)
			prior := w.rt.extra
			w.rt.extra = func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/v1/vpcs" {
					if status == http.StatusOK {
						return contractRT{status: 200, body: `{"vpcs":[]}`}.RoundTrip(req)
					}
					return contractRT{status: status, body: `{"message":"catalog failure"}`}.RoundTrip(req)
				}
				return prior(req)
			}
			catalogCode, _, catalogErr := runCLI(t, "", "vpc", "list", "--json")
			var failure struct{ Error struct{ Reason string } }
			wantCode := catalogCode
			wantText := "catalog failure"
			if status == http.StatusOK {
				if catalogCode != 0 {
					t.Fatal(catalogCode, catalogErr)
				}
				wantCode, wantText = 1, "no VPC"
				failure.Error.Reason = "USAGE"
			} else if json.Unmarshal([]byte(catalogErr), &failure) != nil || failure.Error.Reason == "" || catalogCode == 0 {
				t.Fatal(catalogCode, catalogErr)
			}
			t.Logf("catalog HTTP %d: expected CLI exit %d, reason %s", status, wantCode, failure.Error.Reason)
			code, out, stderr := runCLI(t, "", "lb", "create", "web", "--target-group-id", parityVM, "--port", "80", "--vpc-id", "private", "--yes", "--json")
			if code != wantCode || out != "" || !strings.Contains(stderr, failure.Error.Reason) || !strings.Contains(stderr, wantText) {
				t.Error(code, out, stderr)
			}
			resp, _ := roDecode(t, stdioRaw(t, parityServers(t).stdio, "lb_create", map[string]any{"target_group_id": parityVM, "name": "web", "port": 80, "vpc_id": "private", "confirmed": true}))
			if !resp.isErr || !strings.Contains(resp.text, failure.Error.Reason) || !strings.Contains(resp.text, wantText) || len(w.rt.takeWrites()) != 0 {
				t.Fatal(resp)
			}
		})
	}
}
