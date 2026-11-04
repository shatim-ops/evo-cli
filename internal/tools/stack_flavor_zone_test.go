// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// zonedFlavorCloud is raCloud with two flavors: gpu-1 only in ru.AZ-2,
// any-1 with no zones in the answer.
type zonedFlavorCloud struct{ raCloud }

func (zonedFlavorCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{
		{ID: "f-gpu", Name: "gpu-1", Zones: []provision.FlavorZone{{ID: raAZ2, Name: "ru.AZ-2", Enabled: true}}},
		{ID: "f-any", Name: "any-1"},
	}, nil
}

// D67: stack_plan refuses a VM whose flavor is not in its zone, as
// provision does, before anything is created; a flavor of the zone and one
// with no zones in the answer pass.
func TestStackPlan_FlavorOfAnotherZone(t *testing.T) {
	stackHome(t)
	r := raRegistry(t, &raAPI{}, zonedFlavorCloud{raCloud{subnets: raSubnets()}}, nil)
	plan := func(manifest string) (map[string]any, string, bool) {
		var mf map[string]any
		if err := json.Unmarshal([]byte(manifest), &mf); err != nil {
			t.Fatal(err)
		}
		return callNamed(t, r, "stack_plan", map[string]any{"manifest": mf})
	}
	m, text, isErr := plan(`{"name":"s","vm":{"image":"Ubuntu-24.04","zone":"ru.AZ-1","flavor":"gpu-1"}}`)
	if !isErr || !strings.Contains(text, "ru.AZ-2") {
		t.Errorf("a flavor of another zone: %s", text)
	}
	wantReason(t, "gpu-1 in ru.AZ-1", m, text, "USAGE")
	for _, ok := range []string{
		`{"name":"s","vm":{"image":"Ubuntu-24.04","zone":"ru.AZ-2","flavor":"gpu-1"}}`,
		`{"name":"s","vm":{"image":"Ubuntu-24.04","zone":"ru.AZ-1","flavor":"any-1"}}`,
	} {
		if _, text, isErr := plan(ok); isErr {
			t.Errorf("%s: refused: %s", ok, text)
		}
	}
}

// flakyFlavorCloud reads the catalogue once; a second read fails.
type flakyFlavorCloud struct {
	zonedFlavorCloud
	reads *int
}

func (c flakyFlavorCloud) ListFlavors(ctx context.Context) ([]*provision.Flavor, error) {
	*c.reads++
	if *c.reads > 1 {
		return nil, errors.New("API 503")
	}
	return c.zonedFlavorCloud.ListFlavors(ctx)
}

// FB160-19: the zone check of the VM's flavor uses the flavor the
// manifest's validation found — one read of the catalogue. A second read
// that failed used to skip the check silently and plan a VM whose flavor
// is not in its zone.
func TestStackPlan_FlavorZoneCheckIsNotSkippedByASecondRead(t *testing.T) {
	stackHome(t)
	reads := 0
	r := raRegistry(t, &raAPI{}, flakyFlavorCloud{zonedFlavorCloud{raCloud{subnets: raSubnets()}}, &reads}, nil)
	var mf map[string]any
	if err := json.Unmarshal([]byte(`{"name":"s","vm":{"image":"Ubuntu-24.04","zone":"ru.AZ-1","flavor":"gpu-1"}}`), &mf); err != nil {
		t.Fatal(err)
	}
	m, text, _ := callNamed(t, r, "stack_plan", map[string]any{"manifest": mf})
	wantReason(t, "gpu-1 in ru.AZ-1", m, text, "USAGE")
	if reads != 1 {
		t.Errorf("the catalogue was read %d times, want once", reads)
	}
}
