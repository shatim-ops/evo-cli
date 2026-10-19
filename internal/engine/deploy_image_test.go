// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// deploy: образ по имени (image: в .evo.yaml), отказ на ОС без Docker и
// проверка обязательных полей образа — до создания чего-либо (шаг C).
// Все ID выдуманы.

func imageCatalogCloud(images ...*provision.ImageDetail) *mockCloudAPI {
	byID := map[string]*provision.ImageDetail{}
	var list []*provision.Image
	for _, d := range images {
		byID[d.ID] = d
		list = append(list, &provision.Image{ID: d.ID, Name: d.Name, DisplayName: d.DisplayName, OS: d.OS, Type: "public",
			AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: "az-1", State: "created"}}})
	}
	return &mockCloudAPI{
		listImagesFn: func(context.Context) ([]*provision.Image, error) { return list, nil },
		getImageFn: func(_ context.Context, id string) (*provision.ImageDetail, error) {
			if d, ok := byID[id]; ok {
				return d, nil
			}
			return nil, clerrors.New(clerrors.CodeNotFound, "not found", "")
		},
	}
}

const (
	imgUbuntu = "00000000-0000-0000-0000-00000000c001"
	imgAstra  = "00000000-0000-0000-0000-00000000c002"
	imgRedOS  = "00000000-0000-0000-0000-00000000c003"
	imgAlt    = "00000000-0000-0000-0000-00000000c004"
	imgProd   = "00000000-0000-0000-0000-00000000c005"
)

