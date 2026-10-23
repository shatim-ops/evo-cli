// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"cmp"
	"strings"
)

// The same command on the same cloud picks the same subnet, flavor and disk
// type whatever order the cloud lists them in (owner's decision 23.10): a
// pick ranks its candidates by what it wants, and candidates that tie go
// by name, then by id — never "the first of the list".

// CompareByNameID orders two candidates that tie on what a pick wants: by
// name, then by id.
func CompareByNameID(aName, aID, bName, bID string) int {
	return cmp.Or(strings.Compare(aName, bName), strings.Compare(aID, bID))
}

// CompareFlavorSize orders flavors smallest first: vCPUs, then RAM, then
// by name and id.
func CompareFlavorSize(a, b *Flavor) int {
	return cmp.Or(cmp.Compare(a.VCPUs, b.VCPUs), cmp.Compare(a.RAMGB, b.RAMGB),
		CompareByNameID(a.Name, a.ID, b.Name, b.ID))
}

// DefaultDiskType is the disk type a VM or a disk gets unless one is named.
const DefaultDiskType = "SSD"

// PickDiskType is the disk type for want (DefaultDiskType when ""): the
// type of that name (case aside), else one whose name has "ssd", else any;
// ties by name and id. nil when there are no types.
func PickDiskType(types []*DiskType, want string) *DiskType {
	if want == "" {
		want = DefaultDiskType
	}
	rank := func(t *DiskType) int {
		switch {
		case strings.EqualFold(t.Name, want):
			return 2
		case strings.Contains(strings.ToLower(t.Name), "ssd"):
			return 1
		}
		return 0
	}
	var best *DiskType
	for _, t := range types {
		if best == nil {
			best = t
			continue
		}
		if r, rb := rank(t), rank(best); r > rb || r == rb && CompareByNameID(t.Name, t.ID, best.Name, best.ID) < 0 {
			best = t
		}
	}
	return best
}
