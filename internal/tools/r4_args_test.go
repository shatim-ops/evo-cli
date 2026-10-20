// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"testing"
	"time"
)

// R4-11 (break r4): одно из пары — только пробелы — USAGE до облака у
// всех пар, не только у vm_get, image_get и pg; оба заданы — «не оба».
// Пустая строка и null — по-прежнему «не задан».
func TestR4_11_BlankArgumentOfEveryPair(t *testing.T) {
	runUsageCases(t, []usageCase{
		{"vm_get", map[string]any{"vm_id": " "}, "vm_id"},
		{"vm_get", map[string]any{"name": "\t"}, "name"},
		{"image_get", map[string]any{"image_id": " "}, "image_id"},
		{"pg_get", map[string]any{"cluster_id": " "}, "cluster_id"},
		{"pg_delete", map[string]any{"id": " ", "confirmed": true}, "id"},
		{"release_floating_ip", map[string]any{"id": " ", "confirmed": true}, "id"},
		{"release_floating_ip", map[string]any{"id": " ", "ip_address": "203.0.113.7", "confirmed": true}, "ip_address"},
		{"destroy", map[string]any{"vm_id": " ", "confirmed": true}, "vm_id"},
		{"destroy", map[string]any{"vm_id": qaID1, "name": "web", "confirmed": true}, "vm_id"},
		{"destroy", map[string]any{"vm_id": " ", "name": "web", "confirmed": true}, "vm_id"},
		{"provision", map[string]any{"name": "web", "image": " "}, "image"},
		{"provision", map[string]any{"name": "web", "image": " ", "image_id": qaID1}, "image_id"},
		{"deploy", map[string]any{"image_id": " "}, "image_id"},
		{"deploy", map[string]any{"image": "ubuntu", "image_id": qaID1}, "image_id"},
		{"vpc_route_create", map[string]any{"vpc_id": qaID1, "subnet": "10.1.0.0/24", "next_hop_vip_id": qaID1, "next_hop_magic_router_id": qaID2}, "next_hop"},
		{"vpc_route_create", map[string]any{"vpc_id": qaID1, "subnet": "10.1.0.0/24", "next_hop_vip_id": " "}, "next_hop_vip_id"},
	})
	// "" and null are not given: the other argument goes on to the cloud.
	rt := &contractRT{kind: "500"}
	reg := contractReg(t, rt)
	for _, args := range []map[string]any{
		{"vm_id": "", "name": "web"},
		{"vm_id": nil, "name": "web"},
		{"vm_id": qaID1, "name": ""},
	} {
		tl, _ := reg.Lookup("vm_get")
		rt.take()
		text, _, _ := contractCall(tl, args, 300*time.Millisecond)
		if len(rt.take()) == 0 {
			t.Errorf("vm_get %v did not reach the API: %s", args, text)
		}
	}
}

// R4-19: порт, названный дважды, — один порт, в порядке первого
// упоминания; так же у deploy_plan (тот же portsArg).
func TestR4_19_PortsDeduplicated(t *testing.T) {
	for _, v := range []any{
		[]any{float64(22), float64(80), float64(22), "80", float64(443)},
		"22, 80,22,80 ,443",
	} {
		got, err := portsArg("ports", v)
		if err != nil || !slices.Equal(got, []int{22, 80, 443}) {
			t.Errorf("%v: %v %v", v, got, err)
		}
	}
}
