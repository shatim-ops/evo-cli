// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
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

// stageVM is the VM of deploy's long stage (docker compose up) as the
// variant C of D69 sees it (owner's decision 03.11): the stage runs on the
// VM, apart from the call that started it, and a call that comes again
// reads how it goes. evo runs the stage through vmstage.Runner (Start,
// Read), which stageVM is: the compose-up stage runs until the test ends
// it; any other stage (the Docker install) is done at once. A compose up
// over the session itself (Exec, ExecStream) is the SSH session's: it dies
// with the session — the end of the call — and the counters show it.
// That the real Runner keeps a stage going past the session is the stage
// script's, tested on a real shell (internal/vmstage).
type stageVM struct {
	mu sync.Mutex
	// running: the stage is going; done: it ended, ok or not.
	running, done bool
	// starts: stages started; startedWhileRunning: of them, over one that
	// was going; stops: commands that stopped a going stage; diedWithCall:
	// stages that died because the call that started them ended.
	starts, startedWhileRunning, stops, diedWithCall int
	// lost: a stage was going and no one holds it now.
	lost bool
	// input: what the compose-up stage was started for.
	input string
	// release ends the going stage.
	release chan struct{}
	// others: the inputs of the other stages, done at once.
	others map[string]string
}

// state is what the marker of the stage says (stageStatus).
func (v *stageVM) state() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case v.running:
		return "running"
	case v.lost:
		return "lost"
	case v.done:
		return "done"
	}
	return "none"
}

func newStageVM() *stageVM { return &stageVM{release: make(chan struct{})} }

// finish ends the going stage as done (the test's «the stage is over»).
func (v *stageVM) finish() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.running {
		v.running, v.done = false, true
		close(v.release)
		v.release = make(chan struct{})
	}
}

// lose drops the going stage without an end: the VM rebooted, the process
// was killed from outside — no one holds the stage any more.
func (v *stageVM) lose() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.running {
		v.running, v.lost = false, true
	}
}

// stageUp and stageStop are the commands that start and stop the stage
// over the session, whatever flags evo gives docker compose (-f, -d).
var (
	stageUp   = regexp.MustCompile(`\bcompose\b.*\bup\b`)
	stageStop = regexp.MustCompile(`\bcompose\b.*\b(down|stop|kill)\b|\b(pkill|kill)\b`)
)

// Start is vmstage.Runner's: the compose-up stage starts unless it goes
// (a start over a going one is counted, and starts nothing); any other
// stage is done at once.
func (v *stageVM) Start(_ context.Context, name, input, _ string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if name != "compose-up" {
		if v.others == nil {
			v.others = map[string]string{}
		}
		v.others[name] = input
		return true, nil
	}
	if v.running {
		v.startedWhileRunning++
		return false, nil
	}
	v.starts++
	v.running, v.done, v.lost, v.input = true, false, false, input
	return true, nil
}

// Read is vmstage.Runner's: the compose-up stage as the VM has it.
func (v *stageVM) Read(_ context.Context, name string) (vmstage.Stage, error) {
	if name != "compose-up" {
		v.mu.Lock()
		defer v.mu.Unlock()
		if input, ok := v.others[name]; ok {
			return vmstage.Stage{State: "done", Input: input}, nil
		}
		return vmstage.Stage{State: "none"}, nil
	}
	st := vmstage.Stage{State: v.state(), LogTail: "web-1  | pulling"}
	v.mu.Lock()
	st.Input = v.input
	v.mu.Unlock()
	return st, nil
}

func detached(cmd string) bool {
	return strings.Contains(cmd, "nohup") || strings.Contains(cmd, "setsid") ||
		strings.Contains(cmd, "systemd-run") || strings.HasSuffix(strings.TrimSpace(cmd), "&")
}

// command runs cmd on the VM; wait is whether the session waits for it.
func (v *stageVM) command(ctx context.Context, cmd string, wait bool) error {
	v.mu.Lock()
	switch {
	case stageStop.MatchString(cmd):
		if v.running {
			v.stops++
			v.running = false
		}
		v.mu.Unlock()
		return nil
	case stageUp.MatchString(cmd):
		v.starts++
		if v.running {
			v.startedWhileRunning++
		}
		v.running, v.done, v.lost = true, false, false
		release := v.release
		v.mu.Unlock()
		if !wait || detached(cmd) {
			return nil
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			v.mu.Lock()
			if v.running {
				v.running = false
				v.diedWithCall++
			}
			v.mu.Unlock()
			return ctx.Err()
		}
	}
	v.mu.Unlock()
	return nil
}

