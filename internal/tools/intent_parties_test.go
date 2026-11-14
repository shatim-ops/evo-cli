// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// partiesCloud is a world where a name moves: the VM web and the security
// group web-sg are the first ones until moved, then another VM and another
// group of the same name. A write is noted as sent; while cut it answers
// API_UNREACHABLE, its outcome unknown. writes are the VMs or the groups
// the writes went to.
type partiesCloud struct {
	provision.CloudAPI
	moved  bool
	cut    bool
	writes []string
}

const (
	partiesVM1, partiesVM2 = 0x41, 0x51
	partiesSG1, partiesSG2 = 0xe1, 0xe4
	partiesFIP             = 0x42
)

func (*partiesCloud) SetToken(string) {}
func (c *partiesCloud) vm() string {
	if c.moved {
		return faultID(partiesVM2)
	}
	return faultID(partiesVM1)
}
func (c *partiesCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: c.vm(), Name: "web"}}, nil
}
func (c *partiesCloud) GetVMDetail(_ context.Context, id string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: id, Name: "web", Interfaces: []provision.VMInterface{{ID: id, Primary: true}}}, nil
}
func (*partiesCloud) GetDisk(context.Context, string) (map[string]any, error) {
	// A lagging read: the disk shows no attachment either time.
	return map[string]any{"id": faultDisk, "state": "available"}, nil
}
func (c *partiesCloud) write(ctx context.Context, to string) error {
	if err := httpretry.NoteWrite(ctx); err != nil {
		return err
	}
	c.writes = append(c.writes, to)
	if c.cut {
		return clerrors.New(clerrors.CodeAPIUnreachable, "timeout", "network")
	}
	return nil
}
func (c *partiesCloud) AttachDisk(ctx context.Context, _, vmID, _ string) (map[string]any, error) {
	if err := c.write(ctx, vmID); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}
func (*partiesCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: faultID(partiesFIP), IPAddress: "203.0.113.10", State: "available"}}, nil
}
func (c *partiesCloud) AttachFloatingIP(ctx context.Context, fipID, _, ifaceID, _ string) (*provision.FloatingIP, error) {
	if err := c.write(ctx, ifaceID); err != nil {
		return nil, err
	}
	return &provision.FloatingIP{ID: fipID, IPAddress: "203.0.113.10", State: "in_use"}, nil
}
func (c *partiesCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	id := faultID(partiesSG1)
	if c.moved {
		id = faultID(partiesSG2)
	}
	return []*provision.SecurityGroup{{ID: id, Name: "web-sg", AvailabilityZoneID: faultZone}}, nil
}
func (*partiesCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}
func (*partiesCloud) ListInterfaces(context.Context, string) ([]*provision.Interface, error) {
	return nil, nil
}
func (c *partiesCloud) AddSecurityGroupRule(ctx context.Context, sgID, _ string, _ provision.SGRule) (*provision.SGRule, error) {
	if err := c.write(ctx, sgID); err != nil {
		return nil, err
	}
	return &provision.SGRule{ID: faultID(0x61)}, nil
}

// The name a call resolved moved to another VM or group: the same
// arguments now change that one, and its outcome says nothing of the cut
// call on the first — the record of the cut call stays (R-101 at the
// tools next to sg_rule_create: Astra, #1817–#1819). The record names the
// resolved parties of the change, not only its target.
func TestIntent_AMovedNameKeepsTheCutCall(t *testing.T) {
	for tool, args := range map[string]map[string]any{
		"disk_attach": {"disk_id": faultDisk, "vm": "web"},
		"fip_attach":  {"fip": "203.0.113.10", "vm": "web"},
		"open_ports":  {"security_group": "web-sg", "ports": []any{float64(8080)}, "cidr": "10.0.0.0/8"},
	} {
		t.Run(tool, func(t *testing.T) {
			c := &partiesCloud{cut: true}
			r, dir := fastRegistry(t, c, config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"})
			args["confirmed"] = true
			_, text, _ := callNamed(t, r, tool, args)
			if len(c.writes) != 1 || len(intent.Pending(intent.Dir(dir))) != 1 {
				t.Fatalf("the cut call: writes %v, records %d: %s", c.writes, len(intent.Pending(intent.Dir(dir))), text)
			}
			c.moved, c.cut = true, false
			_, text, isErr := callNamed(t, r, tool, args)
			if isErr || len(c.writes) != 2 || c.writes[0] == c.writes[1] {
				t.Fatalf("the call after the move: writes %v: %s", c.writes, text)
			}
			if left := intent.Pending(intent.Dir(dir)); len(left) != 1 {
				t.Errorf("the record of the cut call on %s was settled by the call on %s: %d left", c.writes[0], c.writes[1], len(left))
			}
		})
	}
}

// The same name on the same VM or group is the same call: its success
// settles the record of the cut one, as before.
func TestIntent_TheSameNameSettlesTheCutCall(t *testing.T) {
	c := &partiesCloud{cut: true}
	r, dir := fastRegistry(t, c, config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"})
	args := map[string]any{"disk_id": faultDisk, "vm": "web", "confirmed": true}
	callNamed(t, r, "disk_attach", args)
	c.cut = false
	_, text, isErr := callNamed(t, r, "disk_attach", args)
	if isErr || len(c.writes) != 2 {
		t.Fatalf("the repeat: writes %v: %s", c.writes, text)
	}
	if left := intent.Pending(intent.Dir(dir)); len(left) != 0 {
		t.Errorf("the repeat on the same VM left %d records", len(left))
	}
}
