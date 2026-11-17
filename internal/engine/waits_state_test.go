// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// fipStatesCloud answers the floating IP fip-1 in states, one per GET, the
// last one from then on.
type fipStatesCloud struct {
	*mockCloudAPI
	mu     sync.Mutex
	states []string
}

func (c *fipStatesCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.states[0]
	if len(c.states) > 1 {
		c.states = c.states[1:]
	}
	return &provision.FloatingIP{ID: "fip-1", IPAddress: "203.0.113.7", State: st}, nil
}

// CLAUDE.md §6, axis 2 (release plan 1.5, «Ожидания без хода», #457): the
// waits of the engine say the state of the resource they wait for, once
// per change — not only that time passes. The printer is the CLI's stderr
// and, over MCP, the call's progress (logAndProgress, progressOf).
func TestWaits_SayTheStateOfTheResource(t *testing.T) {
	newEngine := func(cloud provision.CloudAPI) (*Engine, *bytes.Buffer) {
		var buf bytes.Buffer
		return &Engine{Cloud: cloud, Printer: output.NewWithWriter(&buf, output.ModePlain, false), Timings: fastTimings}, &buf
	}
	once := func(t *testing.T, out, what string) {
		t.Helper()
		if n := strings.Count(out, what); n != 1 {
			t.Errorf("%q said %d times, want once per change:\n%s", what, n, out)
		}
	}

	t.Run("VM gone", func(t *testing.T) {
		polls := 0
		cloud := &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
			polls++
			if polls > 2 {
				return nil, clerrors.New(clerrors.CodeVMNotFound, "not found", "")
			}
			return &provision.VM{ID: id, Status: "deleting"}, nil
		}}
		e, buf := newEngine(cloud)
		if !e.waitVMGone(context.Background(), "vm-1") {
			t.Fatal("the VM is gone, the wait says it is not")
		}
		once(t, buf.String(), "VM vm-1: deleting")
	})

	t.Run("boot FIP settles", func(t *testing.T) {
		e, buf := newEngine(&fipStatesCloud{mockCloudAPI: &mockCloudAPI{}, states: []string{"creating", "available"}})
		if f := e.settledBootFIP(context.Background(), &provision.FloatingIP{ID: "fip-1", IPAddress: "203.0.113.7", State: "creating"}, "p"); f == nil || !f.Available() {
			t.Fatalf("the FIP became available, got %+v", f)
		}
		once(t, buf.String(), "Floating IP 203.0.113.7: creating")
	})

	t.Run("boot FIP attached", func(t *testing.T) {
		e, buf := newEngine(&fipStatesCloud{mockCloudAPI: &mockCloudAPI{}, states: []string{"available", "available", "in_use"}})
		if _, _, err := e.waitBootFIP(context.Background(), &provision.FloatingIP{ID: "fip-1", IPAddress: "203.0.113.7"}, "vm-1", "p"); err != nil {
			t.Fatal(err)
		}
		once(t, buf.String(), "Floating IP 203.0.113.7: available")
	})
}

// dirtyState is a state as the cloud may send it: an escape sequence that
// clears the screen, a carriage return, an OSC 8 link, NUL and a bidi
// override (Астра #2090).
const dirtyState = "creating\x1b[2J\rOVER\x1b]8;;https://example.com\aLINK\x1b]8;;\a\x00‮"

// stepLineDirty names what a terminal must not get in out: a control
// character other than a line break, or a format character.
func stepLineDirty(out string) string {
	for _, r := range out {
		if r == '\n' {
			continue
		}
		if r < 0x20 || r == 0x7f || r == '‮' || r == ' ' || r == ' ' {
			return string(r)
		}
	}
	return ""
}

// The waits of the engine say the cloud's state in their step lines, and
// the step lines go to the terminal, to the "log" of an MCP answer and to
// its progress: cleaned, as the state of every other line is (Астра #2090;
// the one point is the printer).
func TestWaits_TheStateTheyNameIsCleaned(t *testing.T) {
	newEngine := func(cloud provision.CloudAPI) (*Engine, *bytes.Buffer) {
		var buf bytes.Buffer
		return &Engine{Cloud: cloud, Printer: output.NewWithWriter(&buf, output.ModePlain, false), Timings: fastTimings}, &buf
	}
	t.Run("VM gone", func(t *testing.T) {
		polls := 0
		cloud := &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
			polls++
			if polls > 2 {
				return nil, clerrors.New(clerrors.CodeVMNotFound, "not found", "")
			}
			return &provision.VM{ID: id, Status: dirtyState}, nil
		}}
		e, buf := newEngine(cloud)
		e.waitVMGone(context.Background(), "vm-1")
		if c := stepLineDirty(buf.String()); c != "" || !strings.Contains(buf.String(), "OVER") {
			t.Errorf("%q goes out: %q", c, buf.String())
		}
	})
	t.Run("boot FIP settles", func(t *testing.T) {
		e, buf := newEngine(&fipStatesCloud{mockCloudAPI: &mockCloudAPI{}, states: []string{dirtyState, "available"}})
		e.settledBootFIP(context.Background(), &provision.FloatingIP{ID: "fip-1", IPAddress: "203.0.113.7", State: dirtyState}, "p")
		if c := stepLineDirty(buf.String()); c != "" || !strings.Contains(buf.String(), "OVER") {
			t.Errorf("%q goes out: %q", c, buf.String())
		}
	})
}
