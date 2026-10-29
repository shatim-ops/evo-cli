// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Дерево image (ревью r5 U6, дизайн образов шаг B) на поддельном каталоге.
// Все UUID выдуманы.

const (
	imgZone1 = "00000000-0000-0000-0000-00000000a001"
	imgZone2 = "00000000-0000-0000-0000-00000000a002"
	imgZone3 = "00000000-0000-0000-0000-00000000a003"
)

func imgID(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

// imageCloud — каталог, который ищет как сервер (подстрока без учёта
// регистра в name= и display_name=, type=). С project_id= — каталог, каким
// его видит проект: без образов hidden (по id); projErr — ошибка такого
// вызова.
type imageCloud struct {
	provision.CloudAPI
	shared  []*provision.Image
	detail  map[string]*provision.ImageDetail
	hidden  map[string]bool
	projErr *error
}

func (imageCloud) SetToken(string) {}
func (imageCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: imgZone1, Name: "ru.AZ-1"}, {ID: imgZone2, Name: "ru.AZ-2"}, {ID: imgZone3, Name: "ru.AZ-3"}}, nil
}
func (c imageCloud) ListImages(_ context.Context, filter ...provision.ImageFilter) ([]*provision.Image, error) {
	var f provision.ImageFilter
	if len(filter) > 0 {
		f = filter[0]
	}
	if f.ProjectID != "" && c.projErr != nil && *c.projErr != nil {
		return nil, *c.projErr
	}
	var out []*provision.Image
	for _, img := range c.shared {
		if f.ProjectID != "" && c.hidden[img.ID] {
			continue
		}
		if f.Name != "" && !strings.Contains(strings.ToLower(img.Name), strings.ToLower(f.Name)) ||
			f.DisplayName != "" && !strings.Contains(strings.ToLower(img.DisplayName), strings.ToLower(f.DisplayName)) ||
			f.Type != "" && img.Type != f.Type {
			continue
		}
		cp := *img
		out = append(out, &cp)
	}
	return out, nil
}
func (imageCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, nil
}
func (c imageCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	if d, ok := c.detail[id]; ok {
		cp := *d
		return &cp, nil
	}
	for _, img := range c.shared {
		if img.ID == id {
			return &provision.ImageDetail{ID: id, Name: img.Name, DisplayName: img.DisplayName, Type: img.Type, AvailZones: img.AvailZones}, nil
		}
	}
	return nil, clerrors.New(clerrors.CodeNotFound, "resource not found: GET /api/v1/images/"+id, "")
}

func azs(states ...string) []provision.ImageAvailZone {
	ids := []string{imgZone1, imgZone2, imgZone3}
	var out []provision.ImageAvailZone
	for i, s := range states {
		if s != "" {
			out = append(out, provision.ImageAvailZone{AvailabilityZoneID: ids[i], AvailabilityZoneName: fmt.Sprintf("ru.AZ-%d", i+1), State: s})
		}
	}
	return out
}

