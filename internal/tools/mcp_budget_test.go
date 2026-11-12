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
	"os"
	"path/filepath"
	"runtime"
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

// worldBudget is the budget of a call in the slow world, well under
// budgetWatch, so that a call held to it answers before the watch.
// wall-clock: the tests of this world read only that a call answered in
// time with a state not final, never how far it got; a test that needs a
// call to make progress — a VM made, a stage started, a step reached, in
// the test or in its setup — has progressBudget (progressed, #793).
const worldBudget = 250 * time.Millisecond

// budgetWatch is how long a call of the slow world may run before it
// counts as one that waits the cloud out: a call that does wait it out
// waits the hours the world is set to, and one held to worldBudget ends
// at it. The margin is the loaded runner's: stack_apply ended at its
// 250 ms locally (5 of 5 under -race) but answered past a watch of 1 s on
// Windows (main, f9552ec2, #813).
// wall-clock: a margin over worldBudget, not a wait — a call that answers
// is not waited out.
const budgetWatch = 4 * time.Second

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

// slowDisk is the disk of disk_attach, disk_detach and disk_resize in the
// slow world: the cloud keeps it "creating", a state it does not settle
// from (D53).
var slowDisk = faultResource{host: "compute.api.cloud.ru", path: "/api/v1/disks", ID: faultDisk, Name: "data", before: true}

// faultCluster is the Managed Kubernetes cluster of kubectl and helm in the
// slow world.
const faultCluster = "00000000-0000-4000-8000-0000000000c1"

// kubeBudget is a call of tool (k8s_kubectl, k8s_helm) in a cluster whose
// kubectl and helm never end (a rollout that does not finish, a chart
// whose pods do not come up): the binaries of the slow world on PATH,
// which print a line and wait until they are stopped. Linux and macOS:
// they are shell scripts.
func kubeBudget(tool string, args map[string]any) func(t *testing.T) (func(ctx context.Context) (string, bool), func()) {
	return func(t *testing.T) (func(ctx context.Context) (string, bool), func()) {
		r := budgeted(kubeWorld(t, `echo "$(basename "$0"): waiting"; exec sleep 600`))
		return func(ctx context.Context) (string, bool) { return r.handlers[tool](ctx, args) }, nil
	}
}

// kubeWorld is the API world with kubectl and helm on PATH that run body
// (sh); each writes its arguments, one per line, to $KUBE_ARGS/<name>.
func kubeWorld(t *testing.T, body string) *Registry {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("kubectl and helm of the world are shell scripts")
	}
	bin := t.TempDir()
	for _, name := range []string{"kubectl", "helm"} {
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$KUBE_ARGS/" + name + "\"\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBE_ARGS", t.TempDir())
	return budgetAPIRegistry(t, &faultAPI{slow: true})
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

// progressBudget is the budget of an MCP call in a test that reads how far
// the call got, and progressWatch how long such a call may take before it
// counts as one that waits out a step of production (the VM, Docker, SSH:
// minutes). 250 ms (worldBudget) did not hold on a slow Windows runner
// under -race: the VM, the stage, the setup's provision did not get where
// the test looked (#793); 1 s does with a margin.
// wall-clock: a margin for the steps of a call, which take milliseconds;
// the watch is a fraction of the production waits it tells apart.
const (
	progressBudget = time.Second
	progressWatch  = 10 * time.Second
)

