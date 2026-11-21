// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"github.com/shatim-ops/evo-cli/internal/stack"
	"testing"
)

// Audit cli-ids-1.6, §3.2 (consent gap): the preview of lb_create named
// neither the target group nor the VPC the billable balancer serves, and
// that of tg_create said «targets: 1». Both are read before the question:
// the preview names them by id and by name.
func TestLbTgCreate_PreviewNamesWhatTheyServe(t *testing.T) {
	a := &faultAPI{made: []faultResource{{host: "vpc.api.cloud.ru", path: "/v1/vpcs", ID: "vpc-1", Name: "net", before: true}}}
	r := faultAPIRegistry(t, a)
	m, _, _ := callNamed(t, r, "lb_create", map[string]any{"name": "evo-lb", "target_group_id": "tg-1", "port": float64(80), "zone": "ru.AZ-1"})
	p := previewParams(t, m)
	if m["status"] != "confirmation_required" || p["target_group_id"] != "tg-1" || p["target_group_name"] != "tg" ||
		p["vpc_id"] != "vpc-1" || p["vpc_name"] != "net" {
		t.Errorf("lb_create preview does not name its target group and VPC: %v", p)
	}

	m, _, _ = callNamed(t, r, "tg_create", map[string]any{"name": "evo-tg", "targets": []any{map[string]any{"id": faultTargetVM, "interface_id": faultNIC}}})
	p = previewParams(t, m)
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

func TestLBStackResumeSameJob(t *testing.T) {
	stackHome(t)
	j := planJob(t, `{"name":"stack","vm":{"name":"vm"},"nlb":{"name":"web","port":80}}`)
	j.ProjectID = raProject
	for _, st := range j.Steps {
		switch st.Key {
		case "vm", "tg":
			st.Status = "done"
			st.ResourceID = raTargetGroup
			if st.Key == "vm" {
				st.ResourceID = raMissingLB
			}
			j.Outputs[st.Key] = st.ResourceID
		}
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	f := &raAPI{targetVMs: `{"id":"vm-1","zone_name":"ru.AZ-1","vpc_id":"vpc-1"}`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	a := map[string]any{"job_id": j.ID, "confirmed": true}
	q, raw, _ := callNamed(t, r, "stack_apply", a)
	if q["status"] != "confirmation_required" || f.postCount() != 0 {
		t.Fatal(raw)
	}
	old := q["preview_id"]
	for i := 0; i < 2; i++ {
		m, raw, _ := callNamed(t, r, "stack_status", map[string]any{"job_id": j.ID})
		if m["status"] != "awaiting_lb_approval" || f.postCount() != 0 {
			t.Fatal(raw)
		}
	}
	f.targetVMs = `{"id":"vm-1","zone_name":"ru.AZ-2","vpc_id":"vpc-1"}`
	a["preview_id"] = old
	for i := 0; i < 2; i++ {
		q, raw, _ = callNamed(t, r, "stack_apply", a)
		if q["status"] != "confirmation_required" || q["preview_id"] == old || f.postCount() != 0 {
			t.Fatal(raw)
		}
	}
	a["preview_id"] = q["preview_id"]
	_, raw, _ = callNamed(t, r, "stack_apply", a)
	job, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.postCount() != 1 || job.Outputs["vm"] != raMissingLB || job.Outputs["tg"] != raTargetGroup || job.Steps[len(job.Steps)-1].OpID != "op-lb" {
		t.Fatalf("wrong continuation: %s", raw)
	}
}