func newImageCloud() imageCloud {
	ubuntuFields := []provision.ImageMetadataField{
		{Name: "name", DisplayName: "Логин", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
		{Name: "public_key", DisplayName: "Ключ", FieldType: "public_key", MandatoryGroup: " authentication"},
		{Name: "linux_password", DisplayName: "Пароль", FieldType: "linux_password", MandatoryGroup: "authentication", Sensitive: true},
		{Name: "hostname", DisplayName: "Имя хоста", FieldType: "hostname", MandatoryGroup: "hostname"},
		{Name: "vmm_enabled", FieldType: "flag", DefaultValue: "true"},
	}
	c := imageCloud{
		shared: []*provision.Image{
			{ID: imgID(1), Name: "AstraLinuxSmolensk1.8.3KES", DisplayName: "Astra Linux Смоленск 1.8.3 KES", Type: "marketplace", MinDisk: 10, AvailZones: azs("created", "created", "created")},
			{ID: imgID(2), Name: "alse-base-1-7-7", DisplayName: "Astra Linux SE «Базовый» 1.7.7", Type: "marketplace", MinDisk: 15, AvailZones: azs("created", "created", "")},
			{ID: imgID(3), Name: "astra-voronej", DisplayName: "astra-voronej-cc11079", Type: "marketplace", AvailZones: azs("error", "", "")},
			{ID: imgID(4), Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Type: "public", MinDisk: 10, AvailZones: azs("created", "created", "created")},
			{ID: imgID(5), Name: "OpensearchDashboards", DisplayName: "OpenSearch Dashboards", Type: "marketplace", MinDisk: 40, AvailZones: azs("", "created", "")},
		},
		detail:  map[string]*provision.ImageDetail{},
		hidden:  map[string]bool{},
		projErr: new(error),
	}
	c.detail[imgID(1)] = &provision.ImageDetail{
		ID: imgID(1), Name: "AstraLinuxSmolensk1.8.3KES", DisplayName: "Astra Linux Смоленск 1.8.3 KES", Type: "marketplace",
		Description: "Astra Linux Special Edition «Смоленск» 1.8.3 с антивирусом Kaspersky Endpoint Security. " + strings.Repeat("Длинное описание. ", 30),
		MinCPU:      2, MinRAM: 4, MinDisk: 10, Icon: "<svg>icon</svg>", UserDataTemplate: "#cloud-config\nusers: [{{ name }}]",
		MetadataFields: ubuntuFields, AvailZones: azs("created", "created", "creating"),
	}
	c.detail[imgID(5)] = &provision.ImageDetail{
		ID: imgID(5), Name: "OpensearchDashboards", DisplayName: "OpenSearch Dashboards", Type: "marketplace", MinDisk: 40,
		MetadataFields: append(append([]provision.ImageMetadataField{}, ubuntuFields...),
			provision.ImageMetadataField{Name: "opensearch_host", FieldType: "text", MandatoryGroup: "opensearch"},
			provision.ImageMetadataField{Name: "opensearch_password", FieldType: "plain_password", Sensitive: true, DefaultValue: "not-shown-default"}),
		AvailZones: azs("", "created", ""),
	}
	return c
}

func withImageCloud(t *testing.T) imageCloud {
	t.Helper()
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	t.Setenv("CLOUDRU_REGION", "")
	cloud := newImageCloud()
	old := toolEngine
	stoppedClock(t)
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth, e.Cloud = previewToken{}, cloud
		return e, nil
	}
	t.Cleanup(func() { toolEngine = old })
	return cloud
}

func TestImageList_RussianTable(t *testing.T) {
	russian(t)
	withImageCloud(t)
	out, errOut, code := execCLI(t, "image", "list", "astra")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"ИМЯ                         ОТОБРАЖАЕМОЕ ИМЯ                ТИП          ЗОНЫ            МИН. ДИСК\n",
		"alse-base-1-7-7             Astra Linux SE «Базовый» 1.7.7  marketplace  AZ-1,AZ-2       15 ГБ\n",
		"AstraLinuxSmolensk1.8.3KES  Astra Linux Смоленск 1.8.3 KES  marketplace  AZ-1,AZ-2,AZ-3  10 ГБ\n",
		"Показаны 2 из 3 (не готовы ни в одной зоне: 1; показать — --all). UUID — в --json или 'evo image get <имя>'.\n",
	})
	if strings.Contains(out, "astra-voronej") || strings.Contains(out, imgID(1)) {
		t.Errorf("hidden image or UUID in the table:\n%s", out)
	}

	out, _, _ = execCLI(t, "image", "list", "astra", "--all")
	assertHas(t, out, []string{"astra-voronej  ", "Показаны 3 из 3. UUID"})

	out, _, _ = execCLI(t, "image", "list", "--type", "public")
	assertHas(t, out, []string{"Ubuntu-24.04", "Показаны 1 из 1."})

	out, _, _ = execCLI(t, "image", "list", "--zone", "AZ-3")
	if strings.Contains(out, "alse-base") || !strings.Contains(out, "Ubuntu-24.04") {
		t.Errorf("--zone AZ-3:\n%s", out)
	}

	out, _, code = execCLI(t, "image", "list", "windows")
	if code != 0 || !strings.Contains(out, "Образы не найдены.") {
		t.Errorf("nothing found: exit %d\n%s", code, out)
	}

	if _, errOut, code := execCLI(t, "image", "list", "--type", "gold"); code != clerrors.ExitGeneral || !strings.Contains(errOut, "type") {
		t.Errorf("bad --type: exit %d %s", code, errOut)
	}
}

