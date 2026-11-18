// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const lbOtherSubnet = "00000000-0000-4000-8000-0000000000c7"

// subnetElsewhereCloud is a world whose catalogue names its zone with a
// line feed and whose one subnet is in another zone.
type subnetElsewhereCloud struct{ *zoneLFCloud }

func (c *subnetElsewhereCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: lbOtherSubnet, Name: "other", AvailabilityZoneID: "00000000-0000-4000-8000-0000000000a9", VPCID: "vpc\nASTRA2315"}}, nil
}

// subnetHereCloud is a world whose catalogue names its zone with a line
// feed and whose one subnet is in that zone.
type subnetHereCloud struct{ *zoneLFCloud }

func (c *subnetHereCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: lbOtherSubnet, Name: "here", AvailabilityZoneID: faultZone}}, nil
}

// The refusals and warnings of a balancer's zone and subnet name what the
// cloud gave — a zone's name, a VPC, the zones of the target group's VMs —
// in quotes (Астра #2350, the class of #2324).
func TestLBZoneAndSubnet_CloudNamesQuoted(t *testing.T) {
	c := &subnetElsewhereCloud{&zoneLFCloud{&natCloud{faultCloud: &faultCloud{}}}}
	e := &engine.Engine{Cloud: c, Config: config.Config{ProjectID: faultProject}}

	// A subnet of another zone.
	_, _, _, _, err := lbZoneAndSubnet(context.Background(), e, map[string]any{"zone": faultZone, "subnet_id": lbOtherSubnet}, nil, "")
	if err == nil || !strings.Contains(err.Error(), "ASTRA2315") {
		t.Fatalf("a subnet of another zone: %v", err)
	}
	if strings.Contains(err.Error(), "\nASTRA2315") {
		t.Errorf("a subnet of another zone: the zone's name unquoted: %q", err.Error())
	}

	// A subnet of another VPC: the VPC of the target group is the cloud's.
	_, _, _, _, err = lbZoneAndSubnet(context.Background(), e, map[string]any{"zone": faultZone, "subnet_id": lbOtherSubnet}, nil, "vpc-of\nASTRA2315")
	if err == nil || strings.Contains(err.Error(), "\nASTRA2315") {
		t.Errorf("a subnet of another VPC: %q", err)
	}

	// The zones of the target group's VMs in the warning.
	here := &engine.Engine{Cloud: &subnetHereCloud{&zoneLFCloud{&natCloud{faultCloud: &faultCloud{}}}}, Config: config.Config{ProjectID: faultProject}}
	tg := map[string]any{"target_vms": []any{
		map[string]any{"zone_id": faultZone}, map[string]any{"zone_name": "b"}}}
	_, _, _, warnings, err := lbZoneAndSubnet(context.Background(), here, map[string]any{}, tg, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warnings {
		if strings.Contains(w, "\nASTRA2315") {
			t.Errorf("a warning names a zone of the target group unquoted: %q", w)
		}
	}
	if len(warnings) == 0 {
		t.Errorf("no warning of the target group's zones")
	}
}
