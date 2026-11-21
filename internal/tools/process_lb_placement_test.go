// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"testing"
)

// Each case starts with the same group and an isolated registry.
func TestProcessLBPlacementConsent(t *testing.T) {
	for _, label := range []string{"stable", "group_zone_changed", "same_zone_subnet_replaced"} {
		t.Run(label, func(t *testing.T) {
			member := func(zoneID, zoneName string) string {
				return fmt.Sprintf(`{"id":"vm-1","interface_id":"if-1","vpc_id":"vpc-1","zone_id":%q,"zone_name":%q}`, zoneID, zoneName)
			}
			f := &raAPI{targetVMs: member(raAZ1, "ru.AZ-1")}
			subnets := raSubnets()
			r := raRegistry(t, f, raCloud{subnets: subnets}, nil)
			args := map[string]any{"name": "web", "target_group_id": raTargetGroup, "port": float64(80)}
			question, text, _ := callNamed(t, r, "lb_create", args)
			placement, _ := question["will_create"].(map[string]any)
			if question["status"] != "confirmation_required" || placement["zone"] != "ru.AZ-1" || placement["subnet_id"] != "sn-1" || placement["target_group_id"] != raTargetGroup || f.postCount() != 0 {
				t.Fatalf("initial question: %s, POST=%d", text, f.postCount())
			}
			wantZone, wantSubnet := "ru.AZ-1", "sn-1"
			switch label {
			case "same_zone_subnet_replaced":
				wantSubnet = "sn-replacement"
				for _, sn := range subnets {
					if sn.ID == "sn-1" {
						sn.ID = wantSubnet
					}
				}
			case "group_zone_changed":
				wantZone, wantSubnet = "ru.AZ-2", "sn-2"
				f.targetVMs = member(raAZ2, "ru.AZ-2")
			}
			if token, ok := question["preview_id"]; ok {
				args["preview_id"] = token
			}
			args["confirmed"] = true
			result, text, failed := callNamed(t, r, "lb_create", args)
			body := f.post("/v2/networkLoadBalancers")
			t.Logf("shown=ru.AZ-1/sn-1 result=%v POST=%d body=%v", result["status"], f.postCount(), body)
			if label != "stable" {
				newPlacement, _ := result["will_create"].(map[string]any)
				if result["status"] != "confirmation_required" || newPlacement["zone"] != wantZone || newPlacement["subnet_id"] != wantSubnet || f.postCount() != 0 {
					t.Fatalf("changed placement needs a visible fresh question and zero POST: %s", text)
				}
				placement = newPlacement
				delete(args, "preview_id")
				if token, ok := result["preview_id"]; ok {
					args["preview_id"] = token
				}
				result, text, failed = callNamed(t, r, "lb_create", args)
				body = f.post("/v2/networkLoadBalancers")
			}
			if failed || result["status"] != "creating" || result["operation_id"] != "op-lb" || f.postCount() != 1 || body["project_id"] != raProject || body["vpc_id"] != "vpc-1" {
				t.Fatalf("accepted placement: %s", text)
			}
			zone := body["availability_zone_specs"].([]any)[0].(map[string]any)
			sentSubnets := zone["subnet_ids"].([]any)
			rule := body["rules"].([]any)[0].(map[string]any)
			if zone["name"] != placement["zone"] || len(sentSubnets) != 1 || sentSubnets[0] != placement["subnet_id"] || rule["target_group_id"] != placement["target_group_id"] {
				t.Fatalf("request differs from question: %v", body)
			}
		})
	}
}
