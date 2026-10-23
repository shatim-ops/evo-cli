// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// neverReleaseCloud deletes the VM, but its floating IP stays attached.
type neverReleaseCloud struct{ targetCloud }

func (c *neverReleaseCloud) DeleteFloatingIP(context.Context, string, string) error {
	c.fipTries++
	return clerrors.New(clerrors.CodeAPIError, "floating ip is attached", "")
}

// R1: destroy vm_id=… whose floating IP is not released keeps the record
// (destroying, with the FIP) so a later destroy finishes; it used to drop
// the record first and leave the FIP billed without a trace.
func TestQAr3_DestroyTargeted_FIPReleaseFailsKeepsRecord(t *testing.T) {
	cloud := &neverReleaseCloud{}
	r, dir := targetRegistry(t, cloud)
	text, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "0000a1a1-0000-4000-8000-000000000001", "confirmed": true})
	if !strings.Contains(text, "NOT released") {
		t.Fatalf("answer: %s", text)
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	for _, d := range sf.Deployments {
		if d.VMID == "0000a1a1-0000-4000-8000-000000000001" {
			if d.Status != "destroying" || d.FloatingIPID != "fip-1" {
				t.Errorf("record = %+v, want destroying with floating_ip_id fip-1", d)
			}
			return
		}
	}
	t.Fatalf("FIP not released, record dropped: %+v", sf.Deployments)
}

// The released FIP: the record goes, as before.
func TestQAr3_DestroyTargeted_FIPReleasedDropsRecord(t *testing.T) {
	r, dir := targetRegistry(t, &targetCloud{})
	if _, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "0000a1a1-0000-4000-8000-000000000001", "confirmed": true}); isErr {
		t.Fatal("destroy failed")
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	if len(sf.Deployments) != 0 {
		t.Errorf("record kept after a full destroy: %+v", sf.Deployments)
	}
}

// R3: stack_destroy of a job whose step never ran deletes nothing by name:
// a cluster with that name belongs to someone else.
func TestQAr3_StackDestroy_PlannedJobLeavesForeignAlone(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"s","postgres":{"name":"shared-db"}}`)
	deleted := ""
	r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{map[string]any{"name": "shared-db", "id": "pg-foreign"}}})
	r.handlers["pg_delete"] = func(_ context.Context, a map[string]any) (string, bool) {
		deleted, _ = a["cluster_id"].(string)
		return toJSON(map[string]any{"status": "deleted"}), false
	}
	r.handleStackDestroy(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	if deleted != "" {
		t.Fatalf("a planned job (created nothing) deleted cluster %s", deleted)
	}
}
