// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/provision"
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
		hint, _ := report["hint"].(string)
		if !strings.Contains(hint, "Ask the person") || !strings.Contains(hint, "MCP host") || !strings.Contains(hint, "evo cleanup receipts") {
			t.Fatalf("agent must hand local receipt verification to the owner: %s", raw)
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

type receiptAnswer struct {
	*strings.Reader
	atQuestion func()
}

func (a receiptAnswer) Read(p []byte) (int, error) {
	a.atQuestion()
	return a.Reader.Read(p)
}

// A pending deployment with two distinct local receipts, backed by the real
// Compute adapter. Each CLI invocation builds its own engine from this directory.
func releasedAddressWorld(t *testing.T) (parityWorld, state.DeploymentRecord, state.LostFloatingIP) {
	t.Helper()
	w := withParityWorld(t)
	path := state.DefaultPath(w.dir)
	d := state.DeploymentRecord{ID: "dep", ProjectID: pvProject, VMID: parityVM, Status: "destroying"}
	m := state.LostFloatingIP{Name: "first", Attempt: "first", ProjectID: pvProject, VMID: parityVM}
	n := m
	n.Name, n.Attempt = "other", "other"
	if err := state.Write(path, &state.File{Deployments: []state.DeploymentRecord{d}, LostFloatingIPs: []state.LostFloatingIP{m, n}}); err != nil {
		t.Fatal(err)
	}
	prev := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := prev(o)
		if err == nil {
			e.Cloud = provision.NewCloudRuAdapter(&http.Client{Transport: w.rt}, "")
		}
		return e, err
	}
	t.Cleanup(func() { toolEngine = prev })
	return w, d, n
}

