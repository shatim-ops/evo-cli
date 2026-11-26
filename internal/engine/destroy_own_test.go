// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// D73, the second part (the record ↔ the cloud before destroy): a record an
// evo before 1.7 made by adopting a VM of the name (deploy_apply) holds a
// VM made long before the record. destroy does not delete it: it refuses
// before the question, keeps the record, and names the way out — a delete
// is the person's own (TestHints_DeleteIsThePersonsDecision). A VM made
// with its record, or one of a record without a time, goes as before.
func TestDestroy_ARecordOfAnAdoptedVMIsNotDeleted(t *testing.T) {
	recorded := time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name    string
		made    time.Time
		recTime time.Time
		refused bool
	}{
		{"made a day before the record: adopted", recorded.Add(-24 * time.Hour), recorded, true},
		{"made with the record", recorded.Add(-time.Minute), recorded, false},
		{"no time on the VM: as before", time.Time{}, recorded, false},
		{"no time on the record: as before", recorded.Add(-24 * time.Hour), time.Time{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deleted := 0
			cloud := &mockCloudAPI{
				getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
					if deleted > 0 {
						return nil, clerrors.New(clerrors.CodeVMNotFound, "not found", "")
					}
					return &provision.VM{ID: id, Name: "web", Status: "running", CreatedAt: tc.made}, nil
				},
				deleteVMFn: func(context.Context, string) error { deleted++; return nil },
			}
			e, dir, _ := qaBEngine(t, cloud)
			writeRecord(t, dir, state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMName: "web", ProjectID: e.Config.ProjectID,
				Status: "running", CreatedAt: tc.recTime})
			err := e.Destroy(context.Background(), DestroyOptions{Force: true})
			if !tc.refused {
				if err != nil || deleted != 1 {
					t.Fatalf("own VM: err %v, deletes %d", err, deleted)
				}
				return
			}
			if clerrors.Reason(err) != clerrors.CodeUsage || deleted != 0 {
				t.Fatalf("adopted VM: err %v, deletes %d; want USAGE and no delete", err, deleted)
			}
			if a := readActive(t, dir); a == nil || a.VMID != "vm-1" || a.Status != "running" {
				t.Errorf("the record is not kept as it was: %+v", a)
			}
			// Before the question: the preview is refused the same way.
			if _, _, perr := e.DestroyPreview(context.Background()); clerrors.Reason(perr) != clerrors.CodeUsage {
				t.Errorf("the preview of destroy: %v, want the same refusal", perr)
			}
		})
	}
}

// The check is a read before a delete: a VM it cannot read (a timeout, a
// 5xx) refuses destroy before the question and keeps the record — a delete
// after a failed check would take the very VM D73 guards. The error keeps
// its class; a VM the cloud no longer has goes on to already_deleted (D26),
// and a key the cloud refuses is left to the call that needs it.
func TestDestroy_AnUnreadCheckRefusesTheDelete(t *testing.T) {
	recorded := time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name    string
		getErr  error
		refused string // the reason; "" — destroy goes on as before
	}{
		{"a 5xx", clerrors.New(clerrors.CodeAPIError, "API 503", ""), clerrors.CodeAPIError},
		{"a timeout", context.DeadlineExceeded, clerrors.ReasonGeneral},
		{"the VM is gone", clerrors.New(clerrors.CodeVMNotFound, "not found", ""), ""},
		{"the key is refused", clerrors.New(clerrors.CodeAuthFailed, "401", ""), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deleted := 0
			cloud := &mockCloudAPI{
				getVMFn: func(context.Context, string) (*provision.VM, error) {
					if deleted > 0 {
						return nil, clerrors.New(clerrors.CodeVMNotFound, "not found", "")
					}
					return nil, tc.getErr
				},
				deleteVMFn: func(context.Context, string) error { deleted++; return nil },
			}
			e, dir, _ := qaBEngine(t, cloud)
			writeRecord(t, dir, state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMName: "web", ProjectID: e.Config.ProjectID,
				Status: "running", CreatedAt: recorded})
			_, _, perr := e.DestroyPreview(context.Background())
			err := e.Destroy(context.Background(), DestroyOptions{Force: true})
			if tc.refused == "" {
				if perr != nil {
					t.Errorf("the preview: %v, want none", perr)
				}
				return
			}
			if clerrors.Reason(err) != tc.refused || deleted != 0 {
				t.Fatalf("err %v (%s), deletes %d; want %s and no delete", err, clerrors.Reason(err), deleted, tc.refused)
			}
			var ce *clerrors.CLIError
			if !errors.As(err, &ce) || ce.Hint == "" {
				t.Errorf("the refusal names no way out: %v", err)
			}
			if a := readActive(t, dir); a == nil || a.VMID != "vm-1" || a.Status != "running" {
				t.Errorf("the record is not kept as it was: %+v", a)
			}
			if clerrors.Reason(perr) != tc.refused {
				t.Errorf("the preview of destroy: %v, want the same refusal", perr)
			}
		})
	}
}

