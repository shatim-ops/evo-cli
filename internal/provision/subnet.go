// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import "strings"

// PickSubnet picks the subnet for a new VM among the zone's subnets: the
// one the API marks as the zone's main subnet (default), then a routed
// one (a floating IP binds only there), then by name and id. Technical
// subnets and those not in a usable state come last. The only subnet of
// the project is taken when none is in the zone only if its zone is
// unknown: a subnet of another zone does not boot the VM (error_creating
// with a paid floating IP), so there is no blind "first one". -1 when
// nothing fits.
func PickSubnet(subnets []*Subnet, zoneID, zoneName string) int {
	best := -1
	for i, s := range subnets {
		if !SubnetInZone(s, zoneID, zoneName) {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		b := subnets[best]
		if r, rb := subnetRank(s), subnetRank(b); r > rb || r == rb && CompareByNameID(s.Name, s.ID, b.Name, b.ID) < 0 {
			best = i
		}
	}
	if best < 0 && len(subnets) == 1 && subnets[0].AvailabilityZoneID == "" {
		return 0
	}
	return best
}

// subnetRank orders the zone's subnets for PickSubnet.
func subnetRank(s *Subnet) int {
	switch s.Type {
	case "technical", "zone_technical":
		return -1
	}
	switch s.State {
	case "", "created", "updating":
	default:
		return -1
	}
	r := 0
	if s.Default {
		r += 2
	}
	if s.RoutedNetwork {
		r++
	}
	return r
}

// SubnetInZone: by the zone ID the answer carries, else (no zone in the
// answer) by a name containing the zone name ("Default_ru.AZ-1").
func SubnetInZone(s *Subnet, zoneID, zoneName string) bool {
	if s.AvailabilityZoneID != "" {
		return s.AvailabilityZoneID == zoneID
	}
	return zoneName != "" && strings.Contains(s.Name, zoneName)
}

// SubnetNamesOtherZone: a subnet the API gave without its zone whose name
// carries another zone's name ("Default_ru.AZ-1" for a VM in ru.AZ-3) — the
// same name rule as PickSubnet (review Grok on #86).
func SubnetNamesOtherZone(s *Subnet, zoneID string, zones []*AvailabilityZone) bool {
	if s.AvailabilityZoneID != "" {
		return false
	}
	for _, z := range zones {
		if z.ID != zoneID && z.Name != "" && strings.Contains(s.Name, z.Name) {
			return true
		}
	}
	return false
}
