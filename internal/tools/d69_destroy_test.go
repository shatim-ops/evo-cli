// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// D69, PR 5: destroy over MCP answers within the budget. A VM the cloud is
// still deleting is "deleting" — not deleted, not an error — with the next
// step; its floating IP and record stay until it is gone, and the repeat
// of the call waits for it again without a second DELETE.

// destroyWorld is a VM made by provision in a cloud that then deletes
// slowly, with a budgeted registry.
func destroyWorld(t *testing.T) (*faultCloud, string, *Registry, string) {
	t.Helper()
	faultToolsIsolate(t)
	cloud := &faultCloud{}
	dir := t.TempDir()
	timings := provisionTimings()
	timings.DestroyPollInterval = time.Millisecond
	timings.DestroyPollTimeout = time.Hour
	r := budgeted(budgetComputeRegistry(t, cloud, dir, timings))
	m := decode(t, mustCall(t, r, "provision", map[string]any{"name": "web", "confirmed": true}))
	vmID, _ := m["vm_id"].(string)
	if m["status"] != "ok" || vmID == "" {
		t.Fatalf("provision: %v", m)
	}
	cloud.mu.Lock()
	cloud.slow = true
	cloud.mu.Unlock()
	return cloud, dir, r, vmID
}

func deleteCalls(cloud *faultCloud) int {
	n := 0
	for _, c := range cloud.callLog() {
		if strings.HasPrefix(c, "DeleteVM") {
			n++
		}
	}
	return n
}

func TestD69_DestroyAnswersDeletingWithTheNextStep(t *testing.T) {
	cloud, dir, r, vmID := destroyWorld(t)
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": vmID, "confirmed": true})
	m := decode(t, text)
	if isErr || m["status"] != "deleting" || m["vm_id"] != vmID || m["vm_status"] != "deleting" {
		t.Fatalf("want deleting with the VM and its state, not an error: %s", text)
	}
	if m["next_tool"] != "destroy" {
		t.Errorf("next_tool %v, want destroy: %s", m["next_tool"], text)
	}
	if _, ok := m["way_out"]; ok {
		t.Errorf("the first answer, the delete just sent, names a way out: %s", text)
	}
	if args, _ := m["next_args"].(map[string]any); args["vm_id"] != vmID || args["confirmed"] != true {
		t.Errorf("next_args do not repeat the delete: %v", args)
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	if len(sf.Deployments) != 1 || sf.Deployments[0].Status != "destroying" || sf.Deployments[0].FloatingIPID == "" {
		t.Errorf("the record does not stay destroying with its floating IP: %+v", sf.Deployments)
	}
	cloud.mu.Lock()
	released := 0
	for _, f := range cloud.fips {
		if f.deleted {
			released++
		}
	}
	cloud.mu.Unlock()
	if released != 0 {
		t.Errorf("the floating IP was released while the VM is still there")
	}

	// The repeat while the cloud is still deleting: the same answer, no
	// second DELETE (Cloud.ru refuses one with 409 in deleting, OPS-18).
	before := deleteCalls(cloud)
	text, isErr = r.handlers["destroy"](context.Background(), map[string]any{"vm_id": vmID, "confirmed": true})
	m = decode(t, text)
	if isErr || m["status"] != "deleting" {
		t.Fatalf("repeat while deleting: %s", text)
	}
	// A VM that stays in deleting is a loop for an agent repeating the
	// call: the repeat names the way out of DeleteWayOut, the console or
	// support with the VM's id (OPS-18; axis 3).
	if way, _ := m["way_out"].(string); !strings.Contains(way, "support") || !strings.Contains(way, vmID) ||
		!strings.Contains(fmt.Sprint(m["next"]), way) {
		t.Errorf("the repeat that finds the VM deleting names no way out: %s", text)
	}
	if n := deleteCalls(cloud) - before; n != 0 {
		t.Errorf("the repeat sent %d DELETE of a VM already deleting", n)
	}

	// Gone: the repeat finishes — the record and the floating IP go.
	cloud.mu.Lock()
	for i := range cloud.vms {
		cloud.vms[i].gone = true
	}
	cloud.mu.Unlock()
	text, isErr = r.handlers["destroy"](context.Background(), map[string]any{"vm_id": vmID, "confirmed": true})
	if m := decode(t, text); isErr || (m["status"] != "already_deleted" && m["status"] != "ok") {
		t.Fatalf("the repeat after the VM is gone does not finish: %s", text)
	}
	sf, _ = state.Read(state.DefaultPath(dir))
	if a := sf.ActiveDeployments(); len(a) != 0 {
		t.Errorf("record left: %+v", a)
	}
}
