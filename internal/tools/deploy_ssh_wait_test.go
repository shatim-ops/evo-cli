// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// sshDeadVM is a VM whose SSH never answers (outbound port 22 closed where
// evo runs): every connect times out, and each one moves the fake clock on
// by sshAttemptTakes, as a real connect timeout takes its time.
type sshDeadVM struct {
	*stageVM
	mu       sync.Mutex
	now      time.Time
	attempts int
	// up: the person fixed it (rebooted the VM, opened outbound 22).
	up bool
}

const sshAttemptTakes = 20 * time.Second

// sshSlowNote is the default SSHSlowNote of a deploy (5 minutes).
const sshSlowNote = 5 * time.Minute

func (v *sshDeadVM) Connect(context.Context, string, int, string, []byte, string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.attempts++
	v.now = v.now.Add(sshAttemptTakes)
	if v.up {
		return nil
	}
	return clerrors.New(clerrors.CodeSSHTimeout, "SSH connection timed out", "")
}

func (v *sshDeadVM) clock() time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.now
}

// D80: deploy over MCP whose SSH never answers ends, as the CLI does. The
// count of attempts and the start of the wait are the record's, not the
// call's: each call of the budget began both anew, so the limit of attempts
// never came and the wait never reached SSHSlowNote — deploy answered
// deploying for ever while the VM was billed, and the reason (outbound 22,
// nc -vz), which the CLI names after SSHSlowNote, never came over MCP
// (agent run of 1.7.0, findings №2). Past SSHSlowNote of the wait the
// deploying answer names the reason; past the limit of attempts of the
// wait it is SSH_TIMEOUT.
func TestDeployMCP_SSHNeverAnswersEndsOrSaysWhy(t *testing.T) {
	start := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	vm := &sshDeadVM{stageVM: newStageVM(), now: start}
	old := clock.Now
	clock.Now = vm.clock
	t.Cleanup(func() { clock.Now = old })
	// Between attempts an hour: the budget of the call ends its wait, not
	// the attempts running out within one call.
	r := stageRegistry(t, vm.stageVM, worldBudget, func(e *engine.Engine) {
		e.Deployer = vm
		e.Timings.SSHRetryInterval = time.Hour
		e.Timings.SSHSlowNote = sshSlowNote
	})

	const calls = 80 // the limit of attempts of a new VM is 60, a call makes one or more
	said, ended := false, false
	for i := 0; i < calls && !ended; i++ {
		text := deployCall(t, r)
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatalf("call %d: %v: %q", i, err, text)
		}
		switch {
		case m["status"] == "deploying":
			if m["phase"] != "ssh" {
				continue
			}
			w, _ := m["warning"].(string)
			if clock.Since(start) > sshSlowNote+2*sshAttemptTakes &&
				!(strings.Contains(w, "nc -vz") && strings.Contains(w, "22")) {
				t.Fatalf("call %d: SSH has not answered for %v, the answer does not say why (outbound 22, nc -vz): %s",
					i, clock.Since(start), text)
			}
			said = said || w != ""
		case m["reason"] == string(clerrors.CodeSSHTimeout):
			ended = true
			// What next is the person's (R-1, R-23): no next step for the
			// agent to take on its own, and the hint says so.
			if _, ok := m["next_tool"]; ok {
				t.Errorf("SSH_TIMEOUT with next_tool: an agent would loop deploy → 60 attempts → SSH_TIMEOUT: %s", text)
			}
			if h, _ := m["hint"].(string); !strings.Contains(h, "the person decides") || strings.Contains(h, "destroy") {
				t.Errorf("SSH_TIMEOUT hint does not leave what next to the person: %s", text)
			}
		default:
			t.Fatalf("call %d: %s", i, text)
		}
	}
	if !ended {
		t.Fatalf("%d calls of deploy, %d SSH attempts: it still answers deploying, never SSH_TIMEOUT", calls, vm.attempts)
	}
	if !said {
		t.Errorf("no deploying answer named why SSH does not answer")
	}
	if vm.attempts > 61 {
		t.Errorf("%d SSH attempts before SSH_TIMEOUT, the limit of a new VM is 60", vm.attempts)
	}

	// R-3: the person acted (rebooted the VM in the console) and called
	// deploy again: the wait that ended is not the record's any more, so
	// the new deploy tries SSH anew instead of answering SSH_TIMEOUT at once.
	vm.mu.Lock()
	vm.up = true
	before := vm.attempts
	vm.mu.Unlock()
	text := deployCall(t, r)
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	vm.mu.Lock()
	after := vm.attempts
	vm.mu.Unlock()
	if m["reason"] == string(clerrors.CodeSSHTimeout) || m["phase"] == "ssh" || after == before {
		t.Errorf("deploy after SSH_TIMEOUT and the person's fix does not try SSH anew: %s", text)
	}
}