// progressed is r with progressBudget.
func progressed(r *Registry) *Registry {
	r.SetCallBudget(progressBudget)
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

// vmPowerBudget is a call of a power tool on a VM in state whose
// transition the cloud never ends (the fake's VM stays in the transient
// state: no test steps it): the call can end only by its budget.
func vmPowerBudget(tool, state string) budgetWait {
	return budgetWait{run: func(t *testing.T) (func(context.Context) (string, bool), func()) {
		faultToolsIsolate(t)
		cloud := &faultCloud{}
		vm, err := cloud.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"})
		if err != nil {
			t.Fatal(err)
		}
		cloud.setVMState(vm.ID, state)
		r := budgeted(budgetComputeRegistry(t, cloud, t.TempDir(), engine.Timings{}))
		args := map[string]any{"name": "web", "confirmed": true}
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

// stackDestroyBudget is stack_destroy of a stack whose VM the cloud then
// deletes slowly: the VM step's destroy spends the budget of the outer call
// and answers deleting, which stack_destroy lists with its next step.
func stackDestroyBudget() budgetWait {
	return budgetWait{run: func(t *testing.T) (func(context.Context) (string, bool), func()) {
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
		cloud.mu.Unlock()
		r := budgeted(budgetComputeRegistry(t, cloud, dir, engine.Timings{}))
		return func(ctx context.Context) (string, bool) {
			return r.handlers["stack_destroy"](ctx, map[string]any{"job_id": j.ID, "confirmed": true})
		}, nil
	}, done: func(m map[string]any) string {
		deleting, _ := m["deleting"].([]any)
		switch {
		case len(deleting) != 1 || deleting[0] != "vm":
			return "the VM is not in deleting"
		case m["next_tool"] != "stack_destroy":
			return "its next step is not stack_destroy"
		}
		return ""
	}}
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
	// The VM is made, the compose-up stage goes on on the VM (D69 variant
	// C; the world of TestDeployStage_OnceAndNotCutShort).
	"deploy": {run: func(t *testing.T) (func(context.Context) (string, bool), func()) {
		r := stageRegistry(t, newStageVM(), worldBudget)
		return func(ctx context.Context) (string, bool) {
			return r.handlers["deploy"](ctx, map[string]any{"confirmed": true})
		}, nil
	}},
	"stack_apply":   stackBudget(),
	"stack_destroy": stackDestroyBudget(),
	"provision":     computeBudget("provision", map[string]any{"name": "web", "confirmed": true}, false, nil),
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
	"dns_operation": opBudget("dns_operation"), "k8s_operation": opBudget("k8s_operation"), "kafka_operation": opBudget("kafka_operation"),
	"lb_operation": opBudget("lb_operation"), "pg_operation": opBudget("pg_operation"),
	"redis_operation": opBudget("redis_operation"), "vpc_operation": opBudget("vpc_operation"),

	"dns_zone_delete": apiBudget("dns_zone_delete", map[string]any{"id": "00000000-0000-4000-8000-0000000000a7", "type": "public", "confirmed": true},
		faultResource{host: "dns.api.cloud.ru", path: "/v1/publicZones", ID: "00000000-0000-4000-8000-0000000000a7", Name: "example.com.", before: true}),

	// No world of the slow cloud reaches these yet: what the code shows.
	"stack_status": {static: "the handler waits at most 20 s (tools_stack.go), as its schema says: within the budget"},
	"k8s_kubectl":  {run: kubeBudget("k8s_kubectl", map[string]any{"cluster_id": faultCluster, "args": "rollout status deployment/web -n app"})},
	"k8s_helm":     {run: kubeBudget("k8s_helm", map[string]any{"cluster_id": faultCluster, "action": "install", "release": "web", "chart": "repo/web", "namespace": "app", "confirmed": true})},
	"disk_create":  apiBudget("disk_create", map[string]any{"name": "data", "size_gb": 10, "attach_to": faultTargetVM, "confirmed": true}),
	"disk_attach":  apiBudget("disk_attach", map[string]any{"disk_id": faultDisk, "vm": faultTargetVM, "confirmed": true}, slowDisk),
	"disk_detach":  apiBudget("disk_detach", map[string]any{"disk_id": faultDisk, "vm": faultTargetVM, "confirmed": true}, slowDisk),
	"disk_resize":  apiBudget("disk_resize", map[string]any{"disk_id": faultDisk, "size_gb": 20, "confirmed": true}, slowDisk),
	"vm_start":     vmPowerBudget("vm_start", "stopped"),
}

// mcpOverBudget are the MCP tools a call of which can outlive D69's budget,
// by the slow world, the code (mcpWaits' static) or a schema that lets the
// agent ask for a longer wait. It only shrinks. Closes by 1.7 (release plan
// 1.7, block A; process plan 1.7 §1: red before PR 1 of D69, each PR of
// D69 takes its lines out).
var mcpOverBudget = map[string]string{}

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
	// consented: the tools whose answer names a next step that carries
	// confirmed (consentInNextStep).
	consented := map[string]bool{}
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
				if consentInNextStep(a.text) {
					consented[name] = true
				}
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
	// The consent does not move into the next step (R-85; «Риск · 1.9»,
	// conclusion 123): an agent that calls the step the answer names sends
	// the call with the consent in it — the person was asked once, for the
	// call cut short, not for its repeat.
	for name := range consented {
		if nextStepKeepsConsent[name] == "" && nextStepConsentUnbound[name] == "" {
			bad = append(bad, name+": its answer cut by the budget names a next step with confirmed in next_args — a next step is a read, or the same call without confirmed (nextSameCall, gate.go); the repeat reads the state and does not write (D99, П3). A continuation that keeps the consent is a decision: bound to the id of what was approved, and declared in nextStepKeepsConsent")
		}
	}
	for _, list := range []map[string]string{nextStepKeepsConsent, nextStepConsentUnbound} {
		for name := range list {
			if ran[name] && !consented[name] && !consentShownElsewhere(name) {
				bad = append(bad, name+": listed as a tool whose next step carries confirmed, and it does not: take the line out")
			}
			if _, ok := mcpWaits[name]; !ok {
				bad = append(bad, name+": listed as a tool whose next step carries confirmed, not in mcpWaits")
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Error(strings.Join(bad, "\n"))
	}
	t.Logf("mcpOverBudget: %d lines (Closes by 1.7)", len(mcpOverBudget))
}

// nextStepKeepsConsent are the declared continuations: a next step that
// carries confirmed and is bound to what the person consented to, so that
// the repeat cannot be of anything else (owner's decision 12.11 on R-85;
// «Риск», conclusion 125, with a probe on the worlds: destroy sends no
// second delete, the write of disk_detach and disk_resize is the one
// approved). "tool" → the binding. A closed list: a new tool does not
// join it without a decision.
var nextStepKeepsConsent = map[string]string{
	"deploy":        "compose_hash in next_args: the repeat is of the compose file the person approved, another one asks again (D82)",
	"destroy":       "vm_id in next_args: the delete of the VM the person approved, by its id",
	"stack_destroy": "job_id in next_args: the destroy of the job the person approved, by its id",
	"disk_attach":   "disk_id and vm in next_args: the attach the person approved; a disk of another VM is refused with the consent too (D81)",
	"disk_create":   "disk_id and vm in next_args: the attach of attach_to the person approved",
	"disk_detach":   "disk_id and vm in next_args: the detach the person approved",
	"disk_resize":   "disk_id and size_gb in next_args: the size the person approved",
	"deploy_apply":  "vm_id in next_args: the VM the call made; a step whose VM is gone refuses and creates nothing (D102, continuedVM)",
	"provision":     "vm_id in next_args: the VM the call made; a step whose VM is gone refuses and creates nothing (D102, continuedVM)",
	// Stopped while it adds the chart's repository, before the release is
	// touched, helm names the same call (nextStepOtherWorlds; owner's
	// decision 12.11, «Риск», conclusion 126).
	"k8s_helm": "cluster_id and release in next_args: the install or upgrade of the release the person approved, stopped before the release was touched",
}

// nextStepConsentUnbound are the tools shipped before the invariant whose
// next step carries confirmed and is bound to nothing: the step names the
// VM by its name or by the plan, and a VM gone between the two calls is
// made a second time («Риск», conclusion 125: CreateVM 1 → 2). The step
// gets bound to the id of the VM (D102, «разработка A»); then the line
// moves to nextStepKeepsConsent. "tool" → what the next step is. It only
// shrinks, and a new tool does not join it. Closes by 1.9.
var nextStepConsentUnbound = map[string]string{}

// consentInNextStep reports whether the answer names a next step whose
// arguments carry confirmed.
func consentInNextStep(text string) bool {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return false
	}
	args, _ := m["next_args"].(map[string]any)
	_, has := args["confirmed"]
	return has
}

// finalStatus are the states of a call that is over: an answer cut by the
// budget in the slow world, where nothing ends, cannot have one.
var finalStatus = map[string]bool{"active": true, "running": true, "ready": true, "available": true,
	"done": true, "deleted": true, "created": true, "ok": true}

// budgetAnswerProblem reads the answer of a call the slow world cut short
// (D69 §2; CLAUDE.md §6, axis 2): not an error; a state that is not final;
// what to call next — a tool of the registry, not a command of the CLI —
// in words (next) and for the machine (next_tool, next_args).
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
	named := false
	for _, tl := range New(nil).Tools() {
		if strings.Contains(next, tl.Name) {
			named = true
			break
		}
	}
	if !named {
		return "its next step names no tool of the registry"
	}
	// The step for the machine, beside the text for the person (nextStep;
	// owner's decision 11.11, Н3): the agent calls it without parsing next.
	nextTool, _ := m["next_tool"].(string)
	if _, ok := New(nil).Lookup(nextTool); !ok {
		return "its next_tool " + `"` + nextTool + `"` + " is not a tool of the registry"
	}
	if args, _ := m["next_args"].(map[string]any); len(args) == 0 {
		return "it names next_tool " + nextTool + " without next_args"
	}
	return ""
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
