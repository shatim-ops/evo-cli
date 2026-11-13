// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"sync/atomic"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A deploy whose bound (D69, the budget of an MCP call) ended the wait for
// SSH is in progress: it answers deploying with next_tool deploy, so its
// record stays unfinished and logs and verify answer DEPLOY_IN_PROGRESS
// (D76), not SSH_TIMEOUT — whether the connect failed before the bound or
// hung until it. The record said ssh_timeout, and logs went over SSH and
// answered a timeout the deploy had not reached (agent run of 1.7.0,
// findings №1; the cases are «1.8 — разработка»'s, branch
// dev-1.8/agent-run-1.7.0-candidates).
func TestDeploy_BudgetInSSHPhaseKeepsTheRecordUnfinished(t *testing.T) {
	for _, tc := range []struct {
		name string
		dep  func(held *atomic.Bool) *mockDeployer
	}{
		{"connect fails", func(held *atomic.Bool) *mockDeployer {
			dep := newMockDeployer()
			dep.hold = func(_ context.Context, op string) error {
				if op == "connect" {
					held.Store(true)
					return clerrors.New(clerrors.CodeSSHTimeout, "SSH connection timed out", "")
				}
				return nil
			}
			return dep
		}},
		{"connect hangs", func(held *atomic.Bool) *mockDeployer {
			dep := newMockDeployer()
			dep.hold = func(ctx context.Context, op string) error {
				if op == "connect" {
					held.Store(true)
					return untilCtx(ctx)
				}
				return nil
			}
			return dep
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var eng *Engine
			var dir string
			err := boundedUntilHeld(t, func(bound time.Duration, held *atomic.Bool) error {
				eng, dir = newTestEngine(t, &mockCloudAPI{}, tc.dep(held))
				// Between attempts an hour: the bound ends the wait.
				eng.Timings.SSHRetryInterval = time.Hour
				opts := newDeployOpts(t, dir)
				opts.Until = time.Now().Add(bound)
				return deployBounded(t, eng, opts)
			})
			var going *InProgress
			if !stderrors.As(err, &going) || going.Phase != "ssh" {
				t.Fatalf("err = %v, want InProgress ssh", err)
			}
			rec := readActive(t, dir)
			if rec == nil || !unfinishedStatuses[rec.Status] {
				t.Fatalf("deploy answered in progress, but its record is %+v: not unfinished, so logs and verify go over SSH", rec)
			}
			unfinished(t, "logs", rec.Status, eng.Logs(context.Background(), LogsOptions{Tail: 10}))
			unfinished(t, "verify", rec.Status, eng.Verify(context.Background()))
		})
	}
}

// FB171-1 (delta audit of 1.7.1): a VM this deploy created gets the limit of
// a new VM, 60 attempts, on both surfaces. Over MCP the first call often
// ends at its bound while the VM is still being created, and the next one
// is no new deploy: its record has no host key yet, and it got 30, the
// limit of a VM that may be booting again — the CLI, in one call, has 60.
func TestDeployMCP_NewVMCutInCreationWaitsTheLimitOfANewVM(t *testing.T) {
	var active atomic.Bool
	cloud := &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
		if active.Load() {
			return &provision.VM{ID: id, Status: "active"}, nil
		}
		return &provision.VM{ID: id, Status: "creating"}, nil
	}}
	dep := newMockDeployer()
	dep.connectErr = stderrors.New("ssh: connection timed out")
	eng, dir := newTestEngine(t, cloud, dep)
	eng.Timings.VMPollInterval = time.Millisecond
	eng.Timings.VMPollTimeout = time.Hour
	eng.Timings.SSHRetryInterval = time.Hour

	// The first call: its bound comes while the VM is being created.
	opts := newDeployOpts(t, dir)
	opts.Until = time.Now().Add(testBound)
	var going *InProgress
	if err := deployBounded(t, eng, opts); !stderrors.As(err, &going) || going.Phase != "vm" {
		t.Fatalf("first call: %v, want InProgress vm", err)
	}
	// The next one: the VM is up, SSH does not answer yet. Its bound is
	// doubled while it comes before the first connect (boundedUntilHeld; the
	// class of #1645): the call before SSH changes nothing the test reads.
	active.Store(true)
	err := boundedUntilHeld(t, func(bound time.Duration, held *atomic.Bool) error {
		dep.hold = func(_ context.Context, op string) error {
			if op == "connect" {
				held.Store(true)
			}
			return nil
		}
		opts := newDeployOpts(t, dir)
		opts.Until = time.Now().Add(bound)
		return deployBounded(t, eng, opts)
	})
	if !stderrors.As(err, &going) || going.Phase != "ssh" {
		t.Fatalf("next call: %v, want InProgress ssh", err)
	}
	rec := readActive(t, dir)
	if rec == nil || rec.SSHWait == nil || rec.SSHWait.Limit != sshMaxRetries {
		t.Fatalf("the wait for SSH of a VM this deploy created: %+v, want limit %d as in the CLI", rec, sshMaxRetries)
	}
}
