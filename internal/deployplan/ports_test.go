// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

type portsCloud struct {
	applyCloud
	existing []*provision.SGRule
	added    []provision.SGRule
}

func (*portsCloud) GetVM(context.Context, string) (*provision.VM, error) {
	return &provision.VM{ID: "vm-made", Status: "running"}, nil
}

func (c *portsCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return c.existing, nil
}

func (c *portsCloud) AddSecurityGroupRule(_ context.Context, _, _ string, r provision.SGRule) (*provision.SGRule, error) {
	c.added = append(c.added, r)
	r.ID = "rule-" + r.PortRange
	return &r, nil
}

// deploy_apply opens ports with the same code as the CLI deploy and hands
// back the rules it created, for the caller to keep in state: destroy deletes
// them. A port already open is needed, not owned.
func TestApply_ReturnsCreatedRules(t *testing.T) {
	old := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	cloud := &portsCloud{existing: []*provision.SGRule{{ID: "theirs", Direction: "ingress", IPProtocol: "tcp", PortRange: "443:443"}}}
	plan := &Plan{Product: "wiki", VM: VMSpec{Name: "wiki-1"}, Network: NetworkSpec{SecurityGroupID: "sg-1"}, Ports: []int{80, 443}}
	res, err := ApplyWithTimeout(context.Background(), cloud, plan, 0, time.Time{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.added) != 1 || cloud.added[0].Description != "evo: port 80 (vm vm-made)" {
		t.Fatalf("added = %+v", cloud.added)
	}
	if res.Ports.SecurityGroupID != "sg-1" || len(res.Ports.OpenedRules) != 1 ||
		res.Ports.OpenedRules[0] != (state.SGRule{ID: "rule-80:80", SecurityGroupID: "sg-1", Port: "80/tcp"}) {
		t.Errorf("ports = %+v", res.Ports)
	}
	if strings.Join(res.Ports.SecurityGroupPorts, ",") != "80/tcp,443/tcp" {
		t.Errorf("needed ports = %v", res.Ports.SecurityGroupPorts)
	}
	if res.Note != "opened ports 80; already open 443" {
		t.Errorf("note = %q", res.Note)
	}
}

// P5 (G2-2): deploy_apply opens the plan's ports to the source the plan
// was made with; a plan saved before 1.7 has none and opens them to
// 0.0.0.0/0, as it was made.
func TestApply_OpensToThePlansSource(t *testing.T) {
	old := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = old })
	for _, tc := range []struct{ source, want string }{{"10.0.0.0/8", "10.0.0.0/8"}, {"", "0.0.0.0/0"}} {
		cloud := &portsCloud{}
		plan := &Plan{Product: "db", VM: VMSpec{Name: "db-1"}, Network: NetworkSpec{SecurityGroupID: "sg-1"},
			Ports: []int{5432}, PortsSource: tc.source}
		if _, err := ApplyWithTimeout(context.Background(), cloud, plan, 0, time.Time{}, nil); err != nil {
			t.Fatal(err)
		}
		if len(cloud.added) != 1 || cloud.added[0].RemoteIPPrefix != tc.want {
			t.Errorf("source %q: rules %+v, want one from %s", tc.source, cloud.added, tc.want)
		}
	}
}
