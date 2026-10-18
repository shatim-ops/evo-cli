// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

func byName(fields []FieldInfo) map[string]FieldInfo {
	out := map[string]FieldInfo{}
	for _, f := range fields {
		out[f.Name] = f
	}
	return out
}

// Карточка образа говорит, кто заполнит поле: evo (ключ, имя ВМ, пароль),
// значение по умолчанию, человек (обязательная группа, которую evo не
// закрывает) или никто; служебные флаги не передаются. Заглушки FillMetadata
// наружу не выходят, секретные default — тоже.
func TestFields_WhoFills(t *testing.T) {
	ubuntu := Fields([]provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: " name"},
		{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
		{Name: "linux_password", FieldType: "linux_password", MandatoryGroup: "authentication", Sensitive: true, DefaultValue: "s3cret-default"},
		{Name: "hostname", FieldType: "hostname", MandatoryGroup: "hostname"},
		{Name: "vmm_enabled", FieldType: "flag", DefaultValue: "true"},
	})
	got := byName(ubuntu)
	for name, want := range map[string][2]string{
		"name":           {FilledByDefault, ""},
		"public_key":     {FilledByEvo, RoleKey},
		"linux_password": {FilledByEvo, RolePassword},
		"hostname":       {FilledByEvo, RoleHostname},
		"vmm_enabled":    {FilledByStatus, ""},
	} {
		if f := got[name]; f.FilledBy != want[0] || f.Role != want[1] {
			t.Errorf("%s: %s/%s, want %s/%s", name, f.FilledBy, f.Role, want[0], want[1])
		}
	}
	if got["name"].Default != "user1" || got["name"].Group != "name" || !got["name"].Required {
		t.Errorf("login field: %+v", got["name"])
	}
	if got["linux_password"].Default != "" {
		t.Errorf("a sensitive default leaked: %+v", got["linux_password"])
	}
	for _, f := range ubuntu {
		if strings.Contains(f.Default, "\x00") || strings.Contains(f.Default, "probe") {
			t.Errorf("placeholder leaked: %+v", f)
		}
	}
	if n := NeedsUser(ubuntu); len(n) != 0 {
		t.Errorf("ubuntu needs nothing from the user: %v", n)
	}

	// Логин без default получает логин evo; обязательное текстовое поле без
	// значения — от человека; необязательное — никто.
	product := Fields([]provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", MandatoryGroup: "name"},
		{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
		{Name: "opensearch_host", FieldType: "text", MandatoryGroup: "opensearch"},
		{Name: "opensearch_password", FieldType: "plain_password", Sensitive: true},
		{Name: "opensearch_version", FieldType: "text", DefaultValue: "2.11"},
	})
	got = byName(product)
	if f := got["name"]; f.FilledBy != FilledByEvo || f.Role != RoleLogin {
		t.Errorf("login without default: %+v", f)
	}
	if f := got["opensearch_host"]; f.FilledBy != FilledByUser {
		t.Errorf("mandatory text: %+v", f)
	}
	if f := got["opensearch_password"]; f.FilledBy != FilledByNone {
		t.Errorf("product secret must not be invented: %+v", f)
	}
	if f := got["opensearch_version"]; f.FilledBy != FilledByDefault || f.Default != "2.11" {
		t.Errorf("text with default: %+v", f)
	}
	if n := NeedsUser(product); len(n) != 1 || n[0] != "opensearch_host" {
		t.Errorf("needs user: %v", n)
	}

	// РЕД ОС 7.3: fqdn типа hostname — имя ВМ.
	redos := byName(Fields([]provision.ImageMetadataField{{Name: "fqdn", FieldType: "hostname", MandatoryGroup: "fqdn"}}))
	if f := redos["fqdn"]; f.FilledBy != FilledByEvo || f.Role != RoleHostname {
		t.Errorf("fqdn: %+v", f)
	}
}
