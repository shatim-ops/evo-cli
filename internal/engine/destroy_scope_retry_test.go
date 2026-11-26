// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"os"
	"slices"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A label may be the only link to a rule. The state used by another
// invocation must retain that link until the whole inventory was read.
func TestDestroy_RetryIncompleteLabelInventory(t *testing.T) {
	for _, tc := range []struct {
		name           string
		unreadAttempts int
	}{
		{"readable", 0}, {"one-unavailable", 1}, {"still-unavailable-on-retry", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := runningRecord()
			rec.ProjectID = projectA
			eng, cloud, _ := destroyFixture(t, rec)
			dir := eng.StateDir
			eng.Config.ProjectID = projectA
			rule := provision.SGRule{
				ID: "unrecorded-rule", Direction: "ingress", IPProtocol: "tcp",
				PortRange: "8443:8443", Description: sgrules.RuleLabel("8443", rec.VMID),
			}
			reads, attempt := 0, 0
			cloud.listSecurityGroupsFn = func(_ context.Context, project string) ([]*provision.SecurityGroup, error) {
				if project != "" {
					if project != projectA {
						t.Fatalf("listed groups of %s", project)
					}
					reads++
				}
				if attempt < tc.unreadAttempts {
					return nil, clerrors.New(clerrors.CodeAPIError, "inventory temporarily unavailable", "")
				}
				return []*provision.SecurityGroup{{ID: "sg-1", Rules: []provision.SGRule{rule}}}, nil
			}
			for attempt = 0; attempt <= tc.unreadAttempts; attempt++ {
				// A fresh engine must reconstruct the target from the same state file.
				current, _ := newTestEngine(t, cloud, newMockDeployer())
				current.StateDir = dir
				current.Config = eng.Config
				before := reads
				var result DestroyResult
				err := current.Destroy(context.Background(), DestroyOptions{Force: true, Result: &result})
				if err != nil {
					t.Fatal(err)
				}
				remaining := readState(t, dir).FindDeployment(rec.ID)
				if reads == before {
					t.Errorf("attempt %d: never retried project inventory", attempt+1)
				}
				if attempt < tc.unreadAttempts {
					if !result.CleanupPending || remaining != nil && (remaining.VMID != rec.VMID || remaining.ProjectID != rec.ProjectID) {
						t.Errorf("attempt %d: partial=%t, saved receipt=%t, want the VM and project retained", attempt+1, result.CleanupPending, remaining != nil)
					}
					if _, empty := current.NothingToDestroy(); empty {
						t.Errorf("attempt %d: unresolved inventory reported empty", attempt+1)
					}
					if len(cloud.deletedRules) != 0 {
						t.Fatalf("deleted rules with no inventory: %v", cloud.deletedRules)
					}
				} else if result.CleanupPending || remaining != nil || !slices.Equal(cloud.deletedRules, []string{"sg-1/" + rule.ID}) {
					t.Errorf("final attempt: partial=%t, saved=%t, rule deletes=%v; want confirmed cleanup of the labelled rule",
						result.CleanupPending, remaining != nil, cloud.deletedRules)
				}
			}
		})
	}
}

