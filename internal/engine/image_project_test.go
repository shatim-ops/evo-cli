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

// Образ общего каталога, которого проект не видит (GET /images?project_id=
// его не отдаёт): ВМ из него API не создаст — 422 wrong_image (живой факт
// 16.10.2026). deploy и init отказывают до создания чего-либо. Все ID
// выдуманы.

// hideFromProject makes the catalogue as the project sees it lack the
// images of ids, and counts what deploy creates.
func hideFromProject(cloud *mockCloudAPI, ids ...string) *int {
	hidden := map[string]bool{}
	for _, id := range ids {
		hidden[id] = true
	}
	cloud.projectViewFn = func(ctx context.Context, f provision.ImageFilter) ([]*provision.Image, error) {
		all, err := cloud.listImagesFn(ctx)
		if err != nil {
			return nil, err
		}
		var out []*provision.Image
		for _, img := range all {
			if hidden[img.ID] ||
				f.Name != "" && !strings.Contains(strings.ToLower(img.Name), strings.ToLower(f.Name)) ||
				f.DisplayName != "" && !strings.Contains(strings.ToLower(img.DisplayName), strings.ToLower(f.DisplayName)) {
				continue
			}
			out = append(out, img)
		}
		return out, nil
	}
	fips := 0
	cloud.createFloatingIPFn = func(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		fips++
		return &provision.FloatingIP{ID: "fip-1", IPAddress: "127.0.0.1"}, nil
	}
	return &fips
}

func TestDeploy_ImageNotInProjectRefusedBeforeCreating(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for _, tc := range []struct{ name, id, image, want string }{
		{"по имени", "", "ubuntu-24.04", "Ubuntu-24.04"},
		{"по image_id", imgUbuntu, "", "Ubuntu-24.04"},
		{"по UUID в image", "", imgUbuntu, "Ubuntu-24.04"},
		// Недоступен и без Docker: первым — отказ по доступности.
		{"Astra Linux", "", "astra linux se 1.8", "Astra Linux SE 1.8"},
	} {
		for _, dry := range []bool{false, true} {
			name := tc.name
			if dry {
				name += ", --dry-run"
			}
			t.Run(name, func(t *testing.T) {
				cloud := catalogForDeploy()
				fips := hideFromProject(cloud, imgUbuntu, imgAstra)
				eng, dir := newTestEngine(t, cloud, newMockDeployer())
				eng.Config.ImageID, eng.Config.Image = tc.id, tc.image
				opts := newDeployOpts(t, dir)
				opts.DryRun = dry // и превью не показывается
				err := eng.Deploy(context.Background(), opts)
				var ce *clerrors.CLIError
				if !asCLI(err, &ce) || ce.Code != clerrors.CodeNotFound || clerrors.ExitCode(err) != clerrors.ExitNotFound ||
					!strings.Contains(ce.Message, `образ "`+tc.want+`" есть в каталоге Cloud.ru, но недоступен проекту`) ||
					!strings.Contains(ce.Hint, "evo image list <запрос>") || !strings.Contains(ce.Hint, "не проверено") {
					t.Fatalf("ошибка: %v (%+v)", err, ce)
				}
				if cloud.createCalled || *fips > 0 || len(cloud.addedRules) > 0 {
					t.Error("облако изменено до отказа")
				}
			})
		}
	}

	// Доступные проекту образы разворачиваются как раньше.
	cloud := catalogForDeploy()
	hideFromProject(cloud, imgAstra)
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Config.ImageID, eng.Config.Image = "", "ubuntu-24.04"
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil || !cloud.createCalled {
		t.Fatalf("Deploy: %v (create %v)", err, cloud.createCalled)
	}
}

// Каталог проекта не прочитался — отказа нет (неизвестно не значит
// недоступно): ответит сам вызов создания.
func TestDeploy_ProjectViewFailsDoesNotRefuse(t *testing.T) {
	cloud := catalogForDeploy() // projectViewFn nil: вызов с project_id= падает
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Config.ImageID = imgUbuntu
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil || !cloud.createCalled {
		t.Fatalf("Deploy: %v (create %v)", err, cloud.createCalled)
	}
}

func TestInit_ImageNotInProjectRefused(t *testing.T) {
	for _, ref := range []string{"ubuntu-24.04", imgUbuntu} {
		cloud := catalogForDeploy()
		hideFromProject(cloud, imgUbuntu)
		eng, _ := newTestEngine(t, cloud, newMockDeployer())
		path := filepath.Join(t.TempDir(), ".evo.yaml")
		err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true, Image: ref})
		if clerrors.Reason(err) != clerrors.CodeNotFound || clerrors.ExitCode(err) != clerrors.ExitNotFound ||
			!strings.Contains(err.Error(), "Ubuntu-24.04") {
			t.Fatalf("init --image %s: %v", ref, err)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("init --image %s: файл создан, хотя образ отклонён", ref)
		}
	}
}
