// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// mcpBudgetSeconds is D69's limit of one MCP call (MCPCallBudget).
var mcpBudgetSeconds = MCPCallBudget.Seconds()

// worldBudget is the budget of a call in the slow world: a share of
// budgetWatch, so that a call held to it answers before the watch.
const worldBudget = budgetWatch / 4

// budgetWatch is how long a call of the slow world may run before it
// counts as one that waits the cloud out.
// wall-clock: a share (1/45) of D69's 45 s budget; an answer that does not
// wait comes in milliseconds (the fault matrix's creates: 0.2–1.3 ms).
const budgetWatch = time.Second

// budgetWait is how a tool that can wait is held to D69's limit: a call in
// the slow world (run), or what the code shows when no world reaches it
// (static), with over: the code waits longer than the budget.
type budgetWait struct {
	run    func(t *testing.T) (call func(ctx context.Context) (string, bool), cleanup func())
	static string
	over   bool
	// done reads an answer given in time: "" when it is the honest state of
	// a call cut by the budget (D69 §2, axis 2), else what is wrong.
	done func(m map[string]any) string
}

// budgetComputeRegistry is the compute fault world with the production
// intervals of engine.Timings: in the slow world a wait ends only by the
// call's budget, never by the world's short timeouts.
func budgetComputeRegistry(t *testing.T, cloud *faultCloud, dir string, timings engine.Timings) *Registry {
	t.Helper()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: faultProject, Region: faultZone, ImageID: faultImage,
				Flavor: "low-1-1", DiskSize: 10, KeyID: "k", Secret: "s"},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("fake-key"),
			SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings:   timings,
		}, nil
	})
}

// budgeted is r with the budget of an MCP call in the slow world.
func budgeted(r *Registry) *Registry {
	r.SetCallBudget(worldBudget)
	return r
}

// budgetAPIRegistry is faultAPIRegistry with the production intervals.
func budgetAPIRegistry(t *testing.T, a *faultAPI) *Registry {
	t.Helper()
	faultToolsIsolate(t)
	old := httpretry.DefaultBase
	httpretry.DefaultBase = a
	t.Cleanup(func() { httpretry.DefaultBase = old })
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: a}, "")
	cloud.Backoff = time.Millisecond
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: t.TempDir(),
		}, nil
	})
}

// computeBudget is a call of tool in the slow compute world; setup, when
// set, runs first on the same state with the world fast and the world's
// short intervals (a VM to destroy, a plan to apply).
func computeBudget(tool string, args map[string]any, noDial bool, setup func(t *testing.T, r *Registry, dir string)) budgetWait {
	return budgetWait{run: func(t *testing.T) (func(context.Context) (string, bool), func()) {
		faultToolsIsolate(t)
		cloud := &faultCloud{noDial: noDial}
		dir := t.TempDir()
		if setup != nil {
			setup(t, budgetComputeRegistry(t, cloud, dir, faultToolsTimings()), dir)
		}
		cloud.mu.Lock()
		cloud.slow = true
		cloud.mu.Unlock()
		r := budgeted(budgetComputeRegistry(t, cloud, dir, engine.Timings{}))
		return func(ctx context.Context) (string, bool) { return r.handlers[tool](ctx, args) }, nil
	}}
}

// apiBudget is a call of tool in the slow HTTP world; made is what the world
// holds before the call.
func apiBudget(tool string, args map[string]any, made ...faultResource) budgetWait {
	return budgetWait{run: func(t *testing.T) (func(context.Context) (string, bool), func()) {
		a := &faultAPI{slow: true, made: append([]faultResource(nil), made...)}
		r := budgeted(budgetAPIRegistry(t, a))
		return func(ctx context.Context) (string, bool) { return r.handlers[tool](ctx, args) }, nil
	}}
}

// opBudget is a *_operation call that asks to wait longer than the budget
// for an operation that never ends.
func opBudget(tool string) budgetWait {
	w := apiBudget(tool, map[string]any{"operation_id": "op-1", "wait_seconds": int(mcpBudgetSeconds)})
	w.done = func(m map[string]any) string {
		if m["done"] != false {
			return "an operation that runs on is not done: false"
		}
		return ""
	}
	return w
}

