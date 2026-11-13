// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// unfinishedRecord are the statuses of a record deploy has not finished
// (engine.unfinishedStatuses, D76): logs, verify and status read them as
// «deploy goes on», not as a failure of SSH.
var unfinishedRecord = map[string]bool{"creating": true, "provisioned": true, "deploying": true}

// inProgressLeavesFinished are the phases in which a deploy cut by the
// budget of an MCP call answers deploying but leaves its record in a
// status logs and verify read as finished, with the work item that fixes
// it. A line the walk no longer finds fails: the list only shrinks.
// Closes by 1.8.
var inProgressLeavesFinished = map[string]string{}

// inProgressPhasesElsewhere are the phases of deploy the walk of this test
// need not reach: the test that holds the same for them, or why none does
// yet and the work item. A closed list by the phases of the engine: a name
// that is no phase fails.
var inProgressPhasesElsewhere = map[string]string{
	"vm": "engine.TestDeploy_BoundEndsTheWaitForTheVMInProgress: the record is creating",
	// A probe that never answers held the whole call past its context
	// (05.11, a hypothesis until a test shows it): the world of a slow
	// probe is a line of the process backlog, not this walk.
	"verify": "not reached: the probe is HTTP, not SSH; process-handoff.md, backlog 1.8 «мир медленной пробы verify»",
}

// K1 (process plan 1.8; agent run 1.7.0, defect 1 — it escaped: the tests
// of D69 read the error of a cut deploy, the tests of D76 a record put by
// hand, none the record a cut deploy leaves). Every answer deploying of
// deploy over MCP leaves the record of the deployment unfinished: the
// record in the answer has an unfinished status, and logs read at that
// moment answers DEPLOY_IN_PROGRESS, not a failure of SSH. The walk hangs
// the first SSH call of each kind of a whole deploy (as
// TestDeploySSH_EachStepUnderTheBudget hangs each) and Docker never ready;
// a phase of the engine no walk reaches fails until
// it is reached or named in inProgressPhasesElsewhere.
func TestDeploy_InProgressLeavesTheRecordUnfinished(t *testing.T) {
	const chain = 8
	rec := &slowSSHVM{stageVM: newStageVM()}
	deployChain(t, progressBudgetRegistry(t, rec), rec.stageVM, map[string]any{"confirmed": true}, chain)
	rec.mu.Lock()
	ops := append([]string(nil), rec.ops...)
	rec.mu.Unlock()
	if len(ops) < 4 {
		t.Fatalf("%d SSH calls in a whole deploy: the world sees nothing (%v)", len(ops), ops)
	}
	// A world, and the phase its chain is read to (""; the call that hangs).
	type world struct {
		vm    func() *slowSSHVM
		phase string
	}
	worlds := map[string]world{
		"docker never ready": {func() *slowSSHVM { return &slowSSHVM{stageVM: newStageVM(), notReady: true} }, "docker"},
	}
	// One world per kind of SSH call, at its first: a later one of the same
	// kind is cut in the same phase.
	kinds := map[string]bool{}
	for i, op := range ops {
		if kinds[op] {
			continue
		}
		kinds[op] = true
		n := i + 1
		worlds[strconv.Itoa(n)+" "+op+" hangs"] = world{vm: func() *slowSSHVM { return &slowSSHVM{stageVM: newStageVM(), hang: n} }}
	}
	seen := map[string]bool{}    // the phases of answers deploying
	found := map[string]string{} // phase → what was wrong, first seen
	names := make([]string, 0, len(worlds))
	for name := range worlds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w := worlds[name]
		vm := w.vm()
		r := progressBudgetRegistry(t, vm)
		args := map[string]any{"confirmed": true}
		for k := 0; k < chain; k++ {
			text := deployCall(t, r, args)
			var m map[string]any
			if text == "" || json.Unmarshal([]byte(text), &m) != nil || m["status"] != "deploying" {
				break
			}
			phase, _ := m["phase"].(string)
			seen[phase] = true
			if why := unfinishedProblem(t, r, vm, m); why != "" && found[phase] == "" {
				found[phase] = name + ", call " + strconv.Itoa(k+1) + ": " + why
			}
			vm.mu.Lock()
			cut := vm.hung || w.phase == phase
			vm.mu.Unlock()
			next, _ := m["next_args"].(map[string]any)
			if cut || next == nil {
				break // the world's phase is read; the rest of the chain is another world's
			}
			args = next
			vm.finish()
		}
	}
	for phase, why := range found {
		if inProgressLeavesFinished[phase] == "" {
			t.Errorf("phase %s: deploy answers deploying, but %s — keep the record unfinished, or, while the plan moves it, a line of inProgressLeavesFinished", phase, why)
		}
	}
	for phase := range inProgressLeavesFinished {
		if found[phase] == "" {
			t.Errorf("inProgressLeavesFinished names phase %s, which no longer leaves the record finished: remove the line", phase)
		}
	}
	phases := map[string]bool{}
	for _, phase := range enginePhases(t) {
		phases[phase] = true
	}
	for phase := range inProgressPhasesElsewhere {
		if !phases[phase] {
			t.Errorf("inProgressPhasesElsewhere names %s, which is no phase of the engine: remove the line", phase)
		}
	}
	for _, phase := range enginePhases(t) {
		if !seen[phase] && inProgressPhasesElsewhere[phase] == "" {
			t.Errorf("phase %s of the engine: no walk answers deploying in it; reach it here, or name the test that holds it in inProgressPhasesElsewhere", phase)
		}
	}
	// A phase named elsewhere that this walk reached too is no failure:
	// whether a call is cut as early as the phase vm is the runner's speed
	// (a slow Windows runner cut it there, 12.11), and a phase read twice
	// is held twice. The record it left was read above, like any other.
	for phase := range inProgressPhasesElsewhere {
		if seen[phase] {
			t.Logf("phase %s is named in inProgressPhasesElsewhere and was reached by this walk as well", phase)
		}
	}
}

