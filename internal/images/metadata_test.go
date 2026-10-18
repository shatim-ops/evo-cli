// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package images

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

const (
	testKey = "ssh-ed25519 AAAA test"
	testPW  = "Pw-0123456789abc"
)

type f = provision.ImageMetadataField

// ubuntu — набор полей Ubuntu-24.04 из каталога (проба 16.10); у 58 из 60
// образов маркетплейса он тот же.
func ubuntu() []f {
	return []f{
		{Name: "name", FieldType: TypeLinuxLogin, DefaultValue: "user1", MandatoryGroup: "name"},
		{Name: "public_key", FieldType: TypePublicKey, MandatoryGroup: "authentication"},
		{Name: "linux_password", FieldType: TypeLinuxPassword, MandatoryGroup: "authentication", Sensitive: true},
		{Name: "hostname", FieldType: TypeHostname, MandatoryGroup: "hostname"},
		{Name: "vmm_enabled", FieldType: TypeFlag, DefaultValue: "true"},
		{Name: "guest_agent_ready", FieldType: TypeFlag, DefaultValue: "false"},
	}
}

func with(base []f, extra ...f) []f { return append(base, extra...) }

func opensearch() []f {
	return with(ubuntu(),
		f{Name: "opensearch_user", FieldType: TypeText, MandatoryGroup: "opensearch_user", DisplayName: "Пользователь OpenSearch"},
		f{Name: "opensearch_password", FieldType: TypePlainPassword, Sensitive: true, MandatoryGroup: "opensearch_password", DisplayName: "Пароль OpenSearch"},
		f{Name: "opensearch_host", FieldType: TypeText},
		f{Name: "opensearch_version", FieldType: TypeText, DefaultValue: "2.11.1"},
	)
}

func inputs() Inputs {
	return Inputs{PublicKey: testKey, Hostname: "web-1", Login: "ubuntu", GenPassword: func() string { return testPW }}
}

