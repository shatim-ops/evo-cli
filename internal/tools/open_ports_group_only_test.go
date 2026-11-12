// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A port a rule lets in only from another security group is not open to
// the source open_ports was asked for. open_ports adds no rule for it — as
// before (owner's decision 12.11, variant «б»: the access is not changed)
// — and does not call it «already open»: it is named apart, with the
// groups, and the answer's next step is sg_rule_create. The status and the
// exit code are as they were.

func groupOnlyWorld() *sgMakeWorld {
	w := newSGMakeWorld()
	w.rules[sgOtherGroup] = append(w.rules[sgOtherGroup], provision.SGRule{ID: sgRuleNone, Direction: "ingress", EtherType: "IPv4",
		IPProtocol: "tcp", PortRange: "8080:8080", RemoteSecurityGroup: &provision.SGRuleGroup{ID: sgZoneGroup, Name: "SSH-access_ru.AZ-1"}, State: "created"})
	return w
}

func TestOpenPorts_APortOnlyAGroupLetsInIsNotCalledOpen(t *testing.T) {
	w := groupOnlyWorld()
	r := sgMakeRegistry(t, w)
	args := map[string]any{"security_group": sgOtherGroup, "ports": []any{float64(8080), float64(9090)}, "cidr": "10.0.0.0/8"}
	m, text, _ := callNamed(t, r, "open_ports", args)
	only, _ := json.Marshal(m["open_only_to_groups"])
	if m["status"] != "confirmation_required" || !strings.Contains(string(only), `"port":"8080"`) || !strings.Contains(string(only), "SSH-access_ru.AZ-1") {
		t.Fatalf("the preview does not name 8080 as let in only from the group: %s", text)
	}
	if open, _ := json.Marshal(m["already_open"]); strings.Contains(string(open), "8080") {
		t.Errorf("the preview calls 8080 already open: %s", text)
	}
	if will, _ := json.Marshal(m["will_open"]); string(will) != `["9090"]` || m["next_tool"] != nil {
		t.Errorf("will_open = %s, next_tool %v — want only 9090 and no other step in a preview", will, m["next_tool"])
	}
	args["confirmed"] = true
	m, text, isErr := callNamed(t, r, "open_ports", args)
	if isErr || m["status"] != "ok" || w.added() != 1 || w.adds[0].PortRange != "9090:9090" {
		t.Fatalf("with consent: %s (writes %d) — want one rule, for 9090: the access to 8080 is not changed", text, w.added())
	}
	if open, _ := json.Marshal(m["already_open"]); strings.Contains(string(open), "8080") {
		t.Errorf("the answer calls 8080 already open: %s", text)
	}
	next, _ := m["next_args"].(map[string]any)
	if m["open_only_to_groups"] == nil || m["next_tool"] != "sg_rule_create" || next["ports"] != "8080" || next["cidr"] != "10.0.0.0/8" || next["confirmed"] != nil {
		t.Errorf("the answer does not lead to sg_rule_create for 8080, without a consent in the step: %s", text)
	}
	// Only such a port: nothing is added, the status is ok as it was, and
	// the preview does not say «already open».
	w = groupOnlyWorld()
	r = sgMakeRegistry(t, w)
	one := map[string]any{"security_group": sgOtherGroup, "ports": []any{float64(8080)}, "cidr": "10.0.0.0/8"}
	m, text, _ = callNamed(t, r, "open_ports", one)
	if msg, _ := m["message"].(string); m["status"] != "confirmation_required" || strings.Contains(msg, "already open") || !strings.Contains(msg, "None of the ports") {
		t.Errorf("the preview of a port only a group lets in: %s", text)
	}
	one["confirmed"] = true
	if m, text, isErr := callNamed(t, r, "open_ports", one); isErr || m["status"] != "ok" || w.added() != 0 {
		t.Errorf("with consent: %s (writes %d), want ok and no rule, as before", text, w.added())
	}
	// A rule with an address source next to it: already open, as before.
	w = groupOnlyWorld()
	w.rules[sgOtherGroup] = append(w.rules[sgOtherGroup], provision.SGRule{ID: sgRuleSSH, Direction: "ingress", EtherType: "IPv4",
		IPProtocol: "tcp", PortRange: "8080:8080", RemoteIPPrefix: "10.0.0.0/8", State: "created"})
	m, text, _ = callNamed(t, sgMakeRegistry(t, w), "open_ports", map[string]any{"security_group": sgOtherGroup, "ports": []any{float64(8080)}, "cidr": "10.0.0.0/8"})
	if open, _ := json.Marshal(m["already_open"]); string(open) != `["8080"]` || m["open_only_to_groups"] != nil {
		t.Errorf("an address rule lets 8080 in: %s", text)
	}
}
