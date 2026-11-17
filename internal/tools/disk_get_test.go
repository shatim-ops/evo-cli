// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

const diskGetID = "00000000-0000-4000-8000-0000000000d1"

// diskGetCloud is a world whose card of a disk is card; nil is a disk
// that is not there (404).
type diskGetCloud struct {
	*faultCloud
	card map[string]any
}

func (c *diskGetCloud) GetDisk(ctx context.Context, id string) (map[string]any, error) {
	if err := c.gate("GetDisk"); err != nil {
		return nil, err
	}
	if c.card == nil || c.card["id"] != id {
		return nil, clerrors.New(clerrors.CodeNotFound, "disk not found", "")
	}
	return c.card, nil
}

func fullDiskCard() map[string]any {
	return map[string]any{
		"id": diskGetID, "name": "data", "size": float64(20), "state": "in_use", "bootable": false,
		"encrypted": true, "readonly": false, "shared": false, "serial_id": "sn-1",
		"disk_type":         map[string]any{"id": "t-1", "name": "SSD"},
		"availability_zone": map[string]any{"id": faultZone, "name": "ru.AZ-1"},
		"vms":               []any{map[string]any{"id": "00000000-0000-4000-8000-0000000000e1", "name": "web", "primary": false, "state": "running"}},
	}
}

// 110-09: the card of one disk by its id, by one read — the fields that
// tell its work and its safety as the platform reports them («Риск»,
// conclusion 333, Д3, Д4): a field the answer does not carry is absent,
// never false or "", and named in not_checked; false only when the platform
// said false.
func TestDiskGet_TheCardByOneRead(t *testing.T) {
	c := &diskGetCloud{faultCloud: &faultCloud{}, card: fullDiskCard()}
	r, _ := faultToolsRegistryOver(t, c)
	m, text, isErr := callNamed(t, r, "disk_get", map[string]any{"disk_id": diskGetID})
	if isErr {
		t.Fatal(text)
	}
	for k, want := range map[string]any{"id": diskGetID, "name": "data", "size_gb": float64(20), "state": "in_use",
		"bootable": false, "encrypted": true, "readonly": false, "shared": false, "serial_id": "sn-1",
		"disk_type": "SSD", "zone": "ru.AZ-1"} {
		if m[k] != want {
			t.Errorf("%s: %v, want %v (%s)", k, m[k], want, text)
		}
	}
	vms, _ := m["attached_vms"].([]any)
	if len(vms) != 1 || vms[0].(map[string]any)["name"] != "web" || vms[0].(map[string]any)["primary"] != false {
		t.Errorf("attached_vms: %v", m["attached_vms"])
	}
	if m["not_checked"] != nil {
		t.Errorf("a full card names not_checked: %v", m["not_checked"])
	}
	if n := slices.Index(c.callLog(), "GetDisk"); n < 0 || len(c.callLog()) != 1 {
		t.Errorf("reads: %v", c.callLog())
	}

	for _, f := range []string{"encrypted", "readonly", "shared", "serial_id", "bootable"} {
		card := fullDiskCard()
		delete(card, f)
		c := &diskGetCloud{faultCloud: &faultCloud{}, card: card}
		r, _ := faultToolsRegistryOver(t, c)
		m, text, _ := callNamed(t, r, "disk_get", map[string]any{"disk_id": diskGetID})
		if _, has := m[f]; has {
			t.Errorf("%s not in the card, yet in the answer: %s", f, text)
		}
		var named bool
		nc, _ := m["not_checked"].([]any)
		for _, x := range nc {
			if row, _ := x.(map[string]any); row["check"] == f && row["why"] != "" {
				named = true
			}
		}
		if !named {
			t.Errorf("%s not in the card, not named in not_checked: %s", f, text)
		}
	}
}

// A disk that is not there is NOT_FOUND; a disk_id that is no UUID is
// refused by its form, before any read («Риск», conclusion 333, Д5).
func TestDiskGet_NotThereAndATypo(t *testing.T) {
	c := &diskGetCloud{faultCloud: &faultCloud{}}
	r, _ := faultToolsRegistryOver(t, c)
	m, text, isErr := callNamed(t, r, "disk_get", map[string]any{"disk_id": diskGetID})
	if !isErr || m["reason"] != clerrors.CodeNotFound {
		t.Errorf("a disk not there: %s", text)
	}
	c = &diskGetCloud{faultCloud: &faultCloud{}, card: fullDiskCard()}
	r, _ = faultToolsRegistryOver(t, c)
	m, text, isErr = callNamed(t, r, "disk_get", map[string]any{"disk_id": "data"})
	if !isErr || m["reason"] != clerrors.CodeUsage || len(c.callLog()) != 0 {
		t.Errorf("a disk_id that is no UUID: %s %v", text, c.callLog())
	}
}

// vm_get names the flavor of the VM, from the same card, as vm_resize does:
// flat flavor_id and flavor_name («Риск», conclusion 333).
func TestVMGet_NamesTheFlavor(t *testing.T) {
	r, w, _ := vmResizeWorld(t, "running")
	before := len(w.callLog())
	m, text, isErr := callNamed(t, r, "vm_get", map[string]any{"vm_id": w.id})
	if isErr || m["flavor_id"] != faultFlavor || m["flavor_name"] != "low-1-1" {
		t.Errorf("vm_get: %s", text)
	}
	if n := slices.Index(w.callLog()[before:], "ListFlavors"); n >= 0 {
		t.Errorf("vm_get read the catalogue: %v", w.callLog())
	}
}

// A server bound to its projects (--allow-project) reads no disk of
// another project by its id: disk_get is refused as for a disk not there,
// and the card of the other project's disk is not read («Риск», conclusion
// 333, Д1; REL12-3).
func TestDiskGet_BoundServerReadsNoDiskOfAnotherProject(t *testing.T) {
	f := &ownerFake{}
	r := ownerRegistry(t, f, true, ownProject, ownOther)
	m, text, isErr := callOwned(t, r, "disk_get", map[string]any{"disk_id": resForeign}, true)
	if !isErr || m["reason"] != clerrors.CodeNotFound {
		t.Errorf("a disk of another project: %s", text)
	}
	for _, q := range f.log() {
		if strings.HasSuffix(q, "/api/v1/disks/"+resForeign) {
			t.Errorf("the card of the other project's disk was read: %v", f.log())
		}
	}
	f = &ownerFake{}
	r = ownerRegistry(t, f, true, ownProject, ownOther)
	if m, text, isErr := callOwned(t, r, "disk_get", map[string]any{"disk_id": resMine}, true); isErr || m["id"] != resMine {
		t.Errorf("a disk of the allowed project: %s", text)
	}
}