func TestFillMetadata(t *testing.T) {
	cases := []struct {
		name          string
		fields        []f
		in            func(Inputs) Inputs
		wantMeta      map[string]string
		wantLogin     string
		wantPW        []string
		wantKeyUsed   bool
		wantMissing   []string // группы как в каталоге, без пробелов по краям
		wantSensitive []string
	}{
		{
			name:   "Ubuntu-24.04",
			fields: ubuntu(),
			wantMeta: map[string]string{
				"name": "user1", "public_key": testKey, "linux_password": testPW, "hostname": "web-1",
			},
			wantLogin: "user1", wantPW: []string{"linux_password"}, wantKeyUsed: true,
			wantSensitive: []string{"linux_password"},
		},
		{
			name: "РЕД ОС 7.3: обязательное fqdn типа hostname",
			fields: with(ubuntu(),
				f{Name: "fqdn", FieldType: TypeHostname, MandatoryGroup: "fqdn", DisplayName: "FQDN"}),
			wantMeta: map[string]string{
				"name": "user1", "public_key": testKey, "linux_password": testPW, "hostname": "web-1", "fqdn": "web-1",
			},
			wantLogin: "user1", wantPW: []string{"linux_password"}, wantKeyUsed: true,
			wantSensitive: []string{"linux_password"},
		},
		{
			name: "Astra: флаги не уходят, guest_agent_ready типа text",
			fields: []f{
				{Name: "name", FieldType: TypeLinuxLogin, DefaultValue: "astra", MandatoryGroup: "name"},
				{Name: "public_key", FieldType: TypePublicKey, MandatoryGroup: "authentication"},
				{Name: "linux_password", FieldType: TypeLinuxPassword, MandatoryGroup: "authentication"},
				{Name: "hostname", FieldType: TypeHostname, MandatoryGroup: "hostname"},
				{Name: "vmm_enabled", FieldType: TypeFlag},
				{Name: "guest_agent_ready", FieldType: TypeText, DefaultValue: "false"},
				{Name: "astra_closed_env", FieldType: TypeFlag},
				{Name: "about", FieldType: TypeInfo},
			},
			wantMeta: map[string]string{
				"name": "astra", "public_key": testKey, "linux_password": testPW, "hostname": "web-1",
			},
			wantLogin: "astra", wantPW: []string{"linux_password"}, wantKeyUsed: true,
			wantSensitive: []string{"linux_password"},
		},
		{
			name: "группа с пробелом и в другом регистре удовлетворена",
			fields: []f{
				{Name: "name", FieldType: TypeLinuxLogin, DefaultValue: "user1", MandatoryGroup: " name"},
				{Name: "public_key", FieldType: TypePublicKey, MandatoryGroup: " authentication"},
				{Name: "linux_password", FieldType: TypeLinuxPassword, MandatoryGroup: "authentication"},
				// default в уже закрытой группе не уходит рядом с паролем
				{Name: "auth_hint", FieldType: TypeText, DefaultValue: "x", MandatoryGroup: "Authentication "},
				{Name: "hostname", FieldType: TypeHostname, MandatoryGroup: " hostname"},
			},
			in: func(in Inputs) Inputs { in.PublicKey = ""; return in },
			wantMeta: map[string]string{
				"name": "user1", "linux_password": testPW, "hostname": "web-1",
			},
			wantLogin: "user1", wantPW: []string{"linux_password"},
			wantSensitive: []string{"linux_password"},
		},
		{
			name: "русская группа: удовлетворена и пропущена",
			fields: []f{
				{Name: "name", FieldType: TypeLinuxLogin, DefaultValue: "user1"},
				{Name: "public_key", FieldType: TypePublicKey, MandatoryGroup: "Метод аутентификации"},
				{Name: "linux_password", FieldType: TypeLinuxPassword, MandatoryGroup: " метод аутентификации"},
				{Name: "license", FieldType: TypeText, MandatoryGroup: "Лицензия", MandatoryGroupDisplayName: "Лицензия продукта", DisplayName: "Ключ"},
			},
			in:            func(in Inputs) Inputs { in.PublicKey = ""; return in },
			wantMeta:      map[string]string{"name": "user1", "linux_password": testPW},
			wantLogin:     "user1",
			wantPW:        []string{"linux_password"},
			wantMissing:   []string{"Лицензия"},
			wantSensitive: []string{"linux_password"},
		},
		{
			name: "Windows: логин и linux_password, без ключа и hostname",
			fields: []f{
				{Name: "name", FieldType: TypeLinuxLogin, DefaultValue: "Administrator", MandatoryGroup: "name"},
				{Name: "linux_password", FieldType: TypeLinuxPassword, MandatoryGroup: "authentication", Sensitive: true},
			},
			wantMeta:      map[string]string{"name": "Administrator", "linux_password": testPW},
			wantLogin:     "Administrator",
			wantPW:        []string{"linux_password"},
			wantSensitive: []string{"linux_password"},
		},
		{
			name: "Windows: единственный пароль типа plain_password",
			fields: []f{
				{Name: "name", FieldType: TypeLinuxLogin, MandatoryGroup: "name"},
				{Name: "admin_password", FieldType: TypePlainPassword, MandatoryGroup: "authentication"},
			},
			wantMeta:      map[string]string{"name": "ubuntu", "admin_password": testPW},
			wantLogin:     "ubuntu",
			wantPW:        []string{"admin_password"},
			wantSensitive: []string{"admin_password"},
		},
		{
			name:   "OpensearchDashboards: поля продукта заданы явно",
			fields: opensearch(),
			in: func(in Inputs) Inputs {
				in.Values = map[string]string{"opensearch_user": "admin", "opensearch_password": "Os-secret-1", "opensearch_host": "0.0.0.0"}
				return in
			},
			wantMeta: map[string]string{
				"name": "user1", "public_key": testKey, "linux_password": testPW, "hostname": "web-1",
				"opensearch_user": "admin", "opensearch_password": "Os-secret-1", "opensearch_host": "0.0.0.0",
				"opensearch_version": "2.11.1",
			},
			wantLogin: "user1", wantPW: []string{"linux_password"}, wantKeyUsed: true,
			wantSensitive: []string{"linux_password", "opensearch_password"},
		},
		{
			name:   "OpensearchDashboards: без значений — пароль продукта не выдумывается",
			fields: opensearch(),
			wantMeta: map[string]string{
				"name": "user1", "public_key": testKey, "linux_password": testPW, "hostname": "web-1",
				"opensearch_version": "2.11.1",
			},
			wantLogin: "user1", wantPW: []string{"linux_password"}, wantKeyUsed: true,
			wantMissing:   []string{"opensearch_user", "opensearch_password"},
			wantSensitive: []string{"linux_password"},
		},
		{
			name: "два поля логина: name и linux_login",
			fields: []f{
				{Name: "linux_login", FieldType: TypeLinuxLogin, MandatoryGroup: "login"},
				{Name: "name", FieldType: TypeLinuxLogin, DefaultValue: "user1", MandatoryGroup: "name"},
				{Name: "public_key", FieldType: TypePublicKey, MandatoryGroup: "authentication"},
			},
			wantMeta:    map[string]string{"linux_login": "user1", "name": "user1", "public_key": testKey},
			wantLogin:   "user1",
			wantKeyUsed: true,
		},
		{
			name: "старые образы без field_type: по имени",
			fields: []f{
				{Name: "ssh_key", MandatoryGroup: "ssh"},
				{Name: "instance_name"},
				{Name: "username"},
				{Name: "linux_password"},
				{Name: "vmm_enabled", DefaultValue: "1"},
				{Name: "motd", DefaultValue: "hi"},
				{Name: "comment"},
			},
			wantMeta: map[string]string{
				"ssh_key": testKey, "instance_name": "web-1", "username": "ubuntu", "linux_password": testPW, "motd": "hi",
			},
			wantLogin: "ubuntu", wantPW: []string{"linux_password"}, wantKeyUsed: true,
			wantSensitive: []string{"linux_password"},
		},
		{
			name:   "явные значения побеждают своё заполнение",
			fields: ubuntu(),
			in: func(in Inputs) Inputs {
				in.Values = map[string]string{"name": "admin", "hostname": "db", "linux_password": "Mine-1"}
				return in
			},
			wantMeta: map[string]string{
				"name": "admin", "public_key": testKey, "linux_password": "Mine-1", "hostname": "db",
			},
			wantLogin: "admin", wantKeyUsed: true,
			wantSensitive: []string{"linux_password"},
		},
		{
			name:   "пароль отложен: не в meta, но назван",
			fields: ubuntu(),
			in:     func(in Inputs) Inputs { in.GenPassword = nil; return in },
			wantMeta: map[string]string{
				"name": "user1", "public_key": testKey, "hostname": "web-1",
			},
			wantLogin: "user1", wantPW: []string{"linux_password"}, wantKeyUsed: true,
			wantSensitive: []string{"linux_password"},
		},
		{
			name: "ничем не закрытая группа попадает в Missing с полями",
			fields: []f{
				{Name: "name", FieldType: TypeLinuxLogin},
				{Name: "public_key", FieldType: TypePublicKey, MandatoryGroup: "authentication", DisplayName: "SSH-ключ"},
			},
			in:          func(in Inputs) Inputs { in.PublicKey, in.Login = "", ""; return in },
			wantMeta:    map[string]string{},
			wantMissing: []string{"authentication"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := inputs()
			if tc.in != nil {
				in = tc.in(in)
			}
			meta, res, err := FillMetadata(tc.fields, in)
			if err != nil {
				t.Fatalf("FillMetadata: %v", err)
			}
			if !reflect.DeepEqual(meta, tc.wantMeta) {
				t.Errorf("meta = %v\nждали  %v", meta, tc.wantMeta)
			}
			if res.Login != tc.wantLogin {
				t.Errorf("Login = %q, ждали %q", res.Login, tc.wantLogin)
			}
			if !reflect.DeepEqual(res.PasswordFields, tc.wantPW) {
				t.Errorf("PasswordFields = %v, ждали %v", res.PasswordFields, tc.wantPW)
			}
			if res.KeyUsed != tc.wantKeyUsed {
				t.Errorf("KeyUsed = %v, ждали %v", res.KeyUsed, tc.wantKeyUsed)
			}
			var groups []string
			for _, m := range res.Missing {
				groups = append(groups, m.Group)
				if len(m.Fields) == 0 {
					t.Errorf("группа %q без полей", m.Group)
				}
			}
			if !reflect.DeepEqual(groups, tc.wantMissing) {
				t.Errorf("Missing = %v, ждали %v", groups, tc.wantMissing)
			}
			if !reflect.DeepEqual(res.Sensitive, tc.wantSensitive) {
				t.Errorf("Sensitive = %v, ждали %v", res.Sensitive, tc.wantSensitive)
			}
			for k := range meta {
				if IsStatusField(k) {
					t.Errorf("отправлен флаг состояния %q", k)
				}
			}
			// Секреты — только в meta.
			if dump := fmt.Sprintf("%+v", res); strings.Contains(dump, testPW) || strings.Contains(dump, "Os-secret-1") || strings.Contains(dump, "Mine-1") {
				t.Errorf("пароль попал в Result: %s", dump)
			}
		})
	}
}

