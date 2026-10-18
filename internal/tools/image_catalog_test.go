// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Все UUID выдуманы.
const (
	catZone = "00000000-0000-0000-0000-00000000a001"
	catImg1 = "00000000-0000-0000-0000-000000000001"
	catImg2 = "00000000-0000-0000-0000-000000000002"
)

// catalogCloud отдаёт каталог с иконкой и шаблоном — тем, что не должно
// попадать в компактный ответ агенту.
type catalogCloud struct {
	provision.CloudAPI
}

func (catalogCloud) SetToken(string) {}
func (catalogCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: catZone, Name: "ru.AZ-1"}}, nil
}
func (catalogCloud) ListImages(_ context.Context, f ...provision.ImageFilter) ([]*provision.Image, error) {
	all := []*provision.Image{
		// Зона без имени: имя берётся из списка зон.
		{ID: catImg1, Name: "astra_1.8.1", DisplayName: "Astra Linux 1.8.1", Type: "marketplace", MinDisk: 20,
			AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: catZone, State: "created", Size: 123}}},
		{ID: catImg2, Name: "AstraLinuxSmolensk1.8.3KES", DisplayName: "Astra Linux Смоленск 1.8.3 KES", Type: "marketplace",
			AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: catZone, AvailabilityZoneName: "ru.AZ-1", State: "created"}}},
	}
	if len(f) > 0 && f[0].Name == "" && f[0].DisplayName != "" {
		return nil, nil // display_name= ничего нового не нашёл
	}
	return all, nil
}
func (catalogCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, nil
}
func (catalogCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: id, Name: "astra_1.8.1", Type: "marketplace", Icon: "<svg>x</svg>", UserDataTemplate: "#cloud-config",
		MetadataFields: []provision.ImageMetadataField{
			{Name: "name", FieldType: "linux_login", DefaultValue: "user1", MandatoryGroup: "name"},
			{Name: "public_key", FieldType: "public_key", MandatoryGroup: "authentication"},
			{Name: "linux_password", FieldType: "linux_password", MandatoryGroup: "authentication", Sensitive: true, DefaultValue: "default-secret"},
		},
		AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: catZone, AvailabilityZoneName: "ru.AZ-1", State: "created"}}}, nil
}

func catalogRegistry() *Registry {
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: "00000000-0000-0000-0000-0000000000f1", KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   catalogCloud{},
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

func callNamed(t *testing.T, r *Registry, name string, args map[string]any) (map[string]any, string, bool) {
	t.Helper()
	tl, ok := r.Lookup(name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	text, isErr := tl.Handler(context.Background(), args)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("not JSON: %s", text)
	}
	return m, text, isErr
}

// image_list и его алиас list_images: компактные строки — без иконки,
// шаблона и объектов зон; зоны — короткими именами.
func TestImageList_CompactForMCP(t *testing.T) {
	r := catalogRegistry()
	for _, name := range []string{"image_list"} {
		m, text, isErr := callNamed(t, r, name, map[string]any{"query": "astra"})
		if isErr {
			t.Fatalf("%s: %s", name, text)
		}
		for _, bad := range []string{"icon", "user_data_template", "availability_zones", "<svg", "size"} {
			if strings.Contains(text, bad) {
				t.Errorf("%s answer has %q: %s", name, bad, text)
			}
		}
		rows, _ := m["images"].([]any)
		if len(rows) != 2 || m["total"] != float64(2) || m["shown"] != float64(2) {
			t.Fatalf("%s: %s", name, text)
		}
		first := rows[0].(map[string]any)
		if first["name"] != "astra_1.8.1" || first["min_disk_gb"] != float64(20) || first["zones_ready"].([]any)[0] != "AZ-1" {
			t.Errorf("%s row: %v", name, first)
		}
	}
	// list_images keeps its pre-image_list answer (MCP contract): full
	// image objects, zones included, no query argument.
	tl, _ := r.Lookup("list_images")
	if tl.Group != "image" || tl.Mutating {
		t.Errorf("list_images: group %q mutating %v", tl.Group, tl.Mutating)
	}
	m, text, isErr := callNamed(t, r, "list_images", nil)
	if rows, _ := m["images"].([]any); isErr || len(rows) == 0 || !strings.Contains(text, "availability_zones") {
		t.Errorf("list_images changed its answer: %s", text)
	}

	if m, _, isErr := callNamed(t, r, "image_list", map[string]any{"type": "gold"}); !isErr || m["error"] == nil || m["reason"] != clierrors.CodeUsage {
		t.Errorf("bad type: %v", m)
	}
	if m, _, isErr := callNamed(t, r, "image_list", map[string]any{"zone": "AZ-9"}); !isErr || m["reason"] != clierrors.CodeNotFound {
		t.Errorf("unknown zone: %v", m)
	}
}

func TestImageGet_ByNameCompactAndFull(t *testing.T) {
	r := catalogRegistry()
	m, text, isErr := callNamed(t, r, "image_get", map[string]any{"image": "ASTRA_1.8.1"})
	if isErr {
		t.Fatal(text)
	}
	for _, bad := range []string{`"icon"`, "user_data_template", "<svg", "default-secret"} {
		if strings.Contains(text, bad) {
			t.Errorf("default answer has %q: %s", bad, text)
		}
	}
	if _, dup := m["image"].(map[string]any)["image_metadata"]; dup {
		t.Error("default answer repeats image_metadata: fields[] carries it")
	}
	if m["ready"] != true || m["image"].(map[string]any)["id"] != catImg1 {
		t.Errorf("answer: %v", m)
	}
	fields := m["fields"].([]any)
	if f := fields[2].(map[string]any); f["filled_by"] != "evo" || f["role"] != "password" {
		t.Errorf("password field: %v", f)
	}

	_, text, _ = callNamed(t, r, "image_get", map[string]any{"image_id": catImg1, "full": true})
	if !strings.Contains(text, `"icon"`) || !strings.Contains(text, "#cloud-config") || !strings.Contains(text, `"image_metadata"`) {
		t.Errorf("full=true lacks icon/template/image_metadata: %s", text)
	}

	// Неоднозначная ссылка: USAGE и кандидаты для агента.
	m, _, isErr = callNamed(t, r, "image_get", map[string]any{"image": "astra"})
	cands, _ := m["candidates"].([]any)
	if !isErr || m["reason"] != clierrors.CodeUsage || len(cands) != 2 || cands[0].(map[string]any)["id"] == "" {
		t.Errorf("ambiguous: %v", m)
	}
	if m, _, isErr := callNamed(t, r, "image_get", map[string]any{}); !isErr || m["error"] == nil {
		t.Errorf("no image: %v", m)
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (catalogCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}
