// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// closeCall runs a tool of the compute world and decodes its answer.
func closeCall(t *testing.T, r *Registry, tool string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	text, isErr := r.handlers[tool](context.Background(), args)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("%s: %s", tool, text)
	}
	return m, isErr
}

// openRules are the ingress rules of the world's group with a port.
func openRules(c *faultCloud) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, r := range c.rules {
		if r.Direction == "ingress" && r.PortRange != "" {
			out = append(out, r.IPProtocol+" "+r.PortRange+" "+r.RemoteIPPrefix)
		}
	}
	return out
}

// G13-9 (Grok, wave 2): open_ports had no way back — the rule it opened on
// the shared group of the zone stayed until someone deleted it by hand.
// close_ports deletes it: the preview names the rule and the group and
// says the group is shared; the call deletes it; a repeat is already
// deleted (D26), with no question.
func TestClosePorts_ClosesWhatOpenPortsOpened(t *testing.T) {
	cloud := &faultCloud{}
	r, _ := faultToolsRegistry(t, cloud)
	if m, isErr := closeCall(t, r, "open_ports", map[string]any{"security_group": "SSH-access_ru.AZ-1", "ports": []any{float64(8080)}, "confirmed": true}); isErr {
		t.Fatalf("open_ports: %v", m)
	}
	args := map[string]any{"security_group": "SSH-access_ru.AZ-1", "ports": []any{float64(8080)}}

	m, isErr := closeCall(t, r, "close_ports", args)
	msg, _ := m["message"].(string)
	if !isErr || m["status"] != "confirmation_required" || !strings.Contains(msg, "SSH-access_ru.AZ-1") || !strings.Contains(msg, "every VM") {
		t.Fatalf("close_ports preview: %v", m)
	}
	if rules, _ := m["rules"].([]any); len(rules) != 1 || !strings.Contains(toJSON(rules[0]), "8080") || !strings.Contains(toJSON(rules[0]), "0.0.0.0/0") {
		t.Errorf("close_ports preview names no rule 8080 from 0.0.0.0/0: %v", m)
	}
	if got := openRules(cloud); len(got) != 1 {
		t.Fatalf("the preview changed the rules: %v", got)
	}

	args["confirmed"] = true
	m, isErr = closeCall(t, r, "close_ports", args)
	if closed, _ := m["closed"].([]any); isErr || m["status"] != "ok" || len(closed) != 1 {
		t.Fatalf("close_ports: %v", m)
	}
	if got := openRules(cloud); len(got) != 0 {
		t.Errorf("close_ports left the rule: %v", got)
	}

	for _, confirmed := range []bool{false, true} {
		args["confirmed"] = confirmed
		m, isErr = closeCall(t, r, "close_ports", args)
		if isErr || m["status"] != "already_deleted" || m["existed"] != false {
			t.Errorf("close_ports again confirmed=%v: %v", confirmed, m)
		}
	}
}

// Only the rule of the port, protocol and source given goes: a udp rule
// of the port and a tcp rule from another source stay.
func TestClosePorts_OnlyTheRuleNamed(t *testing.T) {
	cloud := &faultCloud{}
	for _, rule := range []provision.SGRule{
		{Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", RemoteIPPrefix: "0.0.0.0/0"},
		{Direction: "ingress", IPProtocol: "udp", PortRange: "8080:8080", RemoteIPPrefix: "0.0.0.0/0"},
		{Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", RemoteIPPrefix: "10.0.0.0/8"},
	} {
		rule.ID = faultID(0x70 + len(cloud.rules))
		cloud.rules = append(cloud.rules, rule)
	}
	r, _ := faultToolsRegistry(t, cloud)
	m, isErr := closeCall(t, r, "close_ports", map[string]any{"security_group": faultSG, "ports": []any{float64(8080)}, "confirmed": true})
	if isErr || m["status"] != "ok" {
		t.Fatalf("close_ports: %v", m)
	}
	got := openRules(cloud)
	if len(got) != 2 || strings.Contains(strings.Join(got, ","), "tcp 8080:8080 0.0.0.0/0") {
		t.Errorf("rules left: %v, want the udp and the 10.0.0.0/8 one", got)
	}
}

// A group that is not there is the call's error before the question, not
// a repeat.
func TestClosePorts_GroupNotThere(t *testing.T) {
	r, _ := faultToolsRegistry(t, &faultCloud{})
	for _, confirmed := range []bool{false, true} {
		m, isErr := closeCall(t, r, "close_ports", map[string]any{"security_group": "no-such-group", "ports": []any{float64(8080)}, "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeNotFound {
			t.Errorf("close_ports of a group that is not there, confirmed=%v: %v", confirmed, m)
		}
	}
}
