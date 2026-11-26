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

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
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
	for _, surface := range []string{"CLI", "targeted_MCP", "targeted_MCP_unrecorded"} {
		t.Run(surface, func(t *testing.T) {
			w, fake, path := legacyPrimaryWorld(t, true)
			if surface == "targeted_MCP_unrecorded" {
				if err := state.Write(path, &state.File{}); err != nil {
					t.Fatal(err)
				}
			}
			previous := w.rt.extra
			var atDelete []byte
			interrupted := true
			var addressDeletes, confirmations int
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if interrupted && r.Method == http.MethodDelete && r.URL.Path == "/api/v1/vms/"+parityVM {
					if atDelete == nil {
						var err error
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
			text, failed := destroyLegacyPrimary(t, surface, false)
			sf, err = state.Read(path)
			if failed || err != nil || sf.ActiveDeployment() != nil || addressDeletes != 1 || confirmations != 1 {
				t.Fatalf("restart: %s, state %+v, error %v, address DELETEs %d, post-delete GETs %d", text, sf, err, addressDeletes, confirmations)
			}
		})
	}
}