// Missing несёт то, что нужно для вопроса человеку: имя, тип, подпись поля
// и подпись группы.
func TestFillMetadata_MissingDescribesFields(t *testing.T) {
	_, res, err := FillMetadata(opensearch(), inputs())
	if err != nil {
		t.Fatal(err)
	}
	want := []MissingGroup{
		{Group: "opensearch_user", Fields: []FieldRef{{Name: "opensearch_user", FieldType: TypeText, DisplayName: "Пользователь OpenSearch"}}},
		{Group: "opensearch_password", Fields: []FieldRef{{Name: "opensearch_password", FieldType: TypePlainPassword, DisplayName: "Пароль OpenSearch"}}},
	}
	if !reflect.DeepEqual(res.Missing, want) {
		t.Errorf("Missing = %+v\nждали    %+v", res.Missing, want)
	}

	_, res, _ = FillMetadata([]f{
		{Name: "license", FieldType: TypeText, MandatoryGroup: " Лицензия", MandatoryGroupDisplayName: "Лицензия продукта"},
	}, inputs())
	if len(res.Missing) != 1 || res.Missing[0].Group != "Лицензия" || res.Missing[0].DisplayName != "Лицензия продукта" {
		t.Errorf("Missing = %+v", res.Missing)
	}
}

// Ключ, которого ещё нет (deploy делает его после согласия), не уходит
// пустым, но поле названо — его заполнят позже.
func TestFillMetadata_KeyFieldsListedWithoutKey(t *testing.T) {
	in := inputs()
	in.PublicKey = ""
	meta, res, err := FillMetadata([]f{{Name: "ssh_key", MandatoryGroup: "ssh"}, {Name: "name", DefaultValue: "user1"}}, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := meta["ssh_key"]; ok || res.KeyUsed {
		t.Errorf("пустой ключ отправлен: %v", meta)
	}
	if !reflect.DeepEqual(res.KeyFields, []string{"ssh_key"}) {
		t.Errorf("KeyFields = %v", res.KeyFields)
	}
}

func TestFillMetadata_RejectsBadValues(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"флаг состояния":        {"vmm_enabled": "true"},
		"флаг состояния (text)": {"guest_agent_ready": "1"},
		"поле, которого нет":    {"no_such_field": "x"},
		"пустое значение":       {"hostname": " "},
		"без значений":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			in := inputs()
			in.Values = values
			_, _, err := FillMetadata(with(ubuntu(), f{Name: "extra_flag", FieldType: TypeFlag}), in)
			if values == nil {
				if err != nil {
					t.Fatalf("без значений ошибка: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("значения %v приняты", values)
			}
			if strings.Contains(err.Error(), testPW) {
				t.Errorf("пароль в ошибке: %v", err)
			}
		})
	}

	// Флаг и info без default уходят, если их задали явно.
	in := inputs()
	in.Values = map[string]string{"extra_flag": "true"}
	meta, _, err := FillMetadata(with(ubuntu(), f{Name: "extra_flag", FieldType: TypeFlag}), in)
	if err != nil || meta["extra_flag"] != "true" {
		t.Errorf("явный флаг: meta=%v err=%v", meta, err)
	}
}

// Ревью Qwen на #75: у образа с двумя полями пароля оба получают один и тот
// же пароль — человеку возвращается один, и он должен открывать ВМ.
func TestFillMetadata_OnePasswordForAllPasswordFields(t *testing.T) {
	fields := []provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "admin"},
		{Name: "linux_password", FieldType: "linux_password", MandatoryGroup: "authentication"},
		{Name: "root_password", FieldType: "linux_password", MandatoryGroup: "authentication"},
	}
	n := 0
	meta, _, err := FillMetadata(fields, Inputs{GenPassword: func() string { n++; return fmt.Sprintf("pw-%d", n) }})
	if err != nil {
		t.Fatal(err)
	}
	if meta["linux_password"] == "" || meta["linux_password"] != meta["root_password"] {
		t.Fatalf("passwords differ or empty: %q vs %q", meta["linux_password"], meta["root_password"])
	}
}
