// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// FB170-1 (Fable's audit of 1.7): deploy_apply recorded its VM only after
// the wait for it (running, the floating IP, port 22 — minutes), and dated
// the record then. A VM that took over five minutes from the POST to SSH
// was older than its own record, and D73 refused its destroy as «adopted
// by an evo before 1.7», provenance «no». The record is dated by the plan
// the VM was made by — the same time ownVM took the VM as the plan's by.
func TestDeployApply_RecordIsNotYoungerThanItsVM(t *testing.T) {
	const vmID = "0000a1a1-0000-4000-8000-0000000000b7"
	dir := t.TempDir()
	planned := time.Now().Add(-20 * time.Minute)
	made := planned.Add(time.Minute) // the POST, a minute after the plan
	// The apply answers 10+ minutes after the POST: a slow VM.
	res := &deployplan.ApplyResult{VMID: vmID, Status: "running"}
	plan := &deployplan.Plan{ProjectID: "p", CreatedAt: planned, VM: deployplan.VMSpec{Name: "slow"}}
	if err := recordDeployment(dir, plan, res, res.Status); err != nil {
		t.Fatal(err)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	rec := sf.FindDeployment(vmID)
	if rec == nil {
		t.Fatal("no record")
	}
	if err := engine.AdoptedVM(*rec, &provision.VM{ID: vmID, CreatedAt: made}); err != nil {
		t.Errorf("destroy of the apply's own VM refused (record %s, VM made %s): %v", rec.CreatedAt, made, err)
	}
	if p := engine.ProvenanceOf("vm", vmID, made, nil, sf, nil); p.CreatedByEvo != engine.ProvenanceYes {
		t.Errorf("provenance of the apply's own VM: %+v, want yes", p)
	}
	// A record of an earlier apply keeps its date (the repeat adopts the VM).
	if err := recordDeployment(dir, &deployplan.Plan{ProjectID: "p", CreatedAt: time.Now()}, res, res.Status); err != nil {
		t.Fatal(err)
	}
	sf, _ = state.Read(state.DefaultPath(dir))
	if again := sf.FindDeployment(vmID); !again.CreatedAt.Equal(rec.CreatedAt) {
		t.Errorf("a repeat moved the record's date: %s, was %s", again.CreatedAt, rec.CreatedAt)
	}
}
