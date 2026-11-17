// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"
)

// liveWorldDrift are the ways the worlds of internal/tools are known to
// answer otherwise than the platform: «<answer id> (go <world>): …» → the
// work item that makes the world follow it. The list only shrinks: a
// departure not here fails the test, and so does a line no world departs by.
// Its deadline 1.8 was met; the lines of the card of a backup came with
// cloud-e2e 226 and close with the work item of B (3): Closes by 1.9. A line
// lives at most two versions (release plan 1.6, «храповики со сроком»;
// TestMisses_HaveADeadline).
var liveWorldDrift = map[string]string{
	"compute-backup-get (http restoreWorld): resource.resource_exists is sent by the world, not by the platform":                       "plan 1.9, line of «разработка B (3)» on backup_restore (cloud-e2e 226): the world and restore follow the live card",
	"compute-backup-get (http restoreWorld): resource.resource_state.availability_zone.id is sent by the world, not by the platform":   "plan 1.9, line of «разработка B (3)» on backup_restore (cloud-e2e 226): the world and restore follow the live card",
	"compute-backup-get (http restoreWorld): resource.resource_state.availability_zone.name is sent by the world, not by the platform": "plan 1.9, line of «разработка B (3)» on backup_restore (cloud-e2e 226): the world and restore follow the live card",
}

// Release plan 1.6, «Миры из живых ответов», step 2: the worlds of the
// tools that answer through provision.CloudAPI keep to the live form of the
// catalogue (livecatalogue_test.go), as those of internal/cli do.
func TestPlatformAnswers_ToolsWorldsKeepTheLiveForm(t *testing.T) {
	ctx := context.Background()
	found := goWorldsFound(t, map[string]map[string]func() (any, error){
		"compute-flavors": {
			"planCloud":   func() (any, error) { return planCloud{}.ListFlavors(ctx) },
			"cancelCloud": func() (any, error) { return (&cancelCloud{}).ListFlavors(ctx) },
			"faultCloud":  func() (any, error) { return (&faultCloud{}).ListFlavors(ctx) },
		},
		"compute-floating-ips": {
			"faultCloud": func() (any, error) {
				c := &faultCloud{fips: []faultFIP{
					{ID: "fip-1", Name: "fip-web", IP: "203.0.113.9", State: "available"},
					{ID: "fip-2", Name: "fip-db", IP: "203.0.113.10", State: "in_use", VMID: "vm-1"},
				}}
				return c.ListFloatingIPs(ctx, "p")
			},
		},
	})
	holdDrift(t, found, liveWorldDrift, func(k string) bool { return strings.Contains(k, " (go ") })
}

// The card of a backup the world of backup_restore answers keeps to the
// live form (cloud-e2e 226: the live card names no zone of its disk, the
// world named one as the specification has it, and restore refused live
// while its tests passed — the class «worlds by the specification, not by
// the live form», plan 1.9).
func TestPlatformAnswers_BackupCardKeepsTheLiveForm(t *testing.T) {
	var card platformAnswer
	for _, a := range loadCatalogue(t) {
		if a.ID == "compute-backup-get" {
			card = a
		}
	}
	if card.ID == "" {
		t.Fatal("no line compute-backup-get in the catalogue")
	}
	kinds, strs, err := answerShape([]byte((&restoreWorld{}).backup()))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	checkLiveForm(card, "compute-backup-get (http restoreWorld)", kinds, strs, found)
	holdDrift(t, found, liveWorldDrift, func(k string) bool { return strings.HasPrefix(k, "compute-backup-get ") })
}
