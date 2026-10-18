// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// metaCloud отвечает на чтения Resolve и запоминает запрос создания; после
// создания отменяет контекст, чтобы Apply не ждал ВМ.
type metaCloud struct {
	provision.CloudAPI
	fields    []provision.ImageMetadataField
	cancel    context.CancelFunc
	createReq provision.CreateVMRequest
	// rules of the group; nil — the default group's (TCP 22 in, all out).
	rules []*provision.SGRule
}

func (*metaCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: "00000000-0000-0000-0000-0000000000a2", Name: "ru.AZ-2"}}, nil
}
func (*metaCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: "fl-1", Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4}}, nil
}
func (*metaCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return []*provision.Image{{ID: "img-u", Name: "Ubuntu-24.04"}}, nil
}
func (m *metaCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: id, Name: "Ubuntu-24.04", MetadataFields: m.fields}, nil
}
func (*metaCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return nil, nil
}
func (*metaCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: "sn-1", Name: "Default_ru.AZ-2"}}, nil
}
func (*metaCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru.AZ-2"}}, nil
}
func (m *metaCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	if m.rules != nil {
		return m.rules, nil
	}
	return []*provision.SGRule{
		{Direction: "ingress", IPProtocol: "tcp", PortRange: "22:22", RemoteIPPrefix: "0.0.0.0/0"},
		{Direction: "egress", IPProtocol: "any", RemoteIPPrefix: "0.0.0.0/0"},
	}, nil
}
func (*metaCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, nil
}
func (m *metaCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	m.createReq = req
	m.cancel()
	return &provision.VM{ID: "vm-made"}, nil
}

// ubuntu2404Fields — набор полей Ubuntu-24.04 из каталога (проба 16.10).
func ubuntu2404Fields() []provision.ImageMetadataField {
	return []provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
		{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
		{Name: "linux_password", FieldType: "linux_password", MandatoryGroup: "authentication", Sensitive: true},
		{Name: "hostname", FieldType: "hostname", MandatoryGroup: "hostname"},
		{Name: "vmm_enabled", FieldType: "flag", DefaultValue: "true"},
		{Name: "guest_agent_ready", FieldType: "flag", DefaultValue: "false"},
	}
}

// resolveAndApply строит план и выполняет его до запроса создания.
func resolveAndApply(t *testing.T, fields []provision.ImageMetadataField, p Params) (*Plan, provision.CreateVMRequest, *ApplyResult) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &metaCloud{fields: fields, cancel: cancel}
	plan, err := Resolve(ctx, cloud, "proj-1", p)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	res, _ := ApplyWithTimeout(ctx, cloud, plan, 0)
	return plan, cloud.createReq, res
}

// Переход на общее заполнение метаданных не меняет того, что deploy_apply
// отправляет для Ubuntu-24.04: те же ключи и значения, кроме случайного
// пароля; логин и модель доступа в плане — прежние.
func TestApply_ImageMetadataUbuntu2404Unchanged(t *testing.T) {
	const key = "ssh-ed25519 AAAA agent"
	plan, req, _ := resolveAndApply(t, ubuntu2404Fields(), Params{Name: "web", SSHKey: key})
	meta := req.ImageMetadata
	want := map[string]string{"name": "user1", "public_key": key, "hostname": "web"}
	if len(meta) != len(want)+1 {
		t.Errorf("image_metadata = %d ключей, ждали %v и linux_password", len(meta), want)
	}
	for k, v := range want {
		if meta[k] != v {
			t.Errorf("image_metadata[%q] = %q, ждали %q", k, meta[k], v)
		}
	}
	if len(meta["linux_password"]) != 16 {
		t.Errorf("linux_password длиной %d, ждали 16", len(meta["linux_password"]))
	}
	if plan.SSH != (SSHSpec{Login: "user1", KeyName: "ssh-key (deploy)"}) {
		t.Errorf("ssh в плане = %+v", plan.SSH)
	}
	// План лежит на диске: пароля в нём нет, он делается при apply.
	if _, ok := plan.MetaStatic["linux_password"]; ok || len(plan.MetaFields) != 1 || plan.MetaFields[0] != "linux_password" {
		t.Errorf("план: meta_static=%v meta_fields=%v", plan.MetaStatic, plan.MetaFields)
	}

	// Без ключа — только пароль; модель доступа — generated-password.
	plan, req, _ = resolveAndApply(t, ubuntu2404Fields(), Params{Name: "web"})
	if _, ok := req.ImageMetadata["public_key"]; ok {
		t.Errorf("пустой public_key отправлен: %v", req.ImageMetadata)
	}
	if len(req.ImageMetadata) != 3 || plan.SSH.KeyName != "generated-password" {
		t.Errorf("без ключа: метаданные %d ключей, ssh %+v", len(req.ImageMetadata), plan.SSH)
	}
}

// Планы, сохранённые до internal/images, перечисляют в meta_fields ещё name,
// hostname и public_key: apply по-прежнему их заполняет.
func TestApply_OldPlanMetaFields(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &metaCloud{cancel: cancel}
	plan := &Plan{
		VM:         VMSpec{Name: "web"},
		SSH:        SSHSpec{Login: "user1"},
		MetaFields: []string{"name", "hostname", "linux_password", "public_key"},
		MetaStatic: map[string]string{"motd": "hi"},
		PublicKey:  "ssh-ed25519 AAAA agent",
	}
	_, _ = ApplyWithTimeout(ctx, cloud, plan, 0)
	meta := cloud.createReq.ImageMetadata
	if meta["name"] != "user1" || meta["hostname"] != "web" || meta["public_key"] != plan.PublicKey || meta["motd"] != "hi" || len(meta["linux_password"]) != 16 {
		t.Errorf("image_metadata старого плана = %v", meta)
	}
}

// РЕД ОС 7.3: обязательное fqdn типа hostname теперь заполняется (раньше
// поле с незнакомым именем пропускалось — 422 от API).
func TestApply_RedOSFqdnFilled(t *testing.T) {
	fields := append(ubuntu2404Fields(), provision.ImageMetadataField{Name: "fqdn", FieldType: "hostname", MandatoryGroup: "fqdn"})
	_, req, _ := resolveAndApply(t, fields, Params{Name: "web", SSHKey: "ssh-ed25519 AAAA agent"})
	if req.ImageMetadata["fqdn"] != "web" {
		t.Errorf("fqdn = %q, image_metadata = %v", req.ImageMetadata["fqdn"], req.ImageMetadata)
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (*metaCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}
