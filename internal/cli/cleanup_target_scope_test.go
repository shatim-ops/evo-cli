// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

const reopenedAddressID = "00000000-0000-4000-8000-00000000b123"

type scopedReceiptCloud struct {
	provision.CloudAPI
	fake    *jsonFake
	binding *atomic.Int32
	deletes *atomic.Int32
}

func (c scopedReceiptCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	if c.fake.gone() {
		return nil, nil
	}
	vm := &provision.VM{ID: parityVM, Name: "evo-web", Status: "running"}
	if c.binding.Load() == 1 || c.binding.Load() == 4 {
		vm.FloatingIP = "203.0.113.10"
	}
	return []*provision.VM{vm}, nil
}
func (c scopedReceiptCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	switch c.binding.Load() {
	case 0:
		return nil, nil
	case 3:
		return nil, clerrors.New(clerrors.CodeAPIError, "list unavailable", "")
	}
	f := &provision.FloatingIP{ID: reopenedAddressID, IPAddress: "203.0.113.10"}
	if c.binding.Load() == 1 || c.binding.Load() == 4 {
		f.VM = json.RawMessage(`{"id":"` + parityVM + `"}`)
	}
	if c.binding.Load() == 5 {
		// Same immutable ID, but already detached, renamed and readdressed.
		f.Name, f.IPAddress = "renamed-address", "203.0.113.55"
	}
	if c.binding.Load() == 4 {
		other := *f
		other.ID = parityRecordedFIP
		return []*provision.FloatingIP{f, &other}, nil
	}
	return []*provision.FloatingIP{f}, nil
}
func (c scopedReceiptCloud) GetFloatingIP(ctx context.Context, id string) (*provision.FloatingIP, error) {
	if id != reopenedAddressID {
		return c.CloudAPI.GetFloatingIP(ctx, id)
	}
	if c.binding.Load() != 0 {
		return &provision.FloatingIP{ID: id, IPAddress: "203.0.113.10"}, nil
	}
	return nil, clerrors.Wrap(clerrors.CodeNotFound, "missing", "", &provision.StatusError{Status: 404, Body: `{"message":"not found"}`})
}
func (c scopedReceiptCloud) DeleteFloatingIP(ctx context.Context, id, project string) error {
	c.deletes.Add(1)
	return c.CloudAPI.DeleteFloatingIP(ctx, id, project)
}

func scopedReceiptWorld(t *testing.T) (parityWorld, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	w, fake := withJSONWorld(t)
	rt := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1.1/vms" {
			fake.mu.Lock()
			fake.vmGone = false
			fake.mu.Unlock()
		}
		return rt(r)
	}
	prev := toolEngine
	bound := new(atomic.Int32)
	deletes := new(atomic.Int32)
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := prev(o)
		if err == nil {
			e.Cloud = scopedReceiptCloud{e.Cloud, fake, bound, deletes}
		}
		return e, err
	}
	t.Cleanup(func() { toolEngine = prev })
	_, done := recordDeployment(t, w)
	t.Cleanup(done)
	return w, bound, deletes
}

