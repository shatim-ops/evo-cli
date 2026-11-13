// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/bootstrap"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/vmstage"
)

// D69, variant C: the bound of a deploy (DeployOptions.Until, the budget of
// an MCP call) ends the wait for the cloud and the VM with InProgress — the
// VM in the state as creating, the phase and its state — not an error and
// not a VM left without a record. Without a bound (the CLI) deploy waits as
// before.
func TestDeploy_BoundEndsTheWaitForTheVMInProgress(t *testing.T) {
	cloud := &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
		return &provision.VM{ID: id, Status: "creating"}, nil
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Timings.VMPollInterval = time.Millisecond
	eng.Timings.VMPollTimeout = time.Hour
	opts := newDeployOpts(t, dir)
	opts.Until = time.Now().Add(testBound)

	err := eng.Deploy(context.Background(), opts)
	var going *InProgress
	if !stderrors.As(err, &going) || going.Phase != "vm" || going.Detail != "creating" {
		t.Fatalf("err = %v, want InProgress vm creating", err)
	}
	if a := readActive(t, dir); a == nil || a.Status != "creating" || a.VMID == "" {
		t.Fatalf("the VM is not in the state as creating: %+v", a)
	}
}

// SSH that does not answer yet ends with the bound too. The bound is
// doubled while it comes before the first connect (boundedUntilHeld), as for
// the wait for Docker (#1645).
func TestDeploy_BoundEndsTheWaitForSSHInProgress(t *testing.T) {
	err := boundedUntilHeld(t, func(bound time.Duration, held *atomic.Bool) error {
		dep := newMockDeployer()
		dep.connectErr = stderrors.New("ssh: connection refused")
		dep.hold = func(_ context.Context, op string) error {
			if op == "connect" {
				held.Store(true)
			}
			return nil
		}
		eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
		// Between attempts an hour: the bound, not the attempts running
		// out (60 at 1 ms ended in 60 ms, before a bound of testBound),
		// ends the wait.
		eng.Timings.SSHRetryInterval = time.Hour
		opts := newDeployOpts(t, dir)
		opts.Until = time.Now().Add(bound)
		return deployBounded(t, eng, opts)
	})
	var going *InProgress
	if !stderrors.As(err, &going) || going.Phase != "ssh" {
		t.Fatalf("err = %v, want InProgress ssh", err)
	}
}

// untilCtx blocks until ctx ends: an SSH that hangs.
func untilCtx(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// testBound is the bound of a deploy in these tests, from its start: the
// steps before the point a test holds — the VM, SSH, the Docker install —
// take milliseconds, and must not reach the bound before it. 50 ms did not
// hold on a loaded runner under -race (#789: the bound came in vm or docker,
// not where the test held).
// wall-clock: a margin for the steps before the hold, which end in
// milliseconds; the bound itself is what the hold waits for.
const testBound = time.Second

// finishBound is the bound of a call a test expects to end the deploy, not
// to leave it in progress: testBound is the point a hold waits for, and a
// deploy that must finish within it fails on a slow runner (#813: engine
// took 76 s on Windows against 17–26 s, and the repeat after --force came
// to its bound in verify).
// wall-clock: a margin for a call that ends in milliseconds, never waited
// out — the deploy ends long before it.
const finishBound = time.Minute

// boundWatch is how long past its bound a deploy of these tests may take:
// a deploy that waits for its own timeouts takes the hour they are set to
// (wall-clock: a margin over the bound, not a wait).
const boundWatch = 5 * time.Second

// maxTestBound is the largest bound boundedUntilHeld gives a case.
// wall-clock: a margin for a loaded runner; a case that reaches its hold
// ends at testBound on a quiet one.
const maxTestBound = 8 * time.Second

// boundedUntilHeld runs a case with the bound testBound, doubled up to
// maxTestBound while the case ends before it reached the step it holds:
// on a loaded runner the steps before the hold can outlast the bound
// (#789, #813; Windows -race, run 41799151202 of 04.11: the bound came in
// vm or in docker starting), and the bound then ends a step the case does
// not test. A case that reached its hold is judged as it is; one that never
// does fails its own check.
func boundedUntilHeld(t *testing.T, run func(bound time.Duration, held *atomic.Bool) error) error {
	t.Helper()
	for bound := testBound; ; bound *= 2 {
		var held atomic.Bool
		err := run(bound, &held)
		if held.Load() || bound >= maxTestBound {
			return err
		}
		t.Logf("the bound of %v came before the hold: once more with %v", bound, 2*bound)
	}
}

// deployBounded runs eng.Deploy with opts and fails the test when it does
// not end within boundWatch past the bound of opts.
func deployBounded(t *testing.T, eng *Engine, opts DeployOptions) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := boundWatch
	if !opts.Until.IsZero() {
		watch += time.Until(opts.Until)
	}
	got := make(chan error, 1)
	go func() { got <- eng.Deploy(ctx, opts) }()
	select {
	case err := <-got:
		return err
	case <-time.After(watch):
		cancel()
		<-got
		t.Fatalf("deploy did not end at its bound (D69): it waits past it")
		return nil
	}
}

