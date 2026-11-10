// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// D82, R-35 (FB172-2): the record of deploy_apply says it is
// deploy_apply's and by which plan, so an unfinished one goes on by
// deploy_apply with that plan, not by deploy.
func TestDeployApply_RecordNamesItsSourceAndPlan(t *testing.T) {
	const vmID = "0000a1a1-0000-4000-8000-0000000000c1"
	dir := t.TempDir()
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", ProjectID: "p", VM: deployplan.VMSpec{Name: "web"}}
	if err := recordDeployment(dir, nil, plan, &deployplan.ApplyResult{VMID: vmID, Status: "creating"}, "creating"); err != nil {
		t.Fatal(err)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if rec := sf.FindDeployment(vmID); rec == nil || rec.Source != state.SourceDeployApply || rec.PlanID != plan.PlanID {
		t.Errorf("the record of deploy_apply: %+v, want source %q and plan %q", rec, state.SourceDeployApply, plan.PlanID)
	}
}

// L3 («Риск», conclusion 43): the record keeps the key that made it.
func TestDeployApply_RecordKeepsItsMaker(t *testing.T) {
	const vmID = "0000a1a1-0000-4000-8000-0000000000c2"
	dir := t.TempDir()
	m := &state.Maker{ServiceAccountID: "sa-1", KeyID: "kid1****0001"}
	if err := recordDeployment(dir, m, &deployplan.Plan{ProjectID: "p"}, &deployplan.ApplyResult{VMID: vmID, Status: "creating"}, "creating"); err != nil {
		t.Fatal(err)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if rec := sf.FindDeployment(vmID); rec == nil || rec.CreatedBy == nil || *rec.CreatedBy != *m {
		t.Errorf("the record does not keep its maker: %+v", rec)
	}
}
