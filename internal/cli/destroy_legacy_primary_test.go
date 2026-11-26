// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The legacy deployment has an external address, no stored address ID and no
// lost create. A binding can be observed only while the original VM exists.
type legacyPrimaryCloud struct {
	provision.CloudAPI
	list func() []*provision.FloatingIP
}

func (c legacyPrimaryCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return c.list(), nil
}

func legacyPrimaryWorld(t *testing.T, bound bool) (parityWorld, *jsonFake, string) {
	t.Helper()
	w, fake := withJSONWorld(t)
	_, done := recordDeployment(t, w)
	t.Cleanup(done)
	path := state.DefaultPath(w.dir)
	sf, err := state.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	sf.ActiveDeployment().FloatingIPID = ""
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}
	base := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := base(o)
		if err == nil {
			e.Cloud = legacyPrimaryCloud{e.Cloud, func() []*provision.FloatingIP {
				if !bound || fake.gone() {
					return nil
				}
				return []*provision.FloatingIP{{ID: parityRecordedFIP, Name: "fip-" + parityVM, IPAddress: "203.0.113.10", VM: json.RawMessage(`{"id":"` + parityVM + `"}`)}}
			}}
		}
		return e, err
	}
	t.Cleanup(func() { toolEngine = base })
	return w, fake, path
}

func destroyLegacyPrimary(t *testing.T, surface string, preview bool) (string, bool) {
	t.Helper()
	if surface != "CLI" {
		s := mcp.New(toolEngine, "fixture")
		declareStateDir(s)
		args := map[string]any{"vm_id": parityVM, "confirmed": !preview}
		if surface == "targeted_MCP_unrecorded" {
			args["release_ip"] = true
		}
		answer, _ := roDecode(t, stdioRaw(t, s, "destroy", args))
		if preview && answer.rpcErr == "" {
			var result struct{ Status string }
			if json.Unmarshal([]byte(answer.text), &result) == nil && result.Status == "confirmation_required" {
				return answer.text, false
			}
		}
		return answer.text, answer.isErr || answer.rpcErr != ""
	}
	flag := "--yes"
	if preview {
		flag = "--dry-run"
	}
	exit, out, errOut := runCLI(t, "", "--json", "destroy", flag)
	if exit != 0 {
		return out + errOut, true
	}
	return out, false
}

func TestDestroyLegacyPrimary_EmptyListKeepsReceipt(t *testing.T) {
	for _, surface := range []string{"CLI", "targeted_MCP"} {
		t.Run(surface, func(t *testing.T) {
			w, _, path := legacyPrimaryWorld(t, false)
			previous := w.rt.extra
			var addressDeletes int
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/floating-ips/") {
					addressDeletes++
				}
				return previous(r)
			}
			var previousReceipt string
			for attempt := 0; attempt < 2; attempt++ {
				text, failed := destroyLegacyPrimary(t, surface, false)
				var answer map[string]any
				if failed || json.Unmarshal([]byte(text), &answer) != nil || answer["cleanup_pending"] != true || !strings.Contains(text, "evo cleanup receipts") {
					t.Fatalf("attempt %d: missing partial cleanup and recovery step: %s", attempt+1, text)
				}
				if strings.Contains(text, "is still billed") || strings.Contains(text, "local record is cleaned up") {
					t.Errorf("unconfirmed address or saved record reported as certain: %s", text)
				}
				if surface == "CLI" && answer["status"] != "partial" {
					t.Fatalf("CLI status: %v", answer["status"])
				}
				sf, err := state.Read(path)
				if err != nil {
					t.Fatal(err)
				}
				d := sf.ActiveDeployment()
				if d == nil || d.Status != "destroying" || d.VMID != parityVM || d.FloatingIPID != "" || !d.LegacyFloatingIPPending || len(sf.LostFloatingIPs) != 0 || addressDeletes != 0 {
					t.Fatalf("attempt %d: primary record %+v, lost requests %v, address DELETEs %d", attempt+1, d, sf.LostFloatingIPs, addressDeletes)
				}
				code, out, errOut := runCLI(t, "", "--json", "cleanup", "receipts", "--deployment-id", d.ID)
				var rows struct{ Receipts []engine.FloatingIPReceipt }
				if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows.Receipts) != 1 {
					t.Fatalf("cleanup receipts: %s%s", out, errOut)
				}
				receipt := rows.Receipts[0]
				if receipt.Kind != "legacy_deployment" || receipt.ReceiptID == "" || receipt.Address != d.VMIP || receipt.DeploymentID != d.ID || previousReceipt != "" && previousReceipt != receipt.ReceiptID {
					t.Fatalf("incorrect primary receipt: %+v (previous %q)", receipt, previousReceipt)
				}
				previousReceipt = receipt.ReceiptID
			}
		})
	}
}

