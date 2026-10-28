// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// zoneCloud — зоны (одна может быть закрыта) и подсети проекта.
type zoneCloud struct {
	provision.CloudAPI
	zones   []*provision.AvailabilityZone
	subnets []*provision.Subnet
}

func (c zoneCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return c.zones, nil
}
func (c zoneCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return c.subnets, nil
}

// Финальный аудит, N-06: pg_create без subnet_id брал первую подсеть
// проекта — в том числе закрытой зоны (OPS-10), где кластер падает в
// FAILED, а его через API не удалить (DB-21). Теперь подсеть открытой зоны,
// а явная подсеть закрытой зоны — CONFIG_INVALID до создания.
func TestDbaasSubnet_SkipsAndRefusesClosedZone(t *testing.T) {
	closed := false
	zones := []*provision.AvailabilityZone{
		{ID: raAZ1, Name: "ru.AZ-1", Enabled: &closed},
		{ID: raAZ2, Name: "ru.AZ-2"},
	}
	subnets := []*provision.Subnet{
		{ID: "sn-1", Name: "Default_ru.AZ-1", AvailabilityZoneID: raAZ1, VPCID: "vpc-1", Default: true},
		{ID: "sn-2", Name: "Default_ru.AZ-2", AvailabilityZoneID: raAZ2, VPCID: "vpc-1", Default: true},
	}
	eng := func(subs []*provision.Subnet) *engine.Engine {
		return &engine.Engine{Config: config.Config{ProjectID: dbaasProject}, Cloud: zoneCloud{zones: zones, subnets: subs}}
	}
	ctx := context.Background()

	id, _, zone, err := dbaasSubnet(ctx, eng(subnets), "")
	if err != nil || id != "sn-2" || zone != "ru.AZ-2" {
		t.Errorf("default subnet = %s in %s (%v), want sn-2 in the open ru.AZ-2", id, zone, err)
	}
	for _, c := range []struct {
		label string
		subs  []*provision.Subnet
		arg   string
	}{
		{"only a subnet of the closed zone", subnets[:1], ""},
		{"explicit subnet of the closed zone", subnets, "sn-1"},
	} {
		_, _, _, err := dbaasSubnet(ctx, eng(c.subs), c.arg)
		if ce, ok := err.(*clierrors.CLIError); !ok || ce.Code != clierrors.CodeConfigInvalid {
			t.Errorf("%s: %v, want CONFIG_INVALID", c.label, err)
		}
	}
}

// What the skills say of pg_create (skills/evo-postgres, G130-25; С9
// proof): without subnet_id the cluster goes to the main subnet of the
// profile's zone — not the first subnet of the project, not another
// subnet of that zone.
func TestDbaasSubnet_MainSubnetOfTheProfilesZone(t *testing.T) {
	zones := []*provision.AvailabilityZone{{ID: raAZ1, Name: "ru.AZ-1"}, {ID: raAZ2, Name: "ru.AZ-2"}}
	// The first subnet of the project is not the main one, and without a
	// region evo would take ru.AZ-2 first: only the profile's zone gives
	// sn-az1-main.
	subnets := []*provision.Subnet{
		{ID: "sn-az1-app", Name: "app", AvailabilityZoneID: raAZ1, VPCID: "vpc-1"},
		{ID: "sn-az2-main", Name: "Default_ru.AZ-2", AvailabilityZoneID: raAZ2, VPCID: "vpc-1", Default: true},
		{ID: "sn-az1-main", Name: "Default_ru.AZ-1", AvailabilityZoneID: raAZ1, VPCID: "vpc-1", Default: true},
	}
	for _, region := range []string{raAZ1, "ru.AZ-1"} {
		eng := &engine.Engine{Config: config.Config{ProjectID: dbaasProject, Region: region}, Cloud: zoneCloud{zones: zones, subnets: subnets}}
		id, _, zone, err := dbaasSubnet(context.Background(), eng, "")
		if err != nil || id != "sn-az1-main" || zone != "ru.AZ-1" {
			t.Errorf("region %s: subnet = %s in %s (%v), want sn-az1-main, the main subnet of the profile's ru.AZ-1", region, id, zone, err)
		}
	}
}
