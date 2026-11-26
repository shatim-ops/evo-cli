// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The service can accept deletion before a paid address disappears. The receipt
// must survive a pending or unreadable result, so the next destroy can finish it.
func TestDestroy_FloatingIPConfirmation(t *testing.T) {
	for _, initial := range []string{"visible", "unreadable", "gone"} {
		t.Run(initial, func(t *testing.T) {
			rec := runningRecord()
			eng, cloud, out := destroyFixture(t, rec)
			eng.Printer = output.NewWithWriters(out, io.Discard, output.ModeJSON, false)
			readback := initial
			afterDelete := false
			confirmedRead := false
			fip := &provision.FloatingIP{ID: rec.FloatingIPID, IPAddress: rec.VMIP}

			cloud.deleteFloatingIPFn = func(_ context.Context, id, project string) error {
				if id != rec.FloatingIPID || project != eng.Config.ProjectID {
					t.Fatalf("release requested for %s/%s, want the recorded address", project, id)
				}
				afterDelete = true
				return nil
			}
			readAddress := func() (*provision.FloatingIP, error) {
				if afterDelete {
					confirmedRead = true
					switch readback {
					case "unreadable":
						return nil, errors.New("fixture: readback unavailable")
					case "gone":
						return nil, nil
					}
				}
				return fip, nil
			}
			cloud.getFloatingIPFn = func(_ context.Context, id string) (*provision.FloatingIP, error) {
				if id != rec.FloatingIPID {
					t.Fatalf("read address %s, want %s", id, rec.FloatingIPID)
				}
				got, err := readAddress()
				if got == nil && err == nil {
					return nil, clerrors.New(clerrors.CodeNotFound, "address gone", "")
				}
				return got, err
			}
			cloud.listFloatingIPsFn = func(_ context.Context, project string) ([]*provision.FloatingIP, error) {
				if project != eng.Config.ProjectID {
					t.Fatalf("read addresses for project %s, want %s", project, eng.Config.ProjectID)
				}
				got, err := readAddress()
				if got == nil {
					return nil, err
				}
				return []*provision.FloatingIP{got}, nil
			}

			for {
				out.Reset()
				confirmedRead = false
				var result DestroyResult
				err := eng.Destroy(context.Background(), DestroyOptions{Force: true, Result: &result})
				if code := clerrors.ExitCode(err); code != 0 {
					t.Fatalf("%s: destroy exit %d, want 0: %v", readback, code, err)
				}
				var answer map[string]any
				if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
					t.Fatalf("%s: invalid destroy JSON: %v", readback, err)
				}
				pending := readback != "gone"
				wantStatus := "destroyed"
				if pending {
					wantStatus = "partial"
				}
				if answer["status"] != wantStatus || answer["cleanup_pending"] != pending || result.CleanupPending != pending || !confirmedRead {
					t.Fatalf("%s: status=%v cleanup_pending=%v result.CleanupPending=%t read_after_delete=%t; want %s/%t with a final read", readback, answer["status"], answer["cleanup_pending"], result.CleanupPending, confirmedRead, wantStatus, pending)
				}
				active := readActive(t, eng.StateDir)
				if !pending {
					if active != nil {
						t.Fatalf("gone: active receipt left after complete cleanup: %+v", active)
					}
					break
				}
				if active == nil || active.FloatingIPID != rec.FloatingIPID {
					t.Fatalf("%s: receipt lost before confirmed deletion: %+v", readback, active)
				}
				readback = "gone"
			}
		})
	}
}
