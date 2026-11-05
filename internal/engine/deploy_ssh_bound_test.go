// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Agent run of 1.7.0 (m0, m1, m2): deploy over MCP whose budget ended in
// the SSH phase answered deploying with next_tool deploy, but wrote the
// record as ssh_timeout; logs and verify then went over SSH and answered
// SSH_TIMEOUT instead of DEPLOY_IN_PROGRESS (D76). A deploy cut by its
// bound while it waits for SSH leaves its record unfinished, whether the
// connect failed before the bound or hung until it.
func TestDeploy_BoundInTheSSHPhaseKeepsTheRecordUnfinished(t *testing.T) {
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

// sshCallBound is the bound of each deploy call in
// TestDeployMCP_SSHNeverAnswersEndsOrSaysWhy.
// wall-clock: the bound each call waits out; a few attempts fit in it.
const sshCallBound = 300 * time.Millisecond

// Agent run of 1.7.0 (m0, m1, m2): with outbound port 22 closed, deploy
// over MCP answered deploying with next_tool deploy on every call, without
// end: the attempts and the time before the note on a closed port 22 began
// anew in each call, a call is 45 s against a note at 5 minutes, and the VM
// is billed all along. The CLI says why after SSHSlowNote and ends with
// SSH_TIMEOUT after its attempts. Repeated bounded calls are one wait: the
// note comes once their wait passes SSHSlowNote, and the attempts run out
// across the calls with SSH_TIMEOUT.
func TestDeployMCP_SSHNeverAnswersEndsOrSaysWhy(t *testing.T) {
	dep := newMockDeployer()
	dep.connectErr = clerrors.New(clerrors.CodeSSHTimeout, "SSH connection timed out", "")
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)
	// A few attempts in each call; the note past two calls' wait.
	eng.Timings.SSHRetryInterval = sshCallBound / 3
	eng.Timings.SSHSlowNote = 2 * sshCallBound

	// Each call makes one attempt at least, and no wait has more than
	// sshMaxRetries: past that many calls the wait has no end.
	var err error
	calls := 0
	for calls < sshMaxRetries {
		calls++
		opts := newDeployOpts(t, dir)
		opts.Until = time.Now().Add(sshCallBound)
		err = deployBounded(t, eng, opts)
		var going *InProgress
		if !stderrors.As(err, &going) {
			break
		}
	}
	if !strings.Contains(out.String(), "nc -vz") {
		t.Errorf("after %d calls (%v of waiting for SSH, the note at %v) deploy did not say why SSH may not answer (outbound port 22)",
			calls, time.Duration(calls)*sshCallBound, eng.Timings.SSHSlowNote)
	}
	if clerrors.Reason(err) != clerrors.CodeSSHTimeout {
		t.Fatalf("after %d calls deploy answers %v: the wait for SSH has no end across calls, want SSH_TIMEOUT once the attempts run out", calls, err)
	}
}
