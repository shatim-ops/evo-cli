// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"errors"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// zonedFlavorCloud is metaCloud whose lowcost10-2-4 is only in ru.AZ-1,
// and whose lowcost10-4-8 is in ru.AZ-2, the zone of the plan.
type zonedFlavorCloud struct{ *metaCloud }

func (zonedFlavorCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{
		{ID: "fl-1", Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4, Zones: []provision.FlavorZone{{ID: "az-1", Name: "ru.AZ-1", Enabled: true}}},
		{ID: "fl-2", Name: "lowcost10-4-8", VCPUs: 4, RAMGB: 8, Zones: []provision.FlavorZone{{ID: "00000000-0000-0000-0000-0000000000a2", Name: "ru.AZ-2", Enabled: true}}},
	}, nil
}

// D67 in deploy_plan (Fable, FB160-17): a flavor named out of the plan's
// zone is USAGE before the plan; the automatic pick is the zone's (the
// default lowcost10-2-4 is in another zone).
func TestResolve_FlavorOfTheZone(t *testing.T) {
	cloud := zonedFlavorCloud{&metaCloud{fields: ubuntu2404Fields()}}
	_, err := Resolve(context.Background(), cloud, "proj-1", Params{Name: "web", SSHKey: "k", Zone: "ru.AZ-2", Flavor: "lowcost10-2-4"})
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeUsage {
		t.Errorf("a flavor of another zone: want USAGE, got %v", err)
	}
	plan, err := Resolve(context.Background(), cloud, "proj-1", Params{Name: "web", SSHKey: "k", Zone: "ru.AZ-2"})
	if err != nil || plan.VM.Flavor.Name != "lowcost10-4-8" {
		t.Errorf("auto in ru.AZ-2: %v, %+v", err, plan)
	}
}
