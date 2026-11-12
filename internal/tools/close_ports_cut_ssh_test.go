// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// close_ports deletes rules as sg_rule_delete does, by port instead of by
// id: the last rule that lets TCP 22 in to the group of a deployment of
// this folder was refused by the one and closed by the other on the next
// call (D105). One decision for both (sgCutsSSH): refused before the
// question without allow_cut_ssh, for the whole call.

var sshDeployment = state.DeploymentRecord{ID: faultID(0x11), VMID: faultID(0x11), VMName: "web", VMIP: "203.0.113.9",
	SecurityGroupID: sgZoneGroup, SecurityGroupName: "SSH-access_ru.AZ-1", Status: "running"}

func TestClosePorts_TheLastSSHRuleOfADeploymentIsRefused(t *testing.T) {
	// 22 alone, and 22 among other ports: the call is refused whole.
	for _, ports := range [][]any{{22}, {443, 22}} {
		for _, confirmed := range []bool{false, true} {
			w := newSGWorld()
			r, dir := sgRuleRegistry(t, w)
			writeRecords(t, dir, sshDeployment)
			m, text, isErr := callNamed(t, r, "close_ports", map[string]any{"security_group": sgZoneGroup, "ports": ports, "confirmed": confirmed})
			if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "allow_cut_ssh") || w.deleted() != 0 {
				t.Fatalf("ports %v, confirmed=%v: %s (deletes %d), want the refusal of the last SSH rule and no delete", ports, confirmed, text, w.deleted())
			}
		}
	}
	// With the word: the question, and the preview says SSH is cut.
	w := newSGWorld()
	r, dir := sgRuleRegistry(t, w)
	writeRecords(t, dir, sshDeployment)
	args := map[string]any{"security_group": sgZoneGroup, "ports": []any{22}, "allow_cut_ssh": true}
	m, text, _ := callNamed(t, r, "close_ports", args)
	if m["status"] != "confirmation_required" || m["cuts_ssh"] != true || w.deleted() != 0 {
		t.Errorf("the last SSH rule with allow_cut_ssh: %s, want the question that says SSH is cut", text)
	}
	args["confirmed"] = true
	if m, text, isErr := callNamed(t, r, "close_ports", args); isErr || m["status"] != "ok" || w.deleted() != 1 {
		t.Errorf("the last SSH rule with allow_cut_ssh and the yes: %s (deletes %d)", text, w.deleted())
	}
}

// A rule from a group, one still being created and one being deleted are
// not «another rule that lets SSH in»; a rule that lets addresses in is.
func TestClosePorts_WhatCountsAsAnotherSSHRule(t *testing.T) {
	other := func(change func(*provision.SGRule)) provision.SGRule {
		r := provision.SGRule{ID: sgRuleOther, Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "1:1024",
			RemoteIPPrefix: "10.0.0.0/8", State: "created"}
		if change != nil {
			change(&r)
		}
		return r
	}
	for name, c := range map[string]struct {
		rule    provision.SGRule
		refused bool
	}{
		"an address rule over 22": {other(nil), false},
		"22 from a group": {other(func(r *provision.SGRule) {
			r.RemoteIPPrefix, r.RemoteSecurityGroup = "", &provision.SGRuleGroup{ID: sgOtherGroup, Name: "db"}
		}), true},
		"22 still being created": {other(func(r *provision.SGRule) { r.State = "creating" }), true},
		"22 being deleted":       {other(func(r *provision.SGRule) { r.State = "deleting" }), true},
	} {
		w := newSGWorld()
		w.stuck = true // a rule in "deleting" stays listed
		w.rules[sgZoneGroup] = append(w.rules[sgZoneGroup], c.rule)
		r, dir := sgRuleRegistry(t, w)
		writeRecords(t, dir, sshDeployment)
		m, text, isErr := callNamed(t, r, "close_ports", map[string]any{"security_group": sgZoneGroup, "ports": []any{22}})
		if refused := isErr && m["reason"] == clerrors.CodeUsage; refused != c.refused {
			t.Errorf("%s as the other rule: refused %v, want %v: %s", name, refused, c.refused, text)
		}
		// The same decision at sg_rule_delete.
		m, text, isErr = callNamed(t, r, "sg_rule_delete", map[string]any{"security_group": sgZoneGroup, "rule_id": sgRuleSSH})
		if refused := isErr && m["reason"] == clerrors.CodeUsage; refused != c.refused {
			t.Errorf("%s as the other rule, sg_rule_delete: refused %v, want %v: %s", name, refused, c.refused, text)
		}
	}
}

// No deployment of this folder in the group, a port that is not 22, udp 22:
// close_ports asks as it did.
func TestClosePorts_NoRefusalWhereSSHIsNotCut(t *testing.T) {
	w := newSGWorld()
	r, _ := sgRuleRegistry(t, w)
	if m, text, _ := callNamed(t, r, "close_ports", map[string]any{"security_group": sgZoneGroup, "ports": []any{22}}); m["status"] != "confirmation_required" || m["cuts_ssh"] != nil {
		t.Errorf("22 of a group with no deployment of this folder: %s", text)
	}
	w = newSGWorld()
	w.rules[sgZoneGroup] = append(w.rules[sgZoneGroup], provision.SGRule{ID: sgRuleOther, Direction: "ingress", EtherType: "IPv4",
		IPProtocol: "udp", PortRange: "22:22", RemoteIPPrefix: "0.0.0.0/0", State: "created"})
	r, dir := sgRuleRegistry(t, w)
	writeRecords(t, dir, sshDeployment)
	for name, args := range map[string]map[string]any{
		"443":    {"security_group": sgZoneGroup, "ports": []any{443}},
		"udp 22": {"security_group": sgZoneGroup, "ports": []any{22}, "protocol": "udp"},
	} {
		if m, text, _ := callNamed(t, r, "close_ports", args); m["status"] != "confirmation_required" || m["cuts_ssh"] != nil {
			t.Errorf("%s with a deployment in the group: %s", name, text)
		}
	}
}
