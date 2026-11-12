// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The world of the power tools (D99, docs/internal/briefs/ops-wave-1.md §2)
// is the fake's state machine of a VM. These tests keep the world itself:
// a tool tested on a world that takes any command in any state proves
// nothing about repeats and refusals.

// powerVM is a world with one VM in state.
func powerVM(t *testing.T, state string) (*faultCloud, string) {
	t.Helper()
	c := &faultCloud{}
	vm, err := c.CreateVM(context.Background(), provision.CreateVMRequest{Name: "vm-power"})
	if err != nil {
		t.Fatal(err)
	}
	c.setVMState(vm.ID, state)
	return c, vm.ID
}

func vmState(t *testing.T, c *faultCloud, id string) string {
	t.Helper()
	vm, err := c.GetVM(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return vm.Status
}

// A command taken puts the VM in the transient state and leaves it there
// until the test steps it: the end of a transition is the test's decision,
// not a clock's.
func TestFaultWorld_PowerTransitions(t *testing.T) {
	for _, tc := range []struct{ command, from, via, to string }{
		{"power_on", "stopped", "starting", "running"},
		{"power_off", "running", "stopping", "stopped"},
		{"reboot", "running", "rebooting", "running"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			c, id := powerVM(t, tc.from)
			if err := c.SetVMPower(context.Background(), id, tc.command); err != nil {
				t.Fatalf("%s from %s: %v", tc.command, tc.from, err)
			}
			if got := vmState(t, c, id); got != tc.via {
				t.Fatalf("after %s the VM is %q, want the transient %q", tc.command, got, tc.via)
			}
			if got := vmState(t, c, id); got != tc.via {
				t.Fatalf("a read moved the VM to %q: only stepVMPower ends a transition", got)
			}
			if !c.stepVMPower(id) {
				t.Fatal("stepVMPower: the VM was not in a transition")
			}
			if got := vmState(t, c, id); got != tc.to {
				t.Fatalf("after the step the VM is %q, want %q", got, tc.to)
			}
			if c.stepVMPower(id) {
				t.Fatal("stepVMPower moved a VM that was in no transition")
			}
			if n := c.powerCalls(); n != 1 {
				t.Fatalf("powerCalls = %d, want 1", n)
			}
		})
	}
}

// A command the state does not allow is the platform's 422 and changes
// nothing; a VM that is not there is the same 422 with another code — the
// specification declares no 404 for the power request.
func TestFaultWorld_PowerRefusals(t *testing.T) {
	for _, tc := range []struct{ command, state string }{
		{"power_on", "running"}, {"power_on", "starting"}, {"power_on", "stopping"},
		{"power_on", "creating"}, {"power_on", "deleting"}, {"power_on", "error"},
		{"power_off", "stopped"}, {"power_off", "stopping"}, {"power_off", "starting"},
		{"reboot", "stopped"}, {"reboot", "rebooting"},
	} {
		t.Run(tc.command+" in "+tc.state, func(t *testing.T) {
			c, id := powerVM(t, tc.state)
			err := c.SetVMPower(context.Background(), id, tc.command)
			if provision.HTTPStatus(err) != 422 {
				t.Fatalf("%s in %s: err = %v, want HTTP 422", tc.command, tc.state, err)
			}
			if got := vmState(t, c, id); got != tc.state {
				t.Fatalf("a refused command moved the VM from %q to %q", tc.state, got)
			}
		})
	}
	t.Run("no such VM", func(t *testing.T) {
		c := &faultCloud{}
		err := c.SetVMPower(context.Background(), faultID(0x7f), "power_on")
		if provision.HTTPStatus(err) != 422 {
			t.Fatalf("err = %v, want HTTP 422", err)
		}
	})
	t.Run("unknown command", func(t *testing.T) {
		c, id := powerVM(t, "stopped")
		if err := c.SetVMPower(context.Background(), id, "suspend"); provision.HTTPStatus(err) != 422 {
			t.Fatalf("err = %v, want HTTP 422", err)
		}
	})
}

// The other answer a platform may give to a repeat: success with nothing
// changed. A reboot is never a repeat — each one is a change.
func TestFaultWorld_PowerRepeatAccepted(t *testing.T) {
	for _, tc := range []struct{ command, state string }{
		{"power_on", "running"}, {"power_on", "starting"},
		{"power_off", "stopped"}, {"power_off", "stopping"},
	} {
		c, id := powerVM(t, tc.state)
		c.powerRepeatAccepted = true
		if err := c.SetVMPower(context.Background(), id, tc.command); err != nil {
			t.Fatalf("%s in %s: %v", tc.command, tc.state, err)
		}
		if got := vmState(t, c, id); got != tc.state {
			t.Fatalf("an accepted repeat moved the VM from %q to %q", tc.state, got)
		}
	}
	c, id := powerVM(t, "rebooting")
	c.powerRepeatAccepted = true
	if err := c.SetVMPower(context.Background(), id, "reboot"); provision.HTTPStatus(err) != 422 {
		t.Fatalf("reboot of a rebooting VM: err = %v, want HTTP 422", err)
	}
}

// A lost answer is a command the platform took; any other fault is a
// command it did not. A transition can end in a state that is not the
// command's target.
func TestFaultWorld_PowerFaults(t *testing.T) {
	t.Run("lost answer: taken", func(t *testing.T) {
		c, id := powerVM(t, "stopped")
		c.failNth("SetVMPower", 1, faultLost)
		if err := c.SetVMPower(context.Background(), id, "power_on"); err == nil {
			t.Fatal("a lost answer returned no error")
		}
		if got := vmState(t, c, id); got != "starting" {
			t.Fatalf("after a lost answer the VM is %q, want starting", got)
		}
	})
	for _, kind := range []string{faultTransport, faultHTTP500, faultAuth} {
		t.Run(kind+": not taken", func(t *testing.T) {
			c, id := powerVM(t, "stopped")
			c.failNth("SetVMPower", 1, kind)
			if err := c.SetVMPower(context.Background(), id, "power_on"); err == nil {
				t.Fatal("the fault returned no error")
			}
			if got := vmState(t, c, id); got != "stopped" {
				t.Fatalf("after %s the VM is %q, want stopped", kind, got)
			}
		})
	}
	t.Run("transition ends in error", func(t *testing.T) {
		c, id := powerVM(t, "stopped")
		if err := c.SetVMPower(context.Background(), id, "power_on"); err != nil {
			t.Fatal(err)
		}
		c.stepVMPower(id, "error")
		if got := vmState(t, c, id); got != "error" {
			t.Fatalf("the VM is %q, want error", got)
		}
	})
}
