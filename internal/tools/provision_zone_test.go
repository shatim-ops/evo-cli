// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// provision в зоне, где у проекта нет подсети или группы безопасности:
// подсеть или группа другой зоны не даёт ВМ загрузиться (error_creating),
// а FIP и ВМ уже платные. Отказ CONFIG_INVALID до любых изменений — как
// у deploy (ревью Fable, A1). ID выдуманы.

// otherZoneCloud — подсети и группы проекта; любая мутация считается.
type otherZoneCloud struct {
	cancelCloud
	subnets   []*provision.Subnet
	groups    []*provision.SecurityGroup
	zones     []*provision.AvailabilityZone
	sgErr     error
	mutations []string
}

func (c *otherZoneCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	if c.zones != nil {
		return c.zones, nil
	}
	return []*provision.AvailabilityZone{{ID: "z", Name: "ru.AZ-3"}, {ID: "z1", Name: "ru.AZ-1"}}, nil
}
func (c *otherZoneCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return c.subnets, nil
}
func (c *otherZoneCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return c.groups, c.sgErr
}
func (*otherZoneCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}
func (c *otherZoneCloud) CreateFloatingIP(ctx context.Context, r provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.mutations = append(c.mutations, "CreateFloatingIP")
	return c.cancelCloud.CreateFloatingIP(ctx, r)
}
func (c *otherZoneCloud) CreateVM(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.mutations = append(c.mutations, "CreateVM")
	return c.cancelCloud.CreateVM(ctx, req)
}

func TestProvision_NoSubnetOrGroupInZoneRefusedBeforeAnyMutation(t *testing.T) {
	subnetsAZ1 := []*provision.Subnet{
		{ID: "sn-a", Name: "Default_ru.AZ-1", AvailabilityZoneID: "z1", Default: true},
		{ID: "sn-b", Name: "net_ru.AZ-1", AvailabilityZoneID: "z1"},
	}
	subnetsAZ3 := []*provision.Subnet{{ID: "sn-3", Name: "Default_ru.AZ-3", AvailabilityZoneID: "z", Default: true}}
	groupsAZ1 := []*provision.SecurityGroup{
		{ID: "sg-a", Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: "z1"},
		{ID: "sg-b", Name: "web_ru.AZ-1", AvailabilityZoneID: "z1"},
	}
	for name, tc := range map[string]struct {
		subnets []*provision.Subnet
		groups  []*provision.SecurityGroup
		want    string
	}{
		"нет подсети в зоне": {subnetsAZ1, nil, "no subnet for zone ru.AZ-3"},
		"нет группы в зоне":  {subnetsAZ3, groupsAZ1, "no security group for zone ru.AZ-3"},
		// Единственная подсеть проекта — в чужой зоне (ревью Grok на #83).
		"единственная подсеть чужой зоны": {subnetsAZ1[:1], groupsAZ1, "no subnet for zone ru.AZ-3"},
		"единственная группа чужой зоны":  {subnetsAZ3, groupsAZ1[:1], "no security group for zone ru.AZ-3"},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}, subnets: tc.subnets, groups: tc.groups}
			r, _ := cancelRegistry(t, cloud)
			text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
			if !isErr {
				t.Fatalf("ждали отказ: %s", text)
			}
			var res map[string]any
			_ = json.Unmarshal([]byte(text), &res)
			if res["reason"] != "CONFIG_INVALID" || !strings.Contains(res["error"].(string), tc.want) || res["hint"] == nil {
				t.Errorf("ответ: %s", text)
			}
			if len(cloud.mutations) > 0 {
				t.Errorf("до отказа облако изменено: %v", cloud.mutations)
			}
		})
	}
}

// Зона закрыта (enabled=false, cloud-e2e 17.10.2026: FIP создан, ВМ — 422
// wrong_az, FIP завис в error_deleting): отказ до любых изменений, хотя
// подсеть и группа зоны есть.
func TestProvision_ClosedZoneRefusedBeforeAnyMutation(t *testing.T) {
	closed := false
	cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}},
		zones:   []*provision.AvailabilityZone{{ID: "z", Name: "ru.AZ-3", Enabled: &closed}, {ID: "z1", Name: "ru.AZ-1"}},
		subnets: []*provision.Subnet{{ID: "sn-3", Name: "Default_ru.AZ-3", AvailabilityZoneID: "z", Default: true}},
		groups:  []*provision.SecurityGroup{{ID: "sg-3", Name: "SSH-access_ru.AZ-3", AvailabilityZoneID: "z"}}}
	r, _ := cancelRegistry(t, cloud)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr || !strings.Contains(text, "CONFIG_INVALID") || !strings.Contains(text, "ru.AZ-3") || !strings.Contains(text, "ru.AZ-1") {
		t.Errorf("ответ: %s", text)
	}
	if len(cloud.mutations) > 0 {
		t.Errorf("до отказа облако изменено: %v", cloud.mutations)
	}
}

