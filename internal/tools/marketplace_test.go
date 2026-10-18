// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// marketplace_list на поддельных каталогах: образы — как ищет сервер (с
// project_id= — без недоступных проекту), плагины — fakeMK8s (две
// страницы). Все UUID выдуманы.

type marketCloud struct {
	provision.CloudAPI
	err error
}

func (marketCloud) SetToken(string) {}
func (c marketCloud) ListImages(_ context.Context, f ...provision.ImageFilter) ([]*provision.Image, error) {
	if c.err != nil {
		return nil, c.err
	}
	ready := []provision.ImageAvailZone{{AvailabilityZoneID: catZone, AvailabilityZoneName: "ru.AZ-1", State: "created"}}
	all := []*provision.Image{
		{ID: catImg1, Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Type: "public", AvailZones: ready},
		{ID: catImg2, Name: "AstraLinuxSmolensk1.8.3KES", DisplayName: "Astra Linux Смоленск 1.8.3 KES", Type: "marketplace", AvailZones: ready},
		{ID: "00000000-0000-0000-0000-000000000003", Name: "nginx-ubuntu-22.04", DisplayName: "Nginx", Type: "marketplace", AvailZones: ready},
	}
	var out []*provision.Image
	for _, img := range all {
		if len(f) > 0 && f[0].ProjectID != "" && img.ID == catImg2 {
			continue // каталог, каким его видит проект, — без Astra
		}
		cp := *img
		out = append(out, &cp)
	}
	return out, nil
}
func (marketCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return nil, stderrors.New("the project's own images must not be read")
}

// downMK8s — mk8s отвечает 500 на всё.
type downMK8s struct{}

func (downMK8s) RoundTrip(*http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	http.Error(rec, `{"message":"internal"}`, 500)
	return rec.Result(), nil
}

func marketRegistry(t *testing.T, cloud marketCloud, rt http.RoundTripper) *Registry {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: addonProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   cloud,
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

func marketItems(t *testing.T, m map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, r := range m["items"].([]any) {
		row := r.(map[string]any)
		out[row["name"].(string)] = row
	}
	return out
}

func TestMarketplaceList_Answer(t *testing.T) {
	r := marketRegistry(t, marketCloud{}, &fakeMK8s{})
	tl, _ := r.Lookup("marketplace_list")
	if tl.Group != "image" || tl.Mutating {
		t.Errorf("group %q mutating %v", tl.Group, tl.Mutating)
	}
	m, text, isErr := callNamed(t, r, "marketplace_list", nil)
	if isErr {
		t.Fatal(text)
	}
	t.Log(text)
	// Ubuntu, nginx и три плагина; Astra проекту недоступна — скрыта.
	if m["shown"] != float64(5) || m["total"] != float64(6) || m["hidden"] != float64(1) || m["hidden_not_in_project"] != float64(1) {
		t.Errorf("counters: %s", text)
	}
	counts := m["counts"].(map[string]any)
	if counts["vm-os"] != float64(1) || counts["vm-app"] != float64(1) || counts["k8s-addon"] != float64(3) {
		t.Errorf("counts: %v", counts)
	}
	if !strings.Contains(m["note"].(string), "not available to this project") || m["warnings"] != nil {
		t.Errorf("note/warnings: %s", text)
	}
	rows := m["items"].([]any)
	if rows[0].(map[string]any)["name"] != "Ubuntu-24.04" || rows[1].(map[string]any)["name"] != "nginx-ubuntu-22.04" || rows[2].(map[string]any)["name"] != "cert-manager" {
		t.Errorf("order: %s", text)
	}
	items := marketItems(t, m)
	ub := items["Ubuntu-24.04"]
	if ub["kind"] != "vm-os" || ub["id"] != catImg1 || ub["available_in_project"] != true || ub["billable"] != nil || ub["ready"] != nil {
		t.Errorf("ubuntu row: %v", ub)
	}
	if d := ub["deploy"].(map[string]any); d["tool"] != "provision" || d["args"].(map[string]any)["image"] != "Ubuntu-24.04" || d["cli"] != "evo vm create <vm-name> --image Ubuntu-24.04" {
		t.Errorf("ubuntu deploy: %v", d)
	}
	gpu := items["gpu-operator"]
	if gpu["kind"] != "k8s-addon" || gpu["billable"] != true || gpu["available_in_project"] != nil || gpu["id"] != nil {
		t.Errorf("addon row: %v", gpu)
	}
	if d := gpu["deploy"].(map[string]any); d["tool"] != "k8s_add_addon" || d["args"].(map[string]any)["name"] != "gpu-operator" || d["cli"] != "evo k8s add-addon <cluster-id> gpu-operator" {
		t.Errorf("addon deploy: %v", d)
	}
	if items["cert-manager"]["billable"] != false {
		t.Errorf("free addon: %v", items["cert-manager"])
	}

	// all=true: Astra — с available_in_project=false и без deploy.
	m, _, _ = callNamed(t, r, "marketplace_list", map[string]any{"all": true, "kind": "vm-os"})
	items = marketItems(t, m)
	astra := items["AstraLinuxSmolensk1.8.3KES"]
	if len(items) != 2 || astra["kind"] != "vm-os" || astra["available_in_project"] != false || astra["deploy"] != nil {
		t.Errorf("all, vm-os: %v", m)
	}

	if m, _, isErr := callNamed(t, r, "marketplace_list", map[string]any{"kind": "app"}); !isErr || m["reason"] != clierrors.CodeUsage {
		t.Errorf("bad kind: %v", m)
	}
}

// Каталог плагинов недоступен — образы всё равно в ответе, с warnings и
// без ошибки; недоступны оба — ошибка.
func TestMarketplaceList_PartialFailure(t *testing.T) {
	r := marketRegistry(t, marketCloud{}, downMK8s{})
	m, text, isErr := callNamed(t, r, "marketplace_list", nil)
	if isErr {
		t.Fatal(text)
	}
	warnings, _ := m["warnings"].([]any)
	if m["shown"] != float64(2) || len(warnings) != 1 || !strings.Contains(warnings[0].(string), "addon catalogue was not read") {
		t.Errorf("addons down: %s", text)
	}
	// Только плагины — ошибка.
	if _, text, isErr := callNamed(t, r, "marketplace_list", map[string]any{"kind": "k8s-addon"}); !isErr {
		t.Errorf("only addons, down: %s", text)
	}

	r = marketRegistry(t, marketCloud{err: clierrors.New(clierrors.CodeAPIUnreachable, "compute unreachable", "")}, &fakeMK8s{})
	m, text, isErr = callNamed(t, r, "marketplace_list", nil)
	if isErr || m["shown"] != float64(3) || m["warnings"] == nil {
		t.Errorf("images down: %s", text)
	}

	r = marketRegistry(t, marketCloud{err: clierrors.New(clierrors.CodeAPIUnreachable, "compute unreachable", "")}, downMK8s{})
	m, text, isErr = callNamed(t, r, "marketplace_list", nil)
	if !isErr || m["reason"] != clierrors.CodeAPIUnreachable || !strings.Contains(text, "compute unreachable") || !strings.Contains(text, "addon catalogue") {
		t.Errorf("both down: %s", text)
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (marketCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}