// The bound covers the whole deploy, not only the waits that check it: an
// SSH connect, an upload or a command that hangs ends with the bound as
// InProgress of the step it was in («1.7 — релиз» on #769: the bound of the
// call must not depend on each step checking it).
func TestDeploy_BoundEndsAHangingStepInProgress(t *testing.T) {
	for _, tc := range []struct{ op, phase string }{
		{"connect", "ssh"},
		{"exec " + dockerReadyCmd, "docker"},
		{"upload", "upload"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			err := boundedUntilHeld(t, func(bound time.Duration, held *atomic.Bool) error {
				dep := newMockDeployer()
				dep.hold = func(ctx context.Context, op string) error {
					if op == tc.op {
						held.Store(true)
						return untilCtx(ctx)
					}
					return nil
				}
				eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
				eng.Timings.DockerWaitTimeout = time.Hour
				opts := newDeployOpts(t, dir)
				opts.Until = time.Now().Add(bound)
				return deployBounded(t, eng, opts)
			})
			var going *InProgress
			if !stderrors.As(err, &going) || going.Phase != tc.phase {
				t.Fatalf("err = %v, want InProgress %s", err, tc.phase)
			}
		})
	}
}

// Docker installed (the stage done) but not ready: the wait between
// attempts ends with the bound too, as InProgress docker. The bound is
// doubled while it comes before the first check of Docker (boundedUntilHeld):
// on Windows the steps before it outlasted 1 s once — «deploy is still in
// progress: vm», run 43391080735 of 13.11, #1645.
func TestDeploy_BoundEndsTheWaitForDockerReadyInProgress(t *testing.T) {
	err := boundedUntilHeld(t, func(bound time.Duration, held *atomic.Bool) error {
		dep := newMockDeployer()
		dep.hold = func(_ context.Context, op string) error {
			if op == "exec "+dockerReadyCmd {
				held.Store(true)
				return stderrors.New("docker: permission denied")
			}
			return nil
		}
		eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
		eng.Timings.DockerPollInterval = time.Hour
		eng.Timings.DockerWaitTimeout = time.Hour
		opts := newDeployOpts(t, dir)
		opts.Until = time.Now().Add(bound)
		return deployBounded(t, eng, opts)
	})
	var going *InProgress
	if !stderrors.As(err, &going) || going.Phase != "docker" {
		t.Fatalf("err = %v, want InProgress docker", err)
	}
}

// The install done but Docker not ready: the next attempt runs the install
// again — a stage done for this input is not taken for the attempt's own
// («1.7 — релиз» on #769: before, the attempts only waited).
func TestDeploy_DockerNotReadyRunsTheInstallAgain(t *testing.T) {
	dep := newMockDeployer()
	ready := 0
	dep.hold = func(_ context.Context, op string) error {
		if op == "exec "+dockerReadyCmd {
			ready++
			if ready == 1 {
				return stderrors.New("docker: permission denied")
			}
		}
		return nil
	}
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatal(err)
	}
	install := bootstrap.InstallCommand("user1")
	runs := 0
	dep.mu.Lock()
	for _, c := range dep.execCalls {
		if c == install {
			runs++
		}
	}
	dep.mu.Unlock()
	if runs != 2 {
		t.Errorf("the install ran %d times, want 2 (once more after Docker was not ready)", runs)
	}
}

