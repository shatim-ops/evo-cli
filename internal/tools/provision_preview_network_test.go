// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Agent run of 1.7.0 (6 of 9; findings 2.1, №8): the preview of provision
// named neither the subnet nor the security group evo picked itself — the
// group only with ports — while deploy's preview names both. Axis 4: what
// evo picks, the preview names before the question — the same subnet and
// group the create takes (engine.SubnetForVM, provision.PickSecurityGroup).
func TestProvisionPreview_NamesSubnetAndSecurityGroup(t *testing.T) {
	sgs := []*provision.SecurityGroup{{ID: "sg-az1", Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: raAZ1,
		Rules: []provision.SGRule{{Direction: "ingress", EtherType: "IPv4", IPProtocol: "tcp", PortRange: "22:22", RemoteIPPrefix: "0.0.0.0/0"}}}}
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets(), sgs: sgs},
		func(c *config.Config) { c.ImageID, c.Region = raImage, raAZ1 })
	want, err := engine.SubnetForVM(raSubnets(), nil, raAZ1, "", "")
	if err != nil || want == nil {
		t.Fatalf("the subnet provision takes: %v, %v", want, err)
	}
	m, text, _ := callNamed(t, r, "provision", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("provision without confirmed: %s", text)
	}
	preview, _ := m["preview"].(map[string]any)
	group, _ := preview["security_group"].(map[string]any)
	if preview["subnet_name"] != want.Name || preview["subnet_id"] != want.ID {
		t.Errorf("the preview does not name the subnet the create takes (%s, %s): %s", want.Name, want.ID, text)
	}
	if group["id"] != "sg-az1" || group["name"] != "SSH-access_ru.AZ-1" {
		t.Errorf("the preview does not name the zone's security group: %s", text)
	}
	if msg, _ := m["message"].(string); !strings.Contains(text, "SSH-access_ru.AZ-1") {
		t.Errorf("the person's lines do not name the group: %s %s", msg, text)
	}
}
