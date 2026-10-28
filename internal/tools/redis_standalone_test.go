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

// The preview of the same call: the argument as given (replicas 0, the
// replicas without the master) and one host per shard — what redis_get
// reports after the create (replicas 1).
func TestRedisCreate_StandalonePreviewShowsOneHost(t *testing.T) {
	f := &apiFake{own: dbaasCreateOK}
	r := apiFakeRegistry(t, f)
	m, text, _ := callNamed(t, r, "redis_create", map[string]any{"name": "cache", "type": "STANDALONE", "cpu": 2, "ram": 4})
	will, _ := m["will_create"].(map[string]any)
	if m["status"] != "confirmation_required" || will["replicas"] != float64(0) || will["hosts_per_shard"] != float64(1) {
		t.Errorf("STANDALONE preview: want replicas 0 and one host per shard: %s", text)
	}
	if _, ok := f.posted("redis.api.cloud.ru/v1/clusters"); ok {
		t.Error("the preview created a cluster")
	}
}
