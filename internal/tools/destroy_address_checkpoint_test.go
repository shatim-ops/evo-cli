// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func TestDestroyTargeted_NewAddressCheckpointScope(t *testing.T) {
	const vmID = "0000a1a1-0000-4000-8000-000000000001"
	for _, change := range []string{"project changed", "record replaced", "state locked", "keep address"} {
		t.Run(change, func(t *testing.T) {
			cloud := &fipNameCloud{fips: []*provision.FloatingIP{{ID: "our-address", IPAddress: "203.0.113.5", VM: json.RawMessage(`{"id":"` + vmID + `"}`)}}}
			r, dir := targetRegistry(t, cloud)
			path := state.DefaultPath(dir)
			d := state.DeploymentRecord{ID: "legacy", VMID: vmID, VMIP: "203.0.113.5", ProjectID: "p", Status: "running"}
			sf := &state.File{Deployments: []state.DeploymentRecord{d}}
			if err := state.Write(path, sf); err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"vm_id": vmID, "release_ip": change != "keep address"}
			plan, err := r.planDestroyTargeted(t.Context(), args, vmID, "")
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "project changed":
				sf.Deployments[0].ProjectID = "other"
			case "record replaced":
				sf.Deployments[0].ID = "replacement"
			case "state locked":
				unlock, err := state.Lock(path)
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			}
			if change == "project changed" || change == "record replaced" {
				if err := state.Write(path, sf); err != nil {
					t.Fatal(err)
				}
			}
			answer, failed := plan.Execute(t.Context())
			keep := change == "keep address"
			if failed == keep || cloud.vmGone != keep || cloud.releasedFIP != "" {
				t.Fatalf("result %s, VM deleted %t, address released %q", answer, cloud.vmGone, cloud.releasedFIP)
			}
			final, err := state.Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if !keep {
				got := final.ActiveDeployment()
				if got == nil || got.ID != sf.Deployments[0].ID || got.ProjectID != sf.Deployments[0].ProjectID || got.FloatingIPID != "" || got.Status != "running" {
					t.Fatalf("changed deployment after refused checkpoint: %+v", got)
				}
			}
		})
	}
}
