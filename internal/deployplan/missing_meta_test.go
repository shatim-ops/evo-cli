// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// deploy_plan: обязательное поле образа, которое evo не заполнит, — нет
// плана (значит, deploy_apply ничего не создаст); image_metadata его
// заполняет и уходит в запрос создания (шаг C).
func TestResolve_MissingFieldNoPlan(t *testing.T) {
	fields := append(ubuntu2404Fields(), provision.ImageMetadataField{Name: "admin_email", FieldType: "text", MandatoryGroup: "product"})
	cloud := &metaCloud{fields: fields, cancel: func() {}}
	_, err := Resolve(context.Background(), cloud, "proj-1", Params{Name: "web", SSHKey: "ssh-ed25519 AAAA agent"})
	ce, ok := err.(*clierrors.CLIError)
	if !ok || ce.Code != clierrors.CodeUsage || !strings.Contains(ce.Message, "admin_email") ||
		!strings.Contains(ce.Hint, `image_metadata {"admin_email"`) {
		t.Fatalf("ошибка: %#v", err)
	}

	_, err = Resolve(context.Background(), cloud, "proj-1", Params{Name: "web", Meta: map[string]string{"nosuch": "x"}})
	if ce, ok := err.(*clierrors.CLIError); !ok || ce.Code != clierrors.CodeUsage || !strings.Contains(ce.Hint, "admin_email") {
		t.Fatalf("неизвестное поле: %#v", err)
	}

	plan, req, _ := resolveAndApply(t, fields, Params{Name: "web", SSHKey: "ssh-ed25519 AAAA agent",
		Meta: map[string]string{"admin_email": "ops@example.test"}})
	if plan.MetaStatic["admin_email"] != "ops@example.test" || req.ImageMetadata["admin_email"] != "ops@example.test" {
		t.Errorf("план %v, запрос %v", plan.MetaStatic, req.ImageMetadata)
	}
}

// astraCloud — каталог, в котором есть только Astra Linux.
type astraCloud struct{ metaCloud }

func (*astraCloud) ListImages(context.Context, ...provision.ImageFilter) ([]*provision.Image, error) {
	return []*provision.Image{{ID: "img-a", Name: "astra_1.8.1", DisplayName: "Astra Linux SE 1.8.1"}}, nil
}

// deploy_plan с compose_file на Astra Linux (Альт, РЕД ОС): Docker туда не
// встанет — плана нет, как без обязательного поля; без compose_file — это
// обычная ВМ, план строится.
func TestResolve_NoDockerOSWithComposeNoPlan(t *testing.T) {
	cloud := &astraCloud{metaCloud{fields: ubuntu2404Fields(), cancel: func() {}}}
	plan, err := Resolve(context.Background(), cloud, "proj-1", Params{Name: "web", Image: "astra_1.8.1", ComposeFile: "services: {}"})
	ce, ok := err.(*clierrors.CLIError)
	if plan != nil || !ok || ce.Code != clierrors.CodeUsage || !strings.Contains(ce.Message, "astra_1.8.1") ||
		!strings.Contains(ce.Message, "download.docker.com") || !strings.Contains(ce.Message, "Astra Linux") {
		t.Fatalf("ошибка: %#v", err)
	}
	plan, err = Resolve(context.Background(), cloud, "proj-1", Params{Name: "web", Image: "astra_1.8.1"})
	if err != nil || plan == nil || plan.VM.Image.Name != "astra_1.8.1" {
		t.Fatalf("без compose_file: %v", err)
	}
}
