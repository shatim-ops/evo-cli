// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// FB15-8 (release plan 1.6, «Квоты в превью»): org_quotas names each quota
// and its unit from the catalogue of quota types (/v3/quotas/resourceTypes),
// not only by its code; a type the catalogue does not name, or a catalogue
// that is not read, leaves the row as it was.
func TestOrgQuotas_NameAndUnitOfEachQuota(t *testing.T) {
	for _, typesOK := range []bool{true, false} {
		f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Host != "organization.api.cloud.ru" {
				return false
			}
			switch r.URL.Path {
			case "/v3/quotas/usages":
				_, _ = io.WriteString(w, `{"usages":[{"resource_type_code":"compute.vcpu","individual_limit":8,"individual_usage":6},`+
					`{"resource_type_code":"other.unknown","individual_limit":1,"individual_usage":0}]}`)
			case "/v3/quotas/resourceTypes":
				if !typesOK {
					http.Error(w, `{"message":"internal"}`, http.StatusInternalServerError)
					return true
				}
				_, _ = io.WriteString(w, `{"resource_types":[{"code":"compute.vcpu","name":"vCPU","product_name":"Evolution Compute","unit":"pcs"}]}`)
			default:
				http.Error(w, `{"message":"no such path"}`, http.StatusNotFound)
			}
			return true
		}}
		r := apiFakeRegistry(t, f)
		m, text, isErr := callNamed(t, r, "org_quotas", nil)
		if isErr {
			t.Fatalf("types read %v: %s", typesOK, text)
		}
		rows, _ := m["quota_usages"].([]any)
		if len(rows) != 2 {
			t.Fatalf("types read %v: rows %s", typesOK, text)
		}
		vcpu, _ := rows[0].(map[string]any)
		other, _ := rows[1].(map[string]any)
		if vcpu["resource"] != "compute.vcpu" || vcpu["limit"] == nil || other["resource"] != "other.unknown" {
			t.Errorf("types read %v: the rows lost what they had: %s", typesOK, text)
		}
		named := vcpu["name"] == "vCPU" && vcpu["unit"] == "pcs" && vcpu["product"] == "Evolution Compute"
		if typesOK != named {
			t.Errorf("types read %v: name and unit of compute.vcpu: %s", typesOK, text)
		}
		if _, has := other["name"]; has || strings.Contains(text, `"name":""`) {
			t.Errorf("a type the catalogue does not name got a name: %s", text)
		}
	}
}

// A key without the organization's quota scope gets a note in the CLI's
// language (D6; Astra's list #1835); status and reason stay machine words.
func TestOrgQuotas_UnavailableNoteSpeaksCLILanguage(t *testing.T) {
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang("ru")
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "organization.api.cloud.ru" {
			return false
		}
		http.Error(w, `{"message":"dont have permissions to customers"}`, http.StatusForbidden)
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "org_quotas", nil)
	if isErr || m["status"] != "unavailable" {
		t.Fatalf("want the soft «unavailable»: %s", text)
	}
	note, _ := m["note"].(string)
	if !strings.Contains(note, "квот") || strings.Contains(note, "lacks") {
		t.Errorf("note in Russian CLI: %q", note)
	}
}

func TestOrgQuotas_CompletePagesAndFirstPageScope(t *testing.T) {
	const one = `{"resource_type_code":"compute.vcpu","subject_id":"a","individual_limit":"8","individual_usage":"3"}`
	const first = `{"usages":[` + one + `],"next_page_token":"p2"}`
	const second = `{"usages":[{"resource_type_code":"compute.vcpu","subject_id":"b","individual_limit":"16","individual_usage":"1"}]}`
	for _, tc := range []struct {
		name, initial, last               string
		firstStatus, lastStatus, wantRows int
		unavailable                       bool
	}{
		{"same type distinct owners", first, second, 200, 200, 2, false},
		{"explicit empty", `{"usages":[]}`, "", 200, 0, 0, false},
		{"protobuf empty", "{}", "", 200, 0, 0, false},
		{"empty intermediate", `{"usages":[],"next_page_token":"p2"}`, second, 200, 200, 1, false},
		{"first 404", `{"message":"not found"}`, "", 404, 0, -1, false},
		{"first actual 403", `{"message":"refused"}`, "", 403, 0, 0, true},
		{"400 mentioning permission and 403", `{"message":"permission lookup 403"}`, "", 400, 0, -1, false},
		{"same 403 later", first, `{"message":"no permission"}`, 200, 403, -1, false},
		{"later 404", first, `{"message":"not found"}`, 200, 404, -1, false},
		{"missing continuation HTTP representation", first, "", 200, 204, -1, false},
		{"bad later token", first, `{"usages":[],"next_page_token":false}`, 200, 200, -1, false},
		{"null later array", first, `{"usages":null}`, 200, 200, -1, false},
		{"null mix", first, `{"usages":[{},null]}`, 200, 200, -1, false},
		{"ignored token", first, `{"usages":[` + one + `]}`, 200, 200, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pages, catalog int
			r := apiFakeRegistry(t, &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host != "organization.api.cloud.ru" {
					return false
				}
				switch r.URL.Path {
				case "/v3/quotas/resourceTypes":
					catalog++
					http.Error(w, "unavailable", http.StatusForbidden)
				case "/v3/quotas/usages":
					pages++
					switch r.URL.Query().Get("page_token") {
					case "":
						w.WriteHeader(tc.firstStatus)
						io.WriteString(w, tc.initial)
					case "p2":
						w.WriteHeader(tc.lastStatus)
						io.WriteString(w, tc.last)
					default:
						t.Errorf("wrong continuation: %v", r.URL.Query())
						w.WriteHeader(400)
					}
				default:
					return false
				}
				return true
			}})
			result, text, bad := callNamed(t, r, "org_quotas", nil)
			wantPages := 1
			if tc.lastStatus != 0 {
				wantPages++
			}
			if pages != wantPages {
				t.Fatalf("pages=%d, want %d: %s", pages, wantPages, text)
			}
			if tc.wantRows < 0 {
				reason := "API_ERROR"
				if tc.firstStatus == http.StatusNotFound {
					reason = "NOT_FOUND"
				}
				if !bad || result["reason"] != reason || result["quota_usages"] != nil || catalog != 0 {
					t.Fatalf("partial quotas or wrong reason: %s (catalog=%d)", text, catalog)
				}
				return
			}
			rows, ok := result["quota_usages"].([]any)
			if bad || !ok || len(rows) != tc.wantRows || (result["status"] == "unavailable") != tc.unavailable {
				t.Fatalf("wrong quota result: %s", text)
			}
			if len(rows) > 0 {
				if catalog != 1 || rows[len(rows)-1].(map[string]any)["usage"] != "1" {
					t.Fatalf("usage lost with optional catalog refused: %s (catalog=%d)", text, catalog)
				}
			}
		})
	}
}
