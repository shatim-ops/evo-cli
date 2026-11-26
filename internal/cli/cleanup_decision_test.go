// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Give each deployment an independent unresolved request. The other receipt is
// a control for writes that accidentally affect more than the selected request.
func pendingReceiptPair(t *testing.T) ([]engine.FloatingIPReceipt, string) {
	t.Helper()
	path := state.DefaultPath(".")
	e, err := toolEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	sf := &state.File{Version: state.CurrentVersion}
	for _, id := range []string{"blue-stack", "green-stack"} {
		vm := "vm-" + id
		sf.Deployments = append(sf.Deployments, state.DeploymentRecord{ID: id, ProjectID: pvProject, VMID: vm, Status: "destroying"})
		sf.LostFloatingIPs = append(sf.LostFloatingIPs, state.LostFloatingIP{Name: "ip-" + id, VMID: vm, ProjectID: pvProject})
	}
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}
	var receipts []engine.FloatingIPReceipt
	for _, d := range sf.Deployments {
		rs, err := e.PendingFloatingIPReceipts(d.ID)
		if err != nil || len(rs) != 1 {
			t.Fatalf("fixture %s: receipts=%v err=%v", d.ID, rs, err)
		}
		receipts = append(receipts, rs[0])
	}
	return receipts, path
}

func receiptStateBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Inspect the exact address before asking permission, then recheck consented
// operations. At the time of each GET the scripted answer is either unread or
// consumed; this distinguishes two early reads from a real post-consent one.
func TestCleanupReleasedDecision(t *testing.T) {
	const address = "00000000-0000-4000-8000-000000000077"
	present := contractRT{200, `{"id":"` + address + `"}`}
	absent := contractRT{404, `{"code":5,"message":"resource not found"}`}
	gateway := contractRT{404, `{"code":5,"message":"Not Found","details":[]}`}
	denied := contractRT{403, `{"message":"forbidden"}`}
	for _, mode := range []string{"dry-run", "decline", "accept", "yes"} {
		for _, tc := range []struct {
			name          string
			first, second contractRT
		}{
			{"present", present, present}, {"absent", absent, absent},
			{"read_denied", denied, denied}, {"gateway", gateway, gateway}, {"appeared", absent, present},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				w := withParityWorld(t)
				lang := i18n.Lang()
				i18n.SetLang(i18n.EN)
				t.Cleanup(func() { i18n.SetLang(lang) })
				base := toolEngine
				toolEngine = func(args map[string]any) (*engine.Engine, error) {
					e, err := base(args)
					if err == nil {
						e.Cloud = provision.NewCloudRuAdapter(&http.Client{Transport: httpretry.UserAgent(w.rt)}, "https://compute.fixture.invalid")
					}
					return e, err
				}
				t.Cleanup(func() { toolEngine = base })
				receipts, path := pendingReceiptPair(t)
				before := receiptStateBytes(t, path)
				interactive := mode == "accept" || mode == "decline"
				input := "y\n"
				if mode == "decline" {
					input = "n\n"
				}
				answer := strings.NewReader(input)
				oldTTY, oldInput := confirm.StdinIsTTY, confirm.Stdin
				confirm.StdinIsTTY = func() bool { return interactive }
				confirm.Stdin = answer
				t.Cleanup(func() { confirm.StdinIsTTY, confirm.Stdin = oldTTY, oldInput })
				var reads []bool
				var projectReads int
				w.rt.extra = func(r *http.Request) (*http.Response, error) {
					if r.Method != http.MethodGet {
						t.Fatalf("unexpected cloud write: %s %s", r.Method, r.URL)
					}
					switch r.URL.Path {
					case "/api/v1/floating-ips":
						if r.URL.Query().Get("project_id") != pvProject {
							t.Fatalf("list outside receipt project: %s", r.URL)
						}
						projectReads++
						return contractRT{200, `{"items":[],"total":0}`}.RoundTrip(r)
					case "/api/v1/floating-ips/" + address:
						if projectReads == 0 {
							t.Error("exact address read before proving access to the receipt project")
						}
						if len(reads) == 0 {
							visible := string(receiptStateBytes(t, os.Stdout.Name())) + string(receiptStateBytes(t, os.Stderr.Name()))
							if strings.Contains(visible, confirm.Prompt()) || strings.Contains(visible, `"preview"`) || strings.Contains(visible, "Record released for receipt") {
								t.Error("consent or preview was already visible at the first exact GET")
							}
						}
						reads = append(reads, answer.Len() == 0)
						if len(reads) == 1 {
							return tc.first.RoundTrip(r)
						}
						return tc.second.RoundTrip(r)
					default:
						t.Fatalf("unexpected resource: %s", r.URL)
						return nil, nil
					}
				}
				args := []string{"cleanup", "resolve", receipts[0].ReceiptID, "--deployment-id", receipts[0].DeploymentID, "--outcome", "released", "--evidence", "operation-8842", "--address-id", address}
				switch mode {
				case "dry-run":
					args = append(args, "--dry-run", "--json")
				case "yes":
					args = append(args, "--yes", "--json")
				}
				exit, out, stderr := runCLI(t, "", args...)
				eligible := tc.first == absent
				apply := eligible && (mode == "yes" || mode == "accept")
				resolved := apply && tc.second.status == 404
				wantReads := []bool{false}
				if apply {
					wantReads = append(wantReads, interactive)
				}
				if !reflect.DeepEqual(reads, wantReads) {
					t.Errorf("exact GETs (answer consumed): %v, want %v", reads, wantReads)
				}
				wantPrompt := interactive && eligible
				if strings.Contains(stderr, confirm.Prompt()) != wantPrompt || (answer.Len() == 0) != wantPrompt {
					t.Errorf("consent: prompt=%t answer consumed=%t, want %t", strings.Contains(stderr, confirm.Prompt()), answer.Len() == 0, wantPrompt)
				}
				wantExit := 0
				switch {
				case tc.first == denied:
					wantExit = 2 // AUTH_FAILED
				case !eligible:
					wantExit = 5 // API_ERROR for conflicting evidence or an indeterminate read
				case mode == "decline":
					wantExit = 3
				case apply && !resolved:
					wantExit = 5
				}
				if exit != wantExit {
					t.Errorf("exit=%d, want %d: %s%s", exit, wantExit, out, stderr)
				}
				if mode == "dry-run" {
					var preview map[string]any
					_ = json.Unmarshal([]byte(out), &preview)
					if (preview["preview"] != nil) != eligible {
						t.Errorf("dry-run preview=%s, eligible=%t", out, eligible)
					}
				}
				if !eligible && (strings.Contains(stderr, "About to ") || strings.Contains(stderr, "Record released for receipt") || strings.Contains(out, `"preview"`)) {
					t.Errorf("ineligible receipt presented for consent: %s%s", out, stderr)
				}
				after := receiptStateBytes(t, path)
				t.Logf("exit=%d project_reads=%d exact_reads_after_answer=%v prompted=%t state_changed=%t", exit, projectReads, reads, strings.Contains(stderr, confirm.Prompt()), !bytes.Equal(before, after))
				if !resolved && !bytes.Equal(before, after) {
					t.Error("unconfirmed outcome changed the state file")
				}
				if resolved {
					var pre, post state.File
					if err := json.Unmarshal(before, &pre); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(after, &post); err != nil {
						t.Fatal(err)
					}
					d := post.FindDeployment(receipts[0].DeploymentID)
					if d == nil || len(d.FloatingIPResolutions) != 1 {
						t.Fatalf("missing recorded decision: %+v", d)
					}
					r := d.FloatingIPResolutions[0]
					if r.ReceiptID != receipts[0].ReceiptID || r.Outcome != "released" || r.AddressID != address || r.ProjectID != pvProject || r.Evidence != "operation-8842" {
						t.Errorf("wrong decision: %+v", r)
					}
					pre.LostFloatingIPs = pre.LostFloatingIPs[1:]
					pre.FindDeployment(receipts[0].DeploymentID).FloatingIPResolutions = d.FloatingIPResolutions
					if !reflect.DeepEqual(pre, post) {
						t.Error("recording one decision changed an unrelated receipt or deployment")
					}
				}
				if writes := w.rt.takeWrites(); len(writes) != 0 {
					t.Errorf("cloud writes: %v", writes)
				}
			})
		}
	}
}

