// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// The repeat of deploy_apply after its plan was applied (release plan 1.9,
// «Повтор у выпущенных инструментов», п. 3; Н1). The plan goes once its VM
// runs, so the repeat found no plan and answered «plan not found (it may
// have expired; run deploy_plan again)»: the state named wrongly, and a
// hint that leads the agent to plan — and then make — a second VM. The
// record of the deployment names the plan that made it: the refusal says
// the plan was applied and names its VM, and does not send to deploy_plan.
// The reason and the exit code are the ones of a plan that is not there:
// only the words change.
func TestDeployApply_RepeatAfterAppliedSaysSo(t *testing.T) {
	const vmID = "0000a1a1-0000-4000-8000-0000000000c1"
	const planID = "plan-0123456789ab"
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	answer := func(id string, confirmed bool) (map[string]any, string) {
		text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": id, "confirmed": confirmed})
		if !isErr {
			t.Fatalf("%s (confirmed=%v): not refused: %s", id, confirmed, text)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatalf("not JSON: %s", text)
		}
		return m, text
	}
	// The reason of a plan that is not there, before anything was applied.
	missing, _ := answer(planID, true)

	plan := &deployplan.Plan{PlanID: planID, ProjectID: "p", VM: deployplan.VMSpec{Name: "web"}}
	if err := recordDeployment(dir, nil, plan, &deployplan.ApplyResult{VMID: vmID, Status: "running"}, "running"); err != nil {
		t.Fatal(err)
	}
	for _, confirmed := range []bool{false, true} {
		m, text := answer(planID, confirmed)
		if m["reason"] != missing["reason"] {
			t.Errorf("confirmed=%v: reason %v, want %v — the one of a plan that is not there (not a breaking change)", confirmed, m["reason"], missing["reason"])
		}
		if !strings.Contains(text, "applied") || !strings.Contains(text, vmID) {
			t.Errorf("confirmed=%v: the refusal does not say the plan was applied and by which VM: %s", confirmed, text)
		}
		if strings.Contains(text, "deploy_plan") || strings.Contains(text, "expired") {
			t.Errorf("confirmed=%v: the refusal of an applied plan sends to a new plan: %s", confirmed, text)
		}
	}
	// Another plan of the folder is still «not found».
	if _, text := answer("plan-ffffffffffff", true); !strings.Contains(text, "not found") {
		t.Errorf("a plan that was never there: %s", text)
	}
	if calls := cloud.callLog(); len(calls) != 0 {
		t.Errorf("a refused apply called the cloud: %v", calls)
	}
}

// «Applied» is said only of a plan that is gone and whose VM came to run
// («Риск», conclusion 131). A plan that is there — expired, corrupt —
// keeps the words of what is wrong with it: a step of a VM not finished
// yet, after the term of its plan, must not hear «applied, nothing to
// apply». A record in error or of a VM not finished is not «applied»
// either: the VM is there, billed, and the answer says so. And a record
// without a name gets no empty brackets.
func TestDeployApply_AppliedOnlyWhenThePlanIsGoneAndTheVMRuns(t *testing.T) {
	const vmID = "0000a1a1-0000-4000-8000-0000000000c1"
	const planID = "plan-0123456789ab"
	type world struct {
		name       string
		planFile   string // "" none, "expired", "corrupt"
		status     string
		vmName     string
		want, deny []string
	}
	for _, w := range []world{
		{name: "expired plan, VM still creating", planFile: "expired", status: "creating", vmName: "web",
			want: []string{"expired"}, deny: []string{"applied"}},
		{name: "corrupt plan, VM still creating", planFile: "corrupt", status: "creating", vmName: "web",
			want: []string{"corrupt"}, deny: []string{"applied"}},
		{name: "no plan, record in error", status: "error", vmName: "web",
			want: []string{"error state", vmID, "ask the person"}, deny: []string{"applied", "nothing to apply"}},
		{name: "no plan, record ssh_timeout", status: "ssh_timeout", vmName: "web",
			want: []string{"not finished", "ssh_timeout", vmID}, deny: []string{"applied", "nothing to apply"}},
		{name: "no plan, VM runs, no name", status: "running",
			want: []string{"applied", vmID}, deny: []string{"()"}},
	} {
		t.Run(w.name, func(t *testing.T) {
			cloud := &faultCloud{}
			r, dir := faultToolsRegistry(t, cloud)
			plan := faultPlan()
			plan.PlanID = planID
			switch w.planFile {
			case "expired":
				plan.ExpiresAt = plan.CreatedAt.Add(-48 * time.Hour)
				if err := writePlanFile(dir, planID, plan); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := writePlanFile(dir, planID, "{"); err != nil {
					t.Fatal(err)
				}
			}
			rec := &deployplan.Plan{PlanID: planID, ProjectID: "p", VM: deployplan.VMSpec{Name: w.vmName}}
			if err := recordDeployment(dir, nil, rec, &deployplan.ApplyResult{VMID: vmID, Status: w.status}, w.status); err != nil {
				t.Fatal(err)
			}
			for _, confirmed := range []bool{false, true} {
				text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": planID, "confirmed": confirmed})
				if !isErr {
					t.Fatalf("confirmed=%v: not refused: %s", confirmed, text)
				}
				for _, s := range w.want {
					if !strings.Contains(text, s) {
						t.Errorf("confirmed=%v: the refusal does not say %q: %s", confirmed, s, text)
					}
				}
				for _, s := range w.deny {
					if strings.Contains(text, s) {
						t.Errorf("confirmed=%v: the refusal says %q: %s", confirmed, s, text)
					}
				}
			}
		})
	}
}

// writePlanFile writes the file of plan id in dir: a plan as JSON, or raw
// text for a file that is not a plan.
func writePlanFile(dir, id string, plan any) error {
	path := filepath.Join(config.DataDir(dir), "plans")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	data, ok := plan.(string)
	if !ok {
		b, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		data = string(b)
	}
	return os.WriteFile(filepath.Join(path, id+".json"), []byte(data), 0o600)
}
