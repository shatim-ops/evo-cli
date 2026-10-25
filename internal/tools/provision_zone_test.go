// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		// The preview refuses it as the call with confirmed does: before
		// the question (W5, D35).
		for _, confirmed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/confirmed=%v", name, confirmed), func(t *testing.T) {
				cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}, subnets: tc.subnets, groups: tc.groups}
				r, _ := cancelRegistry(t, cloud)
				text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": confirmed})
				if !isErr {
					t.Fatalf("ждали отказ: %s", text)
				}
				var res map[string]any
				_ = json.Unmarshal([]byte(text), &res)
				hint, _ := res["hint"].(string)
				if res["reason"] != "CONFIG_INVALID" || !strings.Contains(res["error"].(string), tc.want) || res["hint"] == nil {
					t.Errorf("ответ: %s", text)
				}
				if strings.Contains(tc.want, "no subnet") &&
					(!strings.Contains(hint, "subnet_id") || !strings.Contains(hint, "list_subnets")) {
					t.Errorf("подсказка без subnet_id/list_subnets: %s", text)
				}
				if len(cloud.mutations) > 0 {
					t.Errorf("до отказа облако изменено: %v", cloud.mutations)
				}
			})
		}
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
// нечитаемый список групп — отказ до любых изменений. (Имя зоны в region
// с 1.2.1 — зона, а не отказ: TestProvision_ZoneByName.)
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
		"список групп не прочитан":      {nil, errors.New("connection reset"), "list security groups"},
		"subnet_id без зоны, имя чужой": {map[string]any{"subnet_id": "sn-n"}, nil, "subnet Default_ru.AZ-1-legacy is not in zone ru.AZ-3"},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}, subnets: subnets, groups: groups, sgErr: tc.sgErr}
			args := map[string]any{"name": "web", "confirmed": true}
			for k, v := range tc.args {
				args[k] = v
			}
			r := New(func(map[string]any) (*engine.Engine, error) {
				return &engine.Engine{
					Config: config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
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

// zoneRegistry — настройки cfg, поверх которых, как у настоящей фабрики,
// ложатся аргументы вызова region, image и image_id.
func zoneRegistry(t *testing.T, cloud provision.CloudAPI, cfg config.Config) *Registry {
	t.Helper()
	dir := t.TempDir()
	return New(func(o map[string]any) (*engine.Engine, error) {
		c := cfg
		if v, ok := o["Region"].(string); ok {
			c.Region = v
		}
		if v, ok := o["Image"].(string); ok {
			c.ImageID, c.Image = "", v
		}
		if v, ok := o["ImageID"].(string); ok {
			c.ImageID = v
		}
		return &engine.Engine{
			Config: c, Auth: staticToken{}, Cloud: cloud,
			Printer:  output.NewWithWriter(&bytes.Buffer{}, output.ModeText, false),
			StateDir: dir, SSHKeyPEM: []byte("key"), SSHPubKey: "ssh-ed25519 AAAA test",
		}, nil
	})
}

// Догфуд 1.2.0: --region и region MCP принимают имя зоны, как vm.zone
// стека и evo init, — в любом регистре; ВМ создаётся в зоне по её UUID, а
// превью называет зону. Имя — из аргумента или из настроек
// (CLOUDRU_REGION, профиль).
func TestProvision_ZoneByName(t *testing.T) {
	az3 := []*provision.Subnet{{ID: "sn-3", Name: "Default_ru.AZ-3", AvailabilityZoneID: "z", Default: true}}
	sg3 := []*provision.SecurityGroup{{ID: "sg-3", Name: "SSH-access_ru.AZ-3", AvailabilityZoneID: "z"}}
	for name, tc := range map[string]struct {
		cfgRegion string
		args      map[string]any
	}{
		"region MCP по имени":      {"", map[string]any{"region": "ru.AZ-3"}},
		"region в другом регистре": {"", map[string]any{"region": "RU.az-3"}},
		"имя из настроек":          {"ru.AZ-3", nil},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: cancel}, subnets: az3, groups: sg3}
			r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", Region: tc.cfgRegion, ImageID: "img-1", KeyID: "k", Secret: "s"})
			args := map[string]any{"name": "web"}
			for k, v := range tc.args {
				args[k] = v
			}
			m, text, isErr := callNamed(t, r, "provision", args)
			if !isErr || m["status"] != "confirmation_required" {
				t.Fatalf("превью: %s", text)
			}
			var resp struct {
				Preview engine.CreatePreview `json:"preview"`
			}
			_ = json.Unmarshal([]byte(text), &resp)
			if vm := resp.Preview.VM; vm == nil || vm.Zone.ID != "z" || vm.Zone.Name != "ru.AZ-3" {
				t.Errorf("зона превью: %s", text)
			}
			args["confirmed"] = true
			r.handlers["provision"](ctx, args)
			if cloud.creates != 1 || cloud.createReq.AvailabilityZoneID != "z" {
				t.Errorf("CreateVM %d раз, зона %q: %v", cloud.creates, cloud.createReq.AvailabilityZoneID, cloud.mutations)
			}
		})
	}
}

