// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Kafka и Redis на поддельном API. Пути и параметры — по спецификациям
// openapi-kafka.yaml и openapi-redis.yaml; обязательность
// availability_zone_id у /v1/specifications — как на живом API (в
// спецификации она необязательна). UUID выдуманы.
const (
	dbaasProject = "00000000-0000-0000-0000-0000000000d1"
	zoneAZ1      = "00000000-0000-0000-0000-0000000000a1"
	zoneAZ2      = "00000000-0000-0000-0000-0000000000a2"
)

type fakeDbaas struct {
	mu   sync.Mutex
	reqs []string // host + path + "?" + query
}

func (f *fakeDbaas) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	f.mu.Lock()
	f.reqs = append(f.reqs, r.URL.Host+r.URL.Path+"?"+r.URL.RawQuery)
	f.mu.Unlock()
	f.serve(rec, r)
	return rec.Result(), nil
}

func (f *fakeDbaas) serve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	host := r.URL.Host
	switch {
	case r.URL.Path == "/api/v1/availability-zones":
		_, _ = io.WriteString(w, `{"items":[{"id":"`+zoneAZ1+`","name":"ru.AZ-1"},{"id":"`+zoneAZ2+`","name":"ru.AZ-2"}]}`)
	case r.URL.Path == "/api/v1/subnets":
		// Подсеть есть только в ru.AZ-1, как в новом проекте.
		_, _ = io.WriteString(w, `{"items":[{"id":"sn-1","name":"Default_ru.AZ-1","default":true,"availability_zone":{"id":"`+zoneAZ1+`","name":"ru.AZ-1"}}],"total":1}`)
	case host != "kafka.api.cloud.ru" && host != "redis.api.cloud.ru":
		http.Error(w, "unexpected host "+host, 500)
	case r.URL.Path == "/v1/clusters":
		if q.Get("project_id") != dbaasProject {
			http.Error(w, `{"message":"project_id is required"}`, 400)
			return
		}
		if host == "kafka.api.cloud.ru" && q.Get("product_type") != "kafka" {
			http.Error(w, `{"message":"product_type is required"}`, 400)
			return
		}
		if q.Get("page_size") != "100" {
			http.Error(w, `{"message":"page_size out of range"}`, 400)
			return
		}
		switch q.Get("page_token") {
		case "":
			_, _ = io.WriteString(w, `{"clusters":[{"id":"c1","name":"one","status":"RUNNING"}],"next_page_token":"p2"}`)
		case "p2":
			_, _ = io.WriteString(w, `{"clusters":[{"id":"c2","name":"two","status":"RUNNING"}],"next_page_token":""}`)
		default:
			http.Error(w, `{"message":"bad page_token"}`, 400)
		}
	case r.URL.Path == "/v1/versions":
		_, _ = io.WriteString(w, `{"versions":[{"id":"v1","name":"1"},{"id":"v2","name":"2"}]}`)
	case r.URL.Path == "/v1/specifications":
		if q.Get("availability_zone_id") == "" {
			// Живой API: пустое тело, причина в grpc-message.
			w.Header().Set("grpc-message", "availability_zone_id is required")
			w.WriteHeader(400)
			return
		}
		_, _ = io.WriteString(w, `{"specifications":[{"id":"s-`+q.Get("availability_zone_id")[34:]+`","name":"small","cpu":2,"memory":4}]}`)
	default:
		http.Error(w, `{"message":"not found"}`, 404)
	}
}

func (f *fakeDbaas) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.reqs
	f.reqs = nil
	return out
}