// --json — ответ инструмента как есть: компактные строки без иконки,
// шаблона и объектов зон.
func TestImageList_JSONIsCompact(t *testing.T) {
	withImageCloud(t)
	out, errOut, code := execCLI(t, "--json", "image", "list", "astra")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got struct {
		Images []map[string]any `json:"images"`
		Shown  int              `json:"shown"`
		Total  int              `json:"total"`
		Hidden int              `json:"hidden"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got.Shown != 2 || got.Total != 3 || got.Hidden != 1 || len(got.Images) != 2 {
		t.Fatalf("counts: %+v", got)
	}
	allowed := map[string]bool{"id": true, "name": true, "display_name": true, "type": true, "zones_ready": true, "min_disk_gb": true}
	for _, row := range got.Images {
		for k := range row {
			if !allowed[k] {
				t.Errorf("unexpected key %q in a compact row: %v", k, row)
			}
		}
	}
	if got.Images[1]["id"] != imgID(1) || fmt.Sprint(got.Images[1]["zones_ready"]) != "[AZ-1 AZ-2 AZ-3]" {
		t.Errorf("row: %v", got.Images[1])
	}
}

func TestImageGet_RussianCard(t *testing.T) {
	russian(t)
	withImageCloud(t)
	out, errOut, code := execCLI(t, "image", "get", "astralinuxsmolensk1.8.3kes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"Имя:              AstraLinuxSmolensk1.8.3KES\n",
		"Отображаемое имя: Astra Linux Смоленск 1.8.3 KES\n",
		"Тип:              marketplace\n",
		"UUID:             " + imgID(1) + "\n",
		"Описание:         Astra Linux Special Edition «Смоленск» 1.8.3",
		"…\n",
		"Минимум:          2 vCPU, 4 ГБ RAM, диск 10 ГБ\n",
		"Зоны:             AZ-1, AZ-2 — готов; AZ-3: creating\n",
		"Поля при создании ВМ:\n",
		"  name            linux_login     да            по умолчанию: user1\n",
		"  public_key      public_key      да            evo: ключ SSH\n",
		"  linux_password  linux_password  да            evo: случайный пароль\n",
		"  hostname        hostname        да            evo: имя ВМ\n",
		"  vmm_enabled     flag                          служебный флаг, не передаётся\n",
		"Создать ВМ:\n  evo vm create <имя-вм> --image AstraLinuxSmolensk1.8.3KES\n",
		"flavor — не меньше минимума выше",
	})
	for _, bad := range []string{"<svg", "cloud-config", "\x00", "probe"} {
		if strings.Contains(out, bad) {
			t.Errorf("card shows %q:\n%s", bad, out)
		}
	}

	// Обязательное поле продукта — от человека; секретный default не виден.
	out, _, code = execCLI(t, "image", "get", "OpenSearch Dashboards")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	assertHas(t, out, []string{
		"  opensearch_host      text            да            ВЫ: обязательное, evo его не заполнит\n",
		"  opensearch_password  plain_password                значение образа по умолчанию\n",
		"Нужны значения, которых evo сам не знает: opensearch_host — передайте их: --meta поле=значение.\n",
		"  evo vm create <имя-вм> --image OpensearchDashboards --meta opensearch_host=<значение>\n",
	})
	if strings.Contains(out, "not-shown-default") {
		t.Errorf("a secret default is shown:\n%s", out)
	}
}

func TestImageGet_JSONAndErrors(t *testing.T) {
	withImageCloud(t)
	out, errOut, code := execCLI(t, "--json", "image", "get", imgID(1))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	card, _ := got["image"].(map[string]any)
	if _, ok := card["icon"]; ok {
		t.Error("icon without --full")
	}
	if _, ok := card["user_data_template"]; ok {
		t.Error("user_data_template without --full")
	}
	if got["ready"] != true || fmt.Sprint(got["zones_ready"]) != "[AZ-1 AZ-2]" || fmt.Sprint(got["fields_required"]) != "[]" {
		t.Errorf("answer: %v", got)
	}
	create, _ := got["create"].(map[string]any)
	if create["cli"] != "evo vm create <vm-name> --image AstraLinuxSmolensk1.8.3KES" || create["tool"] != "provision" {
		t.Errorf("create: %v", create)
	}
	fields, _ := got["fields"].([]any)
	if len(fields) != 5 || fields[1].(map[string]any)["filled_by"] != "evo" {
		t.Errorf("fields: %v", fields)
	}

	out, _, _ = execCLI(t, "--json", "image", "get", imgID(1), "--full")
	if !strings.Contains(out, `"icon"`) || !strings.Contains(out, "cloud-config") {
		t.Errorf("--full lacks icon/template: %s", out)
	}

	// Неоднозначно — USAGE (1) с кандидатами и подсказкой; не найдено —
	// NOT_FOUND (4) с подсказкой поиска. Через run: так печатает main.
	russian(t)
	setupCLI(t)
	withImageCloud(t)
	code, _, errOut = runCLI(t, "", "image", "get", "astra")
	if code != clerrors.ExitGeneral {
		t.Fatalf("ambiguous: exit %d\n%s", code, errOut)
	}
	t.Logf("\n%s", errOut)
	assertHas(t, errOut, []string{
		"образ \"astra\" неоднозначен: совпадений — 2\n",
		"  alse-base-1-7-7 — Astra Linux SE «Базовый» 1.7.7 — marketplace\n",
		"  AstraLinuxSmolensk1.8.3KES — Astra Linux Смоленск 1.8.3 KES — marketplace\n",
		"Укажите точное имя или UUID; все совпадения: evo image list astra\n",
	})
	code, _, errOut = runCLI(t, "", "image", "get", "windows-2022")
	if code != clerrors.ExitNotFound || !strings.Contains(errOut, "Найдите образ: evo image list windows-2022") {
		t.Errorf("not found: exit %d\n%s", code, errOut)
	}
	code, _, errOut = runCLI(t, "", "--json", "image", "get", imgID(99))
	if code != clerrors.ExitNotFound || !strings.Contains(errOut, `"reason":"NOT_FOUND"`) {
		t.Errorf("unknown UUID: exit %d\n%s", code, errOut)
	}
}

// Старая команда работает, скрыта и подсказывает новую — в --json строкой
// JSON в stderr, stdout остаётся данными.
func TestListImages_DeprecatedStillWorks(t *testing.T) {
	russian(t)
	withImageCloud(t)
	out, errOut, code := execCLI(t, "list-images")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "AstraLinuxSmolensk1.8.3KES") || !strings.Contains(errOut, "evo list-images устарела: используйте 'evo image list [запрос]'") {
		t.Errorf("stdout:\n%s\nstderr:\n%s", out, errOut)
	}
	if strings.Count(strings.TrimSpace(errOut), "\n") != 0 {
		t.Errorf("the hint must be one line:\n%s", errOut)
	}

	out, errOut, code = execCLI(t, "--json", "list-images")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list) != 5 {
		t.Errorf("stdout is not the image list: %v\n%s", err, out)
	}
	var warn map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(errOut)), &warn); err != nil || !strings.Contains(warn["warning"], "evo image list") {
		t.Errorf("stderr: %v %q", err, errOut)
	}

	root := newRootCmd()
	if c, _, err := root.Find([]string{"list-images"}); err != nil || !c.Hidden {
		t.Error("list-images must be hidden")
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (imageCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}

// ListDiskTypes: превью ВМ ищет UUID типа диска для расчёта цены.
func (imageCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	return []*provision.DiskType{{ID: "00000000-0000-0000-0000-0000000000e6", Name: "SSD"}}, nil
}
