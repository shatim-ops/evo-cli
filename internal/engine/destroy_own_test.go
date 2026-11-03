// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
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
