// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sizer

import (
	"math"
	"slices"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const headroomFactor = 1.3 // 30% headroom

// FlavorMatch is the result of auto-sizing.
type FlavorMatch struct {
	Flavor   provision.Flavor
	Headroom float64 // e.g., 1.3 = 30% headroom applied
}

// Match selects the smallest flavor that satisfies the resource requirements
// (with 30% headroom): the first sufficient flavor, smallest first
// (provision.CompareFlavorSize: ties by name and id).
// If no flavor is sufficient, it returns nil.
func Match(resources compose.Resources, flavors []provision.Flavor) *FlavorMatch {
	// Apply minimum defaults when no limits are specified.
	cpuCores := resources.CPUCores
	memMB := resources.MemoryMB
	if cpuCores <= 0 && memMB <= 0 {
		cpuCores = 1.0
		memMB = 1024
	}

	// Apply headroom.
	requiredCPU := int(math.Ceil(cpuCores * headroomFactor))
	requiredMemMB := int(math.Ceil(float64(memMB) * headroomFactor))

	sorted := slices.Clone(flavors)
	slices.SortFunc(sorted, func(a, b provision.Flavor) int { return provision.CompareFlavorSize(&a, &b) })

	for _, f := range sorted {
		if f.VCPUs >= requiredCPU && f.RAMGB*1024 >= requiredMemMB {
			return &FlavorMatch{
				Flavor:   f,
				Headroom: headroomFactor,
			}
		}
	}
	return nil
}
