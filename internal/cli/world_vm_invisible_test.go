// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// recordByAnotherKey marks the deployment record of the directory as made
// by a key other than the one evo now runs with. The record has no field
// for who made it yet (release plan 1.8, L3, conclusion 43 of «Риск», (1)):
// the change that adds it sets it here, one line, and the case «another
// key» turns green.
func recordByAnotherKey(t *testing.T, w parityWorld) {
	t.Helper()
	path := state.DefaultPath(w.dir)
	f, err := state.Read(path)
	if err != nil || len(f.Deployments) == 0 {
		t.Fatalf("no deployment record to mark: %v", err)
	}
	f.Deployments[0].CreatedBy = &state.Maker{KeyID: "othe****0001"}
	if err := state.Write(path, f); err != nil {
		t.Fatal(err)
	}
}

// recordByAnotherKeyDeployment is recordDeployment with the record marked
// as made by another key.
func recordByAnotherKeyDeployment(t *testing.T, w parityWorld) (map[string]any, func()) {
	t.Helper()
	args, done := recordDeployment(t, w)
	recordByAnotherKey(t, w)
	return args, done
}

// Release plan 1.8, L3 (R2 C24b; conclusion 43 of «Риск», (4), 10.11):
// after the key changed, Cloud.ru answers 404 for a VM the new key does not
// see, though the VM lives and is billed. A 404 under another key is no
// proof the VM is gone: evo destroy and the tool destroy keep the record,
// send the cloud no write (the VM, its floating IP) and do not answer
// success; evo status does not advise destroy. Control: the same key and 404 — the VM is gone (D26), the
// record goes; without it the case «another key»
// could not tell anything.
func TestParityWorld_VMInvisibleToAnotherKeyKeepsTheRecord(t *testing.T) {
	w, fake := withJSONWorld(t)
	world := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "compute.api.cloud.ru" && r.URL.Path == "/api/v1/vms/"+parityVM &&
			(r.Method == http.MethodGet || r.Method == http.MethodDelete) {
			// The key does not see the VM: 404 to a read and to a delete, as
			// for a VM that is gone.
			if r.Method == http.MethodDelete {
				fake.record(r)
			}
			return consentJSON(http.StatusNotFound, `{"code":"not_found","message":"vm not found"}`), nil
		}
		return world(r)
	}
	for _, c := range []struct {
		name    string
		another bool
	}{{"the same key (control)", false}, {"another key", true}} {
		t.Run(c.name, func(t *testing.T) {
			if c.another {
				// status: «not checked: the record was made by another key»,
				// not an advice to destroy (conclusion 43, (2)).
				_, done := recordByAnotherKeyDeployment(t, w)
				fake.reset()
				newProcess()
				_, so, se := runCLI(t, "", "--json", "status")
				done()
				forgetRun(w)
				if strings.Contains(so+se, "destroy") {
					t.Errorf("evo status: a VM another key cannot see — the answer names destroy:\n%s", clipLine(so+se))
				}
			}
			for _, surface := range []string{"evo destroy", "tool destroy"} {
				_, done := recordDeployment(t, w)
				if c.another {
					recordByAnotherKey(t, w)
				}
				fake.reset()
				newProcess()
				var out string
				var failed bool
				if surface == "evo destroy" {
					code, so, se := runCLI(t, "", "--json", "--yes", "destroy")
					out, failed = so+se, code != 0
				} else {
					flagJSON, flagDryRun, flagYes = false, false, false
					tl, _ := newRegistry().Lookup("destroy")
					out, failed = tl.Handler(context.Background(), map[string]any{"vm_id": parityVM, "confirmed": true})
				}
				writes := fake.takeWrites()
				f, _ := state.Read(state.DefaultPath(w.dir))
				kept := f != nil && len(f.Deployments) == 1
				done()
				forgetRun(w)
				what := clipLine(out)
				if c.another {
					if !kept || len(writes) > 0 || !failed {
						t.Errorf("%s: a VM another key cannot see: record kept %v, writes %v, failed %v — want the record kept, no write, no success\n%s",
							surface, kept, writes, failed, what)
					}
					continue
				}
				if kept || failed {
					t.Errorf("%s, control: the VM is gone (404 under the same key): record kept %v, failed %v — want success and the record gone (D26); without it the case «another key» tells nothing\n%s",
						surface, kept, failed, what)
				}
			}
		})
	}
}
