// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/confirm"
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

	// The restricted server can no longer match the VM in the project. Neither
	// transport exposes a local receipt. The owner finishes from this directory.
	agent.SetProjectBound()
	handler := mcp.NewHTTPHandler(agent, mcp.HTTPOptions{BearerToken: "parity-token"})
	defer handler.Close()
	wires := roServers{roHTTP: httptest.NewServer(handler)}
	defer wires.roHTTP.Close()
	params := map[string]any{"name": "destroy", "arguments": map[string]any{"vm_id": d.VMID, "confirmed": true}}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	w.rt.takeWrites()
	stdio, _ := wires.stdio(t, agent, "tools/call", params)
	httpAnswer, _ := wires.http(t, "tools/call", params)
	for _, answer := range []roAnswer{stdio, httpAnswer} {
		var report map[string]any
		if !answer.isErr || json.Unmarshal([]byte(answer.text), &report) != nil || report["reason"] != "NOT_FOUND" ||
			report["cleanup_pending"] != nil || strings.Contains(answer.text, "evo cleanup receipts") {
			t.Fatalf("restricted destroy after the VM disappeared: %s", answer.text)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || len(w.rt.takeWrites()) != 0 {
		t.Fatalf("restricted destroy changed cloud or local state: %v", err)
	}
	exit, out, errOut := runCLI(t, "", "destroy", "--yes")
	if exit != 0 || !strings.Contains(out+errOut, "evo cleanup receipts") {
		t.Fatalf("same-directory terminal retry: exit %d, %s%s", exit, out, errOut)
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

// A preview and a consent request both advertise a locally valid action.
// Neither may be shown for a reference the locked action would refuse.
func TestCleanup_InvalidEvidenceBeforeConsent(t *testing.T) {
	fc := setupCLI(t)
	oldClient := httpClient
	httpClient = &http.Client{Transport: contractRT{status: http.StatusForbidden, body: `{"message":"no cloud access"}`}}
	t.Cleanup(func() { httpClient = oldClient })
	const project = "11111111-1111-1111-1111-111111111111"
	saveProfile(t, "default", project)
	fixture := localReceiptArgs(t)
	const address = "00000000-0000-4000-8000-000000000077"
	before, err := os.ReadFile(state.DefaultPath("."))
	if err != nil {
		t.Fatal(err)
	}
	oldTTY, oldIn := confirm.StdinIsTTY, confirm.Stdin
	t.Cleanup(func() { confirm.StdinIsTTY, confirm.Stdin = oldTTY, oldIn })
	for _, mode := range []struct {
		name  string
		flags []string
		tty   bool
	}{
		{"dry_run", []string{"--json", "--dry-run"}, false},
		{"no_terminal", []string{"--json"}, false},
		{"terminal", nil, true},
		{"yes", []string{"--json", "--yes"}, false},
	} {
		t.Run(mode.name, func(t *testing.T) {
			confirm.StdinIsTTY = func() bool { return mode.tty }
			for _, tc := range []struct{ name, ref string }{
				{"missing", ""}, {"empty_value", ""}, {"blank", "   "}, {"control", "case\n123"},
				{"too_long", strings.Repeat("x", 1025)}, {"list", "[123]"}, {"object", `{"items":[]}`},
				{"punctuation", "..."}, {"sentinel", "  EmPtY  "}, {"null", "null"}, {"none", "none"}, {"outcome", "released"},
				{"receipt", fixture[0]}, {"field_label", "receipt_id"}, {"deployment", fixture[2]}, {"vm", "test-vm"},
				{"cloud_name", "pending-test-address"}, {"project", project}, {"address_id", address},
			} {
				t.Run(tc.name, func(t *testing.T) {
					args := []string{"cleanup", "resolve", fixture[0], "--deployment-id", fixture[2]}
					if tc.name == "address_id" {
						args = append(args, "--outcome", "released", "--address-id", address)
					} else {
						args = append(args, "--outcome", "not_created")
					}
					if tc.name != "missing" {
						args = append(args, "--evidence", tc.ref)
					}
					answer := strings.NewReader("n\n")
					confirm.Stdin = answer
					exit, out, stderr := runCLI(t, "", append(args, mode.flags...)...)
					if exit != 1 || out != "" || answer.Len() != 2 || strings.Contains(stderr, confirm.Prompt()) || strings.Contains(stderr, `"preview"`) {
						t.Fatalf("invalid evidence: exit=%d, read answer=%t, stdout=%q, stderr=%q", exit, answer.Len() != 2, out, stderr)
					}
					if !mode.tty {
						if e, ok := errorEnvelope(stderr); !ok || e.Reason != "USAGE" || e.Code != 1 {
							t.Fatalf("expected USAGE/1, got %q", stderr)
						}
					}
				})
			}
		})
	}
	// A reference to an independent operation still reaches the human prompt.
	confirm.StdinIsTTY = func() bool { return true }
	answer := strings.NewReader("n\n")
	confirm.Stdin = answer
	exit, _, stderr := runCLI(t, "", append([]string{"cleanup", "resolve"}, fixture...)...)
	if exit != 3 || answer.Len() != 0 || !strings.Contains(stderr, confirm.Prompt()) {
		t.Fatalf("valid reference should prompt and respect a decline: exit=%d, stderr=%q", exit, stderr)
	}
	after, err := os.ReadFile(state.DefaultPath("."))
	if err != nil || !bytes.Equal(before, after) || fc.iamCalls.Load() != 0 {
		t.Fatalf("receipt state changed or cloud credentials used: read=%v, IAM calls=%d", err, fc.iamCalls.Load())
	}
}

// A reopened receipt can have more than one disproved decision. Their
// references stay in the local audit, never in the listing or consent preview.
func TestCleanup_ContradictedEvidenceBeforeConsent(t *testing.T) {
	w, _ := withJSONWorld(t)
	_, done := recordDeployment(t, w)
	t.Cleanup(done)
	path := state.DefaultPath(w.dir)
	sf, err := state.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	d := sf.ActiveDeployment()
	d.Status, d.FloatingIPID, d.LegacyFloatingIPPending = "destroying", "", true
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}
	eng, err := toolEngine(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := eng.PendingFloatingIPReceipts(d.ID)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("initial receipts: %v, %v", receipts, err)
	}
	id := receipts[0].ReceiptID
	const first, last, other = "independent case 301", "https://records.example/Case?rev=A#Final", "other case 408"
	decision := state.FloatingIPResolution{Kind: "legacy_deployment", ProjectID: d.ProjectID, ReceiptID: id, Contradicted: true, Outcome: "not_created"}
	for _, ref := range []string{first, last, other} {
		decision.Evidence = ref
		// Older state can mark the deployment pending without flagging its latest resolution.
		decision.Contradicted = ref != last
		if ref == other {
			decision.ReceiptID = "different-receipt"
		}
		d.FloatingIPResolutions = append(d.FloatingIPResolutions, decision)
	}
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	exit, out, stderr := runCLI(t, "", "cleanup", "receipts", "--json")
	if exit != 0 || !strings.Contains(out, id) || strings.Contains(out+stderr, first) || strings.Contains(out+stderr, last) {
		t.Fatalf("reopened receipt should be visible without prior evidence: exit %d, %s %s", exit, out, stderr)
	}
	args := []string{"cleanup", "resolve", id, "--deployment-id", d.ID, "--outcome", "not_created", "--json", "--evidence"}
	prevTTY, prevIn := confirm.StdinIsTTY, confirm.Stdin
	t.Cleanup(func() { confirm.StdinIsTTY, confirm.Stdin = prevTTY, prevIn })
	confirm.StdinIsTTY = func() bool { return true }
	for _, ref := range []string{first, strings.ToUpper(first), " " + last + " ", "HTTPS://RECORDS.EXAMPLE/Case?rev=A#Final"} {
		for _, flags := range [][]string{nil, {"--dry-run"}, {"--yes"}} {
			answer := strings.NewReader("n\n")
			confirm.Stdin = answer
			exit, out, stderr := runCLI(t, "", append(append(args, ref), flags...)...)
			e, ok := errorEnvelope(stderr)
			if exit != 1 || !ok || e.Reason != "USAGE" || out != "" || answer.Len() != 2 || strings.Contains(stderr, confirm.Prompt()) {
				t.Errorf("reused evidence %q: flags %v, exit %d, stdout %q, stderr %q", ref, flags, exit, out, stderr)
			}
		}
	}
	// A different receipt or a different document in the same service can supply fresh evidence.
	for _, ref := range []string{other, "https://records.example/case?rev=A#Final", "https://records.example/Case?rev=a#Final", "https://records.example/Case?rev=A#final"} {
		exit, out, stderr = runCLI(t, "", append(args, ref, "--dry-run")...)
		if exit != 0 || parseObject(out)["status"] != "dry_run" || strings.Contains(out+stderr, first) || strings.Contains(out+stderr, last) {
			t.Errorf("fresh evidence %q preview: exit %d, %s %s", ref, exit, out, stderr)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("evidence validation or preview changed the local audit", err)
	}
}
