// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"
	"time"
)

// REL-10: пробельное значение называется раньше, чем расхождение пары:
// id=" " cluster_id=a — «пустое значение id», а не «id и cluster_id
// расходятся».
func TestRel10_PgBlankBeforeDisagree(t *testing.T) {
	runUsageCases(t, []usageCase{
		{"pg_get", map[string]any{"id": " ", "cluster_id": qaID1}, "empty value of id"},
		{"pg_delete", map[string]any{"id": " ", "cluster_id": qaID1, "confirmed": true}, "empty value of id"},
		{"pg_get", map[string]any{"id": qaID1, "cluster_id": "  "}, "empty value of cluster_id"},
	})
}

// REL-11: tg_create (vm_id / targets) и dns_record_create (value / values):
// пробельные значения и оба аргумента сразу — USAGE до облака.
func TestRel11_TgAndDNSRecordArgs(t *testing.T) {
	target := []any{map[string]any{"id": qaID1}}
	runUsageCases(t, []usageCase{
		{"tg_create", map[string]any{"name": "tg", "vm_id": " "}, "empty value of vm_id"},
		{"tg_create", map[string]any{"name": "tg", "targets": []any{map[string]any{"id": "  "}}}, "empty value of targets[0].id"},
		{"tg_create", map[string]any{"name": "tg", "vm_id": qaID2, "targets": target}, "vm_id or targets, not both"},
		{"tg_create", map[string]any{"name": "tg", "vm_id": qaID2, "targets": target, "confirmed": true}, "not both"},
		{"dns_record_create", map[string]any{"zone_id": qaID1, "name": "www", "record_type": "a", "value": " "}, "empty value of value"},
		{"dns_record_create", map[string]any{"zone_id": qaID1, "name": "www", "record_type": "a", "values": []any{"192.0.2.1", " "}}, "empty value of values[1]"},
		{"dns_record_create", map[string]any{"zone_id": qaID1, "name": "www", "record_type": "a", "value": "192.0.2.1", "values": []any{"192.0.2.2"}}, "value or values, not both"},
	})
	// Один из аргументов — как раньше: превью DNS без облака.
	reg := contractReg(t, &contractRT{kind: "500"})
	tl, _ := reg.Lookup("dns_record_create")
	for _, args := range []map[string]any{
		{"zone_id": qaID1, "name": "www", "record_type": "a", "value": "192.0.2.1"},
		{"zone_id": qaID1, "name": "www", "record_type": "a", "values": []any{"192.0.2.1", "192.0.2.2"}},
	} {
		text, _, _ := contractCall(tl, args, 300*time.Millisecond)
		if m := qaDecode(t, text); m["status"] != "confirmation_required" {
			t.Errorf("dns_record_create %v: %s", args, text)
		}
	}
}

// REL-15: billing_finops days — от 1 до 366, иначе USAGE до облака.
func TestRel15_FinopsDaysUpperBound(t *testing.T) {
	runUsageCases(t, []usageCase{
		{"billing_finops", map[string]any{"days": 367}, "maximum 366"},
		{"billing_finops", map[string]any{"days": 1000000000}, "maximum 366"},
	})
}
