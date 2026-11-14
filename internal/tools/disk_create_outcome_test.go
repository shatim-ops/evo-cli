// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// disk_create without attach_to answers created only for a disk the
// platform made available; one still being made is creating, with its
// state and disk_list of that disk as the next step — not created over a
// disk creating (Астра #1814; release plan 1.9, «Честный ход операций»,
// (1)). disk_list disk_id shows that one disk.
func TestDiskCreate_ADiskStillBeingMadeIsNotCreated(t *testing.T) {
	a := &faultAPI{slow: true}
	r := budgetAPIRegistry(t, a)
	text, isErr := r.handlers["disk_create"](context.Background(), map[string]any{
		"name": "data", "size_gb": 10, "zone": "ru.AZ-1", "confirmed": true,
	})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	next, _ := m["next_args"].(map[string]any)
	id, _ := m["disk_id"].(string)
	if isErr || m["status"] != "creating" || m["disk_state"] != "creating" || id == "" ||
		m["next_tool"] != "disk_list" || next["disk_id"] != id {
		t.Fatalf("disk_create of a disk the platform still makes: %s", text)
	}

	text, isErr = r.handlers["disk_list"](context.Background(), map[string]any{"disk_id": id})
	var rows []map[string]any
	if err := json.Unmarshal([]byte(text), &rows); err != nil {
		var wrapped map[string]any
		_ = json.Unmarshal([]byte(text), &wrapped)
		b, _ := json.Marshal(wrapped["disks"])
		_ = json.Unmarshal(b, &rows)
	}
	if isErr || len(rows) != 1 || rows[0]["id"] != id {
		t.Errorf("disk_list disk_id=%s: %s", id, text)
	}
}

// diskCreated keeps the answer of a disk the platform made: created.
func TestDiskCreate_AnAvailableDiskIsCreated(t *testing.T) {
	for _, disk := range []map[string]any{
		{"id": "d-1", "state": "available"},
		{"id": "d-1", "status": "AVAILABLE"},
	} {
		res := diskCreated(map[string]any{"status": "created", "disk": disk}, disk)
		if res["status"] != "created" || res["next_tool"] != nil {
			t.Errorf("disk %v: %v", disk, res)
		}
	}
}