func TestCleanup_AddressedDestroyBeforeVMReceipt(t *testing.T) {
	for _, wire := range []string{"stdio", "HTTP"} {
		for _, which := range []string{"old project by ID", "old project by name", "recorded project", "ambiguous name", "wrong project"} {
			t.Run(wire+"/"+which, func(t *testing.T) {
				w, _, deletes := scopedReceiptWorld(t)
				path := state.DefaultPath(w.dir)
				sf, err := state.Read(path)
				if err != nil {
					t.Fatal(err)
				}
				d := sf.ActiveDeployment()
				d.ProjectID, d.FloatingIPID, d.VMIP = "", "", ""
				receipt := state.LostFloatingIP{Name: "unconfirmed-address", VMName: d.VMName, ProjectID: pvProject}
				wantErr := ""
				switch which {
				case "recorded project":
					d.ProjectID = pvProject
				case "ambiguous name":
					other := *d
					other.ID = "other-deployment"
					other.VMID = parityStoppedVM
					other.Status = "destroyed"
					sf.UpsertDeployment(other)
					wantErr = "USAGE"
				case "wrong project":
					receipt.ProjectID = "00000000-0000-4000-8000-00000000f111"
					wantErr = "CONFIG_INVALID"
				}
				sf.LostFloatingIPs = []state.LostFloatingIP{receipt}
				if err := state.Write(path, sf); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				agent := mcp.New(toolEngine, "fixture")
				declareStateDir(agent)
				var http roServers
				if wire == "HTTP" {
					h := mcp.NewHTTPHandler(agent, mcp.HTTPOptions{BearerToken: "parity-token"})
					defer h.Close()
					http.roHTTP = httptest.NewServer(h)
					defer http.roHTTP.Close()
				}
				for attempt := 0; attempt < 2; attempt++ {
					args := map[string]any{"confirmed": true, "vm_id": parityVM}
					if which == "old project by name" && attempt == 0 {
						delete(args, "vm_id")
						args["name"] = "evo-web"
					}
					params := map[string]any{"name": "destroy", "arguments": args}
					var a roAnswer
					if wire == "HTTP" {
						a, _ = http.http(t, "tools/call", params)
					} else {
						a, _ = http.stdio(t, agent, "tools/call", params)
					}
					var result map[string]any
					if err := json.Unmarshal([]byte(a.text), &result); err != nil {
						t.Fatal(err, a)
					}
					if wantErr != "" {
						after, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						if !a.isErr || result["reason"] != wantErr || !bytes.Equal(before, after) || len(w.rt.takeWrites()) != 0 {
							t.Fatalf("scope refusal: want %s without modifying state or cloud; got %s", wantErr, a.text)
						}
					} else {
						if a.isErr || result["cleanup_pending"] != true || !strings.Contains(a.text, "evo cleanup receipts") {
							t.Fatalf("attempt %d: want partial with owner handoff, got %s", attempt+1, a.text)
						}
						sf, err := state.Read(path)
						if err != nil || sf.ActiveDeployment() == nil || len(sf.LostFloatingIPs) != 1 || sf.LostFloatingIPs[0] != receipt {
							t.Fatalf("lost target on attempt %d: %+v (%v)", attempt+1, sf, err)
						}
					}
					if deletes.Load() != 0 {
						t.Fatal("destroy sent DELETE for a name-only address")
					}
				}
			})
		}
	}
}

