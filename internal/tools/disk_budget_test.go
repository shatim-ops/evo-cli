// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func diskAnswer(t *testing.T, call func() (string, bool)) map[string]any {
	t.Helper()
	text, isErr := call()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil || isErr {
		t.Fatalf("answer (isError=%v): %s", isErr, text)
	}
	return m
}

// disk_create with attach_to whose disk is still being created at the limit
// of an MCP call (D69): not an error — the disk, its state, and disk_attach
// to finish with the consent this call was given.
func TestDiskCreate_AttachAtTheLimitGoesOnWithDiskAttach(t *testing.T) {
	r := budgeted(budgetAPIRegistry(t, &faultAPI{slow: true}))
	m := diskAnswer(t, func() (string, bool) {
		return r.handlers["disk_create"](context.Background(),
			map[string]any{"name": "data", "size_gb": 10, "attach_to": faultTargetVM, "confirmed": true})
	})
	next, _ := m["next_args"].(map[string]any)
	id, _ := m["disk_id"].(string)
	if m["status"] != "creating" || id == "" || m["next_tool"] != "disk_attach" ||
		next["disk_id"] != id || next["vm"] != faultTargetVM || next["confirmed"] != true {
		t.Errorf("disk_create at the limit: %v", m)
	}
}

// disk_attach whose disk does not settle by the limit asks nothing yet: the
// disk's state and the same call again (D53, D69).
func TestDiskAttach_UnsettledAtTheLimitAsksNothingYet(t *testing.T) {
	a := &faultAPI{slow: true, made: []faultResource{{host: "compute.api.cloud.ru", path: "/api/v1/disks", ID: faultDisk, Name: "data", before: true}}}
	r := budgeted(budgetAPIRegistry(t, a))
	args := map[string]any{"disk_id": faultDisk, "vm": faultTargetVM}
	m := diskAnswer(t, func() (string, bool) { return r.handlers["disk_attach"](context.Background(), args) })
	if m["status"] != "creating" || m["next_tool"] != "disk_attach" {
		t.Errorf("disk_attach at the limit: %v", m)
	}
	for _, op := range a.callLog() {
		if strings.HasSuffix(op, "/attach") {
			t.Errorf("attached without consent: %v", a.callLog())
		}
	}
}

// After the attach disk_create answers the disk as it is then, not as the
// create answered it (creating; C2, Ц-14): the disk is read after the
// attach.
func TestDiskCreate_ReadsTheDiskAfterTheAttach(t *testing.T) {
	a := &faultAPI{}
	r := budgetAPIRegistry(t, a)
	m := diskAnswer(t, func() (string, bool) {
		return r.handlers["disk_create"](context.Background(),
			map[string]any{"name": "data", "size_gb": 10, "attach_to": faultTargetVM, "confirmed": true})
	})
	id, _ := m["disk_id"].(string)
	log := a.callLog()
	attached := -1
	for i, op := range log {
		if strings.HasSuffix(op, "/api/v1/disks/"+id+"/attach") {
			attached = i
		}
	}
	read := false
	for _, op := range log[attached+1:] {
		if op == "GET compute.api.cloud.ru/api/v1/disks/"+id {
			read = true
		}
	}
	if attached < 0 || !read {
		t.Errorf("the disk is not read after the attach: %v", log)
	}
}
