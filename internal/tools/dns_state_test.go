// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"testing"
)

// Живой прогон 18.10: приватная зона после принятого удаления висела в
// консоли «Удаляется», а dns_zones показывал её как обычную — поля
// состояния в ответе не было (valid и delegated в API нет, всегда null).
// dns_zones отдаёт state (dns/openapi.yaml) и confirm_state публичной
// зоны (confirmState). Значения состояний выдуманы для теста.
func TestDnsZones_State(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "dns.api.cloud.ru" {
			return false
		}
		switch r.URL.Path {
		case "/v1/publicZones":
			_, _ = io.WriteString(w, `{"zones":[{"id":"`+pagesID1+`","domain":"pub.test.","state":"STATE_ACTIVE","confirmState":"CONFIRM_STATE_PENDING"}]}`)
		case "/v1/privateZones":
			_, _ = io.WriteString(w, `{"zones":[{"id":"`+pagesID2+`","domain":"priv.test.","state":"STATE_DELETING"}]}`)
		default:
			http.Error(w, `{"message":"no such path"}`, http.StatusNotFound)
		}
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "dns_zones", nil)
	if isErr {
		t.Fatal(text)
	}
	zones, _ := m["zones"].([]any)
	got := map[string][2]any{}
	for _, z := range zones {
		zm, _ := z.(map[string]any)
		got[zm["domain"].(string)] = [2]any{zm["state"], zm["confirm_state"]}
	}
	if got["pub.test."] != [2]any{"STATE_ACTIVE", "CONFIRM_STATE_PENDING"} || got["priv.test."] != [2]any{"STATE_DELETING", nil} {
		t.Fatalf("состояние зон не отдано: %s", text)
	}
}