// Both snapshots use the real Compute adapter and the same receipt and ID.
// The project's list is empty even when GET by ID still finds the address.
func TestCleanup_ReleasedAddressBeforeConsentAndAtWrite(t *testing.T) {
	const address = "00000000-0000-4000-8000-000000000077"
	for _, tc := range []struct {
		name, mode string
		statuses   []int
		wantCode   int
	}{
		{"visible dry run", "dry", []int{200}, 5},
		{"gone dry run", "dry", []int{404}, 0},
		{"visible interactive", "decline", []int{200}, 5},
		{"gone declined", "decline", []int{404}, 3},
		{"gone accepted", "accept", []int{404, 404}, 0},
		{"reappeared at acceptance", "accept", []int{404, 200}, 5},
		{"inaccessible", "yes", []int{403}, 2},
		{"incomplete card", "dry", []int{200}, 5},
		{"visible confirmed", "yes", []int{200}, 5},
		{"reappeared", "yes", []int{404, 200}, 5},
		{"gone confirmed", "yes", []int{404, 404}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, d, n := releasedAddressWorld(t)
			path := state.DefaultPath(w.dir)
			var steps []string
			gets := 0
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatalf("unexpected %s", r.Method)
				}
				switch r.URL.Path {
				case "/api/v1/floating-ips":
					if r.URL.Query().Get("project_id") != pvProject {
						t.Fatal("unscoped read")
					}
					steps = append(steps, "project")
					return consentJSON(200, `{"items":[],"total":0}`), nil
				case "/api/v1/floating-ips/" + address:
					if gets >= len(tc.statuses) {
						t.Fatal("unexpected repeated GET")
					}
					status := tc.statuses[gets]
					gets++
					steps = append(steps, "address")
					body := `{"message":"forbidden"}`
					if status == 200 {
						body = `{"id":"` + address + `"}`
						if tc.name == "incomplete card" {
							body = `{}`
						}
					} else if status == 404 {
						body = `[{"message":"floating IP not found"}]`
					}
					return consentJSON(status, body), nil
				default:
					t.Fatalf("unexpected read %s", r.URL.Path)
					return nil, nil
				}
			}
			code, out, stderr := runCLI(t, "", "--json", "cleanup", "receipts", "--deployment-id", d.ID)
			if code != 0 {
				t.Fatal(stderr)
			}
			var listing struct{ Receipts []engine.FloatingIPReceipt }
			if err := json.Unmarshal([]byte(out), &listing); err != nil || len(listing.Receipts) != 2 {
				t.Fatalf("invalid receipts %s", out)
			}
			args := []string{"--json", "cleanup", "resolve", listing.Receipts[0].ReceiptID, "--deployment-id", d.ID, "--outcome", "released", "--evidence", "audit-case-741", "--address-id", address}
			oldIn, oldTTY := confirm.Stdin, confirm.StdinIsTTY
			t.Cleanup(func() { confirm.Stdin, confirm.StdinIsTTY = oldIn, oldTTY })
			input := "n\n"
			if tc.mode == "accept" {
				input = "y\n"
			}
			answer := strings.NewReader(input)
			confirm.Stdin = receiptAnswer{answer, func() {
				if strings.Join(steps, " ") != "project address" {
					t.Errorf("the operator was asked after %v; want a scoped address check first", steps)
				}
			}}
			interactive := tc.mode == "decline" || tc.mode == "accept"
			confirm.StdinIsTTY = func() bool { return interactive }
			switch tc.mode {
			case "dry":
				args = append(args, "--dry-run")
			case "yes":
				args = append(args, "--yes")
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			code, out, stderr = runCLI(t, "", args...)
			if code != tc.wantCode || gets != len(tc.statuses) {
				t.Fatalf("got exit %d, steps %v: %s %s", code, steps, out, stderr)
			}
			if strings.Join(steps, " ") != strings.TrimSpace(strings.Repeat("project address ", gets)) {
				t.Fatalf("out of scope or out of order: %v", steps)
			}
			prompt := interactive && tc.statuses[0] == 404
			if strings.Contains(stderr, confirm.Prompt()) != prompt || (answer.Len() == 0) != prompt {
				t.Fatalf("wrong consent for %v: %s", steps, stderr)
			}
			if tc.name == "incomplete card" && !strings.Contains(stderr, "release is unconfirmed") {
				t.Fatalf("incomplete response reported a definite outcome: %s", stderr)
			}
			if tc.statuses[0] != 404 && (strings.Contains(out+stderr, `"preview"`) || strings.Contains(out+stderr, `"confirmed"`)) {
				t.Fatalf("invalid preview: %s %s", out, stderr)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if (tc.mode == "yes" || tc.mode == "accept") && code == 0 {
				sf, err := state.Read(path)
				if err != nil || len(sf.LostFloatingIPs) != 1 || sf.LostFloatingIPs[0].Name != n.Name || len(sf.Deployments[0].FloatingIPResolutions) != 1 || sf.Deployments[0].FloatingIPResolutions[0].ReceiptID != listing.Receipts[0].ReceiptID {
					t.Fatalf("incorrect applied receipt: %+v (%v)", sf, err)
				}
			} else if !bytes.Equal(before, after) {
				t.Fatal("pending receipt changed")
			}
			if writes := w.rt.takeWrites(); len(writes) != 0 {
				t.Fatalf("cloud writes: %v", writes)
			}
		})
	}
}

