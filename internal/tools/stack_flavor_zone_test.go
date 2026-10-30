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
