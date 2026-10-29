// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"testing"
)

// previewParams are the key parameters of a preview answer, by key.
func previewParams(t *testing.T, text string) map[string]any {
	t.Helper()
	var m struct {
		Preview struct {
			Params []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			} `json:"params"`
		} `json:"preview"`
	}
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("not JSON: %s", text)
	}
	out := map[string]any{}
	for _, p := range m.Preview.Params {
		out[p.Key] = p.Value
	}
	return out
}

// Audit cli-ids-1.6, §3.2 (consent gap): the preview of lb_create named
// neither the target group nor the VPC the billable balancer serves, and
// that of tg_create said «targets: 1». Both are read before the question:
// the preview names them by id and by name.
func TestLbTgCreate_PreviewNamesWhatTheyServe(t *testing.T) {
	a := &faultAPI{made: []faultResource{{host: "vpc.api.cloud.ru", path: "/v1/vpcs", ID: "vpc-1", Name: "net", before: true}}}
	r := faultAPIRegistry(t, a)
	m, text, _ := callNamed(t, r, "lb_create", map[string]any{"name": "evo-lb", "target_group_id": "tg-1", "port": float64(80), "zone": "ru.AZ-1"})
	p := previewParams(t, text)
	if m["status"] != "confirmation_required" || p["target_group_id"] != "tg-1" || p["target_group_name"] != "tg" ||
		p["vpc_id"] != "vpc-1" || p["vpc_name"] != "net" {
		t.Errorf("lb_create preview does not name its target group and VPC: %v", p)
	}

	m, text, _ = callNamed(t, r, "tg_create", map[string]any{"name": "evo-tg", "targets": []any{map[string]any{"id": faultTargetVM, "interface_id": faultNIC}}})
	p = previewParams(t, text)
	targets, _ := p["targets"].([]any)
	first, _ := func() (map[string]any, bool) {
		if len(targets) != 1 {
			return nil, false
		}
		v, ok := targets[0].(map[string]any)
		return v, ok
	}()
	if m["status"] != "confirmation_required" || first["id"] != faultTargetVM || first["name"] != "web" {
		t.Errorf("tg_create preview does not name its target VMs: %v", p)
	}
}
