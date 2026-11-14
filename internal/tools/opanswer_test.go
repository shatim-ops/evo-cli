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

// FB160-4 (audit of Fable 1.6): a done operation without the time of its
// end did not stop: elapsed_sec counted to now at every read. Such an
// answer has no elapsed_sec, and says why (elapsed_note); so does an
// operation without the time of its creation, which had neither.
func TestOperation_ElapsedOnlyWhereTheTimesAre(t *testing.T) {
	for _, tc := range []struct {
		name     string
		m        map[string]any
		wantSec  bool
		wantNote bool
	}{
		{"done with both times", map[string]any{"done": true, "created_at": "2026-11-03T10:00:00Z", "updated_at": "2026-11-03T10:01:30Z"}, true, false},
		{"running", map[string]any{"done": false, "createdAt": "2026-11-03T10:00:00Z"}, true, false},
		{"done without its end", map[string]any{"done": true, "created_at": "2026-11-03T10:00:00Z"}, false, true},
		{"without its creation", map[string]any{"done": false}, false, true},
	} {
		addElapsed(tc.m)
		_, sec := tc.m["elapsed_sec"]
		_, note := tc.m["elapsed_note"]
		if sec != tc.wantSec || note != tc.wantNote {
			t.Errorf("%s: elapsed_sec %v (want %v), elapsed_note %v (want %v): %v", tc.name, sec, tc.wantSec, note, tc.wantNote, tc.m)
		}
	}
	m := map[string]any{"done": true, "created_at": "2026-11-03T10:00:00Z", "updated_at": "2026-11-03T10:01:30Z"}
	if addElapsed(m); m["elapsed_sec"] != 90 {
		t.Errorf("done: elapsed_sec %v, want 90", m["elapsed_sec"])
	}
}

// operationErrorUnmarked are the *_operation tools shipped before the
// invariant whose answer carries the error of the operation, as the
// service words it, with no "_untrusted" marker. It only shrinks, and a new
// *_operation does not join it. Empty since 1.9 (release plan 1.9, «Текст
// ошибки операции — недоверенный у каждого *_operation», R-83).
var operationErrorUnmarked = map[string]string{}

// The error of an operation is the service's text, with the names of the
// project's resources in it — anyone with access to the project writes
// those. Every tool that reads an operation (operationReads) declares the
// field untrusted, so that its answer carries the marker and its
// description the note («Риск · 1.9», заключение 115, R-83: only the new
// k8s_operation declared it).
func TestOperation_ErrorIsUntrusted(t *testing.T) {
	for name := range operationReads {
		marked := false
		for _, f := range decls[name].Untrusted.fields {
			if f == "error" {
				marked = true
			}
		}
		_, pending := operationErrorUnmarked[name]
		switch {
		case !marked && !pending:
			t.Errorf("%s answers the error of an operation as the service words it, and Decl.Untrusted does not declare its field error: add Untrusted: untrustedAnswer{fields: []string{\"error\"}}", name)
		case marked && pending:
			t.Errorf("operationErrorUnmarked names %s, whose error is declared untrusted now: remove the line (the list only shrinks)", name)
		}
	}
	for name := range operationErrorUnmarked {
		if _, ok := operationReads[name]; !ok {
			t.Errorf("operationErrorUnmarked names %s, which reads no operation (operationReads): remove the line", name)
		}
	}
}
