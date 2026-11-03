// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// P1 (agent runs of Grok on 1.6.0, G2-1; release plan 1.7 «Присваивать
// только своё», D73 reserved): a VM of the plan's name that this folder's
// state does not hold and that is older than the plan is someone else's.
// deploy_apply does not adopt it: USAGE before the question with
// existing_id — no SSH, no rules, no record in the state. The world of the
// consent worlds' variant «a VM of the same name, older than the plan»,
// over every path that picks a VM up by name.
func TestAdopt_ForeignVMOlderThanThePlanIsNotTaken(t *testing.T) {
	foreign := "00000000-0000-4000-8000-0000000000f9"
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"deploy_apply", map[string]any{"plan_id": faultPlan().PlanID, "confirmed": true}},
		{"provision", map[string]any{"name": faultPlan().VM.Name, "confirmed": true}},
	} {
		t.Run(c.tool, func(t *testing.T) {
			cloud, r, dir, _ := foreignVMWorld(t, foreign)
			before := len(cloud.callLog())
			text, isErr := r.handlers[c.tool](context.Background(), c.args)
			m := faultJSON(t, text)
			if !isErr || m["reason"] != "USAGE" || m["existing_id"] != foreign {
				t.Errorf("%s with the name of a VM that is not this folder's and older than the plan: want USAGE with existing_id %s, got isErr=%v reason=%v existing_id=%v:\n%s", c.tool, foreign, isErr, m["reason"], m["existing_id"], text)
			}
			for _, call := range cloud.callLog()[before:] {
				if !strings.HasPrefix(call, "List") && !strings.HasPrefix(call, "Get") && call != "PriceCalculation" {
					t.Errorf("%s wrote to the cloud for someone else's VM: %s", c.tool, call)
				}
			}
			if st := stateText(t, dir); strings.Contains(st, foreign) {
				t.Errorf("%s recorded someone else's VM %s in the folder's state", c.tool, foreign)
			}
		})
	}
}

// foreignVMWorld is the compute world with the plan of deploy_apply saved
// and a VM of its name, three hours older than the plan, that the folder's
// state does not hold.
func foreignVMWorld(t *testing.T, id string) (*faultCloud, *Registry, string, *deployplan.Plan) {
	t.Helper()
	cloud := &faultCloud{noDial: true}
	r, dir := faultToolsRegistry(t, cloud)
	plan := faultPlan()
	plan.CreatedAt = time.Now()
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	cloud.mu.Lock()
	cloud.vms = append(cloud.vms, faultVM{ID: id, Name: plan.VM.Name, Status: "active", CreatedAt: plan.CreatedAt.Add(-3 * time.Hour)})
	cloud.mu.Unlock()
	return cloud, r, dir, plan
}
