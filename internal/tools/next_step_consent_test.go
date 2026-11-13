// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// A declared continuation keeps the consent in its next step
// (nextStepKeepsConsent), so the step must send nothing but what the
// person approved (owner's decision 12.11 on R-85; release plan 1.9,
// «Согласие не переезжает в следующий шаг»). stack_destroy: the destroy of
// a stack whose VM the cloud deletes slowly is cut by the budget and names
// stack_destroy job_id confirmed as the next step. The step, called as the
// answer names it, creates nothing and deletes nothing the first call did
// not ask to delete: the only writes of the two calls are deletes of the
// job's own resources.
func TestNextStep_StackDestroyStepSendsNothingButTheApprovedDelete(t *testing.T) {
	faultToolsIsolate(t)
	stackHome(t)
	cloud := &faultCloud{}
	dir := t.TempDir()
	j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
	setup := budgetComputeRegistry(t, cloud, dir, faultToolsTimings())
	if text, isErr := setup.gated("stack_apply", setup.planStackApply)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true}); isErr {
		t.Fatalf("stack_apply before stack_destroy: %s", text)
	}
	cloud.mu.Lock()
	cloud.slow = true
	made := len(cloud.vms)
	applied := len(cloud.log)
	cloud.mu.Unlock()
	if made != 1 {
		t.Fatalf("the stack made %d VMs, want 1", made)
	}

	r := budgeted(budgetComputeRegistry(t, cloud, dir, engine.Timings{}))
	text, isErr := r.handlers["stack_destroy"](context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	if isErr {
		t.Fatalf("stack_destroy: %s", text)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(text), &first); err != nil {
		t.Fatal(err)
	}
	next, _ := first["next_tool"].(string)
	args, _ := first["next_args"].(map[string]any)
	if next != "stack_destroy" || args["job_id"] != j.ID || args["confirmed"] != true {
		t.Fatalf("the answer cut by the budget names %s %v, want stack_destroy with job_id %s and confirmed: the world of the test changed\n%s", next, args, j.ID, text)
	}

	// The step, as the agent sends it: the tool and the arguments of the
	// answer, nothing added.
	if text, isErr = r.handlers[next](context.Background(), args); isErr {
		t.Fatalf("the next step: %s", text)
	}

	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	deletes := 0
	for _, op := range cloud.log[applied:] {
		switch {
		case strings.HasPrefix(op, "Get"), strings.HasPrefix(op, "List"), strings.HasPrefix(op, "Find"), op == "SetToken":
		case strings.HasPrefix(op, "Delete"), strings.HasPrefix(op, "Release"), strings.HasPrefix(op, "Detach"):
			deletes++
		default:
			t.Errorf("the destroy and its next step sent %s: a step that keeps the consent sends only the deletes the person approved", op)
		}
	}
	if deletes == 0 {
		t.Fatalf("the world recorded no delete of the destroy (%v): the check sees nothing", cloud.log[applied:])
	}
	if len(cloud.vms) > made {
		t.Errorf("the world has %d VMs after the destroy and its step, %d before: the step made one", len(cloud.vms), made)
	}
}

// nextStepWorld is a call cut by the budget in a world beside the cases
// of mcpWaits.
type nextStepWorld struct {
	tool string
	run  func(t *testing.T) (func(ctx context.Context) (string, bool), func())
	// read: the call changes nothing — a list, a status, a kubectl read —
	// and was sent with confirmed all the same (an agent that adds it to
	// every call).
	read bool
}

// nextStepOtherWorlds are the worlds, beside the cases of mcpWaits, in
// which a tool is cut by the budget at another place and names another
// next step: a tool has one case in mcpWaits, and the step of that case
// need not be the only one (k8s_helm: the case of mcpWaits stops helm at
// the release, where the step is a read; stopped at the chart's repository
// the step is the same call).
var nextStepOtherWorlds = map[string]nextStepWorld{
	"k8s_helm: stopped at repo add": {tool: "k8s_helm", run: kubeBudget("k8s_helm", map[string]any{"cluster_id": faultCluster, "action": "install", "release": "web",
		"chart": "repo/web", "repo_url": "https://charts.example.com", "namespace": "app", "confirmed": true})},
	"k8s_helm: status with confirmed": {tool: "k8s_helm", read: true, run: kubeBudget("k8s_helm", map[string]any{"cluster_id": faultCluster, "action": "status",
		"release": "web", "namespace": "app", "confirmed": true})},
	"k8s_kubectl: a read with confirmed": {tool: "k8s_kubectl", read: true, run: kubeBudget("k8s_kubectl", map[string]any{"cluster_id": faultCluster,
		"args": "rollout status deployment/web -n app", "confirmed": true})},
}

// consentShownElsewhere reports whether a world of nextStepOtherWorlds
// that changes the cloud is of tool: its next step with confirmed is shown
// there, not in the case of mcpWaits.
func consentShownElsewhere(tool string) bool {
	for _, w := range nextStepOtherWorlds {
		if w.tool == tool && !w.read {
			return true
		}
	}
	return false
}

// readStepCarriesConsent are the reads cut by the budget whose next step
// passes on the confirmed the call was sent with: the step is a read, and a
// read carries no consent — an agent that learns to send confirmed with a
// read sends it with the change after it (owner's decision 12.11, the
// condition of «Риск», conclusion 126). "world" → where the step is made. It
// only shrinks. Empty: its deadline 1.9 is met, and a new line comes with a
// deadline of its own and its plan line («разработка C»: the steps of a read
// are made without confirmed).
var readStepCarriesConsent = map[string]string{}

// The rule of TestMCP_NoCallOutlivesTheBudget — a next step carries
// confirmed only for a tool of the closed lists — in the other worlds a
// tool is cut in (nextStepOtherWorlds), and its other half: the next step
// of a read carries no confirmed at all.
func TestNextStep_ConsentOnlyFromTheListsInOtherWorlds(t *testing.T) {
	for name, w := range nextStepOtherWorlds {
		t.Run(name, func(t *testing.T) {
			call, cleanup := w.run(t)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}
			text, _ := call(context.Background())
			carries := consentInNextStep(text)
			if w.read {
				_, pending := readStepCarriesConsent[name]
				switch {
				case carries && !pending:
					t.Errorf("%s: the next step of a read carries confirmed: make the step without it — a read needs no consent, and its step passes none on\n%s", name, text)
				case !carries && pending:
					t.Errorf("readStepCarriesConsent names %q, whose next step carries no confirmed now: remove the line (the list only shrinks)", name)
				}
				return
			}
			listed := nextStepKeepsConsent[w.tool] != "" || nextStepConsentUnbound[w.tool] != ""
			switch {
			case carries && !listed:
				t.Errorf("%s: its answer names a next step with confirmed in next_args, and the tool is in none of the lists\n%s", name, text)
			case !carries && listed:
				t.Errorf("%s: listed as a tool whose next step carries confirmed, and in this world it does not: take the line out, or the world\n%s", name, text)
			}
		})
	}
	for name := range readStepCarriesConsent {
		if w, ok := nextStepOtherWorlds[name]; !ok || !w.read {
			t.Errorf("readStepCarriesConsent names %q, which is no read of nextStepOtherWorlds: remove the line", name)
		}
	}
}