// heldStages is a VM whose compose-up stage goes until the test ends it;
// other stages are done at once.
type heldStages struct {
	mu      sync.Mutex
	starts  int
	running bool
	input   string
	others  map[string]string
}

func (h *heldStages) Start(_ context.Context, name, input, _ string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name != "compose-up" {
		if h.others == nil {
			h.others = map[string]string{}
		}
		h.others[name] = input
		return true, nil
	}
	if h.running {
		return false, nil
	}
	h.starts++
	h.running, h.input = true, input
	return true, nil
}

func (h *heldStages) Read(_ context.Context, name string) (vmstage.Stage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name != "compose-up" {
		if in, ok := h.others[name]; ok {
			return vmstage.Stage{State: "done", Input: in}, nil
		}
		return vmstage.Stage{State: "none"}, nil
	}
	switch {
	case h.running:
		return vmstage.Stage{State: "running", Input: h.input}, nil
	case h.input != "":
		return vmstage.Stage{State: "done", Input: h.input}, nil
	}
	return vmstage.Stage{State: "none"}, nil
}

func (h *heldStages) finish() {
	h.mu.Lock()
	h.running = false
	h.mu.Unlock()
}

// --force runs compose up again once: the repeat of the call (without
// force — the tool's next_args) takes the stage it started, done, and does
// not start another («1.7 — релиз» on #769: a nonce of the time in the
// input made each repeat a new input, and deploy never ended).
func TestDeploy_ForceRunsComposeUpOnceAcrossRepeats(t *testing.T) {
	stages := &heldStages{}
	eng, dir := newTestEngine(t, &mockCloudAPI{}, newMockDeployer())
	eng.Stages = stages
	call := func(force bool, bound time.Duration) error {
		opts := newDeployOpts(t, dir)
		opts.Force = force
		opts.Until = time.Now().Add(bound)
		return deployBounded(t, eng, opts)
	}
	var going *InProgress
	if err := call(false, testBound); !stderrors.As(err, &going) || going.Phase != "compose-up" {
		t.Fatalf("first: %v, want InProgress compose-up", err)
	}
	stages.finish()
	if err := call(true, testBound); !stderrors.As(err, &going) || going.Phase != "compose-up" {
		t.Fatalf("--force: %v, want compose up started again", err)
	}
	stages.finish()
	// The repeat ends the deploy: its bound is not the point of the test.
	if err := call(false, finishBound); err != nil {
		t.Fatalf("the repeat after --force: %v", err)
	}
	if stages.starts != 2 {
		t.Errorf("compose up started %d times, want 2: the repeat of a forced deploy started it again", stages.starts)
	}
}

