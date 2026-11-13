// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/verify"
	"github.com/shatim-ops/evo-cli/internal/vmstage"
)

// slowSSHVM is the stage VM over an SSH that hangs: the n-th SSH call of
// the world (hang, counted from 1 across the calls of deploy; 0 — none)
// does not answer until its context ends, as a connect to a VM that drops
// the packets, an upload or a command that never returns. notReady makes
// the check that Docker is ready fail on every call, as a VM whose Docker
// never comes up. silent makes every connect wait connectWait (or its
// context) and fail as a timeout: SSH that never answers, as an outbound
// port 22 closed where evo runs. unreachable fails every SSH call at once,
// uncounted: what a test reads between two calls (logs) does not move the
// walk. The other calls are the stage VM's.
type slowSSHVM struct {
	*stageVM
	mu          sync.Mutex
	n, hang     int
	hung        bool
	ops         []string
	notReady    bool
	silent      bool
	connectWait time.Duration
	unreachable bool
}

// errSSHSilent is a connect to a VM whose SSH never answers.
var errSSHSilent = errors.New("dial tcp 192.0.2.10:22: i/o timeout")

// call counts an SSH call and holds it when it is the one to hang.
func (v *slowSSHVM) call(ctx context.Context, op string) error {
	v.mu.Lock()
	if v.unreachable {
		v.mu.Unlock()
		return errSSHSilent
	}
	v.n++
	v.ops = append(v.ops, op)
	held := v.n == v.hang
	if held {
		v.hung = true
	}
	silent := v.silent && op == "connect"
	v.mu.Unlock()
	if held {
		<-ctx.Done()
		return ctx.Err()
	}
	if silent {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(v.connectWait):
			return errSSHSilent
		}
	}
	return nil
}

// reading runs read with every SSH call failing at once and uncounted.
func (v *slowSSHVM) reading(read func()) {
	v.mu.Lock()
	v.unreachable = true
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.unreachable = false
		v.mu.Unlock()
	}()
	read()
}

func (v *slowSSHVM) Connect(ctx context.Context, host string, port int, user string, key []byte, known string) error {
	if err := v.call(ctx, "connect"); err != nil {
		return err
	}
	return v.stageVM.Connect(ctx, host, port, user, key, known)
}

func (v *slowSSHVM) Upload(ctx context.Context, files []deploy.FileEntry) error {
	if err := v.call(ctx, "upload"); err != nil {
		return err
	}
	return v.stageVM.Upload(ctx, files)
}

func (v *slowSSHVM) Exec(ctx context.Context, cmd string) (string, error) {
	if err := v.call(ctx, "exec "+firstWords(cmd)); err != nil {
		return "", err
	}
	if v.notReady && strings.Contains(cmd, "docker info") {
		return "", errors.New("Cannot connect to the Docker daemon")
	}
	return v.stageVM.Exec(ctx, cmd)
}

func (v *slowSSHVM) ExecStream(ctx context.Context, cmd string, stdout, stderr io.Writer) error {
	if err := v.call(ctx, "stream "+firstWords(cmd)); err != nil {
		return err
	}
	return v.stageVM.ExecStream(ctx, cmd, stdout, stderr)
}

// Start and Read of a stage are SSH calls on a real VM (vmstage.Over runs
// the stage script over the session): they count and hang as the others.
func (v *slowSSHVM) Start(ctx context.Context, name, input, script string) (bool, error) {
	if err := v.call(ctx, "stage start "+name); err != nil {
		return false, err
	}
	return v.stageVM.Start(ctx, name, input, script)
}

func (v *slowSSHVM) Read(ctx context.Context, name string) (vmstage.Stage, error) {
	if err := v.call(ctx, "stage read "+name); err != nil {
		return vmstage.Stage{}, err
	}
	return v.stageVM.Read(ctx, name)
}

func firstWords(cmd string) string {
	f := strings.Fields(cmd)
	return strings.Join(f[:min(len(f), 3)], " ")
}

