// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"testing"
)

// G140-6: the API creates a STANDALONE cluster only with replicas 1 in the
// POST — 0 and no field are 400 «replicas must be positive» — and that
// cluster is one host (register DB-3; cloud-e2e redis_standalone_probe,
// run 40695706334, 1 of 1). evo sent replicas 0, so no STANDALONE cluster
// could be made through it.
func TestRedisCreate_StandaloneSendsWhatTheAPICreates(t *testing.T) {
	f := &apiFake{own: dbaasCreateOK}
	r := apiFakeRegistry(t, f)
	_, text, isErr := callNamed(t, r, "redis_create", map[string]any{"name": "cache", "type": "STANDALONE", "cpu": 2, "ram": 4, "confirmed": true})
	if isErr {
		t.Fatalf("create: %s", text)
	}
	body, ok := f.posted("redis.api.cloud.ru/v1/clusters")
	if !ok {
		t.Fatal("no POST /v1/clusters")
	}
	var b map[string]any
	_ = json.Unmarshal([]byte(body), &b)
	if b["type"] != "STANDALONE" || b["replicas"] != float64(1) {
		t.Errorf("POST /v1/clusters for STANDALONE: the API creates it with replicas 1 only (DB-3), the body is %s", body)
	}
}
