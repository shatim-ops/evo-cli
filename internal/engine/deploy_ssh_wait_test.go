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