// A syntactically complete service message cannot settle a receipt if the
// exact GET or the project list is incomplete. Check before consent and
// again under the write lock. Two complete readbacks can settle the same receipt.
func TestCleanup_ReleasedAddressNeedsCompleteReadAfterRestart(t *testing.T) {
	const address = "00000000-0000-4000-8000-000000000077"
	const neighborID = "00000000-0000-4000-8000-000000000088"
	const brokenList, missingRowID = "project list read error", "unidentified project member"
	for _, tc := range []struct {
		name        string
		firstBroken int
		condition   string
	}{
		{"body shorter than Content-Length before consent", 1, shortAddressBody},
		{"body shorter than Content-Length at write", 2, shortAddressBody},
		{"body read error before consent", 1, brokenAddressBody},
		{"body read error at write", 2, brokenAddressBody},
		{"list read error at write", 2, brokenList},
		{"list member missing ID at write", 2, missingRowID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, d, neighbor := releasedAddressWorld(t)
			path := state.DefaultPath(w.dir)
			exactReads, projectReads := 0, 0
			complete := false
			listFault := tc.condition == brokenList || tc.condition == missingRowID
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatalf("resolve wrote to the cloud: %s %s", r.Method, r.URL.Path)
				}
				switch r.URL.Path {
				case "/api/v1/floating-ips":
					projectReads++
					if r.URL.Query().Get("project_id") != d.ProjectID {
						t.Fatalf("out-of-scope project read: %s", r.URL)
					}
					listBody := `{"items":[{"id":"` + neighborID + `","name":"other","description":"other"}],"total":1}`
					if !complete && listFault && projectReads >= tc.firstBroken {
						if tc.condition == missingRowID {
							return consentJSON(200, `{"items":[{"name":"other","description":"other"}],"total":1}`), nil
						}
						resp := consentJSON(200, listBody)
						resp.Body = io.NopCloser(io.MultiReader(resp.Body, iotest.ErrReader(io.ErrClosedPipe)))
						return resp, nil
					}
					return consentJSON(200, listBody), nil
				case "/api/v1/floating-ips/" + address:
					exactReads++
					if exactReads > 2 {
						t.Fatal("unexpected repeated GET")
					}
					condition := ""
					if !complete && !listFault && exactReads >= tc.firstBroken {
						condition = tc.condition
					}
					return missingAddressReadback(r, missingAddressBody, condition)
				default:
					t.Fatalf("resolve read an unrelated resource: %s", r.URL.Path)
					return nil, nil
				}
			}
			list := func() []engine.FloatingIPReceipt {
				t.Helper()
				code, out, stderr := runCLI(t, "", "cleanup", "receipts", "--deployment-id", d.ID, "--json")
				var got struct{ Receipts []engine.FloatingIPReceipt }
				if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
					t.Fatalf("list: exit %d, stdout %s, stderr %s", code, out, stderr)
				}
				return got.Receipts
			}
			initial := list()
			if len(initial) != 2 || initial[0].Name != "first" || initial[1].Name != neighbor.Name ||
				initial[0].ReceiptID == "" || initial[1].ReceiptID == "" || initial[0].ReceiptID == initial[1].ReceiptID {
				t.Fatalf("expected target and neighbor: %+v", initial)
			}
			args := []string{"--json", "cleanup", "resolve", initial[0].ReceiptID, "--deployment-id", d.ID, "--outcome", "released", "--address-id", address, "--evidence", "audit-case-741", "--yes"}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			code, out, stderr := runCLI(t, "", args...)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			newProcess()
			pending := list()
			refusal, ok := errorEnvelope(stderr)
			advice := refusal.Message + " " + refusal.Hint
			confirmed := strings.Contains(stderr, `"status":"confirmed"`)
			t.Logf("first resolve: exit %d, project/exact GET %d/%d, pending receipts %d, state unchanged %t", code, projectReads, exactReads, len(pending), bytes.Equal(before, after))
			if code != 5 || !ok || refusal.Reason != "API_ERROR" || out != "" || confirmed != (tc.firstBroken == 2) ||
				!strings.Contains(advice, "still pending") || !strings.Contains(advice, "evo cleanup resolve") {
				t.Fatalf("incomplete readback must leave a pending receipt and name the retry: exit %d, stdout %s, stderr %s", code, out, stderr)
			}
			// A bad project page can be rejected before rereading the exact ID.
			checkedID := exactReads == tc.firstBroken || (listFault && exactReads == tc.firstBroken-1)
			if !checkedID || projectReads != tc.firstBroken || !bytes.Equal(before, after) || len(pending) != 2 ||
				pending[0].ReceiptID != initial[0].ReceiptID || pending[1].ReceiptID != initial[1].ReceiptID {
				t.Fatalf("incomplete readback: project/exact GET %d/%d, receipts %d, state unchanged %t", projectReads, exactReads, len(pending), bytes.Equal(before, after))
			}
			complete, projectReads, exactReads = true, 0, 0
			code, out, stderr = runCLI(t, "", args...)
			result := parseObject(out)
			if code != 0 || result["status"] != "reconciled" || result["outcome"] != "released" || result["receipt_id"] != initial[0].ReceiptID ||
				result["deployment_id"] != d.ID || projectReads != 2 || exactReads != 2 {
				t.Fatalf("complete 404 on retry: exit %d, project/exact GET %d/%d, stdout %s, stderr %s", code, projectReads, exactReads, out, stderr)
			}
			newProcess()
			pending = list()
			saved, err := state.Read(path)
			if err != nil {
				t.Fatal(err)
			}
			resolutions := saved.Deployments[0].FloatingIPResolutions
			writes := w.rt.takeWrites()
			t.Logf("retry: exit %d, project/exact GET %d/%d, pending receipts %d, total cloud writes %d", code, projectReads, exactReads, len(pending), len(writes))
			if len(pending) != 1 || pending[0].ReceiptID != initial[1].ReceiptID || len(resolutions) != 1 ||
				resolutions[0].ReceiptID != initial[0].ReceiptID || resolutions[0].AddressID != address || resolutions[0].Outcome != "released" || len(writes) != 0 {
				t.Fatalf("retry must settle only the target: pending=%+v resolutions=%+v", pending, resolutions)
			}
		})
	}
}

