// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// refusedVMDeleteMisses are the checks of
// TestParityWorld_RefusedVMDeleteNamesTheState known to fail: "case, state:
// check" → the work item of the release plan that fixes it. The list only
// shrinks: a miss not here fails the test, and so does a line whose check
// passes.
var refusedVMDeleteMisses = map[string]string{}

// CLAUDE.md §6, axis 3: a VM the platform does not delete is not a success
// and not a guess. Cloud.ru answered DELETE /vms/{id} with 409 while the VM
// read deleting (Fable on run 147, register OPS-18) and error_deleting
// (CMP-24, the final of the Grok wave on rc.1, E11), and destroy said «it
// is probably still being created … wait until it is running». In this
// world every DELETE of the VM is 409 and the VM reads in the state: evo
// destroy and the tool destroy, in each of their cases, fail alike (the
// same reason and exit code, as TestParity_ErrorReasonEqualsTool), their
// error and hint name the state read and do not guess (that the VM is
// probably, or still, being created); a state the VM
// does not leave by itself names support, and, with a deployment whose
// floating IP stays billed, its release (evo vpc fip-release).
func TestParityWorld_RefusedVMDeleteNamesTheState(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, bad := jsonCases(t)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	world := w.rt.extra
	var vmState string
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "compute.api.cloud.ru" && r.URL.Path == "/api/v1/vms/"+parityVM {
			switch r.Method {
			case http.MethodDelete:
				fake.record(r)
				return consentJSON(http.StatusConflict, `{"code":"vm_can_not_be_deleted","message":"VM can not be deleted in its current state"}`), nil
			case http.MethodGet:
				return consentJSON(http.StatusOK, `{"id":"`+parityVM+`","name":"web","state":"`+vmState+`"}`), nil
			}
		}
		return world(r)
	}

	found := map[string]string{}
	ran := 0
	for _, c := range cases {
		if c.tool.Name != "destroy" {
			continue
		}
		name := c.path
		if c.state != "" {
			name += " (" + c.state + ")"
		}
		for _, st := range []string{"deleting", "error_deleting"} {
			vmState = st
			r := runJSONCase(t, c, w, fake)
			key := fmt.Sprintf("%s, %s", name, st)
			if c.state == "no deployment" {
				// Nothing to delete: the command reads no VM.
				continue
			}
			ran++
			env, failedCLI := errorEnvelope(r.errOut)
			tool := parseObject(r.text)
			reason, _ := tool["reason"].(string)
			what := fmt.Sprintf("exit %d err=%s; tool: %s", r.code, clipLine(r.errOut), clipLine(r.text))
			switch {
			case !r.isErr || r.code == 0 || !failedCLI:
				t.Errorf("%s: a refused delete is not a success on either surface: %s", key, what)
				continue
			case env.Reason != reason:
				t.Errorf("%s: reason %s ≠ the tool's %s: %s", key, env.Reason, reason, what)
			}
			cli := strings.ToLower(r.errOut)
			mcp := strings.ToLower(fmt.Sprint(tool["error"], " ", tool["hint"]))
			final := st == "error_deleting"
			checks := []textCheck{
				{"names the state", func(s string) bool { return strings.Contains(s, st) }},
				{"does not guess", func(s string) bool {
					for _, guess := range []string{"probably", "вероятно", "still being created", "ещё создаётся"} {
						if strings.Contains(s, guess) {
							return false
						}
					}
					return true
				}},
			}
			if final {
				checks = append(checks, textCheck{"names support", func(s string) bool {
					return strings.Contains(s, "support") || strings.Contains(s, "поддержк")
				}})
			}
			if final && c.state == "deployment recorded" {
				checks = append(checks, textCheck{"names fip-release", func(s string) bool { return strings.Contains(s, "fip-release") }})
			}
			for _, ch := range checks {
				cliOK, mcpOK := ch.ok(cli), ch.ok(mcp)
				switch {
				case cliOK != mcpOK:
					// The surfaces differ: never a declared miss.
					t.Errorf("%s: %s on the CLI = %v, in MCP = %v: %s", key, ch.name, cliOK, mcpOK, what)
				case !cliOK:
					found[key+": "+ch.name] = what
				}
			}
		}
	}
	if ran == 0 {
		t.Fatal("no case of destroy reads the VM: the world tests nothing")
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if refusedVMDeleteMisses[k] == "" {
			t.Errorf("%s: fails — %s", k, found[k])
		}
	}
	for k := range refusedVMDeleteMisses {
		if found[k] == "" {
			t.Errorf("refusedVMDeleteMisses names %s, which no longer fails: remove the line", k)
		}
	}
}

// textCheck is one thing an answer's text must say.
type textCheck struct {
	name string
	ok   func(text string) bool
}
