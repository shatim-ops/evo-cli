// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

func TestPortsAndPrefix_AsTheAPITakesThem(t *testing.T) {
	for in, want := range map[string]string{"443": "443:443", "8000:8100": "8000:8100", " 22 ": "22:22",
		"0": "", "65536": "", "90:80": "", "ssh": "", "": "", "1:2:3": ""} {
		if got, ok := Ports(in); got != want || ok != (want != "") {
			t.Errorf("Ports(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
	for in, want := range map[string][2]string{
		"203.0.113.7/24":    {"203.0.113.0/24", "IPv4"},
		"203.0.113.7":       {"203.0.113.7/32", "IPv4"},
		"2001:db8::1/48":    {"2001:db8::/48", "IPv6"},
		"::ffff:10.0.0.1":   {"10.0.0.1/32", "IPv4"},
		"::ffff:0.0.0.0/96": {"0.0.0.0/0", "IPv4"},
		"::ffff:0.0.0.0/8":  {"", ""},
		"10.0.0.0/33":       {"", ""},
		"web":               {"", ""},
	} {
		if p, e, ok := Prefix(in); p != want[0] || e != want[1] || ok != (want[0] != "") {
			t.Errorf("Prefix(%q) = %q, %q, %v, want %v", in, p, e, ok, want)
		}
	}
}

// Same is the rule field by field; Wider is an active rule that lets
// through all of it and more. A rule that names no source is neither: what
// the platform answers for «from anywhere» is not verified.
func TestSameAndWider(t *testing.T) {
	want := provision.SGRule{Direction: "ingress", IPProtocol: "tcp", PortRange: "443:443", RemoteIPPrefix: "203.0.113.0/24"}
	rule := func(change func(*provision.SGRule)) *provision.SGRule {
		r := want
		r.State = "created"
		if change != nil {
			change(&r)
		}
		return &r
	}
	for name, c := range map[string]struct {
		r           *provision.SGRule
		same, wider bool
	}{
		"the rule itself": {rule(nil), true, false},
		"written another way": {rule(func(r *provision.SGRule) {
			r.PortRange, r.IPProtocol, r.RemoteIPPrefix = "443", "TCP", "203.0.113.9/24"
		}), true, false},
		"egress":            {rule(func(r *provision.SGRule) { r.Direction = "egress" }), false, false},
		"udp":               {rule(func(r *provision.SGRule) { r.IPProtocol = "udp" }), false, false},
		"a range around":    {rule(func(r *provision.SGRule) { r.PortRange = "1:1024" }), false, true},
		"any protocol":      {rule(func(r *provision.SGRule) { r.IPProtocol = "any" }), false, true},
		"a wider source":    {rule(func(r *provision.SGRule) { r.RemoteIPPrefix = "0.0.0.0/0" }), false, true},
		"a narrower source": {rule(func(r *provision.SGRule) { r.RemoteIPPrefix = "203.0.113.0/25" }), false, false},
		"another family":    {rule(func(r *provision.SGRule) { r.RemoteIPPrefix = "::/0" }), false, false},
		"no source":         {rule(func(r *provision.SGRule) { r.RemoteIPPrefix = "" }), false, false},
		"from a group": {rule(func(r *provision.SGRule) {
			r.RemoteIPPrefix, r.RemoteSecurityGroup = "", &provision.SGRuleGroup{ID: "g"}
		}), false, false},
		"wider, being deleted":  {rule(func(r *provision.SGRule) { r.PortRange, r.State = "1:1024", "deleting" }), false, false},
		"the same, in an error": {rule(func(r *provision.SGRule) { r.State = "error_creating" }), true, false},
	} {
		if same, wider := Same(c.r, want), Wider(c.r, want); same != c.same || wider != c.wider {
			t.Errorf("%s: same %v, wider %v, want %v, %v", name, same, wider, c.same, c.wider)
		}
	}
	// A source group is the same only as that group.
	fromGroup := provision.SGRule{Direction: "ingress", IPProtocol: "tcp", PortRange: "5432:5432", RemoteSecurityGroupID: "g"}
	have := &provision.SGRule{Direction: "ingress", IPProtocol: "tcp", PortRange: "5432:5432", RemoteSecurityGroup: &provision.SGRuleGroup{ID: "g"}}
	other := &provision.SGRule{Direction: "ingress", IPProtocol: "tcp", PortRange: "5432:5432", RemoteSecurityGroup: &provision.SGRuleGroup{ID: "h"}}
	anywhere := &provision.SGRule{Direction: "ingress", IPProtocol: "tcp", PortRange: "5432:5432", RemoteIPPrefix: "0.0.0.0/0"}
	if !Same(have, fromGroup) || Same(other, fromGroup) || Wider(other, fromGroup) || Same(anywhere, fromGroup) || Wider(anywhere, fromGroup) {
		t.Errorf("a source group: same %v, other %v/%v, anywhere %v/%v", Same(have, fromGroup), Same(other, fromGroup), Wider(other, fromGroup), Same(anywhere, fromGroup), Wider(anywhere, fromGroup))
	}
}
