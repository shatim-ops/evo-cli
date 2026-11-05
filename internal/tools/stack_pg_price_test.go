// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// The line of a PostgreSQL cluster in stack_plan's cost is the total of
// pg_create's own preview with the same arguments: its nodes and its
// storage (D75). The line took the nodes only, and a stack of one cluster
// of 20 GB was cheaper by its storage than the cluster alone (agent run of
// 1.7.0: 1676.55 against 1799.45 ₽ a month).
func TestStackPlan_PGLineEqualsPgCreatePreviewTotal(t *testing.T) {
	stackHome(t)
	r := apiFakeRegistry(t, &apiFake{own: pgFake})
	args := map[string]any{"name": "new", "specification_id": "std", "storage_gb": 20}
	pm, text, _ := callNamed(t, r, "pg_create", args)
	if pm["status"] != "confirmation_required" {
		t.Fatalf("pg_create preview: %s", text)
	}
	pv, _ := pm["preview"].(map[string]any)
	pc, _ := pv["cost"].(map[string]any)
	want, _ := pc["total_monthly_rub"].(float64)
	if disk, _ := pc["disk_monthly_rub"].(float64); want <= 0 || disk <= 0 {
		t.Fatalf("pg_create preview prices no storage, the test proves nothing: %s", text)
	}

	var mf map[string]any
	_ = json.Unmarshal([]byte(`{"name":"s","postgres":{"name":"new","specification_id":"std","storage_gb":20}}`), &mf)
	m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": mf})
	if isErr {
		t.Fatalf("stack_plan: %s", text)
	}
	cost, _ := m["cost"].(map[string]any)
	lines, _ := cost["by_component"].([]any)
	var got, sum float64
	n := 0
	for _, l := range lines {
		o := l.(map[string]any)
		if o["component"] != "postgres" {
			continue
		}
		rub, _ := o["monthly_rub"].(float64)
		sum += rub
		if o["resource"] == engine.ResourcePGCluster {
			got, n = rub, n+1
		}
	}
	if n != 1 || math.Abs(got-want) > 0.005 || math.Abs(sum-want) > 0.005 {
		t.Errorf("stack_plan prices the cluster %.2f (its lines %.2f), pg_create's preview %.2f: %s", got, sum, want, text)
	}
	if total, _ := cost["total_monthly_rub"].(float64); math.Abs(total-want) > 0.005 {
		t.Errorf("total %.2f of a stack of one cluster, pg_create's preview %.2f", total, want)
	}
}