// stackBudget is stack_apply of a stack with a VM in the slow cloud: the VM
// step's provision spends the budget of the outer call (D69 §2) and answers
// creating, which the step keeps as a VM in progress (D69 PR 3).
func stackBudget() budgetWait {
	return budgetWait{run: func(t *testing.T) (func(context.Context) (string, bool), func()) {
		faultToolsIsolate(t)
		stackHome(t)
		r := budgeted(budgetComputeRegistry(t, &faultCloud{slow: true}, t.TempDir(), engine.Timings{}))
		j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
		return func(ctx context.Context) (string, bool) {
			return r.handlers["stack_apply"](ctx, map[string]any{"job_id": j.ID, "confirmed": true})
		}, nil
	}, done: func(m map[string]any) string {
		details, _ := m["details"].(map[string]any)
		vm, _ := details["vm"].(string)
		switch {
		case m["status"] != "running":
			return "the stack is not running"
		case !strings.Contains(vm, "creating"):
			return "the VM step does not say its VM is creating"
		case !strings.Contains(fmt.Sprint(m["note"]), "stack_status"):
			return "it names no next step (stack_status)"
		}
		return ""
	}}
}

// mcpWaits are the tools of the MCP registry that can wait for the cloud:
// those a wait of production code reaches (budgetReach in
// internal/testrules, from sleepWaits) and those whose schema takes a wait
// (wait_seconds). TestMCP_BudgetCoversTheWaits keeps the list whole.
var mcpWaits = map[string]budgetWait{
	"stack_apply": stackBudget(),
	"provision":   computeBudget("provision", map[string]any{"name": "web", "confirmed": true}, false, nil),
	"deploy_apply": computeBudget("deploy_apply", map[string]any{"plan_id": "plan-0123456789ab", "confirmed": true}, true,
		func(t *testing.T, _ *Registry, dir string) {
			if err := deployplan.Save(dir, faultPlan()); err != nil {
				t.Fatal(err)
			}
		}),
	// Named, as a confirmed call without a terminal must (P2).
	"destroy": computeBudget("destroy", map[string]any{"name": "web", "confirmed": true}, false,
		func(t *testing.T, r *Registry, _ string) {
			if text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true}); isErr {
				t.Fatalf("provision before destroy: %s", text)
			}
		}),

	"ar_operation": opBudget("ar_operation"), "cert_operation": opBudget("cert_operation"),
	"dns_operation": opBudget("dns_operation"), "kafka_operation": opBudget("kafka_operation"),
	"lb_operation": opBudget("lb_operation"), "pg_operation": opBudget("pg_operation"),
	"redis_operation": opBudget("redis_operation"), "vpc_operation": opBudget("vpc_operation"),

	"dns_zone_delete": apiBudget("dns_zone_delete", map[string]any{"id": "00000000-0000-4000-8000-0000000000a7", "type": "public", "confirmed": true},
		faultResource{host: "dns.api.cloud.ru", path: "/v1/publicZones", ID: "00000000-0000-4000-8000-0000000000a7", Name: "example.com.", before: true}),

	// No world of the slow cloud reaches these yet: what the code shows.
	"deploy":        {static: "the engine waits for the VM up to VMPollTimeout (25 min), then SSH and Docker (engine/timings.go)", over: true},
	"stack_destroy": {static: "runs destroy in the call (callTool): DestroyPollTimeout 2 min, DeletableWaitTimeout 10 min (engine/timings.go)", over: true},
	"stack_status":  {static: "the handler waits at most 20 s (tools_stack.go), as its schema says: within the budget"},
	"k8s_kubectl":   {static: "kubectl runs up to 90 s (tools_kube.go, docs/mcp.md)", over: true},
	"k8s_helm":      {static: "helm runs up to 9 min with --wait (tools_kube.go)", over: true},
	"disk_create":   {static: "with vm, waits for the new disk to be available (diskReadyTimeout 5 min; tools_disk.go) before the attach", over: true},
	"disk_attach":   {static: "waits for the disk to settle (diskSettleTimeout 30 s) before the question, then to be available (diskReadyTimeout 5 min; tools_disk.go)", over: true},
	"disk_detach":   {static: "waits for the disk to settle before the question (diskSettleTimeout 30 s; tools_disk.go): within the budget alone, not with the requests around it"},
	"disk_resize":   {static: "waits for the disk to settle before the question (diskSettleTimeout 30 s; tools_disk.go): within the budget alone, not with the requests around it"},
}

