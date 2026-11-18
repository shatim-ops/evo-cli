// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The answers of disk_create, disk_attach and disk_detach carry the disk or
// the attachment as Compute answered it — names anyone in the project gave
// included: every field of the answer with the cloud's text in it is named
// in "_untrusted" (A19-6, the release audit of 1.9 rc.1, #2427). A world
// that no longer reaches the field fails the test.
func TestDisk_BodyOfTheCloudMarked(t *testing.T) {
	faultToolsIsolate(t)
	const id = "00000000-0000-4000-8000-0000000000f1"
	free := hostileBodyIn("available", "available", map[string]any{"vms": []any{}})
	// The disk of disk_detach is attached to the VM of the call.
	attached := hostileBodyIn("in-use", "in-use", map[string]any{"vms": []any{map[string]any{"id": id, "name": hostileName}}})
	for tool, c := range map[string]struct {
		body  []byte
		args  map[string]any
		field string
	}{
		"disk_create": {free, map[string]any{"name": "data", "size_gb": 10, "zone": id, "confirmed": true}, "disk"},
		"disk_attach": {free, map[string]any{"disk_id": id, "vm": id, "confirmed": true}, "result"},
		"disk_detach": {attached, map[string]any{"disk_id": id, "vm": id, "confirmed": true}, "result"},
	} {
		api := &oddAPI{status: http.StatusOK, body: c.body}
		cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
		cloud.SetToken("t")
		cloud.Backoff = 1
		r := raRegistry(t, api, cloud, nil)
		r.SetCallBudget(containerBudget)
		_, text, _ := callNamed(t, r, tool, c.args)
		var m map[string]any
		if json.Unmarshal([]byte(text), &m) != nil {
			t.Fatalf("%s: %s", tool, text)
		}
		b, _ := json.Marshal(m[c.field])
		if !strings.Contains(string(b), hostileMarker) {
			t.Errorf("%s: the world no longer reaches %s of the cloud: %.400s", tool, c.field, text)
			continue
		}
		list, _ := m["_untrusted"].([]any)
		for f, v := range m {
			b, _ := json.Marshal(v)
			if f != "_untrusted" && strings.Contains(string(b), hostileMarker) && !slices.Contains(list, any(f)) {
				t.Errorf("%s: %s carries the cloud's text, \"_untrusted\" %v does not name it", tool, f, list)
			}
		}
	}
}