// unfinishedProblem is what is wrong with the record a deploy that answered
// m (deploying) left: "" when it is unfinished and logs says so.
func unfinishedProblem(t *testing.T, r *Registry, vm *slowSSHVM, m map[string]any) string {
	t.Helper()
	d, _ := m["deployment"].(map[string]any)
	status, _ := d["status"].(string)
	var problems []string
	if !unfinishedRecord[status] {
		problems = append(problems, "its record says "+strconv.Quote(status))
	}
	var logs string
	vm.reading(func() { logs, _ = r.handlers["logs"](context.Background(), map[string]any{}) })
	var l map[string]any
	_ = json.Unmarshal([]byte(logs), &l)
	if l["reason"] != "DEPLOY_IN_PROGRESS" {
		problems = append(problems, "logs answers "+clipText(logs))
	}
	return strings.Join(problems, "; ")
}

// phaseLiteral finds the phases the engine names: e.phase = "x" and the
// stages it runs, runStage(ctx, "x", …).
var phaseLiteral = regexp.MustCompile(`e\.phase = "([a-z-]+)"|runStage\(ctx, "([a-z-]+)"`)

// enginePhases are the phases of deploy in the engine's code.
func enginePhases(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "engine", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range phaseLiteral.FindAllStringSubmatch(string(b), -1) {
			set[m[1]+m[2]] = true
		}
	}
	if len(set) < 4 {
		t.Fatalf("%d phases found in the engine: the scan sees nothing (%v)", len(set), set)
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// sshNeverAnswersOpen is the work item that ends the chain of deploy over
// MCP when SSH never answers; "" once it is done. While it is open the
// test reports what it finds and passes; once the chain ends and says why,
// a line still here fails.
const sshNeverAnswersOpen = ""

// k2Attempt is how long a connect to the silent VM takes to fail.
// wall-clock: a share of progressBudget, so that a call makes a score of
// attempts before its budget ends, not one.
const k2Attempt = progressBudget / 20

// K2 (process plan 1.8; agent run 1.7.0, defect 2 — it escaped: the chain
// of TestDeploySSH_EachStepUnderTheBudget hangs a step once, never always).
// SSH never answers: the agent repeats deploy by next_args, and the chain
// ends as the CLI's wait does — SSH_TIMEOUT after the attempts of one wait,
// counted across the calls, not again in each — and before the end an
// answer says why it may never answer (outbound port 22, nc -vz), once the
// wait across the calls is past SSHSlowNote. The CLI says both
// (deploy.go, waitForSSH); a call over MCP is shorter than either.
func TestDeploy_ChainEndsWhenSSHNeverAnswers(t *testing.T) {
	const chain = 8 // 60 attempts of one wait are 3 calls of a score each
	vm := &slowSSHVM{stageVM: newStageVM(), silent: true, connectWait: k2Attempt}
	r := progressBudgetRegistry(t, vm, func(tm *engine.Timings) {
		tm.SSHRetryInterval = time.Millisecond
		// Past one call, well within the chain.
		tm.SSHSlowNote = progressBudget * 3 / 2
	})
	answers := deployChain(t, r, vm.stageVM, map[string]any{"confirmed": true}, chain)
	last := answers[len(answers)-1]
	var m map[string]any
	_ = json.Unmarshal([]byte(last), &m)
	var problems []string
	if last == "" || m["status"] == "deploying" {
		problems = append(problems, strconv.Itoa(len(answers))+" calls by next_args do not end deploy: "+strings.Join(chainStatuses(answers), " "))
	} else if m["reason"] != "SSH_TIMEOUT" {
		problems = append(problems, "the chain ends with "+clipText(last)+", want SSH_TIMEOUT")
	}
	said := false
	for _, a := range answers {
		if strings.Contains(a, "nc -vz") {
			said = true
		}
	}
	if !said {
		problems = append(problems, "no answer says why SSH may never answer (nc -vz, outbound port 22)")
	}
	switch {
	case len(problems) > 0 && sshNeverAnswersOpen != "":
		t.Logf("open (%s): %s", sshNeverAnswersOpen, strings.Join(problems, "; "))
	case len(problems) > 0:
		t.Error(strings.Join(problems, "\n"))
	case sshNeverAnswersOpen != "":
		t.Errorf("the chain ends with SSH_TIMEOUT and says why: set sshNeverAnswersOpen to \"\" (%s)", sshNeverAnswersOpen)
	}
}

// sshMaxAttempts is the attempts of the wait for SSH of a new VM, the CLI's
// in one call (engine.sshMaxRetries, D80): the limit a chain of deploy over
// MCP keeps whichever call created the VM.
const sshMaxAttempts = 60

// FB171-3 (process plan 1.8, from 1.7.1): K2 over a VM the creation outlived.
// The first call of the chain ends while the VM is being created (its
// record creating, no SSH yet); the VM comes up and SSH never answers. The
// chain ends with SSH_TIMEOUT after the attempts of a new VM, as the CLI
// gives it in one call — not the half the wait of a VM never reached gets
// (FB171-1: the limit was picked anew in the call that met SSH first).
func TestDeploy_ChainKeepsTheNewVMLimitPastItsCreation(t *testing.T) {
	const chain = 10
	vm := &slowSSHVM{stageVM: newStageVM(), silent: true, connectWait: k2Attempt}
	cloud := &faultCloud{slow: true}
	r := progressBudgetRegistryIn(t, vm, cloud, func(tm *engine.Timings) {
		tm.SSHRetryInterval = time.Millisecond
		tm.SSHSlowNote = progressBudget * 3 / 2
	})
	first := deployChain(t, r, vm.stageVM, map[string]any{"confirmed": true}, 1)
	var m map[string]any
	if len(first) != 1 || json.Unmarshal([]byte(first[0]), &m) != nil || m["status"] != "deploying" || m["phase"] != "vm" {
		t.Fatalf("the first call does not end while the VM is created: %s", clipText(strings.Join(first, " ")))
	}
	next, _ := m["next_args"].(map[string]any)
	if next == nil {
		t.Fatalf("the first call names no next step: %s", clipText(first[0]))
	}
	// The VM is up now; SSH never answers.
	cloud.mu.Lock()
	cloud.slow = false
	for i := range cloud.vms {
		cloud.vms[i].Status = "active"
	}
	cloud.mu.Unlock()
	answers := deployChain(t, r, vm.stageVM, next, chain)
	last := answers[len(answers)-1]
	m = nil
	_ = json.Unmarshal([]byte(last), &m)
	if last == "" || m["reason"] != "SSH_TIMEOUT" {
		t.Fatalf("%d calls after the creation do not end with SSH_TIMEOUT: %s", len(answers), strings.Join(chainStatuses(answers), " "))
	}
	vm.mu.Lock()
	connects := 0
	for _, op := range vm.ops {
		if op == "connect" {
			connects++
		}
	}
	vm.mu.Unlock()
	if connects < sshMaxAttempts || connects > sshMaxAttempts+1 {
		t.Errorf("%d SSH attempts before SSH_TIMEOUT over a VM the first call left creating, want %d as the CLI gives a new VM", connects, sshMaxAttempts)
	}
}