// mcpOverBudget are the MCP tools a call of which can outlive D69's budget,
// by the slow world, the code (mcpWaits' static) or a schema that lets the
// agent ask for a longer wait. It only shrinks. Closes by 1.7 (release plan
// 1.7, block A; process plan 1.7 §1: red before PR 1 of D69, each PR of
// D69 takes its lines out).
var mcpOverBudget = map[string]string{
	"destroy":       "waits for the VM to be gone: DestroyPollTimeout 2 min",
	"deploy":        "waits for the VM, SSH and Docker",
	"stack_destroy": "destroy waits for the VM to be gone",
	"k8s_kubectl":   "kubectl up to 90 s",
	"k8s_helm":      "helm up to 9 min",
	"disk_create":   "with vm: waits for the disk up to 5 min",
	"disk_attach":   "waits for the disk to settle up to 30 s before the question, then to be available up to 5 min",
}

// waitProps are the schema properties by which an agent asks a tool to
// wait, in seconds.
var waitProps = map[string]bool{"wait_seconds": true}

// schemaOverBudget reports whether a wait property of tl's schema lets the
// agent ask for more than the budget: no maximum, or one above it (D36:
// static rules in the schema, argcheck applies them).
func schemaOverBudget(tl Tool) bool {
	props, _ := tl.InputSchema["properties"].(map[string]any)
	for name, p := range props {
		if !waitProps[name] {
			continue
		}
		pm, _ := p.(map[string]any)
		max, ok := pm["maximum"].(float64)
		if !ok {
			if n, isInt := pm["maximum"].(int); isInt {
				max, ok = float64(n), true
			}
		}
		if !ok || max > mcpBudgetSeconds {
			return true
		}
	}
	return false
}

