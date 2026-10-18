// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import "strings"

// IsDefaultGroup reports the zone's default security group. The API says so
// itself (PublicSecurityGroupResponse.default, required in the compute
// spec). By name, for an answer without the flag: "SSH-access_<zone>" — the
// name Cloud.ru gives the default group of a zone (docs, «Группы
// безопасности»; it cannot be deleted) — and "Default_<zone>" / "default".
// Not any "default…" prefix: "DefaultWeb" or "default-ssh" are someone's
// groups. Without the zone name any "default_<zone>"/"ssh-access_<zone>"
// counts.
func IsDefaultGroup(g *SecurityGroup, zoneName string) bool {
	if g == nil {
		return false
	}
	if g.Default {
		return true
	}
	n, z := strings.ToLower(g.Name), strings.ToLower(zoneName)
	if n == "default" {
		return true
	}
	if z == "" {
		return strings.HasPrefix(n, "default_") || strings.HasPrefix(n, "ssh-access_")
	}
	return n == "default_"+z || n == "ssh-access_"+z
}

// PickSecurityGroup picks the security group for a new VM among the zone's
// groups: the zone's default group first (the API flag, then the name),
// then the first by name
// (case-insensitive); the only group of the project when none is in the
// zone. The API order is not a choice: it put port 80 for 0.0.0.0/0 into
// another service's group (QA r4 Q3), and a re-deploy after a reordering
// would take a different group. -1 when nothing fits.
func PickSecurityGroup(sgs []*SecurityGroup, zoneID, zoneName string) int {
	best := -1
	for i, g := range sgs {
		if !groupInZone(g, zoneID, zoneName) {
			continue
		}
		switch {
		case best < 0:
			best = i
		case groupRank(g, zoneName) > groupRank(sgs[best], zoneName):
			best = i
		case groupRank(g, zoneName) == groupRank(sgs[best], zoneName) && strings.ToLower(g.Name) < strings.ToLower(sgs[best].Name):
			best = i
		}
	}
	// The only group of the project, when none is in the zone, only if its
	// zone is unknown: a group of another zone does not boot the VM.
	if best < 0 && len(sgs) == 1 && sgs[0].AvailabilityZoneID == "" {
		return 0
	}
	return best
}

// groupRank orders the zone's groups: the API's default flag first (the
// answer is authoritative), then a default group by name, then the rest.
func groupRank(g *SecurityGroup, zoneName string) int {
	switch {
	case g.Default:
		return 2
	case IsDefaultGroup(g, zoneName):
		return 1
	}
	return 0
}

// groupInZone: the zone by ID, else by a name containing the zone name.
func groupInZone(g *SecurityGroup, zoneID, zoneName string) bool {
	return (g.AvailabilityZoneID != "" && g.AvailabilityZoneID == zoneID) || (zoneName != "" && strings.Contains(g.Name, zoneName))
}
