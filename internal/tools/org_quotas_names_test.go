// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"strings"
	"testing"
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
