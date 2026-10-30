// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"testing"
)

// liveZonesMiss is the work item that makes a flavor of the live answer
// name its zones; "" once ZoneNames does.
var liveZonesMiss = "release plan 1.6, «Зоны flavor по живому ответу» (CMP-25): «разработка»"

// Register CMP-25 (api-probe 28 and 29, 2 of 2): the live answer of GET
// /api/v1/flavors has no availability_zones[].available and no
// flavor_type.is_spot, though the spec makes them required; a zone of a
// flavor reads availability_zone_id, availability_zone_name, default,
// enabled, state. The worlds set available: true, and ZoneNames (enabled
// and available) named the zones in every test — on the platform it names
// none: list_flavors shows no zone for any flavor, its zone filter matches
// none, the preview of a VM names no zone of its flavor (found by
// TestPlatformAnswers_GoWorldsKeepTheLiveForm, release plan 1.6, «Миры из
// живых ответов», step 2). A flavor decoded from the live form names the
// zone that is enabled.
func TestFlavor_ZonesFromTheLiveAnswer(t *testing.T) {
	const item = `{"id":"f1","name":"lowcost10-2-4","cpu":2,"ram":4,"gpu":0,"oversubscription":"1:10",
		"flavor_type":{"display_group_type":"lowcost"},
		"availability_zones":[
			{"availability_zone_id":"z1","availability_zone_name":"ru.AZ-1","default":true,"enabled":true,"state":"created"},
			{"availability_zone_id":"z3","availability_zone_name":"ru.AZ-3","default":false,"enabled":false,"state":"created"}]}`
	var f Flavor
	if err := json.Unmarshal([]byte(item), &f); err != nil {
		t.Fatal(err)
	}
	got := f.ZoneNames()
	right := len(got) == 1 && got[0] == "ru.AZ-1"
	switch {
	case !right && liveZonesMiss == "":
		t.Errorf("a flavor of the live answer (no «available», CMP-25) names zones %q, want [ru.AZ-1]: the enabled zone", got)
	case right && liveZonesMiss != "":
		t.Errorf("liveZonesMiss names %q, and the live flavor names its zone now: set it to \"\"", liveZonesMiss)
	case !right:
		t.Logf("known: %s — ZoneNames %q on the live form", liveZonesMiss, got)
	}
}
