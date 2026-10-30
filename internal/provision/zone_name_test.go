// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"testing"
)

// A subnet and a security group carry their zone as availability_zone
// {id, name} (P-N1): both are read, so a person sees the zone by its name
// beside its id (release plan 1.6, «Линия CLI»).
func TestZoneNameReadWithItsID(t *testing.T) {
	const zone = `"availability_zone":{"id":"z1","name":"ru.AZ-1"}`
	var s Subnet
	var g SecurityGroup
	if err := json.Unmarshal([]byte(`{"id":"s1",`+zone+`}`), &s); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"id":"g1",`+zone+`}`), &g); err != nil {
		t.Fatal(err)
	}
	if s.AvailabilityZoneID != "z1" || s.AvailabilityZoneName != "ru.AZ-1" || g.AvailabilityZoneID != "z1" || g.AvailabilityZoneName != "ru.AZ-1" {
		t.Errorf("subnet zone %q %q, group zone %q %q; want z1 ru.AZ-1", s.AvailabilityZoneID, s.AvailabilityZoneName, g.AvailabilityZoneID, g.AvailabilityZoneName)
	}
}