func TestCleanup_ReportsReappearingBinding(t *testing.T) {
	for _, tc := range []struct {
		name, surface             string
		keepIP, newVMIP, detached bool
	}{
		{"CLI JSON", "CLI JSON", false, false, false},
		{"CLI text", "CLI text", false, false, false},
		{"stdio", "stdio", false, false, false},
		{"HTTP", "HTTP", false, false, false},
		{"stdio retain IP", "stdio", true, false, false},
		{"stdio newly visible IP", "stdio", false, true, false},
		{"HTTP newly visible retained IP", "HTTP", true, true, false},
		{"detached released ID", "CLI JSON", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			surface := tc.surface
			w, world, deletes := scopedReceiptWorld(t)
			path := state.DefaultPath(w.dir)
			sf, err := state.Read(path)
			if err != nil {
				t.Fatal(err)
			}
			d := sf.ActiveDeployment()
			d.FloatingIPID = ""
			if tc.newVMIP {
				d.VMIP = ""
				d.LegacyFloatingIPPending = true
			}
			d.Status = "destroying"
			if err := state.Write(path, sf); err != nil {
				t.Fatal(err)
			}
			run := func(args ...string) map[string]any {
				t.Helper()
				exit, out, e := runCLI(t, "", append([]string{"--json"}, args...)...)
				var obj map[string]any
				if exit != 0 || json.Unmarshal([]byte(out), &obj) != nil {
					t.Fatalf("%v: %d %s %s", args, exit, out, e)
				}
				return obj
			}
			list := func() []any {
				t.Helper()
				return run("cleanup", "receipts", "--deployment-id", d.ID)["receipts"].([]any)
			}
			rows := list()
			if len(rows) != 1 {
				t.Fatalf("initial receipts: %v", rows)
			}
			id := rows[0].(map[string]any)["receipt_id"].(string)
			resolve := []string{"cleanup", "resolve", id, "--deployment-id", d.ID, "--outcome", "released", "--address-id", reopenedAddressID, "--yes", "--evidence", "provider case 519"}
			run(resolve...)
			if rows := list(); len(rows) != 0 {
				t.Fatalf("resolved receipt still pending: %v", rows)
			}
			// Capture the persisted receipt as the VM DELETE leaves the process.
			// After this call the provider may have already detached the address.
			var checkpointed atomic.Bool
			nextRequest := w.rt.extra
			w.rt.extra = func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodDelete && req.URL.Path == "/api/v1/vms/"+parityVM {
					snapshot, readErr := state.Read(path)
					if readErr == nil {
						record := snapshot.FindDeployment(d.ID)
						if record != nil && record.LegacyFloatingIPPending && len(record.FloatingIPResolutions) == 1 && record.FloatingIPResolutions[0].Contradicted {
							checkpointed.Store(true)
						}
					}
				}
				return nextRequest(req)
			}
			agent := mcp.New(toolEngine, "fixture")
			declareStateDir(agent)
			var servers roServers
			if surface == "HTTP" {
				h := mcp.NewHTTPHandler(agent, mcp.HTTPOptions{BearerToken: "parity-token"})
				defer h.Close()
				servers.roHTTP = httptest.NewServer(h)
				defer servers.roHTTP.Close()
			}
			binding := int32(1)
			if surface == "CLI text" || surface == "HTTP" && !tc.newVMIP {
				binding = 4
			} // two conflicting bindings.
			if tc.detached {
				binding = 5
			}
			for _, observation := range []int32{binding, 2, 3, 0} { // then detached, unreadable, and absent.
				world.Store(observation)
				var report string
				switch surface {
				case "CLI text":
					exit, out, e := runCLI(t, "", "destroy", "--yes")
					if exit != 0 {
						t.Fatal(e)
					}
					report = out + e
				case "CLI JSON":
					r := run("destroy", "--yes")
					if r["status"] != "partial" || r["cleanup_pending"] != true {
						t.Fatalf("unknown cleanup: %v", r)
					}
					report = fmt.Sprint(r["floating_ip_warning"], " ", r["cleanup_hint"])
				default:
					args := map[string]any{"vm_id": parityVM, "confirmed": true}
					args["release_ip"] = !tc.keepIP
					params := map[string]any{"name": "destroy", "arguments": args}
					var a roAnswer
					if surface == "HTTP" {
						a, _ = servers.http(t, "tools/call", params)
					} else {
						a, _ = servers.stdio(t, agent, "tools/call", params)
					}
					var r map[string]any
					if a.isErr || json.Unmarshal([]byte(a.text), &r) != nil || r["cleanup_pending"] != true {
						t.Fatalf("unknown cleanup: %s", a.text)
					}
					report = a.text
				}
				if observation == binding && !checkpointed.Load() {
					t.Error("binding was not saved as contradicted before the VM DELETE")
				}
				if !strings.Contains(report, "evo cleanup receipts") || (!strings.Contains(report, "billed") && !strings.Contains(report, "charges may continue")) {
					t.Errorf("missing next step or possible billing: %s", report)
				}
				rows = list()
				if len(rows) != 1 {
					t.Errorf("partial cleanup named an unavailable receipt: %v", rows)
					if observation == binding {
						continue
					}
					t.FailNow()
				}
				r := rows[0].(map[string]any)
				if tc.newVMIP && (r["address"] != nil || r["address_id"] != reopenedAddressID) {
					t.Fatalf("changed the original receipt address or lost the newly observed ID: %v", r)
				}
				if r["receipt_id"] != id || r["conflicts_with_resolution"] != true {
					t.Fatalf("missing conflicting receipt: %v", r)
				}
				if _, out, e := runCLI(t, "", "cleanup", "receipts", "--deployment-id", d.ID); !strings.Contains(out+e, id) || !strings.Contains(out+e, "conflicts_with_resolution") {
					t.Fatalf("text receipt hides conflict: %s %s", out, e)
				}
				before, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				exit, _, stderr := runCLI(t, "", append([]string{"--json"}, resolve...)...)
				after, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if exit != 1 || lastJSONError(t, stderr).Reason != "USAGE" || !bytes.Equal(before, after) {
					t.Fatalf("old evidence repeated, resolve %d: %s", exit, stderr)
				}
				if observation != 0 {
					next := slices.Clone(resolve)
					next[len(next)-1] = "support follow-up 824"
					if code, _, _ := runCLI(t, "", append([]string{"--json"}, next...)...); code != 5 {
						t.Fatalf("unconfirmed or still-live record accepted with new evidence: exit %d", code)
					}
				}
				if exit, _, _ := runCLI(t, "", "--json", "deploy", "--yes"); exit == 0 {
					t.Fatal("new deploy passed before a fresh resolution")
				}
			}
			resolve[len(resolve)-1] = "new provider follow-up 824"
			run(resolve...)
			if rows := list(); len(rows) != 0 {
				t.Fatalf("newly confirmed receipt still pending: %v", rows)
			}
			if r := run("destroy", "--yes"); r["status"] != "destroyed" || r["cleanup_pending"] != false {
				t.Fatalf("retry after independent verification: %v", r)
			}
			exit, _, _ := runCLI(t, "", append([]string{"--json"}, resolve...)...)
			if exit != 4 || deletes.Load() != 0 {
				t.Fatalf("closed receipt: repeated resolve exit %d, address DELETE=%d", exit, deletes.Load())
			}
			run("deploy", "--yes")
		})
	}
}

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
