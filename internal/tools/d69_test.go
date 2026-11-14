// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// D69 (решение владельца 31.10): долгий вызов MCP не держит клиента дольше
// предела, а отменённый отвечает INTERRUPTED с тем, что уже создано.

func decode(t *testing.T, text string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("not JSON: %s", text)
	}
	return m
}

// A cancel is not the network: INTERRUPTED, without "check your network"
// (FB160-11; agent run M2 on 1.6.0 rc.3).
func TestD69_CancelIsInterrupted(t *testing.T) {
	err := fmt.Errorf("list zones: %w", &url.Error{Op: "Get", URL: "https://dns.api.cloud.ru", Err: context.Canceled})
	m := decode(t, errAnswer(err, map[string]any{"vm_id": "vm-1"}))
	if m["reason"] != "INTERRUPTED" {
		t.Errorf("reason %v, want INTERRUPTED", m["reason"])
	}
	if h, _ := m["hint"].(string); strings.Contains(strings.ToLower(h), "network") {
		t.Errorf("a cancel answered with a network hint: %q", h)
	}
	// A deadline of the cloud's answer stays the network.
	m = decode(t, errResult(fmt.Errorf("x: %w", context.DeadlineExceeded)))
	if m["reason"] != "API_UNREACHABLE" {
		t.Errorf("a deadline: reason %v, want API_UNREACHABLE", m["reason"])
	}
}

// provision cancelled after the VM was made: INTERRUPTED, not
// VM_PROVISION_FAILED, with the VM it made.
func TestD69_ProvisionCancelledIsInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := cancelRegistry(t, &cancelCloud{cancel: cancel})
	text, isErr := r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true})
	m := decode(t, text)
	if !isErr || m["reason"] != "INTERRUPTED" || m["vm_id"] != "vm-made" {
		t.Errorf("want INTERRUPTED with vm_id: %s", text)
	}
}

// deploy_apply cancelled after the VM was made: INTERRUPTED with the VM, the
// plan to repeat and the password, which the repeat cannot give (D69: the
// password is not lost).
func TestD69_DeployApplyCancelledNamesVMPlanAndPassword(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, dir := cancelRegistry(t, &cancelCloud{cancel: cancel})
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", ExpiresAt: time.Now().Add(time.Hour),
		VM: deployplan.VMSpec{Name: "web"}, MetaFields: []string{"linux_password"}}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["deploy_apply"](ctx, map[string]any{"plan_id": plan.PlanID, "confirmed": true})
	m := decode(t, text)
	if !isErr || m["reason"] != "INTERRUPTED" || m["vm_id"] != "vm-made" || m["plan_id"] != plan.PlanID {
		t.Errorf("want INTERRUPTED with vm_id and plan_id: %s", text)
	}
	if p, _ := m["password"].(string); p == "" {
		t.Errorf("the password made for this VM is lost: %s", text)
	}
	// The agent learns it gets the password once (Qwen on #745).
	if n, _ := m["note"].(string); n != strings.TrimSpace(passwordOnce()) {
		t.Errorf("the answer does not say the password is shown once: %s", text)
	}
}

// deploy_apply in the slow cloud under the budget of an MCP call: the state
// so far, not an error, with the machine's next step and the plan kept
// (D69 §3).
func TestD69_DeployApplyAnswersCreatingWithTheNextStep(t *testing.T) {
	call, _ := mcpWaits["deploy_apply"].run(t)
	text, isErr := call(context.Background())
	m := decode(t, text)
	if isErr || m["status"] != "creating" || m["vm_id"] == "" || m["plan_id"] != "plan-0123456789ab" {
		t.Fatalf("want creating with vm_id and plan_id: %s", text)
	}
	if m["next_tool"] != "deploy_apply" {
		t.Errorf("next_tool %v, want deploy_apply: %s", m["next_tool"], text)
	}
	args, _ := m["next_args"].(map[string]any)
	if args["plan_id"] != "plan-0123456789ab" || args["confirmed"] != true {
		t.Errorf("next_args do not repeat the plan: %v", args)
	}
	if _, ok := m["vm_status"]; !ok {
		t.Errorf("no vm_status: %s", text)
	}
}

// wait=false answers once the cloud took the create, on any surface: a CLI
// registry has no budget, and the call still does not wait.
func TestD69_WaitFalseDoesNotWait(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &faultCloud{noDial: true}
	dir := t.TempDir()
	if err := deployplan.Save(dir, faultPlan()); err != nil {
		t.Fatal(err)
	}
	cloud.slow = true
	r := budgetComputeRegistry(t, cloud, dir, faultToolsTimings())
	done := make(chan string, 1)
	go func() {
		text, _ := r.handlers["deploy_apply"](context.Background(), map[string]any{"plan_id": "plan-0123456789ab", "confirmed": true, "wait": false})
		done <- text
	}()
	select {
	case text := <-done:
		if m := decode(t, text); m["status"] != "creating" {
			t.Errorf("wait=false: want creating: %s", text)
		}
	// wall-clock: a fraction of the production wait for the VM (minutes);
	// wait=false waits for nothing. A second did not hold provision's on a
	// slow Windows runner under -race (#793).
	case <-time.After(progressWatch):
		t.Fatal("wait=false waited for the VM")
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil || len(sf.Deployments) != 1 || sf.Deployments[0].Status != "creating" {
		t.Errorf("the VM is not in the state as creating: %+v %v", sf, err)
	}
}

// wait=true lifts the budget; wait=false is no wait at all; no wait argument
// is the budget the registry has (none in the CLI).
func TestD69_WaitLeft(t *testing.T) {
	r := New(nil)
	ctx := r.withCallStart(context.Background())
	if _, capped := r.waitLeft(ctx, nil); capped {
		t.Error("a registry without a budget caps the wait")
	}
	r.SetCallBudget(time.Minute)
	ctx = r.withCallStart(context.Background())
	if left, capped := r.waitLeft(ctx, nil); !capped || left <= 0 || left > time.Minute {
		t.Errorf("budget of a minute: left %v, capped %v", left, capped)
	}
	if _, capped := r.waitLeft(ctx, map[string]any{"wait": true}); capped {
		t.Error("wait=true still capped")
	}
	if left, capped := r.waitLeft(ctx, map[string]any{"wait": false}); !capped || left != 0 {
		t.Errorf("wait=false: left %v, capped %v", left, capped)
	}
	if !errors.Is(context.Canceled, context.Canceled) {
		t.Fatal("unreachable")
	}
}
