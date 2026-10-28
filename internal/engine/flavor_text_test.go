// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// Release plan 1.5, С5 (FB15-1, FB15-3): the flavor of a VM preview says
// the guaranteed share of a vCPU, the GPUs and spot when the catalogue
// says them, and nothing of them when it does not (no field, no line).
func TestFlavorText_ShareGPUSpotFromTheCatalogue(t *testing.T) {
	got := flavorText(deployplan.FlavorSpec{Name: "spot-gpu-8-64", VCPUs: 8, RAMGB: 64, Share: "1:10", GPU: 1, Spot: true})
	for _, want := range []string{"1:10", "GPU", "spot"} {
		if !strings.Contains(got, want) {
			t.Errorf("flavorText = %q, want %q in it", got, want)
		}
	}
	bare := flavorText(deployplan.FlavorSpec{Name: "x-2-4", VCPUs: 2, RAMGB: 4})
	for _, never := range []string{":", "GPU", "spot"} {
		if strings.Contains(bare, never) {
			t.Errorf("flavorText = %q claims %q the catalogue did not say", bare, never)
		}
	}
}