func dbaasRegistry(t *testing.T, f *fakeDbaas, region string) *Registry {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: dbaasProject, Region: region, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

// kafka_list и redis_list читают все страницы по next_page_token с
// page_size=100 (было page_size=1000 и одна страница).
func TestDbaasList_PagesWithProjectAndProductType(t *testing.T) {
	for _, tool := range []string{"kafka_list", "redis_list"} {
		f := &fakeDbaas{}
		r := dbaasRegistry(t, f, "")
		m, text, isErr := callNamed(t, r, tool, nil)
		if isErr {
			t.Fatalf("%s: error %s", tool, text)
		}
		cl, _ := m["clusters"].([]any)
		if len(cl) != 2 {
			t.Errorf("%s: clusters %d, want 2 (both pages): %s", tool, len(cl), text)
		}
		if n := len(f.take()); n != 2 {
			t.Errorf("%s: %d requests, want 2", tool, n)
		}
	}
}

// Без настроенного региона kafka_specs и redis_specs шли без
// availability_zone_id, и API отвечал ошибкой (cloud-e2e readonly #23).
// Теперь зона: аргумент zone (имя или UUID) → регион профиля → ru.AZ-2.
// Зона по UUID показывается именем (ревью Fable, A8).
func TestDbaasSpecs_ZoneResolution(t *testing.T) {
	cases := []struct {
		name, region, zone, wantZone, wantShown string
	}{
		{"default", "", "", zoneAZ2, "ru.AZ-2"},
		{"region", zoneAZ1, "", zoneAZ1, "ru.AZ-1"},
		{"zone name", zoneAZ2, "ru.AZ-1", zoneAZ1, "ru.AZ-1"},
		{"zone uuid", "", zoneAZ1, zoneAZ1, "ru.AZ-1"},
	}
	for _, tool := range []string{"kafka_specs", "redis_specs"} {
		for _, c := range cases {
			f := &fakeDbaas{}
			r := dbaasRegistry(t, f, c.region)
			args := map[string]any{}
			if c.zone != "" {
				args["zone"] = c.zone
			}
			m, text, isErr := callNamed(t, r, tool, args)
			if isErr {
				t.Errorf("%s %s: error %s", tool, c.name, text)
				continue
			}
			if m["availability_zone"] != c.wantShown {
				t.Errorf("%s %s: availability_zone %v, want %s", tool, c.name, m["availability_zone"], c.wantShown)
			}
			if m["resolved_version_id"] != "v2" {
				t.Errorf("%s %s: version %v, want the latest v2", tool, c.name, m["resolved_version_id"])
			}
			var specReq string
			for _, q := range f.take() {
				if strings.Contains(q, "/v1/specifications?") {
					specReq = q
				}
			}
			if !strings.Contains(specReq, "availability_zone_id="+c.wantZone) {
				t.Errorf("%s %s: request %q, want availability_zone_id=%s", tool, c.name, specReq, c.wantZone)
			}
		}
	}
}

// Неизвестное имя зоны — USAGE, а не запрос без зоны.
func TestDbaasSpecs_UnknownZoneIsUsage(t *testing.T) {
	f := &fakeDbaas{}
	r := dbaasRegistry(t, f, "")
	m, text, isErr := callNamed(t, r, "kafka_specs", map[string]any{"zone": "ru.AZ-9"})
	if !isErr || m["reason"] != "USAGE" {
		t.Fatalf("want USAGE error, got %s", text)
	}
	for _, q := range f.take() {
		if strings.Contains(q, "/v1/specifications") {
			t.Errorf("specifications requested with an unknown zone: %s", q)
		}
	}
}

// kafka_create и redis_create: сначала подсеть, зона — её. Подсеть есть
// только в ru.AZ-1, поэтому спецификации — для ru.AZ-1 (раньше: регион
// профиля или пусто, а подсеть — первая в любой зоне). В регионе без
// подсети — USAGE.
func TestDbaasCreate_ZoneFromSubnet(t *testing.T) {
	for _, tool := range []string{"kafka_create", "redis_create"} {
		f := &fakeDbaas{}
		r := dbaasRegistry(t, f, "")
		m, text, _ := callNamed(t, r, tool, map[string]any{"name": "c1", "cpu": 2, "ram": 4})
		wc, _ := m["will_create"].(map[string]any)
		if m["status"] != "confirmation_required" || wc["subnet_id"] != "sn-1" || wc["availability_zone"] != "ru.AZ-1" {
			t.Errorf("%s: %s", tool, text)
		}
		for _, q := range f.take() {
			if strings.Contains(q, "/v1/specifications?") && !strings.Contains(q, "availability_zone_id="+zoneAZ1) {
				t.Errorf("%s: specifications of another zone: %s", tool, q)
			}
		}
		if tool == "redis_create" && (strings.Contains(text, "3 availability zones") || !strings.Contains(text, "Single-AZ")) {
			t.Errorf("redis warning: %s", text)
		}

		r = dbaasRegistry(t, &fakeDbaas{}, zoneAZ2)
		m, text, isErr := callNamed(t, r, tool, map[string]any{"name": "c1", "cpu": 2, "ram": 4})
		if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "zone ru.AZ-2") || strings.Contains(text, zoneAZ2) {
			t.Errorf("%s in a zone without a subnet (the zone by name, not UUID): %s", tool, text)
		}
	}
}
