// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Start with a deployment from an older client and its name-only create receipt.
// From the first destroy onward every change to this directory goes through evo.
func TestCleanup_ReceiptToRedeploy(t *testing.T) {
	w, fake := withJSONWorld(t)
	_, done := recordDeployment(t, w)
	defer done()
	path := state.DefaultPath(w.dir)
	sf, err := state.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	d := *sf.ActiveDeployment()
	sf.LostFloatingIPs = []state.LostFloatingIP{{Name: "old-request", ProjectID: d.ProjectID, VMID: d.VMID, RequestedAt: time.Unix(100, 0)}}
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}

	previous := w.rt.extra
	created := 0
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1.1/vms" {
			// The fake uses a fixed ID. A newly created VM becomes visible
			// again after the old VM with that ID was destroyed.
			fake.mu.Lock()
			fake.vmGone = false
			fake.mu.Unlock()
			created++
		}
		return previous(r)
	}

	run := func(args ...string) map[string]any {
		t.Helper()
		exit, out, errOut := runCLI(t, "", append([]string{"--json"}, args...)...)
		var result map[string]any
		if exit != 0 || json.Unmarshal([]byte(out), &result) != nil {
			t.Fatalf("evo %v: exit %d, %s%s", args, exit, out, errOut)
		}
		return result
	}
	list := func() []any {
		t.Helper()
		result := run("cleanup", "receipts", "--deployment-id", d.ID)
		receipts, ok := result["receipts"].([]any)
		if !ok {
			t.Fatalf("expected an array of receipts, got %v", result)
		}
		return receipts
	}

	if r := run("destroy", "--yes"); r["status"] != "partial" || r["cleanup_pending"] != true {
		t.Fatalf("destroy with unresolved receipt: %v", r)
	}
	agent := mcp.New(toolEngine, "fixture")
	declareStateDir(agent)
	for _, release := range []bool{false, true} {
		raw := stdioRaw(t, agent, "destroy", map[string]any{"vm_id": d.VMID, "release_ip": release, "confirmed": true})
		answer, _ := roDecode(t, raw)
		var report map[string]any
		if answer.isErr || json.Unmarshal([]byte(answer.text), &report) != nil || report["cleanup_pending"] != true || !strings.Contains(answer.text, "evo cleanup receipts") {
			t.Fatalf("MCP release_ip=%t must show how to inspect the pending receipt: %s", release, raw)
		}
	}

	receipts := list()
	if len(receipts) != 1 {
		t.Fatalf("expected one unresolved receipt, got %v", receipts)
	}
	receipt := receipts[0].(map[string]any)
	id, _ := receipt["receipt_id"].(string)
	if id == "" || receipt["name"] != "old-request" || receipt["deployment_id"] != d.ID {
		t.Fatalf("receipt omitted target information: %v", receipt)
	}
	if code, _, _ := runCLI(t, "", "--json", "deploy", "--yes"); code == 0 || created != 0 {
		t.Fatalf("deploy before reconciliation: exit %d, new VMs %d", code, created)
	}
	resolve := []string{"cleanup", "resolve", id, "--deployment-id", d.ID, "--outcome", "not_created", "--evidence", "audit-operation-7341"}
	run(append(resolve, "--dry-run")...)
	if code, _, _ := runCLI(t, "", append([]string{"--json"}, resolve...)...); code == 0 {
		t.Fatal("a non-interactive call without consent closed the receipt")
	}
	if r := list(); len(r) != 1 || r[0].(map[string]any)["receipt_id"] != id {
		t.Fatalf("preview or refused confirmation changed the receipt: %v", r)
	}
	if r := run(append(resolve, "--yes")...); r["status"] != "reconciled" || r["receipt_id"] != id {
		t.Fatalf("confirmation of verified outcome: %v", r)
	}
	if r := list(); len(r) != 0 {
		t.Fatalf("confirmed receipt still pending: %v", r)
	}
	if r := run("destroy", "--yes"); r["status"] != "destroyed" || r["cleanup_pending"] != false {
		t.Fatalf("finish destroy after reconciliation: %v", r)
	}
	run("deploy", "--yes")
	sf, err = state.Read(path)
	if err != nil || created != 1 || sf.ActiveDeployment() == nil || sf.ActiveDeployment().Status != "running" {
		t.Fatalf("same-directory deploy: new VMs %d, state %v, error %v", created, sf, err)
	}
}
