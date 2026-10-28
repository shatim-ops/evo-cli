// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Dogfood 28.10: every refusal the call shows without a write comes in one
// answer, not one per run (no zone, then no image, then no flavor). The
// first, in the order of the checks, gives reason, error and hint, so the
// exit code does not change; refusals names each, and missing each
// argument a setting lacks. Axis 1 of CLAUDE.md §6.
func TestProvision_AllRefusalsInOneAnswer(t *testing.T) {
	cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", KeyID: "k", Secret: "s"})
	m, text, isErr := callNamed(t, r, "provision", map[string]any{"name": "web"})
	refusals, _ := m["refusals"].([]any)
	missing, _ := m["missing"].([]any)
	if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "no image is set") || len(refusals) != 2 ||
		len(missing) != 2 || missing[0] != "image" || missing[1] != "region" {
		t.Fatalf("no image and no zone: want both in one answer, the image's first: %s", text)
	}
	second, _ := refusals[1].(map[string]any)
	if second["reason"] != "CONFIG_INVALID" || !strings.Contains(second["error"].(string), "no availability zone is set") || second["hint"] == nil {
		t.Errorf("the second refusal: %v", second)
	}

	// What the reads find: the image the catalogue lacks and a zone
	// without a subnet of the project — both, the image's first (it was
	// the only one before).
	shelf := &shelfZoneCloud{
		otherZoneCloud: otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}},
		images:         []*provision.Image{{ID: catalogUbuntu24, Name: "Ubuntu-24.04", DisplayName: "Ubuntu 24.04", Status: "active"}}}
	r = zoneRegistry(t, shelf, config.Config{ProjectID: "p", Region: "z", KeyID: "k", Secret: "s"})
	m, text, isErr = callNamed(t, r, "provision", map[string]any{"name": "web", "image": "NoSuch-1.0"})
	refusals, _ = m["refusals"].([]any)
	if !isErr || m["reason"] != "NOT_FOUND" || len(refusals) != 2 || !strings.Contains(text, "no subnet") {
		t.Errorf("unknown image, zone without a subnet: want both in one answer, the image's first: %s", text)
	}
}
