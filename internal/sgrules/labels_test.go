// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// groupsCloud is fakeCloud with the project's groups listed.
type groupsCloud struct{ *fakeCloud }

func (c groupsCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	var out []*provision.SecurityGroup
	for id := range c.rules {
		out = append(out, &provision.SecurityGroup{ID: id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// P6: a rule evo opens carries its VM's id (RuleLabel), and a delete of the
// VM from any folder finds it in the project's groups by that label. Only
// the VM's own ingress rules, and not one a record of the folder holds —
// kept for another deployment or marked for the next destroy.
func TestFindLabeled_OnlyTheVMsRulesNoRecordHolds(t *testing.T) {
	f := newFake()
	var rec state.DeploymentRecord
	Open(context.Background(), f, "p", "sg-a", []int{80}, []int{53}, "", "vm-1", &rec)
	f.rules["sg-a"] = append(f.rules["sg-a"],
		provision.SGRule{ID: "other-vm", Direction: "ingress", IPProtocol: "tcp", PortRange: "443:443", Description: RuleLabel("443", "vm-2")},
		provision.SGRule{ID: "egress", Direction: "egress", IPProtocol: "tcp", PortRange: "80:80", Description: RuleLabel("80", "vm-1")},
		provision.SGRule{ID: "people", Direction: "ingress", IPProtocol: "tcp", PortRange: "22:22", Description: "ssh for the office"},
		provision.SGRule{ID: "dying", Direction: "ingress", IPProtocol: "tcp", PortRange: "81:81", Description: RuleLabel("81", "vm-1"), State: "deleting"})
	f.rules["sg-b"] = []provision.SGRule{
		{ID: "held", Direction: "ingress", IPProtocol: "tcp", PortRange: "8080:8080", Description: RuleLabel("8080", "vm-1")},
	}
	if got := f.rules["sg-a"][0].Description; got != "evo: port 80 (vm vm-1)" {
		t.Fatalf("Open labels the rule %q, want evo: port 80 (vm vm-1)", got)
	}
	sf := &state.File{Deployments: []state.DeploymentRecord{{ID: "d-2", Status: "running",
		OpenedRules: []state.SGRule{{ID: "held", SecurityGroupID: "sg-b", Port: "8080/tcp"}}}}}

	found, err := FindLabeled(context.Background(), groupsCloud{f}, "p", "vm-1", "", "", sf)
	if err != nil {
		t.Fatal(err)
	}
	var ports []string
	for _, r := range found {
		ports = append(ports, Label(r.Rule.Port))
	}
	if strings.Join(ports, ",") != "80,53/udp" {
		t.Fatalf("found %+v; want the VM's own 80 and 53/udp only", found)
	}

	f.deleteErr[found[1].Rule.ID] = errors.New("API 503")
	res := CloseLabeled(context.Background(), f, found)
	if strings.Join(res.Closed, ",") != "80 (vm vm-1)" || len(res.Failed) != 1 || !strings.HasPrefix(res.Failed[0], "53/udp (vm vm-1): ") {
		t.Errorf("closed %v, failed %v; want 80 closed and 53/udp reported", res.Closed, res.Failed)
	}
	// A rule gone meanwhile is closed, as Close counts it.
	gone := CloseLabeled(context.Background(), f, []Planned{{Rule: state.SGRule{SecurityGroupID: "sg-a", ID: "no-such", Port: "9/tcp"}, VMID: "vm-1"}})
	if strings.Join(gone.Closed, ",") != "9 (vm vm-1)" {
		t.Errorf("a rule already gone: %+v, want closed", gone)
	}
}

// A description that is not a label of evo names no VM.
func TestLabelVM(t *testing.T) {
	for desc, want := range map[string]string{
		RuleLabel("80", "vm-1"):    "vm-1",
		RuleLabel("53/udp", "x-2"): "x-2",
		"evo: port 80":             "",
		"auto: wiki port 80":       "",
		"evo: port 80 (vm vm-1":    "",
		"opened via open_ports":    "",
	} {
		if got := labelVM(desc); got != want {
			t.Errorf("labelVM(%q) = %q, want %q", desc, got, want)
		}
	}
}

// zonedGroups lists sg-a in zone z-1 and sg-b in z-2; ListSecurityGroupRules
// of a group in failing answers 503.
type zonedGroups struct {
	*fakeCloud
	failing string
	read    []string
}

func (c *zonedGroups) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "sg-a", AvailabilityZoneID: "z-1"}, {ID: "sg-b", AvailabilityZoneID: "z-2"}}, nil
}
func (c *zonedGroups) ListSecurityGroupRules(ctx context.Context, sg string) ([]*provision.SGRule, error) {
	c.read = append(c.read, sg)
	if sg == c.failing {
		return nil, errors.New("API 503")
	}
	return c.fakeCloud.ListSecurityGroupRules(ctx, sg)
}

// The VM's zone known, only its groups are read (the VM's ports open in
// its zone's group); a group that cannot be read is the error beside what
// the others gave — the caller says the rules were not all checked.
func TestFindLabeled_ZoneAndAnUnreadGroup(t *testing.T) {
	f := newFake()
	f.rules["sg-a"] = []provision.SGRule{{ID: "a", Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80", Description: RuleLabel("80", "vm-1")}}
	f.rules["sg-b"] = []provision.SGRule{{ID: "b", Direction: "ingress", IPProtocol: "tcp", PortRange: "81:81", Description: RuleLabel("81", "vm-1")}}

	c := &zonedGroups{fakeCloud: f}
	found, err := FindLabeled(context.Background(), c, "p", "vm-1", "z-1", "", nil)
	if err != nil || len(found) != 1 || found[0].Rule.ID != "a" || strings.Join(c.read, ",") != "sg-a" {
		t.Errorf("in zone z-1: found %+v, err %v, read %v; want rule a, sg-a only", found, err, c.read)
	}

	c = &zonedGroups{fakeCloud: f, failing: "sg-a"}
	found, err = FindLabeled(context.Background(), c, "p", "vm-1", "", "", nil)
	if err == nil || len(found) != 1 || found[0].Rule.ID != "b" {
		t.Errorf("sg-a unread: found %+v, err %v; want rule b and the error", found, err)
	}
}
