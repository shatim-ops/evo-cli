// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// hostileIPCloud is pollCheckCloud whose floating IP is not an address:
// the cloud's text.
type hostileIPCloud struct{ pollCheckCloud }

func (*hostileIPCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: hostileName, State: "in_use"}, nil
}

// The answer of provision carries what the cloud read: the address of the
// VM and the ssh command with it — fields "_untrusted" names; the message,
// evo's text, carries a value of the cloud only in its form (an IP) or
// quoted, and offers no command made of a value that is not an address
// (#2477, Qwen's review of #2475).
func TestProvision_ValuesOfTheCloudMarkedOrQuoted(t *testing.T) {
	cloud := &hostileIPCloud{}
	r, dir := fastRegistry(t, cloud, provisionCfg)
	cloud.dir = dir
	m, text, _ := callNamed(t, r, "provision", map[string]any{"name": "web", "confirmed": true})
	if !strings.Contains(text, hostileMarker) {
		t.Fatalf("the world no longer brings the address of the cloud to provision: %.500s", text)
	}
	list, _ := m["_untrusted"].([]any)
	for f, v := range m {
		b, _ := json.Marshal(v)
		if f == "_untrusted" || !strings.Contains(string(b), hostileMarker) {
			continue
		}
		if f == "message" {
			msg, _ := v.(string)
			if strings.ContainsAny(msg, "\n\r") || !strings.Contains(msg, quoteCloud(hostileName)) || strings.Contains(msg, "SSH: ssh") {
				t.Errorf("the message carries the cloud's address unquoted, or a command made of it: %q", msg)
			}
			continue
		}
		if !slices.Contains(list, any(f)) {
			t.Errorf("provision: %s carries the cloud's text, \"_untrusted\" %v does not name it", f, list)
		}
	}
}
