// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// movingDiskCloud is the fault world with the VM web and one disk a test
// changes between two calls; writes are the disk writes sent, with what
// they asked for.
type movingDiskCloud struct {
	*faultCloud
	mu     sync.Mutex
	disk   map[string]any
	writes []string
}

func (c *movingDiskCloud) set(state string, size int, vms ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var list []any
	for _, id := range vms {
		list = append(list, map[string]any{"id": id, "name": "vm-" + id[len(id)-2:]})
	}
	c.disk = map[string]any{"id": faultDisk, "name": "data", "size": float64(size), "state": state, "vms": list}
}

func (c *movingDiskCloud) GetDisk(context.Context, string) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disk, nil
}

func (c *movingDiskCloud) ListDisks(ctx context.Context, _ string) ([]map[string]any, error) {
	d, _ := c.GetDisk(ctx, faultDisk)
	return []map[string]any{d}, nil
}

func (c *movingDiskCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: settleVM, Name: "web"}}, nil
}

func (c *movingDiskCloud) wrote(op string) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, op)
	return map[string]any{"id": faultDisk}, nil
}

func (c *movingDiskCloud) AttachDisk(_ context.Context, _, vm, _ string) (map[string]any, error) {
	return c.wrote("AttachDisk " + vm)
}

func (c *movingDiskCloud) DetachDisk(_ context.Context, _, vm, _ string) (map[string]any, error) {
	return c.wrote("DetachDisk " + vm)
}

func (c *movingDiskCloud) ResizeDisk(_ context.Context, _ string, size int, _ string) (map[string]any, error) {
	return c.wrote(fmt.Sprintf("ResizeDisk %d", size))
}

func movingDiskRegistry(t testing.TB, c *movingDiskCloud, timings engine.Timings) *Registry {
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   c,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
			Timings: timings,
		}, nil
	})
	r.SetStateDir(t.TempDir())
	return r
}

// otherVM is a VM of the project that is not the one the call named.
const otherVM = "00000000-0000-4000-8000-0000000000f2"

// disk_detach and disk_resize keep the consent in the step they name while
// the disk settles (nextStepKeepsConsent: «the detach the person approved»,
// «the size the person approved»; owner's decision 12.11 on R-85). The
// step is sent later, and the disk may be another by then: the step sends
// the write the person approved or none — the disk as it is read again
// decides, not the consent the step carries.
func TestNextStep_DiskStepSendsTheApprovedWriteOrNone(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
		// then is the disk when the step is sent; want are the writes of
		// the two calls.
		name  string
		then  func(c *movingDiskCloud)
		wants []string
	}{
		{"disk_detach", map[string]any{"disk_id": faultDisk, "vm": "web"},
			"settled as it was", func(c *movingDiskCloud) { c.set("in_use", 20, settleVM) }, []string{"DetachDisk " + settleVM}},
		{"disk_detach", map[string]any{"disk_id": faultDisk, "vm": "web"},
			"detached by another caller", func(c *movingDiskCloud) { c.set("available", 20) }, nil},
		{"disk_detach", map[string]any{"disk_id": faultDisk, "vm": "web"},
			"moved to another VM", func(c *movingDiskCloud) { c.set("in_use", 20, otherVM) }, nil},
		{"disk_resize", map[string]any{"disk_id": faultDisk, "size_gb": float64(40)},
			"settled as it was", func(c *movingDiskCloud) { c.set("in_use", 20, settleVM) }, []string{"ResizeDisk 40"}},
		{"disk_resize", map[string]any{"disk_id": faultDisk, "size_gb": float64(40)},
			"grown to the size by another caller", func(c *movingDiskCloud) { c.set("in_use", 40, settleVM) }, nil},
		{"disk_resize", map[string]any{"disk_id": faultDisk, "size_gb": float64(40)},
			"grown past the size by another caller", func(c *movingDiskCloud) { c.set("in_use", 80, settleVM) }, nil},
	} {
		t.Run(tc.tool+": "+tc.name, func(t *testing.T) {
			c := &movingDiskCloud{faultCloud: &faultCloud{}}
			c.set("extending", 20, settleVM)
			// The call the person said yes to: the disk does not settle by
			// the limit of the call.
			r := budgeted(movingDiskRegistry(t, c, engine.Timings{}))
			m, text, isErr := callNamed(t, r, tc.tool, withConfirmed(tc.args, true))
			next, _ := m["next_args"].(map[string]any)
			if isErr || m["next_tool"] != tc.tool || next["confirmed"] != true {
				t.Fatalf("the call cut while the disk settles names %v %v, want %s with confirmed: the world of the test changed\n%s", m["next_tool"], next, tc.tool, text)
			}
			if len(c.writes) != 0 {
				t.Fatalf("written while the disk was settling: %v", c.writes)
			}
			tc.then(c)
			// The step, as the agent sends it: the arguments of the answer.
			r = movingDiskRegistry(t, c, engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: 50 * time.Millisecond})
			_, text, _ = callNamed(t, r, tc.tool, next)
			if fmt.Sprint(c.writes) != fmt.Sprint(tc.wants) {
				t.Errorf("the step sent %v, want %v: a step that keeps the consent sends the write the person approved, or none when the disk is another by then\n%s", c.writes, tc.wants, text)
			}
		})
	}
}
