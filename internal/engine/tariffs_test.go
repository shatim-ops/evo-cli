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
	ResourceKafka, ResourceLB, ResourceRedis, ResourceSNAT, ResourceDNSZone, ResourceImage, ResourcePGBackup,
	ResourceS3Object, ResourceSecret, ResourceStack, ResourceSubnet, ResourceTargetGroup, ResourceVPC, ResourceVPCRoute}

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
		ResourceARRegistry, ResourceCAApp, ResourceKafka, ResourceLB, ResourceRedis, ResourceSNAT, ResourceDNSZone, ResourceS3Object} {
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

// A resource no line of an Evolution tariff names (an image, a secret, a
// VPC and its route, a PostgreSQL backup, a target group) links the nearest
// tariff page and says the line is not there, rather than a price the
// person would take for the tariff's: a wrong link is worse than none
// (release-plan 1.3, A5). Counted as billable (owner's decision 22.10). A stack points to stack_plan, its resources.
func TestPrice_NoTariffLineSaysSo(t *testing.T) {
	for r, want := range map[string]string{ResourceImage: noTariffLine, ResourceSecret: noTariffLine, ResourceSubnet: noTariffLine, ResourceVPC: noTariffLine,
		ResourceVPCRoute: noTariffLine, ResourcePGBackup: noTariffLine, ResourceTargetGroup: noTariffLine, ResourceStack: stackNotPriced} {
		p := CreatePreview{Resource: r, Name: "x"}
		p.Price()
		if p.Cost != nil || p.TariffURL != tariffs[r] || p.CostNote != want {
			t.Errorf("%s: cost %v, tariff %q, note %q", r, p.Cost, p.TariffURL, p.CostNote)
		}
		line := strings.Join(createCostLines(p), "\n")
		if !strings.Contains(line, tariffs[r]) || (want == noTariffLine) != strings.Contains(line, "no line") {
			t.Errorf("%s: cost line %q", r, line)
		}
	}
}
