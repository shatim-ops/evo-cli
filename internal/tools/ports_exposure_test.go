// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// P5 (G2-2): the previews of provision, deploy and deploy_apply name where
// the ports open — the group (the zone's shared one), the source — and
// warn of a port besides the web's open to anyone; cidr narrows the
// source, the same argument in the three tools. No refusal: the warning
// is the person's to weigh.
func TestPortsPreview_NamesGroupSourceAndWarns(t *testing.T) {
	type answer struct {
		Preview *engine.CreatePreview `json:"preview"`
		Network map[string]any        `json:"network"`
		PlanID  string                `json:"plan_id"`
	}
	call := func(t *testing.T, r *Registry, tool string, args map[string]any) answer {
		t.Helper()
		text, _ := r.handlers[tool](context.Background(), args)
		var a answer
		if json.Unmarshal([]byte(text), &a) != nil || (a.Preview == nil && a.PlanID == "") {
			t.Fatalf("%s: %s", tool, text)
		}
		return a
	}
	param := func(p *engine.CreatePreview, key string) string {
		for _, prm := range p.Params {
			if prm.Key == key {
				s, _ := prm.Value.(string)
				return s
			}
		}
		return ""
	}
	check := func(t *testing.T, p *engine.CreatePreview, source string, warned bool) {
		t.Helper()
		if p == nil {
			t.Fatal("no preview")
		}
		if param(p, "ports_group") != "default" || param(p, "ports_source") != source {
			t.Errorf("group %q, source %q; want default and %s", param(p, "ports_group"), param(p, "ports_source"), source)
		}
		wide := false
		for _, w := range p.Warnings {
			wide = wide || strings.Contains(w, "cidr")
		}
		if wide != warned {
			t.Errorf("warnings %q: a port open to anyone warned %v, want %v", p.Warnings, wide, warned)
		}
		human := strings.Join(engine.ShowCreatePreview("create", *p).Lines, "\n")
		if !strings.Contains(human, "default") || !strings.Contains(human, source) || !strings.Contains(human, "shared") {
			t.Errorf("the human preview does not name the group, the source and that it is shared:\n%s", human)
		}
	}

	t.Run("provision", func(t *testing.T) {
		r, _ := cancelRegistry(t, &cancelCloud{cancel: func() {}})
		check(t, call(t, r, "provision", map[string]any{"name": "app", "ports": []any{"80", "22"}}).Preview, "0.0.0.0/0", true)
		check(t, call(t, r, "provision", map[string]any{"name": "app", "ports": []any{"22"}, "cidr": "10.0.0.0/8"}).Preview, "10.0.0.0/8", false)
	})
	t.Run("deploy", func(t *testing.T) {
		r, _ := cancelRegistry(t, &cancelCloud{cancel: func() {}})
		check(t, call(t, r, "deploy", map[string]any{}).Preview, "0.0.0.0/0", false)
		check(t, call(t, r, "deploy", map[string]any{"cidr": "198.51.100.0/24"}).Preview, "198.51.100.0/24", false)
	})
	t.Run("deploy_plan and deploy_apply", func(t *testing.T) {
		r, dir := cancelRegistry(t, &zonedCloud{cancelCloud{cancel: func() {}}})
		plan := call(t, r, "deploy_plan", map[string]any{"name": "db", "zone": "ru.AZ-1", "ports": []any{5432.0}, "cidr": "10.0.0.0/8"})
		if plan.Network["security_group"] != "default" || plan.Network["ports_source"] != "10.0.0.0/8" || plan.Network["security_group_shared"] != true {
			t.Errorf("deploy_plan network: %v", plan.Network)
		}
		saved, err := deployplan.Load(dir, plan.PlanID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.PortsSource != "10.0.0.0/8" || saved.Network.SecurityGroupName != "default" {
			t.Errorf("the plan keeps source %q, group %q", saved.PortsSource, saved.Network.SecurityGroupName)
		}
		check(t, engine.PlanPreview(saved), "10.0.0.0/8", false)

		wide := call(t, r, "deploy_plan", map[string]any{"name": "db2", "zone": "ru.AZ-1", "ports": []any{5432.0}})
		saved, _ = deployplan.Load(dir, wide.PlanID)
		check(t, engine.PlanPreview(saved), "0.0.0.0/0", true)
	})
}

// zonedCloud lists its zone by a UUID and a name, as deploy_plan resolves
// zones.
type zonedCloud struct{ cancelCloud }

func (*zonedCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: "00000000-0000-4000-8000-0000000000a1", Name: "ru.AZ-1"}}, nil
}
