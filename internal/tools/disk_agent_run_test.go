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

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// agentDiskVM is the VM of agentDiskCloud.
var agentDiskVM = faultID(0x51)

// agentDiskCloud is the fault world with one VM, web, and one disk, data,
// whose GetDisk answer is disk; it counts the writes to the disk.
type agentDiskCloud struct {
	*faultCloud
	disk   map[string]any
	mu     sync.Mutex
	writes []string
}

func (c *agentDiskCloud) GetDisk(context.Context, string) (map[string]any, error) {
	return c.disk, nil
}

func (c *agentDiskCloud) wrote(op string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, op)
}

func (c *agentDiskCloud) AttachDisk(context.Context, string, string, string) (map[string]any, error) {
	c.wrote("attach")
	return map[string]any{}, nil
}

func (c *agentDiskCloud) DetachDisk(context.Context, string, string, string) (map[string]any, error) {
	c.wrote("detach")
	return map[string]any{}, nil
}

func (c *agentDiskCloud) ResizeDisk(_ context.Context, _ string, size int, _ string) (map[string]any, error) {
	c.wrote("resize")
	d := map[string]any{}
	for k, v := range c.disk {
		d[k] = v
	}
	d["size"] = float64(size)
	return d, nil
}

func agentDiskWorld(t *testing.T, disk map[string]any) (*Registry, *agentDiskCloud) {
	t.Helper()
	c := &agentDiskCloud{faultCloud: &faultCloud{vms: []faultVM{{ID: agentDiskVM, Name: "web", Status: "running"}}}, disk: disk}
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   c,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
	r.SetStateDir(t.TempDir())
	return r, c
}

// Agent run of 1.7.0 (5 of 9): the previews of disk_attach and disk_detach
// were «disk_attach changes a VM.» — no disk, no VM, though the plan has
// read both. Axis 4: the preview names what the call changes, so the
// person can check the target before yes.
func TestDiskAttachDetachPreview_NamesDiskAndVM(t *testing.T) {
	for tool, state := range map[string]string{"disk_attach": "available", "disk_detach": "in_use"} {
		disk := map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": state}
		if tool == "disk_detach" {
			disk["vms"] = []any{map[string]any{"id": agentDiskVM, "name": "web", "primary": false}}
		}
		r, c := agentDiskWorld(t, disk)
		m, text, _ := callNamed(t, r, tool, map[string]any{"disk_id": faultDisk, "vm": "web"})
		if m["status"] != "confirmation_required" {
			t.Errorf("%s: %s", tool, text)
			continue
		}
		for _, w := range []string{faultDisk, "data", agentDiskVM, "web"} {
			if !strings.Contains(text, w) {
				t.Errorf("%s: the preview does not name %q: %s", tool, w, text)
			}
		}
		if len(c.writes) != 0 {
			t.Errorf("%s: written before consent: %v", tool, c.writes)
		}
	}
}

// Agent run of 1.7.0 (c0, m1): after disk_resize the note always said
// «growpart /dev/vda 2 && resize2fs /dev/vda2» — the boot disk's first
// device and its second partition, wrong for a data disk, which is another
// device and often has no partition at all.
func TestDiskResize_NoteDoesNotNameTheBootDeviceForADataDisk(t *testing.T) {
	r, c := agentDiskWorld(t, map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "available", "bootable": false})
	m, text, isErr := callNamed(t, r, "disk_resize", map[string]any{"disk_id": faultDisk, "size_gb": float64(40), "confirmed": true})
	if isErr || len(c.writes) != 1 {
		t.Fatalf("disk_resize: %s (writes %v)", text, c.writes)
	}
	if note, _ := m["note"].(string); strings.Contains(note, "/dev/vda") {
		t.Errorf("the note of a data disk names the boot device: %q", note)
	}
}

// «Риск · 1.8», R-34 (#896): the VM of disk_detach's preview is the
// argument, not a fact; the preview names the VMs the disk is attached to
// as the cloud says. A disk on another VM is refused before the question
// since D81's second half (TestDiskAttachDetach_AnotherVMsDiskRefusedBeforeTheQuestion).
func TestDiskDetachPreview_NamesTheVMsTheDiskIsAttachedTo(t *testing.T) {
	r, _ := agentDiskWorld(t, map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "in_use",
		"vms": []any{map[string]any{"id": agentDiskVM, "name": "web", "primary": false}}})
	m, text, _ := callNamed(t, r, "disk_detach", map[string]any{"disk_id": faultDisk, "vm": "web"})
	target, _ := m["will_detach"].(map[string]any)
	vms, _ := target["attached_vms"].([]any)
	if len(vms) != 1 {
		t.Fatalf("will_detach does not name the VMs the disk is attached to: %s", text)
	}
	if vm, _ := vms[0].(map[string]any); vm["id"] != agentDiskVM || vm["name"] != "web" {
		t.Errorf("attached_vms %v, want VM web (%s)", vms, agentDiskVM)
	}
}

// Agent run of 1.7.0 (5 of 9): disk_attach of a disk already attached to
// the same VM asked, and after yes Cloud.ru answered 422 «Disk can not be
// attached from current state». D81 (#897, variant A): the disk's VMs are
// in the GetDisk answer the plan reads before the question — the same VM
// is a repeat (D26, as fip_attach): already_attached, no question, no
// call. A disk of another VM is left to the platform until the probe of
// shared disks (release plan, D81).
func TestDiskAttach_AlreadyAttachedToThisVMIsARepeat(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, c := agentDiskWorld(t, map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "in_use",
			"vms": []any{map[string]any{"id": agentDiskVM, "name": "web", "primary": false}}})
		m, text, isErr := callNamed(t, r, "disk_attach", map[string]any{"disk_id": faultDisk, "vm": "web", "confirmed": confirmed})
		if isErr || m["status"] != "already_attached" || m["disk_id"] != faultDisk || m["vm_id"] != agentDiskVM {
			t.Errorf("confirmed=%v: want already_attached of the disk to VM %s, no question: %s", confirmed, agentDiskVM, text)
		}
		if len(c.writes) != 0 {
			t.Errorf("confirmed=%v: written %v", confirmed, c.writes)
		}
	}
}

// D81, the condition of «Риск · 1.8» (conclusion 9): one rule for attach
// and detach — disk_detach of a disk no VM has (available) is a repeat:
// already_detached, no question, no call.
func TestDiskDetach_ADiskNoVMHasIsARepeat(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, c := agentDiskWorld(t, map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "available"})
		m, text, isErr := callNamed(t, r, "disk_detach", map[string]any{"disk_id": faultDisk, "vm": "web", "confirmed": confirmed})
		if isErr || m["status"] != "already_detached" || m["disk_id"] != faultDisk || m["vm_id"] != agentDiskVM {
			t.Errorf("confirmed=%v: want already_detached, no question: %s", confirmed, text)
		}
		if len(c.writes) != 0 {
			t.Errorf("confirmed=%v: written %v", confirmed, c.writes)
		}
	}
}
