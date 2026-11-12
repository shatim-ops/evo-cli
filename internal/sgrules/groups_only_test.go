// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A rule whose source is another security group lets in the interfaces of
// that group, not an address. Coverage counts it as «open» (D52), and
// deploy and open_ports add no rule next to it — that stays until the
// major (owner's decision 12.11, variant «б»). What changes is the claim:
// a port only such rules cover is not called open; it is named apart, with
// the groups.

// groupRule is such a rule as the platform answers it: the source is the
// object remote_security_group, and remote_ip_prefix is null.
func groupRule(t *testing.T, ports string) *provision.SGRule {
	t.Helper()
	var r provision.SGRule
	raw := `{"id":"r-group","direction":"ingress","ether_type":"IPv4","ip_protocol":"tcp","port_range":"` + ports + `",` +
		`"remote_ip_prefix":null,"remote_security_group":{"id":"sg-db","name":"db","state":"created"},"state":"created"}`
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.RemoteSecurityGroup == nil || r.RemoteIPPrefix != "" {
		t.Fatalf("the rule did not decode as one from a group: %+v", r)
	}
	return &r
}

func TestGroupsOnly_APortOnlyAGroupLetsInIsNotOpenToAnAddress(t *testing.T) {
	fromGroup := groupRule(t, "8080:8080")
	byAddress := &provision.SGRule{ID: "r-addr", Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", RemoteIPPrefix: "0.0.0.0/0", State: "created"}
	narrow := &provision.SGRule{ID: "r-narrow", Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", RemoteIPPrefix: "10.0.0.0/8", State: "created"}
	noSource := &provision.SGRule{ID: "r-none", Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", State: "created"}
	for name, c := range map[string]struct {
		rules []*provision.SGRule
		want  string
	}{
		"only a group":                {[]*provision.SGRule{fromGroup}, "db"},
		"a group and a narrower rule": {[]*provision.SGRule{fromGroup, narrow}, "db"},
		"a group and an address rule": {[]*provision.SGRule{fromGroup, byAddress}, ""},
		// A rule that names no source counts as before D52: not touched
		// until the platform's answer for «from anywhere» is verified.
		"a group and a rule of no source": {[]*provision.SGRule{fromGroup, noSource}, ""},
		"no rule":                         {nil, ""},
		"an address rule":                 {[]*provision.SGRule{byAddress}, ""},
		"a group, another port":           {[]*provision.SGRule{groupRule(t, "443:443")}, ""},
	} {
		if got := strings.Join(GroupsOnly(c.rules, "tcp", 8080, "0.0.0.0/0"), ","); got != c.want {
			t.Errorf("%s: GroupsOnly = %q, want %q", name, got, c.want)
		}
	}
	// The access is not changed: Coverage counts the port as before.
	if covered, _ := Coverage([]*provision.SGRule{fromGroup}, "tcp", 8080, "0.0.0.0/0"); !covered {
		t.Error("Coverage no longer counts a rule from a group: that is variant «а», for the major")
	}
}

// Open (deploy, provision, deploy_apply): no rule is made for such a port,
// as before, and it is not among the ports «already open».
func TestOpen_APortOnlyAGroupLetsInIsNamedApart(t *testing.T) {
	f := newFake()
	f.rules["sg-1"] = []provision.SGRule{*groupRule(t, "8080:8080"),
		{ID: "r-web", Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80", RemoteIPPrefix: "0.0.0.0/0", State: "created"}}
	rec := &state.DeploymentRecord{}
	res := Open(context.Background(), f, "p", "sg-1", []int{80, 8080, 9090}, nil, "", "vm-1", rec)
	if len(res.OnlyFromGroups) != 1 || res.OnlyFromGroups[0].Port != "8080" || strings.Join(res.OnlyFromGroups[0].Groups, ",") != "db" {
		t.Fatalf("OnlyFromGroups = %+v, want port 8080 from group db", res.OnlyFromGroups)
	}
	if strings.Join(res.AlreadyOpen, ",") != "80" || strings.Join(res.Opened, ",") != "9090" {
		t.Errorf("already open %v, opened %v — want 80 and 9090; 8080 is neither", res.AlreadyOpen, res.Opened)
	}
	note := res.Note()
	if !strings.Contains(note, "8080 is NOT open") || !strings.Contains(note, "db") || strings.Contains(note, "already open 80,8080") {
		t.Errorf("the note calls 8080 open, or does not name the group: %s", note)
	}
}