// Every tool that can wait for the cloud is held to D69's limit: a case of
// the slow world, or what its code shows; a tool whose schema takes a wait
// is one of them (process plan 1.7 §1, part 1). A new waiting tool fails
// here until it is named.
func TestMCP_BudgetCoversTheWaits(t *testing.T) {
	var bad []string
	names := map[string]bool{}
	for _, tl := range New(nil).Tools() {
		names[tl.Name] = true
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for p := range props {
			if _, ok := mcpWaits[tl.Name]; waitProps[p] && !ok {
				bad = append(bad, tl.Name+": its schema takes "+p+", not in mcpWaits; add a case of the slow world")
			}
		}
	}
	for name, w := range mcpWaits {
		switch {
		case !names[name]:
			bad = append(bad, name+": in mcpWaits, no such tool")
		case (w.run == nil) == (w.static == ""):
			bad = append(bad, name+": a case of the slow world or what the code shows, one of the two")
		}
	}
	for name := range mcpOverBudget {
		if _, ok := mcpWaits[name]; !ok {
			bad = append(bad, name+": in mcpOverBudget, not in mcpWaits")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// No MCP call outlives D69's budget (process plan 1.7 §1, parts 2–4): in
// the slow cloud — VMs "creating", operations not done, resources
// "CREATING", a deleted VM "deleting", production intervals — a call of a
// tool that waits answers within budgetWatch with the honest state of the
// call, or its tool is a line of mcpOverBudget. A line the call no longer
// needs fails too: the list only shrinks. The world's registry has the
// budget of an MCP call (SetCallBudget, worldBudget).
func TestMCP_NoCallOutlivesTheBudget(t *testing.T) {
	prev := deployplan.SSHPortWait
	deployplan.SSHPortWait = time.Millisecond
	t.Cleanup(func() { deployplan.SSHPortWait = prev })
	over := map[string]string{}
	ran := map[string]bool{} // the cases -run left in
	tools := map[string]Tool{}
	for _, tl := range New(nil).Tools() {
		tools[tl.Name] = tl
	}
	var names []string
	for name := range mcpWaits {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w := mcpWaits[name]
		if schemaOverBudget(tools[name]) {
			over[name] = "schema"
		}
		if w.run == nil {
			if w.over {
				over[name] = w.static
			}
			continue
		}
		t.Run(name, func(t *testing.T) {
			ran[name] = true
			call, cleanup := w.run(t)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type answer struct {
				text  string
				isErr bool
			}
			got := make(chan answer, 1)
			go func() {
				text, isErr := call(ctx)
				got <- answer{text, isErr}
			}()
			select {
			case a := <-got:
				checkNoTerminalCodes(t, name, a.text)
				if _, listed := mcpOverBudget[name]; listed {
					return // a line the call no longer needs: the check below
				}
				if msg := budgetAnswerProblem(w, a.text, a.isErr); msg != "" {
					t.Errorf("%s answered in time, but %s:\n%s", name, msg, a.text)
				}
			case <-time.After(budgetWatch):
				over[name] = "slow world"
				cancel()
				select {
				case a := <-got:
					checkNoTerminalCodes(t, name, a.text)
				case <-time.After(10 * budgetWatch):
					t.Fatalf("%s did not end after its context was cancelled", name)
				}
			}
		})
	}
	var bad []string
	for name, why := range over {
		if _, ok := mcpOverBudget[name]; !ok {
			bad = append(bad, name+": outlives D69's budget ("+why+"), not in mcpOverBudget")
		}
	}
	for name := range mcpOverBudget {
		if w := mcpWaits[name]; w.run != nil && !ran[name] {
			continue
		}
		if _, ok := over[name]; !ok {
			bad = append(bad, name+": in mcpOverBudget, but answers within the budget; take the line out (the list only shrinks)")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Error(strings.Join(bad, "\n"))
	}
	t.Logf("mcpOverBudget: %d lines (Closes by 1.7)", len(mcpOverBudget))
}

// finalStatus are the states of a call that is over: an answer cut by the
// budget in the slow world, where nothing ends, cannot have one.
var finalStatus = map[string]bool{"active": true, "running": true, "ready": true, "available": true,
	"done": true, "deleted": true, "created": true, "ok": true}

// budgetAnswerProblem reads the answer of a call the slow world cut short
// (D69 §2; CLAUDE.md §6, axis 2): not an error; a state that is not final;
// what to call next — a tool of the registry, not a command of the CLI.
func budgetAnswerProblem(w budgetWait, text string, isErr bool) string {
	if isErr {
		return "it is an error (a call cut by the budget is the state so far, isError=false)"
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return "it is not JSON"
	}
	if w.done != nil {
		return w.done(m)
	}
	status, _ := m["status"].(string)
	if status == "" || finalStatus[strings.ToLower(status)] {
		return "its status " + `"` + status + `"` + " is not the state of a call still going"
	}
	next, _ := m["next"].(string)
	if next == "" {
		return "it names no next step (next)"
	}
	if strings.Contains(next, "evo ") {
		return "its next step is a command of the CLI, not a tool (D6)"
	}
	for _, tl := range New(nil).Tools() {
		if strings.Contains(next, tl.Name) {
			return ""
		}
	}
	return "its next step names no tool of the registry"
}

// terminalCode is what only a terminal reads: a carriage return that
// redraws a line, an escape sequence (colour, cursor).
var terminalCode = strings.NewReplacer("\r", "\\r", "\x1b", "\\x1b")

// F9 (agent runs of Grok on 1.6.0; release plan 1.7): no answer of an MCP
// tool carries a carriage return or an ANSI escape — the spinner of a
// printer that writes to a buffer, not a terminal, read by the model as
// text. The slow world runs through the waits that redraw their line; the
// fault matrix through every billable tool's path (TestFault_Matrix).
func checkNoTerminalCodes(t *testing.T, tool, text string) {
	t.Helper()
	if strings.ContainsAny(text, "\r\x1b") {
		t.Errorf("%s: the answer carries a terminal's codes (F9): %s", tool, terminalCode.Replace(text))
	}
}
