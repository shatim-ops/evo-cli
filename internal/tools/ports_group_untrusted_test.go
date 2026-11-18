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

// open_ports and close_ports are called with the id of a group and answer
// its name as the cloud has it — what someone in the project named it —
// in the preview of the question and in the answer: "_untrusted" names
// security_group (A19-4, the release audit of 1.9 rc.1, #2425). The calls
// carry ports, so they reach the cloud: a call refused before it checks
// nothing.
func TestPorts_GroupNameOfTheCloudMarked(t *testing.T) {
	faultToolsIsolate(t)
	// Every list answers one item that is both the group and a rule of
	// it, 443/tcp from anywhere: close_ports has a rule to close.
	item := map[string]any{"id": "00000000-0000-4000-8000-0000000000f1", "name": hostileName, "status": "RUNNING", "state": "running",
		"direction": "ingress", "ether_type": "IPv4", "ip_protocol": "tcp", "port_range": "443:443", "remote_ip_prefix": "0.0.0.0/0"}
	api := &oddAPI{status: http.StatusOK, body: hostileBody(map[string]any{"items": []any{item}})}
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	cloud.Backoff = 1
	r := raRegistry(t, api, cloud, nil)
	r.SetCallBudget(containerBudget)
	for _, tool := range []string{"open_ports", "close_ports"} {
		for _, confirmed := range []bool{false, true} {
			args := map[string]any{"security_group": "00000000-0000-4000-8000-0000000000f1", "ports": []any{443}}
			if confirmed {
				args["confirmed"] = true
			}
			_, text, _ := callNamed(t, r, tool, args)
			var m map[string]any
			if json.Unmarshal([]byte(text), &m) != nil {
				t.Fatalf("%s: %s", tool, text)
			}
			b, _ := json.Marshal(m["security_group"])
			if !strings.Contains(string(b), hostileMarker) {
				if !confirmed {
					t.Errorf("%s: the world no longer reaches the name of the group in the preview: %.400s", tool, text)
				}
				continue
			}
			if list, _ := m["_untrusted"].([]any); !slices.Contains(list, any("security_group")) {
				t.Errorf("%s (confirmed %v): security_group is the name of the cloud, \"_untrusted\" %v does not name it", tool, confirmed, list)
			}
		}
	}
}