// Start without either deployment ID. Only use information available from the
// ambiguity response or the read-only command that response recommends.
func TestCleanupReceiptsChooseDeployment(t *testing.T) {
	for _, lang := range []string{"en", "ru"} {
		for _, format := range []string{"text", "json"} {
			t.Run(lang+"/"+format, func(t *testing.T) {
				w := withParityWorld(t)
				t.Setenv("EVO_LANG", lang)
				prev := i18n.Lang()
				i18n.SetLang(lang)
				t.Cleanup(func() { i18n.SetLang(prev) })
				receipts, path := pendingReceiptPair(t)
				before := receiptStateBytes(t, path)
				args := []string{"cleanup", "receipts"}
				if format == "json" {
					args = append(args, "--json")
				}
				code, out, stderr := runCLI(t, "", args...)
				response := out + stderr
				if code != 0 && code != 1 {
					t.Fatalf("ambiguous receipts exit=%d: %s", code, response)
				}
				if code == 1 && format == "json" {
					e, ok := errorEnvelope(stderr)
					if !ok || e.Reason != "USAGE" {
						t.Fatalf("expected a usable machine-readable ambiguity: %s", response)
					}
				}
				ids := response
				if !strings.Contains(ids, receipts[0].DeploymentID) || !strings.Contains(ids, receipts[1].DeploymentID) {
					// Accept a recommendation to discover the IDs in another view.
					// Execute the advertised command, without filling in an unknown ID.
					for _, cmd := range []string{"evo history --json", "evo history", "evo status --json", "evo status"} {
						if strings.Contains(response, cmd) {
							exit, listing, errOut := runCLI(t, "", strings.Fields(cmd)[1:]...)
							if exit != 0 {
								t.Fatalf("suggested %q failed with %d: %s", cmd, exit, errOut)
							}
							ids = listing + errOut
							break
						}
					}
				}
				if code == 1 && !strings.Contains(response, "--deployment-id") {
					t.Errorf("missing selection step: %s", response)
				}
				for _, receipt := range receipts {
					if !strings.Contains(ids, receipt.DeploymentID) {
						t.Errorf("cannot discover %s from recommended path: %s", receipt.DeploymentID, response)
					}
					selectArgs := append(append([]string{}, args...), "--deployment-id", receipt.DeploymentID)
					exit, selected, errOut := runCLI(t, "", selectArgs...)
					if exit != 0 || !strings.Contains(selected, receipt.ReceiptID) || !strings.Contains(selected, receipt.DeploymentID) {
						t.Fatalf("select %s exit=%d: %s%s", receipt.DeploymentID, exit, selected, errOut)
					}
					for _, other := range receipts {
						if other.DeploymentID != receipt.DeploymentID && strings.Contains(selected, other.ReceiptID) {
							t.Errorf("selected %s also returned receipt from %s", receipt.DeploymentID, other.DeploymentID)
						}
					}
				}
				if !bytes.Equal(before, receiptStateBytes(t, path)) || len(w.rt.takeWrites()) != 0 {
					t.Fatal("discovery or selection changed state or cloud")
				}
			})
		}
	}
}
