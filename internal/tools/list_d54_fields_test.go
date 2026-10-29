// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// catalogueCloud is the fault world whose disk types and floating IPs come
// from the API's JSON as given, decoded as the adapter decodes them.
type catalogueCloud struct {
	*faultCloud
	diskTypes, fips string
}

func (c *catalogueCloud) ListDiskTypes(context.Context, string) ([]*provision.DiskType, error) {
	var out []*provision.DiskType
	return out, json.Unmarshal([]byte(c.diskTypes), &out)
}

func (c *catalogueCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	var out []*provision.FloatingIP
	return out, json.Unmarshal([]byte(c.fips), &out)
}

func rowsByName(t *testing.T, m map[string]any, list, key string) map[string]map[string]any {
	t.Helper()
	by := map[string]map[string]any{}
	rows, _ := m[list].([]any)
	for _, row := range rows {
		o := row.(map[string]any)
		by[fmt.Sprint(o[key])] = o
	}
	return by
}

// D54 before the probes (release plan 1.6, «Пакет отказов D50»; audit
// blind-spots-1.5 §9.1, FB15-6, FB15-18): the lists an agent picks a disk
// type and a floating IP from say what the catalogue says of them — the
// size limits, the zones, the speed by size of a disk type
// (PublicDiskTypeResponse); the zone of an address
// (PublicFloatingIpResponse.availability_zone). Only from the API's fields:
// an answer without them has no such keys (the world of the answers before
// 1.6), and nothing is refused on them yet.
func TestListDiskTypesAndFloatingIPs_SayLimitsAndZones(t *testing.T) {
	cloud := &catalogueCloud{faultCloud: &faultCloud{},
		diskTypes: `[
			{"id":"dt-1","name":"SSD","min_size":1,"max_size":16384,"free_tier":false,
			 "speed_limits":[{"size":100,"limit_bw":200,"limit_rate":25}],
			 "availability_zones":[
			   {"availability_zone_id":"z1","availability_zone_name":"ru.AZ-1","default":true,"enabled":true},
			   {"availability_zone_id":"z2","availability_zone_name":"ru.AZ-2","default":false,"enabled":false}]},
			{"id":"dt-2","name":"HDD"}]`,
		fips: `[
			{"id":"f-1","name":"a","ip_address":"203.0.113.10","state":"available","availability_zone":{"id":"z1","name":"ru.AZ-1"}},
			{"id":"f-2","name":"b","ip_address":"203.0.113.11","state":"available"}]`}
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{Config: config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth: staticToken{}, Cloud: cloud, Printer: output.NewWithWriter(&buf, output.ModeText, false)}, nil
	})

	m, text, isErr := callNamed(t, r, "list_disk_types", map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	dts := rowsByName(t, m, "disk_types", "name")
	ssd := dts["SSD"]
	if ssd["min_size_gb"] != float64(1) || ssd["max_size_gb"] != float64(16384) || ssd["free_tier"] != false ||
		fmt.Sprint(ssd["zones"]) != "[ru.AZ-1]" || fmt.Sprint(ssd["speed_limits"]) != "[map[limit_bw:200 limit_rate:25 size:100]]" {
		t.Errorf("SSD: %v", ssd)
	}
	for _, k := range []string{"min_size_gb", "max_size_gb", "free_tier", "zones", "speed_limits"} {
		if v, ok := dts["HDD"][k]; ok {
			t.Errorf("HDD has %s=%v: the API did not say it", k, v)
		}
	}

	m, text, isErr = callNamed(t, r, "list_floating_ips", map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	fips := rowsByName(t, m, "floating_ips", "id")
	if fips["f-1"]["zone"] != "ru.AZ-1" {
		t.Errorf("f-1: %v, want zone ru.AZ-1", fips["f-1"])
	}
	if v, ok := fips["f-2"]["zone"]; ok {
		t.Errorf("f-2 has zone=%v: the API did not say it", v)
	}
}

// D54 before the probes (FB15-40): k8s_options says the purpose of a
// flavor (FlavorConfiguration.purpose: CONTROLPLANE or DATAPLANE) when the
// catalogue names it, so the agent takes a master flavor for k8s_create and
// a node flavor for k8s_add_nodepool; a flavor without it has no key.
func TestK8sOptions_SaysThePurposeOfAFlavor(t *testing.T) {
	f := &raAPI{flavors: `[
		{"flavorId":"fl-1","name":"gen-2-4","cpu":2,"ram":4,"purpose":"CONTROLPLANE"},
		{"flavorId":"fl-2","name":"gen-4-8","cpu":4,"ram":8}]`}
	r := raRegistry(t, f, raCloud{subnets: raSubnets()}, nil)
	m, text, isErr := callNamed(t, r, "k8s_options", map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	fl := rowsByName(t, m, "flavors", "name")
	if fl["gen-2-4"]["purpose"] != "CONTROLPLANE" {
		t.Errorf("gen-2-4: %v, want purpose CONTROLPLANE", fl["gen-2-4"])
	}
	if v, ok := fl["gen-4-8"]["purpose"]; ok {
		t.Errorf("gen-4-8 has purpose=%v: the catalogue did not say it", v)
	}
}
