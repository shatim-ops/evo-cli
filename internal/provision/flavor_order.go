// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"slices"
	"strings"
)

// IsGPU reports a GPU flavor. The compute flavor list carries no GPU field,
// so it goes by the name ("gpu-…", "…-GPU-…").
func (f *Flavor) IsGPU() bool {
	return f != nil && strings.Contains(strings.ToLower(f.Name), "gpu")
}

// SortFlavors orders flavors for people: without GPU first, then by vCPU,
// RAM and name. The API order put the biggest GPU flavors on top, and the
// small ones a first VM needs scrolled away (BACKLOG R-16).
func SortFlavors(fs []*Flavor) {
	slices.SortStableFunc(fs, func(a, b *Flavor) int {
		if ga, gb := a.IsGPU(), b.IsGPU(); ga != gb {
			if ga {
				return 1
			}
			return -1
		}
		if a.VCPUs != b.VCPUs {
			return a.VCPUs - b.VCPUs
		}
		if a.RAMGB != b.RAMGB {
			return a.RAMGB - b.RAMGB
		}
		return strings.Compare(a.Name, b.Name)
	})
}
