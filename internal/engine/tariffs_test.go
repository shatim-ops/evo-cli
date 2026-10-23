// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// allResources are the resources of a CreatePreview (the constants). A new
// one goes here and in tariffs, or this test fails.
var allResources = []string{ResourceVM, ResourceDisk, ResourceDiskResize, ResourcePGCluster,
	ResourceK8sCluster, ResourceK8sNodePool, ResourceK8sAddon, ResourceS3Bucket, ResourceARRegistry, ResourceCAApp,
	ResourceKafka, ResourceLB, ResourceRedis, ResourceSNAT}

func TestTariffs_EveryResource(t *testing.T) {
	if len(tariffs) != len(allResources) {
		t.Errorf("tariffs has %d entries, the resources are %d", len(tariffs), len(allResources))
	}
	for _, r := range allResources {
		if u := tariffs[r]; !strings.HasPrefix(u, tariffsBase) || len(u) == len(tariffsBase) {
			t.Errorf("%s: tariff %q, want a page under %s", r, u, tariffsBase)
		}
	}
}

// A preview evo cannot price says the resource is billable and where its
// rates are, on every surface (the answer's cost_note and tariff_url, the
// CLI's line): «не оценивается: у evo нет тарифа» read as «free» before a
// paid cluster was created (dogfood 1.2.1).
func TestPrice_UnpricedLinksTariff(t *testing.T) {
	for _, r := range []string{ResourcePGCluster, ResourceK8sCluster, ResourceK8sNodePool, ResourceK8sAddon, ResourceS3Bucket, ResourceDiskResize,
		ResourceARRegistry, ResourceCAApp, ResourceKafka, ResourceLB, ResourceRedis, ResourceSNAT} {
		p := CreatePreview{Resource: r, Name: "x"}
		p.Price()
		if p.Cost != nil || p.TariffURL != tariffs[r] || !strings.Contains(p.CostNote, "tariff_url") || strings.Contains(p.CostNote, "no rate card") {
			t.Errorf("%s: cost %v, tariff %q, note %q", r, p.Cost, p.TariffURL, p.CostNote)
		}
		line := strings.Join(createCostLines(p), "\n")
		if !strings.Contains(line, tariffs[r]) || strings.Contains(line, "no rate card") {
			t.Errorf("%s: cost line %q", r, line)
		}
	}
	// A priced preview has no tariff link: the price is the answer.
	p := CreatePreview{Resource: ResourceDisk, Disk: &deployplan.DiskSpec{SizeGB: 10, Type: "SSD"}}
	p.Price()
	if p.Cost == nil || p.TariffURL != "" || p.CostNote != costNote {
		t.Errorf("disk: cost %v, tariff %q, note %q", p.Cost, p.TariffURL, p.CostNote)
	}
}
