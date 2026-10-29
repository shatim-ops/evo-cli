// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const settleVM = "00000000-0000-4000-8000-0000000000f1"

// transientDiskCloud is the fault world with one VM and a disk in state
// from until the read number settleAt (0: never), then in_use.
type transientDiskCloud struct {
	*faultCloud
	mu       sync.Mutex
	from     string
	settleAt int
	reads    int
	writes   []string
}

func (c *transientDiskCloud) state() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.settleAt > 0 && c.reads >= c.settleAt {
		return "in_use"
	}
	return c.from
}

func (c *transientDiskCloud) GetDisk(context.Context, string) (map[string]any, error) {
	return map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": c.state()}, nil
}

func (c *transientDiskCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	return []map[string]any{{"id": faultDisk, "name": "data", "size": float64(20), "state": c.state()}}, nil
}

func (c *transientDiskCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: settleVM, Name: "web"}}, nil
}

func (c *transientDiskCloud) wrote(op string) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, op)
	return map[string]any{"id": faultDisk}, nil
}

func (c *transientDiskCloud) AttachDisk(context.Context, string, string, string) (map[string]any, error) {
	return c.wrote("AttachDisk")
}

func (c *transientDiskCloud) DetachDisk(context.Context, string, string, string) (map[string]any, error) {
	return c.wrote("DetachDisk")
}

func (c *transientDiskCloud) ResizeDisk(context.Context, string, int, string) (map[string]any, error) {
	return c.wrote("ResizeDisk")
}

func transientDiskRegistry(c *transientDiskCloud) *Registry {
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   c,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
			Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: 50 * time.Millisecond},
		}, nil
	})
}

var diskChanges = map[string]map[string]any{
	"disk_attach": {"disk_id": faultDisk, "vm": "web"},
	"disk_detach": {"disk_id": faultDisk, "vm": "web"},
	"disk_resize": {"disk_id": faultDisk, "size_gb": float64(40)},
}

func withConfirmed(args map[string]any, confirmed bool) map[string]any {
	out := map[string]any{"confirmed": confirmed}
	for k, v := range args {
		out[k] = v
	}
	return out
}

// D53 (#521, п. 2; cloud-e2e of the 1.5 candidate, run 142): disk_detach a
// second after disk_resize got 422 disk_can_not_be_detached_from_current_state
// after consent. A disk in a transient state that does not settle within
// the wait is refused before the question, naming the state, and nothing
// is written.
func TestDiskChange_TransientStateRefusedBeforeTheQuestion(t *testing.T) {
	for name, args := range diskChanges {
		for _, st := range []string{"attaching", "detaching", "extending", "creating"} {
			for _, confirmed := range []bool{false, true} {
				c := &transientDiskCloud{faultCloud: &faultCloud{}, from: st}
				m, text, isErr := callNamed(t, transientDiskRegistry(c), name, withConfirmed(args, confirmed))
				if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, st) || len(c.writes) > 0 {
					t.Errorf("%s, disk %s, confirmed=%v: %s (writes %v)", name, st, confirmed, text, c.writes)
				}
			}
		}
	}
}

// D53, option Б: a disk that settles within the wait goes on as a settled
// one — the preview without consent, the change with it.
func TestDiskChange_WaitsForATransientStateToSettle(t *testing.T) {
	for name, args := range diskChanges {
		c := &transientDiskCloud{faultCloud: &faultCloud{}, from: "extending", settleAt: 3}
		if m, text, _ := callNamed(t, transientDiskRegistry(c), name, withConfirmed(args, false)); m["status"] != "confirmation_required" {
			t.Errorf("%s without consent: %s", name, text)
		}
		c = &transientDiskCloud{faultCloud: &faultCloud{}, from: "extending", settleAt: 3}
		if _, text, isErr := callNamed(t, transientDiskRegistry(c), name, withConfirmed(args, true)); isErr || len(c.writes) != 1 {
			t.Errorf("%s with consent: %s (writes %v)", name, text, c.writes)
		}
	}
}

// The wait says the disk's state while it waits (CLAUDE.md §6, axis 2).
func TestDiskChange_SaysTheStateWhileWaiting(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	ctx := WithProgress(context.Background(), func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() })
	c := &transientDiskCloud{faultCloud: &faultCloud{}, from: "extending", settleAt: 3}
	tl, _ := transientDiskRegistry(c).Lookup("disk_detach")
	if text, isErr := tl.Handler(ctx, withConfirmed(diskChanges["disk_detach"], true)); isErr {
		t.Fatal(text)
	}
	mu.Lock()
	defer mu.Unlock()
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "extending") || !strings.Contains(joined, faultDisk) {
		t.Errorf("the wait must say the disk and its state: %q", joined)
	}
}

// The owner's answer to D53 (#542, 29.10): the wait before the question
// is 30 s — under the call timeout of MCP clients.
func TestDiskChange_SettleWaitIsTheDecided(t *testing.T) {
	if diskSettleTimeout != 30*time.Second {
		t.Errorf("diskSettleTimeout = %s, the decision D53 is 30 s", diskSettleTimeout)
	}
}
