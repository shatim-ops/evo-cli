// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// timeoutErr is the error of a connect nobody answered (net.Error with
// Timeout).
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// withDial replaces the check of port 22 for the test: no test connects to
// a network outside its process (#1707).
func withDial(t *testing.T, dial func(context.Context, string, time.Duration) error) {
	t.Helper()
	prev := DialSSH
	DialSSH = dial
	t.Cleanup(func() { DialSSH = prev })
}

// CLAUDE.md §6, axis 2 (#457): the wait for SSH says the state of port 22
// as seen from this machine — refused, no answer — once per change, not
// only that time passes; and the check is the one DialSSH, so the branches
// are decided by the test, not by the network it runs in (#1707).
func TestWaitForSSHPort_SaysThePortState(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want portState
	}{
		{"refused", errors.New("connect: connection refused"), portRefused},
		{"no answer", timeoutErr{}, portSilent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			withDial(t, func(_ context.Context, addr string, _ time.Duration) error {
				calls++
				if addr != "203.0.113.7:22" {
					t.Errorf("dialled %s", addr)
				}
				return tc.err
			})
			var buf bytes.Buffer
			show := &Progress{P: output.NewWithWriter(&buf, output.ModePlain, false)}
			open, seen := waitForSSHPort(context.Background(), "203.0.113.7", 30*time.Millisecond, show)
			if open || seen != tc.want || calls == 0 {
				t.Fatalf("open %v, state %v after %d checks; want closed, %v", open, seen, calls, tc.want)
			}
			if n := strings.Count(buf.String(), "203.0.113.7"); n != 1 {
				t.Errorf("the state of port 22 said %d times, want once:\n%s", n, buf.String())
			}
		})
	}
	withDial(t, func(context.Context, string, time.Duration) error { return nil })
	if open, _ := waitForSSHPort(context.Background(), "203.0.113.7", time.Second, &Progress{}); !open {
		t.Fatal("an accepted connect is an open port")
	}
}

// The note names what the check saw and where from: no answer does not
// claim the VM boots — a rule that drops the packets gives the same — and
// names the security group as the next check; a refusal names sshd (#1707).
func TestSSHUnreachedNote_SaysTheFactNotACause(t *testing.T) {
	silent := sshUnreachedNote("203.0.113.7", 2*time.Minute, portSilent)
	for _, want := range []string{"did not answer", "from this machine", "within 2m0s", "security group", "does not tell which"} {
		if !strings.Contains(silent, want) {
			t.Errorf("no answer: %q lacks %q", silent, want)
		}
	}
	if strings.Contains(silent, "blame") {
		t.Errorf("no answer still rules out security groups: %s", silent)
	}
	if refused := sshUnreachedNote("203.0.113.7", time.Minute, portRefused); !strings.Contains(refused, "refused") || !strings.Contains(refused, "sshd") {
		t.Errorf("refused: %s", refused)
	}
	if none := sshUnreachedNote("203.0.113.7", 0, portUnchecked); !strings.Contains(none, "not checked") {
		t.Errorf("unchecked: %s", none)
	}
}
