// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
)

// Every *_operation of the registry answers through operation, with the
// reads of its resource declared in operationReads; each read is a tool of
// the registry that takes the id in the argument declared (axis 2).
func TestOperation_EveryOperationDeclared(t *testing.T) {
	reg := New(nil)
	seen := map[string]bool{}
	for _, tl := range reg.Tools() {
		if !strings.HasSuffix(tl.Name, "_operation") {
			continue
		}
		seen[tl.Name] = true
		if len(operationReads[tl.Name]) == 0 {
			t.Errorf("%s: no read of its resource in operationReads — the answer would not say what the resource is now", tl.Name)
		}
	}
	for name, reads := range operationReads {
		if !seen[name] {
			t.Errorf("operationReads names %s, which the registry does not have: remove the line", name)
		}
		for _, r := range reads {
			rt, ok := reg.Lookup(r.tool)
			if !ok {
				t.Errorf("%s reads its resource with %s, which the registry does not have", name, r.tool)
				continue
			}
			props, _ := rt.InputSchema["properties"].(map[string]any)
			if _, ok := props[r.arg]; !ok {
				t.Errorf("%s reads its resource with %s(%s), which has no such argument", name, r.tool, r.arg)
			}
		}
	}
}

// An operation answers how long it has run and the status of its resource,
// whatever its service: elapsed_sec from its own times (done: to its last
// update; running: to now), resource_status from the read of the resource,
// "not_found" when no read knows it (Ц-29, G130-28).
func TestOperation_ElapsedAndResourceStatus(t *testing.T) {
	now := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	old := clock.Now
	clock.Now = func() time.Time { return now }
	t.Cleanup(func() { clock.Now = old })
	op := func(done bool) string {
		return `{"id":"` + contractID + `","done":` + map[bool]string{true: "true", false: "false"}[done] +
			`,"resource_id":"` + contractID + `","resourceId":"` + contractID + `",` +
			`"created_at":"2026-10-30T06:50:00Z","createdAt":"2026-10-30T06:50:00Z",` +
			`"updated_at":"2026-10-30T06:52:30Z","updatedAt":"2026-10-30T06:52:30Z","status":"ACTIVE"}`
	}
	rt := &contractRT{kind: "200"}
	reg := contractReg(t, rt)
	names := make([]string, 0, len(operationReads))
	for name := range operationReads {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, c := range []struct {
		done    bool
		elapsed float64
	}{{true, 150}, {false, 600}} {
		rt.body = op(c.done)
		for _, name := range names {
			tl, _ := reg.Lookup(name)
			text, isErr, _ := contractCall(tl, map[string]any{"operation_id": contractID}, contractCallLimit)
			var m map[string]any
			if isErr || json.Unmarshal([]byte(text), &m) != nil {
				t.Errorf("%s: %s", name, text)
				continue
			}
			if got, _ := m["elapsed_sec"].(float64); got != c.elapsed {
				t.Errorf("%s done=%v: elapsed_sec %v, want %v", name, c.done, m["elapsed_sec"], c.elapsed)
			}
			if m["resource_status"] != "ACTIVE" {
				t.Errorf("%s: resource_status %v, want the status its read gives: %s", name, m["resource_status"], text)
			}
		}
	}
}