func (v *stageVM) Connect(context.Context, string, int, string, []byte, string) error { return nil }
func (v *stageVM) HostKeyFingerprint() string                                         { return "SHA256:stage" }
func (v *stageVM) Upload(context.Context, []deploy.FileEntry) error                   { return nil }
func (v *stageVM) Close() error                                                       { return nil }

func (v *stageVM) Exec(ctx context.Context, cmd string) (string, error) {
	if strings.Contains(cmd, "compose version") {
		return "Docker Compose version v2.24.0", nil
	}
	return "", v.command(ctx, cmd, true)
}

func (v *stageVM) ExecStream(ctx context.Context, cmd string, stdout, _ io.Writer) error {
	err := v.command(ctx, cmd, true)
	if err == nil {
		_, _ = io.WriteString(stdout, "ok\n")
	}
	return err
}

// stageRegistry is the compute fault world with the stage VM, budget as
// the D69 budget of an MCP call, and the world's short intervals: progressBudget
// for a test that reads how far a call got, worldBudget for one that reads
// only that it answered in time (#793). tune changes the engine of a call
// (another Deployer, other timings).
func stageRegistry(t *testing.T, vm *stageVM, budget time.Duration, tune ...func(*engine.Engine)) *Registry {
	t.Helper()
	faultToolsIsolate(t)
	cloud := &faultCloud{}
	dir := t.TempDir()
	compose := writeTestCompose(t)
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		eng := &engine.Engine{
			Config: config.Config{ProjectID: faultProject, Region: faultZone, ImageID: faultImage,
				Flavor: "low-1-1", DiskSize: 10, KeyID: "k", Secret: "s", ComposeFile: compose},
			Auth:      staticToken{},
			Cloud:     cloud,
			Deployer:  vm,
			Printer:   output.NewWithWriter(&buf, output.ModePlain, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("fake-key"),
			SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings:   faultToolsTimings(),
			Probe: func(context.Context, string, []int, time.Duration) []verify.ProbeResult {
				return []verify.ProbeResult{{Port: 80, StatusCode: 200, Healthy: true}}
			},
		}
		for _, f := range tune {
			f(eng)
		}
		return eng, nil
	})
	r.SetStateDir(dir)
	r.SetCallBudget(budget)
	return r
}

// deployCall is one call of deploy over MCP, bounded by progressWatch: its
// answer, or "" when it did not answer in time. args are the call's;
// none — confirmed=true.
// deployStageCalls is how many calls deploy may take to reach its stage
// on the VM: each one answers within the budget and goes on where the last
// one ended (D69).
const deployStageCalls = 10

// deployUntilStageStarts calls deploy until its stage starts on vm — the
// first call on a fast runner, a later one on a slow one, where the steps
// before the stage (the VM, SSH, Docker) take the first call's budget
// (FB171-4: the first call of release.yml on Windows did not start the
// stage within progressBudget) — and answers the last call ("" when one
// did not answer within the budget).
func deployUntilStageStarts(t *testing.T, r *Registry, vm *stageVM) string {
	t.Helper()
	text := ""
	for range deployStageCalls {
		if text = deployCall(t, r); text == "" {
			return ""
		}
		vm.mu.Lock()
		started := vm.starts > 0
		vm.mu.Unlock()
		if started {
			return text
		}
	}
	return text
}

func deployCall(t *testing.T, r *Registry, args ...map[string]any) string {
	t.Helper()
	got := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		a := map[string]any{"confirmed": true}
		if len(args) > 0 {
			a = args[0]
		}
		text, _ := r.handlers["deploy"](ctx, a)
		got <- text
	}()
	select {
	case text := <-got:
		return text
	case <-time.After(progressWatch):
		cancel()
		<-got
		return ""
	}
}

func deployStatus(text string) string {
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	s, _ := m["status"].(string)
	return s
}

// deployStagePending holds the stage invariant while PR 4 of D69 is not
// in: the tool → what is missing. It only shrinks. Closed by PR 4 of D69.
var deployStagePending = map[string]string{}

