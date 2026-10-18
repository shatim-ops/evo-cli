// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// ubuntuCloud — cancelCloud с набором полей Ubuntu-24.04 из каталога
// (проба 16.10): логин с default, ключ и пароль в группе authentication,
// hostname и два флага состояния, которые отправлять нельзя.
type ubuntuCloud struct{ cancelCloud }

func (*ubuntuCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: "img-1", Name: "Ubuntu-24.04", MetadataFields: []provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
		{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
		{Name: "linux_password", FieldType: "linux_password", MandatoryGroup: "authentication", Sensitive: true},
		{Name: "hostname", FieldType: "hostname", MandatoryGroup: "hostname"},
		{Name: "vmm_enabled", FieldType: "flag", DefaultValue: "true"},
		{Name: "guest_agent_ready", FieldType: "flag", DefaultValue: "false"},
	}}, nil
}

// Переход на общее заполнение метаданных не меняет того, что provision
// отправляет для Ubuntu-24.04: те же ключи и значения, кроме случайного
// пароля; ключ — без перевода строки.
func TestProvision_ImageMetadataUbuntu2404Unchanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &ubuntuCloud{cancelCloud{cancel: cancel}}
	r, dir := cancelRegistry(t, cloud)
	if _, isErr := r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true}); !isErr {
		t.Fatal("ждали ответ об отмене после создания")
	}
	meta := cloud.createReq.ImageMetadata
	want := map[string]string{"name": "user1", "public_key": "ssh-ed25519 AAAA test", "hostname": "web"}
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
	if rec := stateHasVM(t, dir, "vm-made"); rec.SSHUser != "user1" {
		t.Errorf("SSH-пользователь в state = %q, ждали user1", rec.SSHUser)
	}
	for k := range meta {
		if strings.Contains(k, "vmm") || strings.Contains(k, "guest_agent") {
			t.Errorf("отправлен флаг состояния %q", k)
		}
	}
}