func TestDestroyLegacyPrimary_CheckpointAtFirstVMDelete(t *testing.T) {
	for _, test := range []struct {
		surface string
		partial bool
	}{{"CLI", false}, {"targeted_MCP", false}, {"targeted_MCP_unrecorded", false}, {"CLI", true}, {"targeted_MCP", true}} {
		surface := test.surface
		name := surface
		if test.partial {
			name += "/intermediate_partial"
		}
		t.Run(name, func(t *testing.T) {
			w, fake, path := legacyPrimaryWorld(t, true)
			if surface == "targeted_MCP_unrecorded" {
				if err := state.Write(path, &state.File{}); err != nil {
					t.Fatal(err)
				}
			}
			previous := w.rt.extra
			var atDelete []byte
			var atDeleteIntents []intent.Intent
			intentDir := intent.Dir(w.dir)
			interrupted := true
			stillVisible := false
			var addressDeletes, confirmations int
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if interrupted && r.Method == http.MethodDelete && r.URL.Path == "/api/v1/vms/"+parityVM {
					if atDelete == nil {
						var err error
						atDeleteIntents = intent.Pending(intentDir)
						atDelete, err = os.ReadFile(path)
						if err != nil {
							return nil, err
						}
						fake.mu.Lock()
						fake.vmGone = true
						fake.mu.Unlock()
					}
					// The DELETE reached Compute, but the caller never
					// receives its response, including on transport retries.
					return nil, io.ErrUnexpectedEOF
				}
				if strings.Contains(r.URL.Path, "/floating-ips/") {
					if r.URL.Path != "/api/v1/floating-ips/"+parityRecordedFIP {
						t.Errorf("wrong address: %s", r.URL.Path)
					}
					if r.Method == http.MethodDelete {
						addressDeletes++
					} else if r.Method == http.MethodGet && addressDeletes > 0 {
						confirmations++
						if stillVisible {
							return contractRT{status: http.StatusOK, body: `{"id":"` + parityRecordedFIP + `","state":"deleting"}`}.RoundTrip(r)
						}
					}
				}
				return previous(r)
			}
			if text, failed := destroyLegacyPrimary(t, surface, true); failed {
				t.Fatalf("preview failed: %s", text)
			}
			sf, err := state.Read(path)
			if err != nil || sf.ActiveDeployment() != nil && sf.ActiveDeployment().FloatingIPID != "" || atDelete != nil {
				t.Fatalf("preview changed state or sent a DELETE: %v, %v", sf, err)
			}
			if text, failed := destroyLegacyPrimary(t, surface, false); !failed || atDelete == nil || addressDeletes != 0 {
				t.Fatalf("first VM DELETE: %s, address DELETEs %d", text, addressDeletes)
			}
			// Restart from precisely the bytes that were durable at the first
			// DELETE, without any state the original caller wrote afterwards.
			interrupted = false
			if err := os.WriteFile(path, atDelete, 0600); err != nil {
				t.Fatal(err)
			}
			sf, err = state.Read(path)
			if err != nil {
				t.Fatal(err)
			}
			d := sf.ActiveDeployment()
			if d == nil || d.VMID != parityVM || d.FloatingIPID != parityRecordedFIP {
				t.Errorf("at the first VM DELETE: deployment %+v, want VM %s and address %s", d, parityVM, parityRecordedFIP)
			} else if surface != "CLI" {
				if !d.UpdatedAt.Equal(clock.Now()) || surface == "targeted_MCP_unrecorded" && !d.CreatedAt.Equal(clock.Now()) {
					t.Errorf("checkpoint must use the operation time %s, got created %s and updated %s", clock.Now(), d.CreatedAt, d.UpdatedAt)
				}
			}
			if len(atDeleteIntents) != 1 {
				t.Fatalf("first VM DELETE must carry its original intent: %+v", atDeleteIntents)
			}
			original := atDeleteIntents[0]
			if original.Target != "vm_id="+parityVM || surface == "CLI" && original.ProjectID != pvProject {
				t.Errorf("original intent has no approved VM or project: %+v", original)
			}
			if test.partial {
				stillVisible = true
				text, failed := destroyLegacyPrimary(t, surface, false)
				var result struct {
					Pending bool `json:"cleanup_pending"`
				}
				if failed || json.Unmarshal([]byte(text), &result) != nil || !result.Pending || confirmations != 1 || addressDeletes != 1 {
					t.Fatalf("intermediate cleanup: %s (address DELETEs %d, GETs %d)", text, addressDeletes, confirmations)
				}
				pending := intent.Pending(intentDir)
				if len(pending) != 1 || pending[0].ID != original.ID {
					t.Fatalf("intermediate cleanup lost the original intent %s: %+v", original.ID, pending)
				}
				stillVisible = false
			}
			before := addressDeletes
			text, failed := destroyLegacyPrimary(t, surface, false)
			sf, err = state.Read(path)
			if failed || err != nil || sf.ActiveDeployment() != nil || addressDeletes != before+1 || confirmations != before+1 {
				t.Fatalf("restart: %s, state %+v, error %v, address DELETEs %d, post-delete GETs %d", text, sf, err, addressDeletes, confirmations)
			}
			for _, pending := range intent.Pending(intentDir) {
				if pending.ID == original.ID {
					t.Errorf("original intent %s remains pending after its VM and address are gone", original.ID)
				}
			}
			log, err := history.ReadAll(history.DirOf(intentDir), func(l history.Line) bool {
				return l.IntentID == original.ID && (l.Outcome == history.Deleted || l.Outcome == history.AlreadyDeleted)
			})
			if err != nil || len(log.Lines) != 1 || log.Lines[0].ResourceID != parityVM {
				t.Errorf("original intent %s has no confirmed deletion in history: %+v, %v", original.ID, log.Lines, err)
			}
		})
	}
}