// progressBudgetRegistry is the stage world with the budget of an MCP call and
// the timeouts of production: an SSH step the budget does not bound runs
// to its own timeout (Docker: 15 min), not to a short one of the world.
// The intervals of the SSH side are production's too — a wait between two
// attempts that the budget does not end lasts seconds, not a millisecond;
// a deploy that goes well never waits them. The cloud's stay short. tune
// changes the timings of the world (a test that reads a note of a wait).
func progressBudgetRegistry(t *testing.T, vm *slowSSHVM, tune ...func(*engine.Timings)) *Registry {
	t.Helper()
	return progressBudgetRegistryIn(t, vm, &faultCloud{}, tune...)
}

// progressBudgetRegistryIn is progressBudgetRegistry over the cloud the
// test holds (a VM kept creating, then up: FB171-3).
func progressBudgetRegistryIn(t *testing.T, vm *slowSSHVM, cloud *faultCloud, tune ...func(*engine.Timings)) *Registry {
	t.Helper()
	faultToolsIsolate(t)
	timings := faultToolsTimings()
	timings.VMPollTimeout, timings.FIPSettleTimeout, timings.DockerWaitTimeout = 0, 0, 0
	timings.DockerPollInterval, timings.SSHRetryInterval = 0, 0
	// A few reads of a going stage per call, not one per millisecond.
	timings.StagePollInterval = progressBudget / 4
	for _, f := range tune {
		f(&timings)
	}
	compose := writeTestCompose(t)
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: faultProject, Region: faultZone, ImageID: faultImage,
				Flavor: "low-1-1", DiskSize: 10, KeyID: "k", Secret: "s", ComposeFile: compose},
			Auth:      staticToken{},
			Cloud:     cloud,
			Deployer:  vm,
			Printer:   output.NewWithWriter(&buf, output.ModePlain, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("fake-key"),
			SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings:   timings,
			Probe: func(context.Context, string, []int, time.Duration) []verify.ProbeResult {
				return []verify.ProbeResult{{Port: 80, StatusCode: 200, Healthy: true}}
			},
		}, nil
	})
	r.SetStateDir(dir)
	r.SetCallBudget(progressBudget)
	return r
}

// deployChain calls deploy over MCP as an agent does: the first call with
// args, each next one with the answer's next_args while it says deploying,
// the stage on the VM ending between two calls. Each call is held to
// progressWatch. It returns the answers; "" is a call that did not answer in
// time, and the chain stops there.
func deployChain(t *testing.T, r *Registry, vm *stageVM, args map[string]any, calls int) []string {
	t.Helper()
	var out []string
	for i := 0; i < calls; i++ {
		got := make(chan string, 1)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			text, _ := r.handlers["deploy"](ctx, args)
			got <- text
		}()
		var text string
		select {
		case text = <-got:
		case <-time.After(progressWatch):
			cancel()
			<-got
		}
		cancel()
		out = append(out, text)
		var m map[string]any
		if text == "" || json.Unmarshal([]byte(text), &m) != nil || m["status"] != "deploying" {
			return out
		}
		next, _ := m["next_args"].(map[string]any)
		if next == nil {
			return out
		}
		args = next
		vm.finish()
	}
	return out
}

// chainStatuses is the status and phase of each answer of a chain.
func chainStatuses(answers []string) []string {
	var out []string
	for _, text := range answers {
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		out = append(out, fmt.Sprintf("%v/%v", m["status"], m["phase"]))
	}
	return out
}

// deployStageStarts is the starts of the compose-up stage on vm.
func deployStageStarts(vm *stageVM) int {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.starts
}

