// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Живой прогон 18.10 (run 39152048559): dns_zone_delete приватной зоны без
// type ушёл в API публичных зон, Cloud.ru ответил 403, зона осталась.
// Без type evo сам узнаёт видимость: зону — по спискам публичных и
// приватных зон (как dns_zones), запись — по GET в обоих API. Явный type
// по-прежнему берётся как есть. UUID выдуманы.

const (
	dnsPrivZone = "00000000-0000-4000-8000-0000000000a1"
	dnsPrivRec  = "00000000-0000-4000-8000-0000000000b1"
	dnsMissing  = "00000000-0000-4000-8000-0000000000c1"
)

// dnsVisFake: одна приватная зона с одной записью. API публичных зон на
// чужой id отвечает 403, как живой Cloud.ru (тот же приём — AR-2, CRT-2).
func dnsVisFake() *apiFake {
	return &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "dns.api.cloud.ru" {
			return false
		}
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && p == "/v1/publicZones":
			_, _ = io.WriteString(w, `{"zones":[]}`)
		case r.Method == http.MethodGet && p == "/v1/privateZones":
			_, _ = io.WriteString(w, `{"zones":[{"id":"`+dnsPrivZone+`","name":"z.test.","domain":"z.test."}]}`)
		case r.Method == http.MethodGet && p == "/v1/privateZones/"+dnsPrivZone:
			_, _ = io.WriteString(w, `{"id":"`+dnsPrivZone+`","domain":"z.test."}`)
		case r.Method == http.MethodGet && p == "/v1/privateRecordsSole":
			_, _ = io.WriteString(w, `{"records":[{"id":"`+dnsPrivRec+`","name":"www","type":"a","values":["198.18.0.1"],"ttl":300}]}`)
		case r.Method == http.MethodGet && p == "/v1/privateRecordsSole/"+dnsPrivRec:
			_, _ = io.WriteString(w, `{"id":"`+dnsPrivRec+`","name":"www","ttl":300}`)
		case (r.Method == http.MethodDelete || r.Method == http.MethodPost) && strings.HasPrefix(p, "/v1/private"):
			_, _ = io.WriteString(w, `{"id":"op-1","done":false}`)
		case strings.HasPrefix(p, "/v1/public"):
			http.Error(w, `{"code":7,"message":"permission denied"}`, http.StatusForbidden)
		default:
			http.Error(w, `{"message":"no such path"}`, http.StatusNotFound)
		}
		return true
	}}
}

func TestDNS_PrivateZoneWithoutType(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string // запрос, который должен уйти
	}{
		{"dns_zone_get", map[string]any{"id": dnsPrivZone}, "GET dns.api.cloud.ru/v1/privateZones/" + dnsPrivZone},
		{"dns_records", map[string]any{"zone_id": dnsPrivZone}, "GET dns.api.cloud.ru/v1/privateRecordsSole"},
		{"dns_record_get", map[string]any{"record_id": dnsPrivRec}, "GET dns.api.cloud.ru/v1/privateRecordsSole/" + dnsPrivRec},
		{"dns_record_create", map[string]any{"zone_id": dnsPrivZone, "name": "api", "record_type": "A", "value": "198.18.0.2", "confirmed": true}, "POST dns.api.cloud.ru/v1/privateRecordsSole"},
		{"dns_record_delete", map[string]any{"record_id": dnsPrivRec, "confirmed": true}, "DELETE dns.api.cloud.ru/v1/privateRecordsSole/" + dnsPrivRec},
		{"dns_zone_delete", map[string]any{"id": dnsPrivZone, "confirmed": true}, "DELETE dns.api.cloud.ru/v1/privateZones/" + dnsPrivZone},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			f := dnsVisFake()
			r := apiFakeRegistry(t, f)
			_, text, isErr := callNamed(t, r, c.tool, c.args)
			if isErr {
				t.Fatalf("%s приватной зоны без type — ошибка: %s", c.tool, text)
			}
			f.mu.Lock()
			calls := slices.Clone(f.calls)
			f.mu.Unlock()
			if !slices.Contains(calls, c.want) {
				t.Fatalf("нет запроса %q; ушли: %v", c.want, calls)
			}
			for _, k := range calls {
				if (strings.HasPrefix(k, "DELETE ") || strings.HasPrefix(k, "POST ")) && strings.Contains(k, "/v1/public") {
					t.Fatalf("изменение ушло в API публичных зон: %s", k)
				}
			}
		})
	}
}

// Превью удаления без type называет видимость найденной зоны.
func TestDNS_ZoneDeletePreviewNamesVisibility(t *testing.T) {
	r := apiFakeRegistry(t, dnsVisFake())
	m, text, _ := callNamed(t, r, "dns_zone_delete", map[string]any{"id": dnsPrivZone})
	if m["status"] != "confirmation_required" || !strings.Contains(text, "private zone") {
		t.Fatalf("превью не называет приватную зону: %s", text)
	}
}

// Зоны нет ни в одном списке — NOT_FOUND до запроса на удаление, а не 403.
func TestDNS_UnknownZoneWithoutType(t *testing.T) {
	f := dnsVisFake()
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "dns_zone_delete", map[string]any{"id": dnsMissing, "confirmed": true})
	if !isErr || m["reason"] != "NOT_FOUND" {
		t.Fatalf("неизвестная зона: ждали NOT_FOUND, ответ %s", text)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.calls {
		if strings.HasPrefix(k, "DELETE ") {
			t.Fatalf("удаление ушло для неизвестной зоны: %s", k)
		}
	}
}

// Явный type берётся как есть: лишних запросов списков нет.
func TestDNS_ExplicitTypeKept(t *testing.T) {
	f := dnsVisFake()
	r := apiFakeRegistry(t, f)
	if _, text, isErr := callNamed(t, r, "dns_zone_get", map[string]any{"id": dnsPrivZone, "type": "private"}); isErr {
		t.Fatalf("type=private: %s", text)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.calls {
		if strings.HasSuffix(k, "/v1/publicZones") || strings.HasSuffix(k, "/v1/privateZones") {
			t.Fatalf("при явном type прочитан список зон: %s", k)
		}
	}
}
