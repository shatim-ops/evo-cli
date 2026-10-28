// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sizer"
)

// Release plan 1.5, С3 (with С5): the catalogue of the shared worlds is
// what the automatic picks of evo choose from — the flavor of provision and
// deploy (engine.PickVMFlavor), of deploy with a compose file (sizer.Match),
// the disk type (provision.PickDiskType); deploy_plan's own pick is held by
// the previews that name lowcost10-2-4. When С5 reads the new fields of a
// flavor and a disk type (the share of a vCPU, GPU, spot, the zones, the
// limits of a disk), the base of the worlds gets values that change no
// pick, and this test stays as it is:
//
//   - a flavor: oversubscription as its name says (lowcost10-* is 1:10,
//     the rest 1:1), gpu 0, is_spot false, every zone of the world
//     enabled and available;
//   - a disk type: min 1 GB, max 16384 GB, the zone of the world enabled.
//
// What limits a pick (a flavor sold out in the zone, a disk too small for
// its type) is a variant of consentWorlds, never the base: the base answers
// every test that does not ask about it (the lesson of W8, D50: a variant
// sets its value explicitly).
func TestWorldCatalogue_AutomaticPicksStay(t *testing.T) {
	ctx := context.Background()
	flavors, err := previewCloud{}.ListFlavors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	values := make([]provision.Flavor, 0, len(flavors))
	for _, f := range flavors {
		byID[f.ID] = f.Name
		values = append(values, *f)
	}
	id, err := engine.PickVMFlavor(flavors, "")
	if got := byID[id]; err != nil || got != "lowcost10-2-4" {
		t.Errorf("provision and deploy without a flavor pick %q (%v), want lowcost10-2-4", got, err)
	}
	for _, c := range []struct {
		name string
		res  compose.Resources
		want string
	}{
		{"no limits", compose.Resources{}, "lowcost10-2-4"},
		{"2 vCPU, 4 GB", compose.Resources{CPUCores: 2, MemoryMB: 4096}, "lowcost10-4-8"},
	} {
		m := sizer.Match(c.res, values)
		if got := ""; m == nil || m.Flavor.Name != c.want {
			if m != nil {
				got = m.Flavor.Name
			}
			t.Errorf("deploy with a compose file (%s) picks %q, want %s", c.name, got, c.want)
		}
	}
	types, err := previewCloud{}.ListDiskTypes(ctx, pvZone)
	if err != nil {
		t.Fatal(err)
	}
	if dt := provision.PickDiskType(types, ""); dt == nil || dt.Name != "SSD" {
		t.Errorf("the disk type without one asked is %v, want SSD", dt)
	}
}