// Имя, которого нет в списке зон, и имя при непрочитанном списке —
// отказ до превью и до любых изменений, на превью и на вызове с
// confirmed одинаково; неизвестное имя называет зоны, из которых выбирать.
func TestProvision_UnknownZoneNameRefusedBeforePreview(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}}
		r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", ImageID: "img-1", KeyID: "k", Secret: "s"})
		m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web", "region": "ru.AZ-9", "confirmed": confirmed})
		hint, _ := m["hint"].(string)
		if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, `\"ru.AZ-9\"`) || !strings.Contains(hint, "ru.AZ-3, ru.AZ-1") {
			t.Errorf("confirmed=%v: %s", confirmed, text)
		}
		if len(cloud.mutations) > 0 {
			t.Errorf("до отказа облако изменено: %v", cloud.mutations)
		}
	}
	cloud := &zoneListFails{otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}}}
	r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", ImageID: "img-1", KeyID: "k", Secret: "s"})
	m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web", "region": "ru.AZ-3"})
	if !isErr || m["status"] == "confirmation_required" || !strings.Contains(text, "connection reset") {
		t.Errorf("непрочитанный список: %s", text)
	}
}

// zoneListFails не отдаёт список зон.
type zoneListFails struct{ otherZoneCloud }

func (*zoneListFails) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return nil, errors.New("connection reset")
}

// Догфуд 1.2.0: профиль от evo configure без зоны — превью писало «не
// задано: зона», спрашивало «Продолжить?», и только после «y» приходил
// отказ. Теперь вызов без зоны или без образа отказывает до превью — с
// confirmed и без одинаково: тот же reason, без confirmation_required, в
// облако ничего не уходит; "missing" называет аргумент.
func TestProvision_NoZoneOrImageRefusedBeforePreview(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg     config.Config
		reason  string
		missing string
		text    string
	}{
		"без зоны":   {config.Config{ProjectID: "p", ImageID: "img-1", KeyID: "k", Secret: "s"}, "CONFIG_INVALID", "region", "no availability zone is set"},
		"без образа": {config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"}, "USAGE", "image", "no image is set"},
	} {
		for _, confirmed := range []bool{false, true} {
			cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}}
			r := zoneRegistry(t, cloud, tc.cfg)
			m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web", "confirmed": confirmed})
			missing, _ := m["missing"].([]any)
			if !isErr || m["status"] != "error" || m["reason"] != tc.reason || len(missing) != 1 || missing[0] != tc.missing ||
				!strings.Contains(text, tc.text) || m["hint"] == nil {
				t.Errorf("%s, confirmed=%v: %s", name, confirmed, text)
			}
			if len(cloud.mutations) > 0 || cloud.creates > 0 {
				t.Errorf("%s: облако изменено: %v", name, cloud.mutations)
			}
		}
	}
}

// MCP deploy и evo deploy понимают имя зоны, как provision (план 1.2.2,
// ревью зоны 1.2.1): превью называет зону по UUID и имени; неизвестное имя
// — отказ USAGE уже на превью, а не после согласия, и в облаке ничего не
// меняется.
func TestDeploy_ZoneByNameOnPreview(t *testing.T) {
	compose := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(compose, []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The zone has its subnet and group: the preview refuses a zone without
	// them before the question (W5).
	cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}},
		subnets: []*provision.Subnet{{ID: "sn-3", Name: "Default_ru.AZ-3", AvailabilityZoneID: "z", Default: true}},
		groups:  []*provision.SecurityGroup{{ID: "sg-3", Name: "SSH-access_ru.AZ-3", AvailabilityZoneID: "z"}}}
	r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", ImageID: "img-1", KeyID: "k", Secret: "s", ComposeFile: compose})
	m, text, isErr := callNamed(t, r, "deploy", map[string]any{"region": "ru.az-3"})
	if !isErr || m["status"] != "confirmation_required" {
		t.Fatalf("превью: %s", text)
	}
	var resp struct {
		Preview engine.CreatePreview `json:"preview"`
	}
	_ = json.Unmarshal([]byte(text), &resp)
	if vm := resp.Preview.VM; vm == nil || vm.Zone.ID != "z" || vm.Zone.Name != "ru.AZ-3" {
		t.Errorf("зона превью: %s", text)
	}
	// Вызов с confirmed встречает имя на шаге сети — до согласия и до
	// любых изменений (engine.TestDeploy_UnknownZoneNameRefused).
	m, text, isErr = callNamed(t, r, "deploy", map[string]any{"region": "ru.AZ-9"})
	if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, `\"ru.AZ-9\"`) {
		t.Errorf("неизвестное имя: %s", text)
	}
	if len(cloud.mutations) > 0 {
		t.Errorf("облако изменено: %v", cloud.mutations)
	}
}