// A deploy whose bound came while the VM was created leaves it in the state
// as creating; the repeat waits for the VM to be active — within its own
// bound — before it gives it a floating IP («1.7 — релиз» on #769: the
// reuse path attached the floating IP to a VM still being created).
func TestDeploy_RepeatWaitsForTheCreatingVMBeforeTheFloatingIP(t *testing.T) {
	cloud := &mockCloudAPI{}
	var mu sync.Mutex
	active, fipBeforeActive := false, false
	polls := 0
	cloud.getVMFn = func(_ context.Context, id string) (*provision.VM, error) {
		mu.Lock()
		defer mu.Unlock()
		polls++
		if polls > 3 {
			active = true
			return &provision.VM{ID: id, Status: "active"}, nil
		}
		return &provision.VM{ID: id, Status: "creating"}, nil
	}
	cloud.createFloatingIPFn = func(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		mu.Lock()
		defer mu.Unlock()
		if !active {
			fipBeforeActive = true
		}
		return &provision.FloatingIP{ID: "fip-new", Name: req.Name, IPAddress: "203.0.113.9", State: "in_use"}, nil
	}
	e, dir, _ := qaBEngine(t, cloud)
	writeRecord(t, dir, state.DeploymentRecord{ID: "vm-new", VMID: "vm-new", ProjectID: e.Config.ProjectID, Region: "az-1", SSHUser: "user1", Status: "creating"})
	opts := newDeployOpts(t, dir)
	if err := e.Deploy(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if fipBeforeActive {
		t.Error("the floating IP was made for a VM still being created")
	}

	// Within the bound: still creating at the bound is InProgress vm.
	cloud.getVMFn = func(_ context.Context, id string) (*provision.VM, error) {
		return &provision.VM{ID: id, Status: "creating"}, nil
	}
	e2, dir2, _ := qaBEngine(t, cloud)
	e2.Timings.VMPollTimeout = time.Hour
	writeRecord(t, dir2, state.DeploymentRecord{ID: "vm-new", VMID: "vm-new", ProjectID: e2.Config.ProjectID, Region: "az-1", SSHUser: "user1", Status: "creating"})
	opts = newDeployOpts(t, dir2)
	opts.Until = time.Now().Add(testBound)
	err := deployBounded(t, e2, opts)
	var going *InProgress
	if !stderrors.As(err, &going) || going.Phase != "vm" || going.Detail != "creating" {
		t.Fatalf("err = %v, want InProgress vm creating", err)
	}
}

// A permanent SSH refusal — the key refused, the host key changed — that
// comes once the bound has come is the refusal, not a deploy going on: a
// repeat answers the same («1.7 — релиз», check of #769, B3).
func TestDeploy_BoundDoesNotHideAPermanentSSHRefusal(t *testing.T) {
	for _, code := range []string{clerrors.CodeSSHAuthFailed, clerrors.CodeSSHHostKeyChanged} {
		t.Run(code, func(t *testing.T) {
			err := boundedUntilHeld(t, func(bound time.Duration, held *atomic.Bool) error {
				dep := newMockDeployer()
				var mu sync.Mutex
				connects := 0
				dep.hold = func(ctx context.Context, op string) error {
					if op != "connect" {
						return nil
					}
					mu.Lock()
					connects++
					n := connects
					mu.Unlock()
					if n < 2 {
						return nil
					}
					// The reconnect after the install: refused once the bound has come.
					held.Store(true)
					<-ctx.Done()
					return clerrors.New(code, "refused", "")
				}
				eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
				opts := newDeployOpts(t, dir)
				opts.Until = time.Now().Add(bound)
				return deployBounded(t, eng, opts)
			})
			if clerrors.Reason(err) != code {
				t.Fatalf("err = %v, want %s", err, code)
			}
		})
	}
}

// A stage is not started in the last seconds of the bound: the end of the
// call could cut the start between its marker and its process, and the
// stage would read lost («1.7 — релиз», check of #769, B1).
func TestDeploy_NoStageStartsInTheLastSecondsOfTheBound(t *testing.T) {
	stages := &heldStages{}
	dep := newMockDeployer()
	until := time.Now().Add(testBound)
	// SSH answers with 10 ms of the bound left: less than its margin
	// (testBound / 9).
	dep.hold = func(_ context.Context, op string) error {
		if op == "connect" {
			<-time.After(time.Until(until) - 10*time.Millisecond)
		}
		return nil
	}
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	eng.Stages = stages
	opts := newDeployOpts(t, dir)
	opts.Until = until
	err := deployBounded(t, eng, opts)
	var going *InProgress
	if !stderrors.As(err, &going) {
		t.Fatalf("err = %v, want InProgress", err)
	}
	stages.mu.Lock()
	defer stages.mu.Unlock()
	if len(stages.others) != 0 || stages.starts != 0 {
		t.Errorf("a stage started in the margin of the bound: others %v, compose-up starts %d", stages.others, stages.starts)
	}
}