// Scope is a property of the whole operation. Resolving a recorded VM and
// IP must not permit deleting them before the rule phase discovers that
// the profile or project cannot be used for the same deployment.
func TestDestroy_OneProvenScopeForAllResources(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		differentProject, differentOwner bool
	}{
		{"same-project-and-owner", false, false}, {"switched-project", true, false}, {"switched-owner", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := rulesRecord()
			rec.ProjectID = projectA
			rec.CreatedBy = state.NewMaker("account-recorded", "key-recorded")
			eng, cloud, _ := destroyFixture(t, rec)
			if !tc.differentProject {
				eng.Config.ProjectID = rec.ProjectID
			}
			eng.Config.KeyID = "key-recorded"
			eng.Config.ServiceAccountID = "account-recorded"
			if tc.differentOwner {
				eng.Config.ServiceAccountID = "account-current"
				eng.Config.KeyID = "key-current"
			}
			cloud.listSecurityGroupsFn = func(_ context.Context, project string) ([]*provision.SecurityGroup, error) {
				// The fake uses an empty project to read one group by ID.
				if project != "" && project != rec.ProjectID {
					t.Fatalf("listed groups in %q, want recorded project %q", project, rec.ProjectID)
				}
				return []*provision.SecurityGroup{{ID: "sg-1"}}, nil
			}
			vmDeletes := 0
			cloud.getVMFn = func(_ context.Context, id string) (*provision.VM, error) {
				if id != rec.VMID {
					t.Fatalf("unexpected VM %s", id)
				}
				if vmDeletes > 0 {
					return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
				}
				return &provision.VM{ID: id, Status: "active"}, nil
			}
			cloud.deleteVMFn = func(_ context.Context, id string) error {
				if id != rec.VMID {
					t.Fatalf("unexpected VM %s", id)
				}
				vmDeletes++
				return nil
			}
			cloud.deleteFloatingIPFn = func(_ context.Context, id, project string) error {
				if id != rec.FloatingIPID || project != rec.ProjectID {
					t.Fatalf("unexpected address or project %q/%q", id, project)
				}
				return nil
			}
			before, err := os.ReadFile(state.DefaultPath(eng.StateDir))
			if err != nil {
				t.Fatal(err)
			}
			preview, offered, previewErr := eng.DestroyPreview(context.Background())
			var result DestroyResult
			runErr := eng.Destroy(context.Background(), DestroyOptions{Force: true, Result: &result})
			if tc.differentOwner || (tc.differentProject && runErr != nil) {
				after, err := os.ReadFile(state.DefaultPath(eng.StateDir))
				if err != nil {
					t.Fatal(err)
				}
				wrote := vmDeletes != 0 || len(cloud.deletedFIPs) != 0 || len(cloud.deletedRules) != 0
				if previewErr == nil || offered || runErr == nil || wrote || !bytes.Equal(before, after) {
					t.Fatalf("unproven scope: offered=%t preview_error=%v run_error=%v deletes VM/IP/rule=%d/%d/%d state_unchanged=%t",
						offered, previewErr, runErr,
						vmDeletes, len(cloud.deletedFIPs), len(cloud.deletedRules), bytes.Equal(before, after))
				}
				return
			}
			if preview.TargetArgs["project_id"] != rec.ProjectID || preview.TargetArgs["vm_id"] != rec.VMID {
				t.Errorf("approved target = %v, want recorded project and VM", preview.TargetArgs)
			}
			addressReleased := slices.Equal(cloud.deletedFIPs, []string{rec.FloatingIPID})
			rulesRemoved := slices.Equal(cloud.deletedRules, []string{"sg-1/rule-80", "sg-1/rule-53"})
			receipt := readState(t, eng.StateDir).FindDeployment(rec.ID)
			if runErr != nil || previewErr != nil || !offered || result.CleanupPending ||
				vmDeletes != 1 || !addressReleased || !rulesRemoved || receipt != nil {
				t.Fatalf("one-scope cleanup: offered=%t preview_error=%v run_error=%v partial=%t deletes VM/IP/rule=%d/%d/%d",
					offered, previewErr, runErr, result.CleanupPending, vmDeletes, len(cloud.deletedFIPs), len(cloud.deletedRules))
			}
		})
	}
}

// A scoped 404 can settle a known deployment. A receipt that predates
// project recording has no such proof, even when a previous call left it
// in the destroying state.
func TestDestroy_LegacyVMReceiptNeedsProjectEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, project, status string
	}{
		{"known-project", projectA, "running"},
		{"legacy-running", "", "running"},
		{"legacy-destroying", "", "destroying"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := runningRecord()
			rec.ProjectID, rec.Status = tc.project, tc.status
			rec.FloatingIPID, rec.VMIP = "", ""
			eng, cloud, _ := destroyFixture(t, rec)
			eng.Config.ProjectID = projectA
			cloud.deleteVMFn = func(_ context.Context, id string) error {
				if id != rec.VMID {
					t.Fatalf("unexpected VM %q", id)
				}
				return clerrors.New(clerrors.CodeVMNotFound, "VM not visible", "")
			}
			var outcome DestroyResult
			err := eng.Destroy(context.Background(), DestroyOptions{Force: true, Result: &outcome})
			saved := readState(t, eng.StateDir).FindDeployment(rec.ID)
			if tc.project != "" {
				if err != nil || outcome.CleanupPending || saved != nil {
					t.Fatalf("confirmed absence in recorded project: err=%v partial=%t saved=%t", err, outcome.CleanupPending, saved != nil)
				}
				return
			}
			if saved == nil || saved.VMID != rec.VMID || saved.ProjectID != "" ||
				(err == nil && !outcome.CleanupPending) {
				t.Fatalf("unconfirmed absence: err=%v partial=%t saved=%+v", err, outcome.CleanupPending, saved)
			}
		})
	}
}
