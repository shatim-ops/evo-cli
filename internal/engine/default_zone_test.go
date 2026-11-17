// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// DefaultZone names a zone only when the list marks it default (or use
// declares it) and it takes new resources: a refusal does not offer the
// first zone of the API's order, nor a closed one.
func TestDefaultZone_OnlyAMarkedOpenZone(t *testing.T) {
	open, closed := true, false
	az1 := &provision.AvailabilityZone{Name: "ru.AZ-1", Enabled: &open}
	az2 := &provision.AvailabilityZone{Name: "ru.AZ-2", Enabled: &open, Default: true}
	az2closed := &provision.AvailabilityZone{Name: "ru.AZ-2", Enabled: &closed, Default: true}
	for name, tc := range map[string]struct {
		zones []*provision.AvailabilityZone
		want  string
	}{
		"marked":         {[]*provision.AvailabilityZone{az1, az2}, "ru.AZ-2"},
		"none marked":    {[]*provision.AvailabilityZone{az1}, ""},
		"marked, closed": {[]*provision.AvailabilityZone{az1, az2closed}, ""},
		"empty list":     {nil, ""},
	} {
		got := ""
		if z := DefaultZone(tc.zones, ZoneVM); z != nil {
			got = z.Name
		}
		if got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}
