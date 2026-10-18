// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// ubuntu2404Fields — набор полей Ubuntu-24.04 из каталога (проба 16.10):
// логин с default, ключ и пароль в группе authentication, hostname и два
// флага состояния, которые отправлять нельзя.
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

// Переход на общее заполнение метаданных не меняет того, что deploy
// отправляет для Ubuntu-24.04: те же ключи и значения (кроме случайного
// пароля), SSH-пользователь — из поля логина.
func TestDeploy_ImageMetadataUbuntu2404Unchanged(t *testing.T) {
	cloud := &mockCloudAPI{getImageFn: func(_ context.Context, id string) (*provision.ImageDetail, error) {
		return &provision.ImageDetail{ID: id, Name: "Ubuntu-24.04", MetadataFields: ubuntu2404Fields()}, nil
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	meta := cloud.createReq.ImageMetadata
	want := map[string]string{
		"name":       "user1",
		"public_key": eng.SSHPubKey,
		"hostname":   cloud.createReq.Name,
	}
	if len(meta) != len(want)+1 {
		t.Errorf("ключи image_metadata = %v, ждали %v и linux_password", metaKeys(meta), want)
	}
	for k, v := range want {
		if meta[k] != v {
			t.Errorf("image_metadata[%q] = %q, ждали %q", k, meta[k], v)
		}
	}
	if len(meta["linux_password"]) != 16 {
		t.Errorf("linux_password длиной %d, ждали 16", len(meta["linux_password"]))
	}
	if rec := readActive(t, dir); rec == nil || rec.SSHUser != "user1" {
		t.Errorf("SSH-пользователь в state = %+v, ждали user1", rec)
	}
}

func metaKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
