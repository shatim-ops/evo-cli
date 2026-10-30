// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// liveWorldDrift are the ways the worlds of internal/engine are known to
// answer otherwise than the platform: «<answer id> (go <world>): …» → the
// work item that makes the world follow it. The list only shrinks: a
// departure not here fails the test, and so does a line no world departs
// by.
// Closes by 1.8: a line lives at most two versions (release plan 1.6,
// «храповики со сроком»; TestMisses_HaveADeadline).
var liveWorldDrift = map[string]string{
	// CMP-25: the world goes with the fix of ZoneNames (internal/provision,
	// TestFlavor_ZonesFromTheLiveAnswer).
	"compute-flavors (go faultCloud): items.[].availability_zones.[].available is sent by the world, not by the platform": "release plan 1.6, «Зоны flavor по живому ответу» (CMP-25): «разработка»",
}

// Release plan 1.6, «Миры из живых ответов», step 2: the worlds of the
// engine that answer through provision.CloudAPI keep to the live form of
// the catalogue (livecatalogue_test.go), as those of internal/cli do.
func TestPlatformAnswers_EngineWorldsKeepTheLiveForm(t *testing.T) {
	ctx := context.Background()
	found := goWorldsFound(t, map[string]map[string]func() (any, error){
		"compute-flavors": {
			"mockCloudAPI":   func() (any, error) { return (&mockCloudAPI{}).ListFlavors(ctx) },
			"interruptCloud": func() (any, error) { return (&interruptCloud{}).ListFlavors(ctx) },
			"faultCloud":     func() (any, error) { return (&faultCloud{}).ListFlavors(ctx) },
		},
		"compute-floating-ips": {
			"faultCloud": func() (any, error) {
				c := &faultCloud{fips: []faultFIP{
					{ID: "fip-1", Name: "fip-web", IP: "203.0.113.9", State: "available"},
					{ID: "fip-2", Name: "fip-db", IP: "203.0.113.10", State: "in_use", VMID: "vm-1"},
				}}
				return c.ListFloatingIPs(ctx, "p")
			},
			"lostBootFIPCloud": func() (any, error) {
				c := &lostBootFIPCloud{}
				_, _ = c.CreateFloatingIP(ctx, provision.CreateFloatingIPRequest{Name: "fip-web-boot"})
				return c.ListFloatingIPs(ctx, "p")
			},
		},
	})
	holdDrift(t, found, liveWorldDrift, func(string) bool { return true })
}
