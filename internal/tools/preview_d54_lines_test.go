// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// minImageCloud is the fault world whose image names its minimums.
type minImageCloud struct {
	*faultCloud
	minCPU, minRAM int
}

func (c *minImageCloud) GetImage(ctx context.Context, id string) (*provision.ImageDetail, error) {
	img, err := c.faultCloud.GetImage(ctx, id)
	if img != nil {
		img.MinCPU, img.MinRAM = c.minCPU, c.minRAM
	}
	return img, err
}

func vmPreviewRegistry(t *testing.T, cloud provision.CloudAPI) *Registry {
	t.Helper()
	faultToolsIsolate(t)
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{ProjectID: faultProject, Region: faultZone, ImageID: faultImage,
				Flavor: "low-1-1", DiskSize: 10, KeyID: "k", Secret: "s"},
			Auth: staticToken{}, Cloud: cloud, Printer: output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir, SSHKeyPEM: []byte("fake-key"), SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings: faultToolsTimings(),
		}, nil
	})
}

func previewParams(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	p, _ := m["preview"].(map[string]any)
	out := map[string]any{}
	params, _ := p["params"].([]any)
	for _, x := range params {
		kv := x.(map[string]any)
		out[kv["key"].(string)] = kv["value"]
	}
	return out
}

// D54 before the probes (audit blind-spots-1.5 §9.1, FB15-2, FB15-5): the
// preview of a VM says the zones its flavor can be created in and the
// minimum of its image, from the catalogue's fields, so the person sees a
// flavor of another zone or under the image's minimum before «yes». Only
// what the catalogue says: a flavor without zones, an image without
// minimums add no line. Nothing is refused on them before the probes
// P35, P36.
func TestVMPreview_SaysFlavorZonesAndImageMinimum(t *testing.T) {
	zone := func(name string, ok bool) provision.FlavorZone {
		return provision.FlavorZone{ID: "id-" + name, Name: name, Enabled: true, Available: &ok}
	}
	withZones := &minImageCloud{faultCloud: &faultCloud{flavors: []*provision.Flavor{{ID: faultFlavor, Name: "low-1-1", VCPUs: 1, RAMGB: 1,
		Zones: []provision.FlavorZone{zone("ru.AZ-1", true), zone("ru.AZ-2", false)}}}}, minCPU: 2, minRAM: 4}
	m, text, _ := callNamed(t, vmPreviewRegistry(t, withZones), "provision", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("provision: %s", text)
	}
	got := previewParams(t, m)
	if fmt.Sprint(got["flavor_zones"]) != "[ru.AZ-1]" || got["image_min_cpu"] != float64(2) || got["image_min_ram_gb"] != float64(4) {
		t.Errorf("params %v: want flavor_zones [ru.AZ-1], image_min_cpu 2, image_min_ram_gb 4", got)
	}
	human := strings.Join(engine.ShowCreatePreview("provision", previewOf(t, m)).Lines, "\n")
	for _, want := range []string{i18n.T("flavor zones", "зоны flavor"), "ru.AZ-1", i18n.T("image minimum", "минимум образа")} {
		if !strings.Contains(human, want) {
			t.Errorf("the human preview does not say %q:\n%s", want, human)
		}
	}

	bare := &minImageCloud{faultCloud: &faultCloud{flavors: []*provision.Flavor{{ID: faultFlavor, Name: "low-1-1", VCPUs: 1, RAMGB: 1}}}}
	m, text, _ = callNamed(t, vmPreviewRegistry(t, bare), "provision", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("provision, bare catalogue: %s", text)
	}
	for _, k := range []string{"flavor_zones", "image_min_cpu", "image_min_ram_gb"} {
		if v, ok := previewParams(t, m)[k]; ok {
			t.Errorf("bare catalogue: %s=%v, the catalogue did not say it", k, v)
		}
	}
}

// previewOf is the CreatePreview of a confirmation_required answer.
func previewOf(t *testing.T, m map[string]any) engine.CreatePreview {
	t.Helper()
	var p engine.CreatePreview
	b, _ := json.Marshal(m["preview"])
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// tgAPI answers the list of target groups (empty) for tg_create's check
// of the name, the rest as raAPI.
type tgAPI struct{ raAPI }

func (f *tgAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet && r.URL.Path == "/v2/targetGroups" {
		rec := httptest.NewRecorder()
		_, _ = io.WriteString(rec, `{"target_groups":[]}`)
		return rec.Result(), nil
	}
	return f.raAPI.RoundTrip(r)
}

// ifaceCloud is a VM whose primary interface is of type typ.
type ifaceCloud struct {
	raCloud
	typ string
}

func (c ifaceCloud) GetVMDetail(_ context.Context, id string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: id, Interfaces: []provision.VMInterface{{ID: "nic-1", Primary: true, Type: c.typ}}}, nil
}

// D54 before the probe P41 (FB15-21): the preview of tg_create names the
// type of the interface it resolved for a target given by its VM — a
// public (direct_ip) VM is not the private target a balancer needs — and
// the body it sends stays as it was. A target given with its interface_id
// is not read, and an interface without a type says nothing.
func TestTgCreatePreview_SaysTheInterfaceType(t *testing.T) {
	const vm = "00000000-0000-4000-8000-0000000000e7"
	for typ, want := range map[string]string{"direct_ip": "direct_ip", "regular": "regular", "": ""} {
		r := raRegistry(t, &tgAPI{}, ifaceCloud{typ: typ}, nil)
		m, text, _ := callNamed(t, r, "tg_create", map[string]any{"name": "tg", "vm_id": vm})
		if m["status"] != "confirmation_required" {
			t.Fatalf("type %q: %s", typ, text)
		}
		got, ok := previewParams(t, m)["interface_types"]
		switch {
		case want == "" && ok:
			t.Errorf("type %q: interface_types %v, the answer named no type", typ, got)
		case want != "" && fmt.Sprint(got) != fmt.Sprintf("map[%s:%s]", vm, want):
			t.Errorf("type %q: interface_types %v", typ, got)
		}
		for _, tg := range m["targets"].([]any) {
			if _, leaked := tg.(map[string]any)["type"]; leaked {
				t.Errorf("type %q: the target spec sent to the API carries the type: %v", typ, tg)
			}
		}
	}
}
