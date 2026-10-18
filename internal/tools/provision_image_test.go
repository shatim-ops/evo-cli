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

// provision: image_metadata, проверка обязательных полей до изменений в
// облаке и ports (шаг C). ID выдуманы.

// productCloud — образ с обязательным полем продукта, которое evo не
// заполнит сам; любая мутация считается.
type productCloud struct {
	cancelCloud
	mutations []string
}

func (*productCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: "img-1", Name: "Product-1", MetadataFields: []provision.ImageMetadataField{
		{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
		{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
		{Name: "admin_email", FieldType: "text", MandatoryGroup: "product", MandatoryGroupDisplayName: "Продукт"},
		{Name: "vmm_enabled", FieldType: "flag", DefaultValue: "true"},
	}}, nil
}
func (c *productCloud) CreateFloatingIP(ctx context.Context, r provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.mutations = append(c.mutations, "CreateFloatingIP")
	return c.cancelCloud.CreateFloatingIP(ctx, r)
}
func (c *productCloud) CreateVM(ctx context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.mutations = append(c.mutations, "CreateVM")
	return c.cancelCloud.CreateVM(ctx, req)
}

func TestProvision_MissingFieldRefusedBeforeAnyMutation(t *testing.T) {
	cloud := &productCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	r, _ := cancelRegistry(t, cloud)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr {
		t.Fatalf("ждали отказ: %s", text)
	}
	var res map[string]any
	_ = json.Unmarshal([]byte(text), &res)
	if res["reason"] != "USAGE" || !strings.Contains(res["error"].(string), "admin_email") ||
		!strings.Contains(res["hint"].(string), `image_metadata {"admin_email"`) {
		t.Errorf("ответ: %s", text)
	}
	if len(cloud.mutations) > 0 {
		t.Errorf("до проверки полей облако изменено: %v", cloud.mutations)
	}

	// Значение из image_metadata (число — как текст) снимает отказ.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud = &productCloud{cancelCloud: cancelCloud{cancel: cancel}}
	r, _ = cancelRegistry(t, cloud)
	r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true,
		"image_metadata": map[string]any{"admin_email": "ops@example.test", "name": 42.0}})
	if cloud.creates != 1 {
		t.Fatalf("CreateVM не вызван: %v", cloud.mutations)
	}
	meta := cloud.createReq.ImageMetadata
	if meta["admin_email"] != "ops@example.test" || meta["name"] != "42" {
		t.Errorf("image_metadata = %v", meta)
	}
}

func TestProvision_ArgumentRefusals(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"image и image_id": {"image": "Ubuntu-24.04", "image_id": "img-1"},
		"не объект":        {"image_metadata": "admin_email=x"},
		"вложенный объект": {"image_metadata": map[string]any{"a": map[string]any{}}},
		"плохой порт":      {"ports": []any{"http"}},
		"неизвестное поле": {"image_metadata": map[string]any{"nosuch": "x", "admin_email": "a"}},
	} {
		t.Run(name, func(t *testing.T) {
			cloud := &productCloud{cancelCloud: cancelCloud{cancel: func() {}}}
			r, _ := cancelRegistry(t, cloud)
			args["confirmed"] = true
			text, isErr := r.handlers["provision"](context.Background(), args)
			if !isErr || !strings.Contains(text, "USAGE") {
				t.Errorf("ответ: %s", text)
			}
			if len(cloud.mutations) > 0 {
				t.Errorf("облако изменено: %v", cloud.mutations)
			}
		})
	}
}

// Превью без подтверждения: образ по имени, порты и имена полей (не
// значения) — без обращения к облаку.
func TestProvision_PreviewEchoesImagePortsFieldNames(t *testing.T) {
	r, _ := cancelRegistry(t, &productCloud{})
	text, _ := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "image": "Product 1",
		"ports": []any{80.0, "53/udp"}, "image_metadata": map[string]any{"admin_email": "secret@example.test"}})
	if strings.Contains(text, "secret@example.test") {
		t.Errorf("значение поля в превью: %s", text)
	}
	var res struct {
		Status     string         `json:"status"`
		WillCreate map[string]any `json:"will_create"`
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatal(err)
	}
	w := res.WillCreate
	if res.Status != "confirmation_required" || w["image"] != "Product 1" ||
		strings.Join(toStrings(w["ports"]), ",") != "80,53/udp" || strings.Join(toStrings(w["image_metadata_fields"]), ",") != "admin_email" {
		t.Errorf("will_create: %v", w)
	}
}

func toStrings(v any) []string {
	a, _ := v.([]any)
	out := make([]string, 0, len(a))
	for _, x := range a {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}