func catalogForDeploy() *mockCloudAPI {
	login := provision.ImageMetadataField{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"}
	key := provision.ImageMetadataField{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"}
	return imageCatalogCloud(
		&provision.ImageDetail{ID: imgUbuntu, Name: "Ubuntu-24.04", MetadataFields: []provision.ImageMetadataField{login, key}},
		&provision.ImageDetail{ID: imgAstra, Name: "Astra Linux SE 1.8", MetadataFields: []provision.ImageMetadataField{login, key}},
		&provision.ImageDetail{ID: imgRedOS, Name: "redos-7.3", DisplayName: "РЕД ОС 7.3", MetadataFields: []provision.ImageMetadataField{login, key}},
		&provision.ImageDetail{ID: imgAlt, Name: "server-10", DisplayName: "Альт Сервер 10", MetadataFields: []provision.ImageMetadataField{login, key}},
		&provision.ImageDetail{ID: imgProd, Name: "Product-Ubuntu", MetadataFields: []provision.ImageMetadataField{login, key,
			{Name: "admin_email", FieldType: "text", MandatoryGroup: "product"}}},
	)
}

func TestDeploy_ImageByNameSendsCatalogueName(t *testing.T) {
	cloud := catalogForDeploy()
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Config.ImageID, eng.Config.Image = "", "ubuntu-24.04"
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !cloud.createCalled || cloud.createReq.ImageName != "Ubuntu-24.04" {
		t.Errorf("image_name = %q, ждали имя из каталога", cloud.createReq.ImageName)
	}
}

func TestDeploy_RefusesOSWithoutDockerBeforeCreating(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for _, tc := range []struct{ id, image, family string }{
		{imgAstra, "", "Astra Linux"},
		{"", "astra linux se 1.8", "Astra Linux"},
		{imgRedOS, "", "RED OS"},
		{imgAlt, "", "Alt Linux"},
	} {
		t.Run(tc.family+tc.image, func(t *testing.T) {
			cloud := catalogForDeploy()
			eng, dir := newTestEngine(t, cloud, newMockDeployer())
			eng.Config.ImageID, eng.Config.Image = tc.id, tc.image
			opts := newDeployOpts(t, dir)
			opts.DryRun = true // even the preview is refused
			err := eng.Deploy(context.Background(), opts)
			var ce *clerrors.CLIError
			if !asCLI(err, &ce) || ce.Code != clerrors.CodeConfigInvalid ||
				!strings.Contains(ce.Message, "Docker не выпускает пакеты для "+tc.family) ||
				!strings.Contains(ce.Hint, "Ubuntu или Debian") || !strings.Contains(ce.Hint, "evo vm create") {
				t.Fatalf("ошибка: %v (%+v)", err, ce)
			}
			if cloud.createCalled || len(cloud.addedRules) > 0 {
				t.Error("облако изменено до отказа")
			}
		})
	}
}

func TestDeploy_MissingImageFieldRefusedBeforeCreating(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	cloud := catalogForDeploy()
	fips := 0
	cloud.createFloatingIPFn = func(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		fips++
		return &provision.FloatingIP{ID: "fip-1", IPAddress: "127.0.0.1"}, nil
	}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Config.ImageID = imgProd
	eng.SSHPubKey = "" // the key is made after consent: its field is not missing
	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	var ce *clerrors.CLIError
	if !asCLI(err, &ce) || ce.Code != clerrors.CodeConfigInvalid || !strings.Contains(ce.Message, "product: admin_email") ||
		!strings.Contains(ce.Hint, "image_metadata: {admin_email: <значение>} в .evo.yaml") {
		t.Fatalf("ошибка: %v (%+v)", err, ce)
	}
	if strings.Contains(ce.Message, "public_key") {
		t.Errorf("ключ, который evo сделает сам, назван недостающим: %s", ce.Message)
	}
	if cloud.createCalled || fips > 0 {
		t.Error("облако изменено до отказа")
	}

	// Неизвестное поле в image_metadata — ошибка файла проекта.
	eng.Config.ImageMetadata = map[string]string{"admin_mail": "x"}
	err = eng.Deploy(context.Background(), newDeployOpts(t, dir))
	if !asCLI(err, &ce) || ce.Code != clerrors.CodeConfigInvalid || !strings.Contains(ce.Message, "image_metadata (.evo.yaml)") ||
		!strings.Contains(ce.Hint, "admin_email") {
		t.Fatalf("неизвестное поле: %v (%+v)", err, ce)
	}

	// Значение из .evo.yaml снимает отказ и уходит в запрос.
	eng.SSHPubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test"
	eng.Config.ImageMetadata = map[string]string{"admin_email": "ops@example.test"}
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if cloud.createReq.ImageMetadata["admin_email"] != "ops@example.test" {
		t.Errorf("image_metadata = %v", cloud.createReq.ImageMetadata)
	}
}

// init --image: имя пишется как в каталоге (image:), UUID — как image_id:,
// ОС без Docker отклоняется.
func TestInit_ImageByName(t *testing.T) {
	for _, tc := range []struct{ ref, want string }{
		{"UBUNTU-24.04", "image: Ubuntu-24.04  # " + imgUbuntu},
		{imgUbuntu, "image_id: " + imgUbuntu + "  # Ubuntu-24.04"},
	} {
		cloud := catalogForDeploy()
		eng, _ := newTestEngine(t, cloud, newMockDeployer())
		path := filepath.Join(t.TempDir(), ".evo.yaml")
		if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true, Image: tc.ref}); err != nil {
			t.Fatalf("Init %s: %v", tc.ref, err)
		}
		b, _ := os.ReadFile(path)
		if !strings.Contains(string(b), "\n"+tc.want+"\n") {
			t.Errorf("init --image %s:\n%s", tc.ref, b)
		}
	}

	cloud := catalogForDeploy()
	eng, _ := newTestEngine(t, cloud, newMockDeployer())
	path := filepath.Join(t.TempDir(), ".evo.yaml")
	err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true, Image: "astra"})
	var ce *clerrors.CLIError
	if !asCLI(err, &ce) || ce.Code != clerrors.CodeUsage {
		t.Fatalf("init --image astra: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("файл создан, хотя образ отклонён")
	}
}

func asCLI(err error, target **clerrors.CLIError) bool {
	if err == nil {
		return false
	}
	ce, ok := err.(*clerrors.CLIError)
	if !ok {
		return false
	}
	*target = ce
	return true
}
