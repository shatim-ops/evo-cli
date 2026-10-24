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

// W6 (G13-3): storage below the specification's minimum (min_storage_gb,
// min_storage_gigs at Kafka) asked for consent, and Cloud.ru refused the
// create after it. It is USAGE before the question, with confirmed or
// without, and nothing is created; the minimum itself is taken.
func TestDbaasCreate_StorageBelowSpecMinimumBeforeTheQuestion(t *testing.T) {
	specs := map[string]string{
		"kafka.api.cloud.ru":      `{"specifications":[{"id":"s-k","name":"small","cpu":2,"memory":4,"min_storage_gigs":50}]}`,
		"redis.api.cloud.ru":      `{"specifications":[{"id":"s-r","name":"small","cpu":2,"memory":4,"min_storage_gb":50}]}`,
		"postgresql.api.cloud.ru": `{"specifications":[{"id":"std","display_name":"std","cpu":2,"memory":4,"deployment_mode":"STANDARD","min_storage_gb":50,"max_hosts":2}]}`,
	}
	fake := func() *apiFake {
		return &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
			body, ok := specs[r.URL.Host]
			if !ok {
				return false
			}
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/v1/specifications":
				_, _ = io.WriteString(w, body)
			case r.URL.Host != "postgresql.api.cloud.ru":
				return false // the rest of Kafka and Redis: fakeDbaas
			case r.Method == http.MethodGet && r.URL.Path == "/v1/clusters":
				_, _ = io.WriteString(w, `{"clusters":[]}`)
			case r.Method == http.MethodGet && r.URL.Path == "/v1/versions":
				_, _ = io.WriteString(w, `{"versions":[{"id":"v17","name":"17"}]}`)
			default:
				http.Error(w, `{"message":"no such path"}`, 404)
			}
			return true
		}}
	}
	for _, c := range []struct {
		tool, host, arg string
		args            map[string]any
	}{
		{"kafka_create", "kafka.api.cloud.ru", "storage_gib", map[string]any{"name": "c1", "cpu": 2, "ram": 4}},
		{"redis_create", "redis.api.cloud.ru", "storage_gb", map[string]any{"name": "c1", "cpu": 2, "ram": 4}},
		{"pg_create", "postgresql.api.cloud.ru", "storage_gb", map[string]any{"name": "web", "specification_id": "std"}},
	} {
		for _, confirmed := range []bool{false, true} {
			f := fake()
			args := map[string]any{c.arg: float64(20), "confirmed": confirmed}
			for k, v := range c.args {
				args[k] = v
			}
			m, text, isErr := callNamed(t, apiFakeRegistry(t, f), c.tool, args)
			if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, c.arg) || !strings.Contains(text, "50") {
				t.Errorf("%s %s=20 under a minimum of 50, confirmed=%v: %s", c.tool, c.arg, confirmed, text)
			}
			if body, ok := f.posted(c.host); ok {
				t.Errorf("%s: created below the minimum: %s", c.tool, body)
			}
		}
		args := map[string]any{c.arg: float64(50)}
		for k, v := range c.args {
			args[k] = v
		}
		if m, text, _ := callNamed(t, apiFakeRegistry(t, fake()), c.tool, args); m["status"] != "confirmation_required" {
			t.Errorf("%s %s at the minimum: %s", c.tool, c.arg, text)
		}
	}
}