func TestDestroy_ResumesDiscoveryAfterRestart(t *testing.T) {
	rec := runningRecord()
	e, c, out := destroyFixture(t, rec)
	path := state.DefaultPath(e.StateDir)
	e.Printer = output.NewWithWriters(out, io.Discard, output.ModeJSON, false)
	available := false
	c.listSecurityGroupsFn = func(context.Context, string) ([]*provision.SecurityGroup, error) {
		if !available {
			return nil, clerrors.New(clerrors.CodeAPIError, "inventory unavailable", "")
		}
		return []*provision.SecurityGroup{{ID: "group", Rules: []provision.SGRule{
			{ID: "ours", Description: sgrules.RuleLabel("80", rec.VMID), Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80"},
			{ID: "other", Description: sgrules.RuleLabel("80", "other-vm"), Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80"},
		}}}, nil
	}
	for attempt, want := range []string{"partial", "rules_pending", "destroyed", "none"} {
		available = attempt >= 2
		restarted := *e // each call reads state from disk as a fresh CLI process would
		out.Reset()
		var result DestroyResult
		if err := restarted.Destroy(t.Context(), DestroyOptions{Force: true, Result: &result}); err != nil {
			t.Fatal(err)
		}
		var answer struct {
			Status         string
			CleanupPending bool `json:"cleanup_pending"`
		}
		if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
			t.Fatal(err, out)
		}
		if answer.Status != want || answer.CleanupPending != (attempt < 2) || result.CleanupPending != answer.CleanupPending {
			t.Fatalf("attempt %d: %s, result=%+v", attempt, out, result)
		}
		sf, err := state.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		if (sf.FindDeployment(rec.ID) != nil) != (attempt < 2) || (attempt < 3 && result.VMID != rec.VMID) {
			t.Fatalf("attempt %d: saved records=%v, result=%+v", attempt, sf.Deployments, result)
		}
	}
	if !slices.Equal(c.deletedRules, []string{"group/ours"}) || len(c.deletedFIPs) != 1 {
		t.Fatalf("deleted rules=%v, addresses=%v", c.deletedRules, c.deletedFIPs)
	}
}

func TestDestroy_ProvesEntireScopeBeforeConsent(t *testing.T) {
	for _, scenario := range []string{"project invisible", "another rule project", "another rule maker", "conflicting request project"} {
		t.Run(scenario, func(t *testing.T) {
			rec := runningRecord()
			e, c, _ := destroyFixture(t, rec)
			path := state.DefaultPath(e.StateDir)
			e.Config.KeyID = "original-test-key"
			rec.ProjectID, rec.CreatedBy = e.Config.ProjectID, e.Maker()
			sf := &state.File{Version: 1}
			switch scenario {
			case "project invisible":
				rec.ProjectID = projectA
			case "conflicting request project":
				sf.LostFloatingIPs = []state.LostFloatingIP{{VMID: rec.VMID, ProjectID: projectA, Name: "unsettled"}}
			default:
				old := state.DeploymentRecord{ID: "past", VMID: "old-vm", Status: "destroyed", ProjectID: e.Config.ProjectID, CreatedBy: e.Maker(), OpenedRules: []state.SGRule{{ID: "old-rule", SecurityGroupID: "group", Port: "80/tcp"}}}
				if scenario == "another rule maker" {
					old.CreatedBy = state.NewMaker("", "foreign-key")
				} else {
					old.ProjectID = projectA
				}
				sf.UpsertDeployment(old)
			}
			sf.UpsertDeployment(rec)
			if err := state.Write(path, sf); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			c.deleteVMFn = func(context.Context, string) error { t.Fatal("VM write before scope verification"); return nil }
			if _, question, err := e.DestroyPreview(t.Context()); err == nil || question {
				t.Fatalf("preview: asked=%v, err=%v", question, err)
			}
			err = e.Destroy(t.Context(), DestroyOptions{Gate: func(context.Context, confirm.Preview) (bool, error) {
				t.Fatal("asked for consent before establishing scope")
				return false, nil
			}})
			if clerrors.Reason(err) != clerrors.CodeConfigInvalid {
				t.Fatalf("destroy: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) || len(c.deletedRules)+len(c.deletedFIPs) != 0 {
				t.Fatal("changed receipts or cloud")
			}
		})
	}
}

// VM DELETE names the recorded project explicitly even when the configured
// project changed. The owner's receipt is also usable for rules-only retries.
type destroyProjectCloud struct {
	*mockCloudAPI
	vmProjects []string
}

func (c *destroyProjectCloud) DeleteVMInProject(ctx context.Context, id, project string) error {
	c.vmProjects = append(c.vmProjects, project)
	return c.mockCloudAPI.DeleteVM(ctx, id)
}

func TestDestroy_RecordedProjectIsUsedThroughout(t *testing.T) {
	rec := rulesRecord()
	rec.ProjectID = projectA
	e, base, _ := destroyFixture(t, rec)
	e.Config.KeyID = "organization-key"
	rec.CreatedBy = e.Maker()
	path := state.DefaultPath(e.StateDir)
	if err := state.Write(path, &state.File{Version: 1, Deployments: []state.DeploymentRecord{rec}}); err != nil {
		t.Fatal(err)
	}
	c := &destroyProjectCloud{mockCloudAPI: base}
	e.Cloud = c
	base.listVMsInProjectFn = func(_ context.Context, project string) ([]*provision.VM, error) {
		if project != projectA {
			t.Fatalf("VM inventory project %q", project)
		}
		return []*provision.VM{{ID: rec.VMID}}, nil
	}
	base.listSecurityGroupsFn = func(_ context.Context, project string) ([]*provision.SecurityGroup, error) {
		// The mock's exact read of the rule's group calls this with no project.
		if project != "" && project != projectA {
			t.Fatalf("rule inventory project %q", project)
		}
		return []*provision.SecurityGroup{{ID: "sg-1", Rules: []provision.SGRule{{ID: rec.OpenedRules[0].ID}}}}, nil
	}
	base.deleteFloatingIPFn = func(_ context.Context, _, project string) error {
		if project != projectA {
			t.Fatalf("floating IP DELETE project %q", project)
		}
		return nil
	}
	pending := true
	base.deleteRuleFn = func(context.Context, string, string) error {
		if pending {
			return clerrors.New(clerrors.CodeAPIError, "try again", "")
		}
		return nil
	}
	var result DestroyResult
	if err := e.Destroy(t.Context(), DestroyOptions{Force: true, Result: &result}); err != nil || !result.CleanupPending {
		t.Fatalf("first destroy: %+v, %v", result, err)
	}
	pending = false
	if err := e.Destroy(t.Context(), DestroyOptions{Force: true, Result: &result}); err != nil || result.CleanupPending {
		t.Fatalf("rule retry: %+v, %v", result, err)
	}
	if !slices.Equal(c.vmProjects, []string{projectA}) || len(base.deletedFIPs) != 1 || len(base.deletedRules) == 0 {
		t.Fatalf("VM DELETE projects=%v, addresses=%v rules=%v", c.vmProjects, base.deletedFIPs, base.deletedRules)
	}
}
