// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Exercise the card through a read-only MCP session, CLI JSON and the human
// command, including fields that are absent from the shared parity fixture.
func TestContract_DiskAndVMCard(t *testing.T) {
	w, _ := withJSONWorld(t)
	srv := newROServers(t)
	old := w.rt.extra
	var reads []string
	var diskOverride string
	diskPath := "/api/v1/disks/" + parityAttachedDisk
	vmPath := "/api/v1/vms/" + parityResizeVM
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "compute.api.cloud.ru" {
			reads = append(reads, r.Method+" "+r.URL.Path)
			if r.URL.Path == diskPath {
				if diskOverride != "" {
					return contractRT{status: 200, body: diskOverride}.RoundTrip(r)
				}
				return contractRT{status: 200, body: `{"id":"` + parityAttachedDisk + `","name":"ledger-data","state":"in_use","size":47,"bootable":false,"readonly":true,"shared":false,"serial_id":"SN-qa-482","disk_type":{"name":"fast-ssd"},"availability_zone":{"name":"test-zone"},"vms":[{"id":"` + parityVM + `","name":"worker-attached","primary":false,"state":"stopped"}]}`}.RoundTrip(r)
			}
			if r.URL.Path == "/api/v1/disks/"+parityVM {
				return contractRT{status: 404, body: `{"message":"disk not found"}`}.RoundTrip(r)
			}
		}
		return old(r)
	}
	for _, tc := range []struct {
		group, id, path string
		want            map[string]any
		marks           []string
	}{
		{"disk", parityAttachedDisk, diskPath, map[string]any{"id": parityAttachedDisk, "size_gb": float64(47), "serial_id": "SN-qa-482", "bootable": false, "readonly": true, "shared": false, "zone": "test-zone", "disk_type": "fast-ssd", "name": "ledger-data", "state": "in_use"}, []string{"serial_id", "name", "state", "disk_type", "zone", "attached_vms"}},
		{"vm", parityResizeVM, vmPath, map[string]any{"flavor_id": pvFlavor, "flavor_name": "lowcost10-2-4"}, []string{"flavor_name"}},
	} {
		t.Run(tc.group, func(t *testing.T) {
			checkReads := func() {
				t.Helper()
				count := 0
				for _, r := range reads {
					if r == "GET "+tc.path {
						count++
						continue
					}
					if tc.group == "disk" || !strings.HasPrefix(r, "GET ") || strings.Contains(r, "flavor") {
						t.Errorf("unexpected read for %s: %s", tc.group, r)
					}
				}
				if count != 1 {
					t.Errorf("card reads: %v", reads)
				}
				reads = nil
			}
			reads = nil
			newProcess()
			code, out, stderr := runCLI(t, "", "--json", tc.group, "get", tc.id)
			if code != 0 {
				t.Fatalf("CLI exit %d: %s%s", code, out, stderr)
			}
			checkReads()
			m := parseObject(out)
			a, _ := srv.stdio(t, srv.ro, "tools/call", map[string]any{"name": tc.group + "_get", "arguments": map[string]any{tc.group + "_id": tc.id}})
			if a.isErr || a.rpcErr != "" || !reflect.DeepEqual(m, parseObject(a.text)) {
				t.Fatalf("CLI %s, MCP %s %s", out, a.text, a.rpcErr)
			}
			checkReads()
			for k, want := range tc.want {
				if !reflect.DeepEqual(m[k], want) {
					t.Errorf("%s = %v, want %v", k, m[k], want)
				}
			}
			marks, _ := m["_untrusted"].([]any)
			for _, k := range tc.marks {
				if !slices.Contains(marks, any(k)) {
					t.Errorf("%s missing from _untrusted: %v", k, marks)
				}
			}
			if tc.group == "disk" {
				want := []any{map[string]any{"id": parityVM, "name": "worker-attached", "state": "stopped", "primary": false}}
				if !reflect.DeepEqual(m["attached_vms"], want) {
					t.Errorf("attached_vms: %v", m["attached_vms"])
				}
				missing, _ := m["not_checked"].([]any)
				named := false
				for _, item := range missing {
					row, _ := item.(map[string]any)
					if why, _ := row["why"].(string); row["check"] == "encrypted" && why != "" {
						named = true
					}
				}
				if _, present := m["encrypted"]; present || !named {
					t.Errorf("missing encryption flag: %s", out)
				}
			}
			newProcess()
			code, human, stderr := runCLI(t, "", tc.group, "get", tc.id)
			if code != 0 {
				t.Fatalf("human exit %d: %s%s", code, human, stderr)
			}
			checkReads()
			for _, miss := range valueMisses(m, human, readForm) {
				if tc.group == "vm" && miss.path != "flavor_id" && miss.path != "flavor_name" {
					continue
				}
				if !shownAsWritten(miss.value, human) && !namedBySibling(m, miss.path, miss.value, human) {
					t.Errorf("human output omits %s (%s): %s", miss.path, miss.value, human)
				}
			}
		})
	}
	// Missing membership data cannot establish that a disk is detached.
	t.Run("attachment_evidence", func(t *testing.T) {
		for _, tc := range []struct {
			name, field string
			known       bool
		}{
			{"explicit_empty", `,"vms":[]`, true}, {"omitted", "", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				diskOverride = `{"id":"` + parityAttachedDisk + `","name":"data","state":"available"` + tc.field + `}`
				defer func() { diskOverride = "" }()
				newProcess()
				code, out, stderr := runCLI(t, "", "--json", "disk", "get", parityAttachedDisk)
				if code != 0 {
					t.Fatalf("CLI exit %d: %s", code, stderr)
				}
				m := parseObject(out)
				a, _ := srv.stdio(t, srv.ro, "tools/call", map[string]any{"name": "disk_get", "arguments": map[string]any{"disk_id": parityAttachedDisk}})
				if a.isErr || a.rpcErr != "" || !reflect.DeepEqual(m, parseObject(a.text)) {
					t.Fatalf("CLI %s, MCP %s %s", out, a.text, a.rpcErr)
				}
				attachments, known := m["attached_vms"].([]any)
				missing, _ := m["not_checked"].([]any)
				unknown := false
				for _, item := range missing {
					row, _ := item.(map[string]any)
					if (row["check"] == "attached_vms" || row["check"] == "vms") && row["why"] != nil && row["why"] != "" {
						unknown = true
					}
				}
				if known != tc.known || len(attachments) != 0 || unknown == tc.known {
					t.Errorf("vms %s: attached_vms=%v, not_checked=%v", tc.name, m["attached_vms"], missing)
				}
			})
		}
	})
	t.Run("disk_errors", func(t *testing.T) {
		for _, tc := range []struct {
			id, reason string
			requests   []string
		}{{parityVM, "NOT_FOUND", []string{"GET /api/v1/disks/" + parityVM}}, {"bad-id", "USAGE", nil}} {
			reads = nil
			newProcess()
			code, _, stderr := runCLI(t, "", "--json", "disk", "get", tc.id)
			e, ok := errorEnvelope(stderr)
			if code == 0 || !ok || e.Reason != tc.reason || !slices.Equal(reads, tc.requests) {
				t.Errorf("CLI %s: exit %d, %s, reads %v", tc.id, code, stderr, reads)
			}
			reads = nil
			a, _ := srv.stdio(t, srv.ro, "tools/call", map[string]any{"name": "disk_get", "arguments": map[string]any{"disk_id": tc.id}})
			if !a.isErr || parseObject(a.text)["reason"] != tc.reason || !slices.Equal(reads, tc.requests) {
				t.Errorf("MCP %s: %s %s, reads %v", tc.id, a.text, a.rpcErr, reads)
			}
		}
	})
	t.Run("disk_discovery", func(t *testing.T) {
		for _, args := range [][]string{{"tools", "list", "--group", "disk"}, {"agent", "skill", "--group", "disk"}} {
			newProcess()
			code, out, stderr := runCLI(t, "", args...)
			if code != 0 || !strings.Contains(out, "disk_get") {
				t.Errorf("%v: missing disk_get: %s%s", args, out, stderr)
			}
		}
	})
}