// The stage of deploy on the VM under D69 (variant C, owner's decision
// 03.11; release plan 1.7, block A, the line of «Процесс»). A call answers
// within the budget and leaves the stage going on the VM; a call that
// comes again:
//
//	(1) does not start the stage a second time while it goes;
//	(2) does not cut the going stage short — no stop, and the stage does
//	    not die with the call that started it;
//	(3) reads a stage that is over instead of running it again (the
//	    input of the stage, the compose and its env, is the same);
//	(4) names a stage no one holds any more (the VM lost it) as
//	    interrupted, not as going on for ever.
//
// While deploy is a line of deployStagePending the world shows the gap
// instead: at least one point fails.
func TestDeployStage_OnceAndNotCutShort(t *testing.T) {
	var broken []string
	{
		vm := newStageVM()
		r := stageRegistry(t, vm, progressBudget)
		first := deployUntilStageStarts(t, r, vm)
		vm.mu.Lock()
		died, starts := vm.diedWithCall, vm.starts
		vm.mu.Unlock()
		switch {
		case first == "":
			broken = append(broken, "deploy did not answer within the budget of the call (D69): the stage is to go on on the VM, the answer to say so")
		case died > 0:
			broken = append(broken, "(2) the stage died with the call that started it: "+first)
		case starts != 1:
			broken = append(broken, "the first call started the stage "+strconv.Itoa(starts)+" times: "+first)
		default:
			again := deployCall(t, r)
			vm.mu.Lock()
			whileRunning, stops := vm.startedWhileRunning, vm.stops
			vm.mu.Unlock()
			if whileRunning > 0 {
				broken = append(broken, "(1) the repeat started the stage again while it went: "+again)
			}
			if stops > 0 {
				broken = append(broken, "(2) the repeat stopped the going stage: "+again)
			}
			vm.finish()
			after := deployCall(t, r)
			vm.mu.Lock()
			starts = vm.starts
			vm.mu.Unlock()
			if starts != 1 {
				broken = append(broken, "(3) the call after the stage was over ran it again ("+strconv.Itoa(starts)+" starts, the input unchanged): "+after)
			}
		}
	}
	{
		vm := newStageVM()
		r := stageRegistry(t, vm, progressBudget)
		if first := deployUntilStageStarts(t, r, vm); first == "" {
			broken = append(broken, "(4) deploy did not answer within the budget of the call")
		} else {
			vm.lose()
			text := deployCall(t, r)
			if st := deployStatus(text); st == "deploying" || st == "running" || st == "creating" || st == "" {
				broken = append(broken, "(4) a stage the VM lost reads as "+strconv.Quote(st)+", not as interrupted: "+text)
			}
		}
	}
	_, pending := deployStagePending["deploy"]
	switch {
	case pending && len(broken) == 0:
		t.Error("deploy keeps the stage invariant now: take it out of deployStagePending (the list only shrinks)")
	case !pending:
		for _, b := range broken {
			t.Error(b)
		}
	default:
		t.Logf("deploy, pending (Closes by 1.7): %s", strings.Join(broken, "; "))
	}
}

// A forced deploy whose bound came in compose up has started it: the next
// step repeats the call without force, or each repeat would start compose
// up again and deploy would never end («1.7 — релиз» on #769); so after
// it (verify). Before compose up, force stays: the forced run has not
// started yet.
func TestDeployInProgress_NextArgsDropForceOnceComposeUpStarted(t *testing.T) {
	args := map[string]any{"confirmed": true, "force": true, "compose_file": "docker-compose.yml"}
	for phase, keep := range map[string]bool{"compose-up": false, "verify": false, "docker": true, "vm": true, "ssh": true, "upload": true} {
		got := deployInProgress(map[string]any{}, &engine.InProgress{Phase: phase, Detail: "running"}, args)
		next, _ := got["next_args"].(map[string]any)
		if _, has := next["force"]; has != keep {
			t.Errorf("%s: next_args %v, force kept = %v, want %v", phase, next, has, keep)
		}
		if next["compose_file"] != "docker-compose.yml" {
			t.Errorf("%s: next_args lost an argument: %v", phase, next)
		}
	}
	if _, ok := args["force"]; !ok {
		t.Error("the call's own args were changed")
	}
}