// The extension of D69 variant C's invariant (release plan 1.7, block A;
// «1.7 — релиз» on #769): every SSH step of deploy's MCP path is under the
// budget of the call. The world records the SSH calls of a whole deploy —
// first call to the end, the agent repeating it by next_args — then hangs
// each of them in turn, one per run (a run of the same call — the reads
// of a going stage — once): the call that meets it answers within
// progressWatch with the state so far (deploying, its next step), and the
// chain of repeats still ends. A new SSH step of deploy is in the walk by
// itself: the world counts calls, it names none.
func TestDeploySSH_EachStepUnderTheBudget(t *testing.T) {
	const chain = 8
	rec := &slowSSHVM{stageVM: newStageVM()}
	answers := deployChain(t, progressBudgetRegistry(t, rec), rec.stageVM, map[string]any{"confirmed": true}, chain)
	if last := answers[len(answers)-1]; last == "" || deployStatus(last) == "deploying" {
		t.Fatalf("the world's deploy does not end in %d calls: %v", chain, chainStatuses(answers))
	}
	rec.mu.Lock()
	ops := append([]string(nil), rec.ops...)
	rec.mu.Unlock()
	if len(ops) < 4 {
		t.Fatalf("%d SSH calls in a whole deploy: the world sees nothing (%v)", len(ops), ops)
	}
	for i, op := range ops {
		n := i + 1
		if i > 0 && op == ops[i-1] {
			continue // a repeat of the step before (the reads of a going stage): one walk per step
		}
		t.Run(strconv.Itoa(n)+" "+op, func(t *testing.T) {
			vm := &slowSSHVM{stageVM: newStageVM(), hang: n}
			answers := deployChain(t, progressBudgetRegistry(t, vm), vm.stageVM, map[string]any{"confirmed": true}, chain)
			for k, text := range answers {
				if text == "" {
					t.Fatalf("call %d did not answer within the budget (D69) with SSH call %d (%s) hanging", k+1, n, op)
				}
				checkNoTerminalCodes(t, "deploy", text)
			}
			vm.mu.Lock()
			hung := vm.hung
			vm.mu.Unlock()
			if !hung {
				t.Skipf("the walk with SSH call %d hanging took another path; it never came", n)
			}
			if last := answers[len(answers)-1]; deployStatus(last) == "deploying" {
				t.Errorf("with SSH call %d (%s) hanging once, %d repeats by next_args do not end deploy: %s", n, op, chain, last)
			}
			if s := deployStageStarts(vm.stageVM); s != 1 {
				t.Errorf("with SSH call %d (%s) hanging once, the compose-up stage started %d times, want 1", n, op, s)
			}
		})
	}
}

// (б) Docker installed but never ready: each call answers within the
// budget, deploying in phase docker, not a wait of DockerWaitTimeout in
// the call (15 min).
func TestDeploySSH_DockerNeverReadyUnderTheBudget(t *testing.T) {
	vm := &slowSSHVM{stageVM: newStageVM(), notReady: true}
	answers := deployChain(t, progressBudgetRegistry(t, vm), vm.stageVM, map[string]any{"confirmed": true}, 3)
	for k, text := range answers {
		if text == "" {
			t.Fatalf("call %d did not answer within the budget (D69) while Docker is never ready", k+1)
		}
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(answers[len(answers)-1]), &m)
	if m["status"] != "deploying" || m["phase"] != "docker" {
		t.Errorf("Docker never ready reads as %v / %v, want deploying in phase docker: %s", m["status"], m["phase"], answers[len(answers)-1])
	}
}

// (в) force while compose up runs longer than the budget: the agent repeats
// by next_args; compose up starts once for the forced call, and the chain
// ends — no repeat starts it again («1.7 — релиз» on #769).
func TestDeploySSH_ForceStartsComposeUpOnceAcrossRepeats(t *testing.T) {
	vm := &slowSSHVM{stageVM: newStageVM()}
	r := progressBudgetRegistry(t, vm)
	if a := deployChain(t, r, vm.stageVM, map[string]any{"confirmed": true}, 8); deployStatus(a[len(a)-1]) == "deploying" || a[len(a)-1] == "" {
		t.Fatalf("the first deploy does not end: %v", chainStatuses(a))
	}
	before := deployStageStarts(vm.stageVM)
	answers := deployChain(t, r, vm.stageVM, map[string]any{"confirmed": true, "force": true}, 8)
	if last := answers[len(answers)-1]; last == "" || deployStatus(last) == "deploying" {
		t.Fatalf("the forced deploy does not end in 8 repeats by next_args: %v (compose up started %d times)", chainStatuses(answers), deployStageStarts(vm.stageVM)-before)
	}
	if got := deployStageStarts(vm.stageVM) - before; got != 1 {
		t.Errorf("force started compose up %d times across the repeats, want 1", got)
	}
}