// An old directory intent with no VM identity cannot prove what it deleted.
// A successful destroy of this deployment must leave that receipt and the
// receipts of other projects or VMs available to their original callers.
func TestDestroyLegacyPrimary_DirectoryRetryKeepsUnrelatedIntents(t *testing.T) {
	w, _, _ := legacyPrimaryWorld(t, true)
	eng, err := toolEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := intent.Dir(w.dir)
	for _, scope := range []struct{ project, vm string }{
		{pvProject, ""}, {pvProject, parityStoppedVM}, {"another-project", parityVM},
	} {
		args := map[string]string{}
		in := intent.Intent{Tool: "destroy", Surface: "cli", ProjectID: scope.project, Key: eng.Maker(), Ended: true, StartedAt: clock.Now()}
		if scope.vm != "" {
			in.Target = "vm_id=" + scope.vm
			args["vm_id"], args["project_id"] = scope.vm, scope.project
		}
		in.Canon, err = json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := intent.Open(dir, in); err != nil {
			t.Fatal(err)
		}
	}
	if text, failed := destroyLegacyPrimary(t, "CLI", false); failed {
		t.Fatalf("destroy failed: %s", text)
	}
	if pending := intent.Pending(dir); len(pending) != 3 {
		t.Fatalf("destroy settled an unrelated intent: %+v", pending)
	}
}

// The VM delete lost its response. Once the VM is gone, the last caller
// has only the rule receipt to identify the operation that it completes.
func TestDestroy_DirectoryIntentSurvivesRulesOnlyRetry(t *testing.T) {
	w, fake := withJSONWorld(t)
	probe := &ruleReceiptHTTP{t: t, phase: "deleting"}
	adapter := provision.NewCloudRuAdapter(&http.Client{Transport: probe}, "https://fixture.invalid")
	adapter.Backoff = time.Millisecond
	base := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := base(o)
		if err == nil {
			e.Cloud = ruleReceiptCloud{e.Cloud, adapter}
		}
		return e, err
	}
	t.Cleanup(func() { toolEngine = base })
	eng, err := toolEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	d := state.DeploymentRecord{ID: "dep-rules", VMID: parityVM, ProjectID: pvProject, Status: "running", CreatedBy: eng.Maker(),
		OpenedRules: []state.SGRule{{ID: "owned-rule", SecurityGroupID: "owned-group", Port: "8081/tcp"}}}
	if err := state.Write(state.DefaultPath(w.dir), &state.File{Deployments: []state.DeploymentRecord{d}}); err != nil {
		t.Fatal(err)
	}
	previous := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/vms/"+parityVM {
			fake.mu.Lock()
			fake.vmGone = true
			fake.mu.Unlock()
			return nil, io.ErrUnexpectedEOF
		}
		return previous(r)
	}
	if _, failed := destroyLegacyPrimary(t, "CLI", false); !failed {
		t.Fatal("missing interruption")
	}
	w.rt.extra = previous
	dir := intent.Dir(w.dir)
	pending := intent.Pending(dir)
	if len(pending) != 1 {
		t.Fatalf("interruption left %+v", pending)
	}
	id := pending[0].ID
	for _, status := range []string{"partial", "rules_pending", "destroyed"} {
		if status == "destroyed" {
			probe.phase = "rule absent"
		}
		text, failed := destroyLegacyPrimary(t, "CLI", false)
		var answer struct{ Status string }
		if failed || json.Unmarshal([]byte(text), &answer) != nil || answer.Status != status {
			t.Fatalf("want %s, got %s", status, text)
		}
		pending = intent.Pending(dir)
		if status != "destroyed" && (len(pending) != 1 || pending[0].ID != id) {
			t.Fatalf("lost the original intent before rule removal: %+v", pending)
		}
	}
	log, err := history.ReadAll(history.DirOf(dir), func(l history.Line) bool { return l.IntentID == id && l.Outcome == history.Deleted })
	if err != nil || len(pending) != 0 || len(log.Lines) != 1 || log.Lines[0].ResourceID != parityVM {
		t.Fatalf("final rules-only cleanup: pending %+v, confirmed history for %s: %+v, %v", pending, id, log.Lines, err)
	}
}