// Подсеть зоны и группа зоны есть: ВМ создаётся в них, а не в первых
// по списку.
func TestProvision_TakesZoneSubnetAndGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: cancel},
		subnets: []*provision.Subnet{
			{ID: "sn-a", Name: "Default_ru.AZ-1", AvailabilityZoneID: "z1", Default: true},
			{ID: "sn-3", Name: "net", AvailabilityZoneID: "z"},
		},
		groups: []*provision.SecurityGroup{
			{ID: "sg-a", Name: "SSH-access_ru.AZ-1", AvailabilityZoneID: "z1"},
			{ID: "sg-3", Name: "SSH-access_ru.AZ-3", AvailabilityZoneID: "z"},
		}}
	r, _ := cancelRegistry(t, cloud)
	r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true})
	if cloud.creates != 1 {
		t.Fatalf("CreateVM не вызван: %v", cloud.mutations)
	}
	ifc := cloud.createReq.Interfaces[0]
	if ifc.SubnetID != "sn-3" || len(ifc.SecurityGroups) != 1 || ifc.SecurityGroups[0] != "sg-3" {
		t.Errorf("интерфейс %+v", ifc)
	}
}

// Ревью Fable на v1.0: явный subnet_id не из проекта или из другой зоны,
// имя зоны в region, нечитаемый список групп — отказ до любых изменений.
func TestProvision_ExplicitSubnetZoneNameAndGroupListRefused(t *testing.T) {
	subnets := []*provision.Subnet{
		{ID: "sn-1", Name: "Default_ru.AZ-1", AvailabilityZoneID: "z1", Default: true},
		{ID: "sn-3", Name: "Default_ru.AZ-3", AvailabilityZoneID: "z", Default: true},
		{ID: "sn-n", Name: "Default_ru.AZ-1-legacy"},
	}
	groups := []*provision.SecurityGroup{{ID: "sg-3", Name: "SSH-access_ru.AZ-3", AvailabilityZoneID: "z", Default: true}}
	for name, tc := range map[string]struct {
		args  map[string]any
		sgErr error
		want  string
	}{
		"subnet_id не из проекта":       {map[string]any{"subnet_id": "sn-x"}, nil, "subnet sn-x is not in the project"},
		"subnet_id другой зоны":         {map[string]any{"subnet_id": "sn-1"}, nil, "subnet Default_ru.AZ-1 is not in zone ru.AZ-3"},
		"имя зоны в region":             {map[string]any{"_region": "ru.AZ-3"}, nil, "zone name"},
		"список групп не прочитан":      {nil, errors.New("connection reset"), "list security groups"},
		"subnet_id без зоны, имя чужой": {map[string]any{"subnet_id": "sn-n"}, nil, "subnet Default_ru.AZ-1-legacy is not in zone ru.AZ-3"},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}, subnets: subnets, groups: groups, sgErr: tc.sgErr}
			region := "z"
			args := map[string]any{"name": "web", "confirmed": true}
			for k, v := range tc.args {
				if k == "_region" {
					region = v.(string)
					continue
				}
				args[k] = v
			}
			r := New(func(map[string]any) (*engine.Engine, error) {
				return &engine.Engine{
					Config: config.Config{ProjectID: "p", Region: region, ImageID: "img-1", KeyID: "k", Secret: "s"},
					Auth:   staticToken{}, Cloud: cloud,
					Printer:  output.NewWithWriter(&bytes.Buffer{}, output.ModeText, false),
					StateDir: t.TempDir(), SSHKeyPEM: []byte("key"), SSHPubKey: "ssh-ed25519 AAAA test",
				}, nil
			})
			text, isErr := r.handlers["provision"](context.Background(), args)
			if !isErr || !strings.Contains(text, tc.want) {
				t.Errorf("ответ: %s", text)
			}
			if len(cloud.mutations) > 0 {
				t.Errorf("до отказа облако изменено: %v", cloud.mutations)
			}
		})
	}
}

// Имя, которое compute отвергнет 422, — USAGE до floating IP и ВМ (C-api 4).
func TestProvision_BadNameRefusedBeforeAnyMutation(t *testing.T) {
	cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	r, _ := cancelRegistry(t, cloud)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "1web", "confirmed": true})
	if !isErr || !strings.Contains(text, "USAGE") || len(cloud.mutations) > 0 {
		t.Errorf("ответ %s, изменения %v", text, cloud.mutations)
	}
}
