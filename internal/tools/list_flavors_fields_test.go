// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Release plan 1.5, С5 (FB15-1, FB15-3): list_flavors says what the
// catalogue says of a flavor beyond its size — the share of a vCPU, GPUs,
// spot, the zones — only from the API's fields (a flavor without them
// has no such keys), and takes the filters an agent sizes with.
func TestListFlavors_ShareGPUSpotZonesAndFilters(t *testing.T) {
	zone := func(name string, ok bool) provision.FlavorZone {
		return provision.FlavorZone{ID: "id-" + name, Name: name, Enabled: true, Available: ok}
	}
	gpus := func(n int) *int { return &n }
	cloud := &faultCloud{flavors: []*provision.Flavor{
		{ID: "f1", Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4, Share: "1:10", GPU: gpus(0), Type: &provision.FlavorType{},
			Zones: []provision.FlavorZone{zone("ru.AZ-1", true), zone("ru.AZ-2", false)}},
		{ID: "f2", Name: "gen-2-4", VCPUs: 2, RAMGB: 4, Share: "1:1", GPU: gpus(0), Type: &provision.FlavorType{},
			Zones: []provision.FlavorZone{zone("ru.AZ-1", true), zone("ru.AZ-2", true)}},
		{ID: "f3", Name: "spot-2-4", VCPUs: 2, RAMGB: 4, Share: "1:1", GPU: gpus(0), Type: &provision.FlavorType{IsSpot: true}},
		{ID: "f4", Name: "gpu-8-64", VCPUs: 8, RAMGB: 64, Share: "1:1", GPU: gpus(1)},
		{ID: "f5", Name: "bare-2-4", VCPUs: 2, RAMGB: 4},
	}}
	r, _ := faultToolsRegistry(t, cloud)
	names := func(args map[string]any) (string, map[string]map[string]any) {
		m, text, isErr := callNamed(t, r, "list_flavors", args)
		if isErr {
			t.Fatalf("list_flavors %v: %s", args, text)
		}
		rows, _ := m["flavors"].([]any)
		by := map[string]map[string]any{}
		var list []string
		for _, row := range rows {
			o := row.(map[string]any)
			by[o["name"].(string)] = o
			list = append(list, o["name"].(string))
		}
		return fmt.Sprint(list), by
	}
	_, all := names(map[string]any{})
	if f := all["lowcost10-2-4"]; f["cpu_share"] != "1:10" || f["spot"] != false || fmt.Sprint(f["zones"]) != "[ru.AZ-1]" {
		t.Errorf("lowcost10-2-4: %v, want cpu_share 1:10, spot false, zones [ru.AZ-1]", f)
	}
	if f := all["gpu-8-64"]; f["gpu"] != float64(1) {
		t.Errorf("gpu-8-64: %v, want gpu 1", f)
	}
	if f := all["spot-2-4"]; f["spot"] != true {
		t.Errorf("spot-2-4: %v, want spot true", f)
	}
	for _, k := range []string{"cpu_share", "spot", "zones", "gpu"} {
		if v, ok := all["bare-2-4"][k]; ok {
			t.Errorf("bare-2-4 has %s=%v: the API did not say it", k, v)
		}
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"share": "1:10"}, "[lowcost10-2-4]"},
		{map[string]any{"zone": "ru.AZ-2"}, "[gen-2-4]"},
		{map[string]any{"gpu": true}, "[gpu-8-64]"},
		{map[string]any{"cpu": float64(2), "ram": float64(4), "share": "1:1", "gpu": false}, "[gen-2-4 spot-2-4]"},
		{map[string]any{"spot": false, "share": "1:1"}, "[gen-2-4]"},
	} {
		if got, _ := names(c.args); got != c.want {
			t.Errorf("list_flavors %v = %s, want %s", c.args, got, c.want)
		}
	}
}
