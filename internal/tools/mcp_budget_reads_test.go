// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// readsPastBudget is how long past its budget a call of these tests may
// answer: the answer itself, not a read of the cloud.
// wall-clock: a margin for a call that ends at its budget on a loaded runner.
const readsPastBudget = 2 * time.Second

// hangingReadsCloud is the fault world whose reads of a VM hang until
// their context ends, once the VM is created: a cloud that answers slowly.
type hangingReadsCloud struct {
	*faultCloud
	created atomic.Bool
}

func (c *hangingReadsCloud) CreateVM(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	vm, err := c.faultCloud.CreateVM(ctx, req)
	c.created.Store(true)
	return vm, err
}

func (c *hangingReadsCloud) GetVM(ctx context.Context, id string) (*provision.VM, error) {
	if c.created.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return c.faultCloud.GetVM(ctx, id)
}

// Agent run of 1.7.0 (m2, h1; findings 2.1, №5): provision over MCP took
// more than 60 s — after its budget it read the VM once more with no bound,
// and so did each tick of its wait. Every read of the cloud after the VM
// is created ends with the budget of the call (D69, H14): a cloud that
// does not answer gets the VM as it is, creating, within the budget.
func TestProvisionMCP_AnswersWithinBudgetWhenReadsAreSlow(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &hangingReadsCloud{faultCloud: &faultCloud{slow: true}}
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: faultProject, Region: faultZone, ImageID: faultImage,
				Flavor: "low-1-1", DiskSize: 10, KeyID: "k", Secret: "s"},
			Auth: staticToken{}, Cloud: cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("fake-key"), SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings: engine.Timings{VMPollInterval: 10 * time.Millisecond},
		}, nil
	})
	r.SetStateDir(dir)
	r.SetCallBudget(worldBudget)
	m, text := callWithin(t, func(ctx context.Context) (string, bool) {
		return r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true})
	}, worldBudget+readsPastBudget)
	if !cloud.created.Load() {
		t.Fatalf("the VM was not created: %s", text)
	}
	if m["status"] != "creating" {
		t.Errorf("the answer at the budget is not the VM as it is (creating): %s", text)
	}
}

// Agent run of 1.7.0 (x; findings 2.1, №5): kafka_operation wait_seconds=45
// was cut by the client at 60 s — the last read of the operation, at the
// end of the budget, had no bound. A read of the operation ends with the
// budget of the call too, and the answer is the operation as last read.
func TestOperationWait_AnswersWithinBudgetWhenReadsAreSlow(t *testing.T) {
	r := New(nil)
	r.SetCallBudget(worldBudget)
	var reads atomic.Int32
	fetch := func(ctx context.Context) (map[string]any, error) {
		if reads.Add(1) == 1 {
			return map[string]any{"id": "op-1", "done": false}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := time.Now()
	ctx, cancel := context.WithCancel(r.withCallStart(context.Background()))
	defer cancel()
	type res struct {
		m   map[string]any
		err error
	}
	got := make(chan res, 1)
	go func() {
		m, err := waitForOpJSON(ctx, int(mcpBudgetSeconds), fetch)
		got <- res{m, err}
	}()
	select {
	case g := <-got:
		if g.err != nil || g.m["done"] != false {
			t.Errorf("the answer at the budget: %v, %v; want the operation as last read, done: false", g.m, g.err)
		}
	case <-time.After(worldBudget + readsPastBudget):
		cancel()
		<-got
		t.Fatalf("the wait for the operation did not answer within its budget (%v): its last read has no bound", time.Since(start).Round(time.Millisecond))
	}
}

// callWithin calls call and fails the test when it does not answer within
// d; the answer decoded.
func callWithin(t *testing.T, call func(context.Context) (string, bool), d time.Duration) (map[string]any, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 1)
	go func() {
		text, _ := call(ctx)
		got <- text
	}()
	select {
	case text := <-got:
		return parseAnswer(t, text), text
	case <-time.After(d):
		cancel()
		<-got
		t.Fatalf("the call did not answer within %v: its reads of the cloud outlive the budget", d)
		return nil, ""
	}
}
