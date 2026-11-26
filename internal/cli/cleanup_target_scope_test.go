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
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
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
