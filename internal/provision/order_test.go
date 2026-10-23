// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"slices"
	"testing"
)

// The disk type without a name: SSD, else a type with "ssd" in its name,
// else any — never the first of the list; ties by name, then id, in either
// order of the list.
func TestPickDiskType(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types []*DiskType
		want  string
		got   string // id
	}{
		{"SSD over the first", []*DiskType{{ID: "1", Name: "HDD"}, {ID: "2", Name: "ssd-nvme"}, {ID: "3", Name: "SSD"}}, "", "3"},
		{"no SSD: a type with ssd in its name, not HDD", []*DiskType{{ID: "1", Name: "HDD"}, {ID: "2", Name: "ssd-nvme"}, {ID: "3", Name: "fast-SSD"}}, "", "3"},
		{"no ssd at all: the first by name", []*DiskType{{ID: "1", Name: "nvme"}, {ID: "2", Name: "HDD"}}, "", "2"},
		{"the type named, case aside", []*DiskType{{ID: "1", Name: "SSD"}, {ID: "2", Name: "HDD"}}, "hdd", "2"},
		{"same name: the smaller id", []*DiskType{{ID: "b", Name: "SSD"}, {ID: "a", Name: "SSD"}}, "", "a"},
	} {
		for _, rev := range []bool{false, true} {
			types := slices.Clone(tc.types)
			if rev {
				slices.Reverse(types)
			}
			if got := PickDiskType(types, tc.want); got == nil || got.ID != tc.got {
				t.Errorf("%s (reversed=%v): got %+v, want id %s", tc.name, rev, got, tc.got)
			}
		}
	}
	if PickDiskType(nil, "") != nil {
		t.Error("no types: want nil")
	}
}

// Flavors smallest first; equal sizes by name, then id.
func TestCompareFlavorSize(t *testing.T) {
	fs := []*Flavor{
		{ID: "b", Name: "x-2-4", VCPUs: 2, RAMGB: 4},
		{ID: "c", Name: "y-2-2", VCPUs: 2, RAMGB: 2},
		{ID: "a", Name: "x-2-4", VCPUs: 2, RAMGB: 4},
		{ID: "d", Name: "a-1-8", VCPUs: 1, RAMGB: 8},
	}
	slices.SortFunc(fs, CompareFlavorSize)
	var ids []string
	for _, f := range fs {
		ids = append(ids, f.ID)
	}
	if want := []string{"d", "c", "a", "b"}; !slices.Equal(ids, want) {
		t.Errorf("order %v, want %v", ids, want)
	}
}

// Subnets of the same rank: by name, then id, whatever the order of the list.
func TestPickSubnet_TieByName(t *testing.T) {
	subnets := []*Subnet{
		{ID: "2", Name: "net-b", AvailabilityZoneID: "z"},
		{ID: "1", Name: "net-a", AvailabilityZoneID: "z"},
		{ID: "3", Name: "net-c", AvailabilityZoneID: "z", Default: true},
	}
	for _, rev := range []bool{false, true} {
		s := slices.Clone(subnets[:2])
		if rev {
			slices.Reverse(s)
		}
		if i := PickSubnet(s, "z", ""); i < 0 || s[i].ID != "1" {
			t.Errorf("reversed=%v: picked %d", rev, i)
		}
	}
	if i := PickSubnet(subnets, "z", ""); subnets[i].ID != "3" {
		t.Errorf("the zone's default subnet ranks first: picked %s", subnets[i].ID)
	}
}
