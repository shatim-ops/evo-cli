// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// callTool вызывает обработчик напрямую и разбирает его JSON-ответ. Эти
// проверки не должны доходить до облака: всё, что здесь тестируется, обязано
// отсекаться до первого сетевого вызова.
func callTool(t *testing.T, fn func(context.Context, map[string]any) (string, bool), args map[string]any) map[string]any {
	t.Helper()
	out, _ := fn(context.Background(), args)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("ответ не JSON: %q", out)
	}
	return m
}

func TestImageCreateRequiresArgs(t *testing.T) {
	s := newTestServer()
	for _, args := range []map[string]any{
		{},
		{"name": "img"},    // без диска
		{"disk_id": "d-1"}, // без имени
	} {
		got := callTool(t, s.handleImageCreate, args)
		if !strings.Contains(got["error"].(string), "required") {
			t.Fatalf("на %v ждали отказ по обязательным полям, получили %v", args, got)
		}
	}
}

// Создание образа — платная операция, поэтому подтверждение обязано
// запрашиваться ДО обращения к облаку, а не после.
func TestImageCreateAsksConfirmationBeforeCloud(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleImageCreate, map[string]any{
		"name": "img", "disk_id": "00000000-0000-0000-0000-000000000000",
	})
	if got["status"] != "confirmation_required" {
		t.Fatalf("без confirmed ждали запрос подтверждения, получили %v", got)
	}
	if _, ok := got["will_create"]; !ok {
		t.Fatal("в запросе подтверждения нет описания того, что будет создано")
	}
}

// Превью — это то, на основании чего оператор одобряет платное действие, и оно
// не должно называть источник зоны, которого может не быть: регион в конфиге
// задан не всегда, и тогда зона берётся с исходного диска.
func TestImageCreatePreviewDoesNotPromiseConfiguredRegion(t *testing.T) {
	s := newTestServer()

	got := callTool(t, s.handleImageCreate, map[string]any{
		"name": "img", "disk_id": "00000000-0000-0000-0000-000000000000",
	})
	will, _ := got["will_create"].(map[string]any)
	zone, _ := will["zone"].(string)
	if zone == "configured region" {
		t.Fatal("превью обещает настроенный регион, хотя его может не быть")
	}
	if !strings.Contains(zone, "disk") {
		t.Fatalf("превью не упоминает запасной вариант — зону диска: %q", zone)
	}

	// Явно заданная зона должна возвращаться как есть.
	got = callTool(t, s.handleImageCreate, map[string]any{
		"name": "img", "disk_id": "00000000-0000-0000-0000-000000000000",
		"zone": "ru.AZ-2",
	})
	will, _ = got["will_create"].(map[string]any)
	if will["zone"] != "ru.AZ-2" {
		t.Fatalf("запрошенная зона искажена в превью: %v", will["zone"])
	}
}

// Платформа оставляет верхнеуровневый status пустым и держит настоящее
// состояние сборки по зонам. Ответ обязан отвечать на вопрос «образ готов?»
// сам, иначе каждый вызывающий обязан знать эту причуду — и ошибётся.
func TestZoneStatesSummarisesBuildState(t *testing.T) {
	img := &provision.ImageDetail{
		Status: "",
		AvailZones: []provision.ImageAvailZone{
			{AvailabilityZoneName: "ru.AZ-2", State: "created"},
			{AvailabilityZoneID: "az-3-uuid", State: "creating"},
		},
	}

	if !img.Ready() {
		t.Fatal("образ со зоной created обязан считаться готовым")
	}
	got := zoneStates(img)
	if got["ru.AZ-2"] != "created" {
		t.Errorf("состояние зоны по имени потеряно: %v", got)
	}
	// У зоны без имени остаётся идентификатор — молча терять её нельзя.
	if got["az-3-uuid"] != "creating" {
		t.Errorf("зона без имени выпала из сводки: %v", got)
	}

	none := &provision.ImageDetail{}
	if none.Ready() {
		t.Error("образ без единой зоны не может быть готовым")
	}
	if len(zoneStates(none)) != 0 {
		t.Error("для образа без зон сводка должна быть пустой")
	}
}

func TestImageDeleteAsksConfirmation(t *testing.T) {
	// The preview looks the image up first (D26), so it needs a cloud.
	text := liveDelete(t, "image_delete", map[string]any{"image_id": liveID})
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "confirmation_required" {
		t.Fatalf("удаление образа без confirmed должно требовать подтверждения, получили %v", got)
	}
}

func TestImageGetAndFipAttachValidateArgs(t *testing.T) {
	s := newTestServer()
	if got := callTool(t, s.handleImageGet, map[string]any{}); got["error"] == nil {
		t.Fatal("image_get без image_id должен отказать")
	}
	if got := callTool(t, s.handleFipAttach, map[string]any{"fip": "1.2.3.4"}); got["error"] == nil {
		t.Fatal("fip_attach без vm должен отказать")
	}
	if got := callTool(t, s.handleFipAttach, map[string]any{"vm": "x", "fip": "1.2.3.4"}); got["status"] != "confirmation_required" {
		t.Fatalf("fip_attach меняет ВМ — ждали подтверждения, получили %v", got)
	}
}

// Платформа требует sensitive у каждого поля метаданных. omitempty выбрасывал
// его при false, и создание образа падало с 422 — тест закрепляет,
// что ключ присутствует всегда.
func TestImageMetadataInputAlwaysSendsSensitive(t *testing.T) {
	blob, err := json.Marshal(provision.ImageMetadataInput{
		Name: "public_key", DisplayName: "SSH-ключ", DisplayPosition: 1, Sensitive: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"sensitive":false`) {
		t.Fatalf("поле sensitive обязано уходить даже при false: %s", blob)
	}
	for _, must := range []string{`"display_name"`, `"display_position"`} {
		if !strings.Contains(string(blob), must) {
			t.Fatalf("платформа требует %s в метаданных: %s", must, blob)
		}
	}
	// Схема на запись не принимает поля схемы на чтение.
	for _, forbidden := range []string{`"id"`, `"is_required"`} {
		if strings.Contains(string(blob), forbidden) {
			t.Fatalf("поле %s запрещено при создании образа: %s", forbidden, blob)
		}
	}
}

// Диск обязан быть указан, а имя — непустым: обе проверки живут в адаптере,
// чтобы негодный запрос не уходил в облако.
func TestCreateImageReqValidatedLocally(t *testing.T) {
	a := &provision.CloudRuAdapter{}
	if _, err := a.CreateImage(context.Background(), provision.CreateImageReq{Name: "x"}); err == nil {
		t.Fatal("без disk_id должна быть локальная ошибка")
	}
	if _, err := a.CreateImage(context.Background(), provision.CreateImageReq{DiskID: "d"}); err == nil {
		t.Fatal("без name должна быть локальная ошибка")
	}
}
