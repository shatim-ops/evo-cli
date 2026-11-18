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
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// cloudLF is a value of the cloud with a line feed and words after it: in
// a text of evo it must stay inside quotes, never a line of its own.
const cloudLF = "vm-other\nASTRA2315"

// The id of another VM a disk is attached to, as the card of the disk names
// it, is the cloud's: in quotes in the refusal of disk_attach and
// disk_detach, and a command only for an id of the form of an id (Астра
// #2323; «Риск», 291-2, 314).
func TestDiskAttachDetach_AnotherVMsIDIsQuoted(t *testing.T) {
	for _, tool := range []string{"disk_attach", "disk_detach"} {
		disk := map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "in_use",
			"vms": []any{map[string]any{"id": cloudLF, "name": "db", "primary": false}}}
		r, c := agentDiskWorld(t, disk)
		m, text, isErr := callNamed(t, r, tool, map[string]any{"disk_id": faultDisk, "vm": "web"})
		if !isErr || len(c.writes) != 0 {
			t.Fatalf("%s: %s", tool, text)
		}
		for _, key := range []string{"error", "hint"} {
			s, _ := m[key].(string)
			if strings.Contains(s, "\nASTRA2315") {
				t.Errorf("%s.%s carries the cloud's id unquoted: %q", tool, key, s)
			}
			if strings.Contains(s, "disk_detach disk_id=") {
				t.Errorf("%s.%s gives a command with an id not of the form of an id: %q", tool, key, s)
			}
		}
	}
}

// zoneLFCloud is a world whose catalogue names its zone with a line feed.
type zoneLFCloud struct{ *natCloud }

func (c *zoneLFCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: faultZone, Name: "zone\nASTRA2315"}}, nil
}

func (c *zoneLFCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return nil, nil
}

// The name of a zone the catalogue gives is the cloud's: in quotes in the
// refusals of snat_create and of a balancer with no subnet in the zone
// (Астра #2324).
func TestZoneNameOfTheCatalogueIsQuoted(t *testing.T) {
	c := &zoneLFCloud{&natCloud{faultCloud: &faultCloud{}, gateways: []map[string]any{
		{"id": okID, "name": "existing", "availability_zone_id": faultZone}}}}
	m, text, _ := callNamed(t, natRegistry(t, c), "snat_create", map[string]any{"name": "nat", "availability_zone_id": faultZone})
	s, _ := m["error"].(string)
	if !strings.Contains(s, "ASTRA2315") {
		t.Fatalf("snat_create did not name the zone: %s", text)
	}
	if strings.Contains(s, "\nASTRA2315") {
		t.Errorf("snat_create: the zone's name unquoted: %q", s)
	}

	c = &zoneLFCloud{&natCloud{faultCloud: &faultCloud{}}}
	e := &engine.Engine{Cloud: c, Config: config.Config{ProjectID: faultProject}}
	_, _, _, _, err := lbZoneAndSubnet(context.Background(), e, map[string]any{"zone": faultZone}, nil, "vpc-1")
	if err == nil {
		t.Fatal("no refusal of a zone with no subnet")
	}
	if strings.Contains(err.Error(), "\nASTRA2315") {
		t.Errorf("a balancer: the zone's name unquoted: %q", err.Error())
	}
}

// The refusal of a balancer with no subnet in the zone names the zone as a
// zone and the VPC as a VPC in both languages: the Russian text names the
// zone first.
func TestLBNoSubnetNamesZoneAndVPCInTheirPlaces(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	c := &zoneLFCloud{&natCloud{faultCloud: &faultCloud{}}}
	e := &engine.Engine{Cloud: c, Config: config.Config{ProjectID: faultProject}}
	for lang, want := range map[string]string{
		i18n.EN: `no subnet of VPC "vpc-1" in zone "zone\nASTRA2315"`,
		i18n.RU: `в зоне "zone\nASTRA2315" нет подсети VPC "vpc-1"`,
	} {
		i18n.SetLang(lang)
		_, _, _, _, err := lbZoneAndSubnet(context.Background(), e, map[string]any{"zone": faultZone}, nil, "vpc-1")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %s", lang, err, want)
		}
	}
}