func TestCleanup_SelectDeploymentFromLocalError(t *testing.T) {
	w := withParityWorld(t)
	path := state.DefaultPath(w.dir)
	t.Setenv("CLOUDRU_KEY_ID", "00000000000000000000000000000001")
	t.Setenv("CLOUDRU_SECRET", "fixture")
	var sf state.File
	for _, id := range []string{"one", "two", "foreign-project", "foreign-key", "duplicate", "duplicate"} {
		project := pvProject
		if id == "foreign-project" {
			project = "other-project"
		}
		d := state.DeploymentRecord{ID: id, VMID: "vm-" + id, ProjectID: project, Status: "destroying"}
		if id == "foreign-key" {
			d.CreatedBy = state.NewMaker("", "ffffffffffffffffffffffffffffffff")
		}
		sf.Deployments = append(sf.Deployments, d)
		sf.LostFloatingIPs = append(sf.LostFloatingIPs, state.LostFloatingIP{Name: "pending-" + id, ProjectID: project, VMID: "vm-" + id})
	}
	if err := state.Write(path, &sf); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		t.Fatalf("local selection reached cloud: %s", r.URL.Path)
		return nil, nil
	}
	for _, lang := range []string{"ru", "en"} {
		t.Run(lang, func(t *testing.T) {
			oldLang := i18n.Lang()
			t.Setenv("EVO_LANG", lang)
			i18n.SetLang(lang)
			t.Cleanup(func() { i18n.SetLang(oldLang) })
			for _, flags := range [][]string{nil, {"--json"}} {
				code, out, stderr := runCLI(t, "", append([]string{"cleanup", "receipts"}, flags...)...)
				if code != 1 || out != "" || !strings.Contains(stderr, "cleanup receipts --deployment-id") || !strings.Contains(stderr, "vm-one") || !strings.Contains(stderr, "vm-two") || strings.Contains(stderr, "foreign") || strings.Contains(stderr, "duplicate") {
					t.Fatalf("selection hint: %d %s %s", code, out, stderr)
				}
				localized := "Выберите"
				if lang == "en" || len(flags) != 0 {
					localized = "Select"
				}
				if !strings.Contains(stderr, localized) {
					t.Errorf("%s hint: %s", lang, stderr)
				}
				for _, id := range []string{"one", "two"} {
					code, out, stderr = runCLI(t, "", append([]string{"cleanup", "receipts", "--deployment-id", id}, flags...)...)
					if code != 0 || !strings.Contains(out, "pending-"+id) || strings.Contains(out, "foreign") {
						t.Fatalf("selected %s: %d %s %s", id, code, out, stderr)
					}
				}
			}
		})
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("selection changed local state", err)
	}
}
