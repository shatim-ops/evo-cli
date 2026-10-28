// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"slices"
	"testing"
)

// Release plan 1.5, С5 (Fable audit 1.5, FB15-1, FB15-3): a flavor of the
// catalogue says the guaranteed share of its vCPU, its GPUs, whether its
// type is spot and the zones it is in (FlavorSchema of the compute spec);
// evo read none of it, and lowcost10-2-4 looked the same as a 1:1 flavor
// of 2 vCPU. What the answer does not carry stays unknown — no field, no
// claim.
func TestFlavor_ReadsShareGPUSpotZones(t *testing.T) {
	const item = `{"id":"f1","name":"lowcost10-2-4","cpu":2,"ram":4,"gpu":0,"oversubscription":"1:10",
		"free_tier":false,"flavor_type":{"name":"lowcost","is_spot":true,"display_group_type":"spot"},
		"availability_zones":[
			{"availability_zone_id":"z1","availability_zone_name":"ru.AZ-1","enabled":true,"available":true,"default":true,"state":"created"},
			{"availability_zone_id":"z2","availability_zone_name":"ru.AZ-2","enabled":true,"available":false,"default":false,"state":"created"},
			{"availability_zone_id":"z3","availability_zone_name":"ru.AZ-3","enabled":false,"available":true,"default":false,"state":"created"}]}`
	var f Flavor
	if err := json.Unmarshal([]byte(item), &f); err != nil {
		t.Fatal(err)
	}
	if f.Share != "1:10" || f.GPU == nil || *f.GPU != 0 || !f.Spot() {
		t.Errorf("share %q, gpu %v, spot %v: want 1:10, 0, true", f.Share, f.GPU, f.Spot())
	}
	if got := f.ZoneNames(); !slices.Equal(got, []string{"ru.AZ-1"}) {
		t.Errorf("zones %v, want [ru.AZ-1]: a zone not enabled or not available is not one to create in", got)
	}
	// An answer without the fields: nothing is claimed.
	var bare Flavor
	if err := json.Unmarshal([]byte(`{"id":"f2","name":"x","cpu":2,"ram":4}`), &bare); err != nil {
		t.Fatal(err)
	}
	if bare.Share != "" || bare.GPU != nil || bare.Spot() || bare.ZoneNames() != nil {
		t.Errorf("a flavor without the fields claims share %q, gpu %v, spot %v, zones %v", bare.Share, bare.GPU, bare.Spot(), bare.ZoneNames())
	}
}
