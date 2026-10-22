// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Closed reports a zone the API lists with enabled=false: it takes no new
// resources. cloud-e2e 17.10.2026, ru.AZ-1 with enabled=false: the boot
// floating IP was created, the VM create answered 422 wrong_az ("Availability
// zone with this id is not found") and the released IP stuck in
// error_deleting. A zone without the field is taken as open.
func (z *AvailabilityZone) Closed() bool {
	return z != nil && z.Enabled != nil && !*z.Enabled
}

// ClosedZoneError refuses a VM in the zone zoneID when the zone list marks
// it closed — before anything is created or billed. nil when the zone is
// open, unknown or the list is empty.
func ClosedZoneError(zones []*AvailabilityZone, zoneID string) error {
	var zone *AvailabilityZone
	var open []string
	for _, z := range zones {
		if z.ID == zoneID {
			zone = z
		} else if !z.Closed() && z.Name != "" {
			open = append(open, z.Name)
		}
	}
	if !zone.Closed() {
		return nil
	}
	// --region and the MCP region take the zone ID, so the hint points to
	// evo list-zones for it (review Grok on #83).
	hint := i18n.T("Pick another zone with 'evo init', or pass its ID from 'evo list-zones' as --region.",
		"Выберите другую зону: 'evo init' или --region с её ID из 'evo list-zones'.")
	if len(open) > 0 {
		hint = i18n.Tf("Pick an open zone (%s) with 'evo init', or pass its ID from 'evo list-zones' as --region.",
			"Выберите открытую зону (%s): 'evo init' или --region с её ID из 'evo list-zones'.", strings.Join(open, ", "))
	}
	return clierrors.New(clierrors.CodeConfigInvalid,
		i18n.Tf("zone %s is closed for new resources (the zone list marks it enabled=false); nothing was created",
			"зона %s закрыта для новых ресурсов (в списке зон enabled=false); ничего не создано", zone.Name),
		hint)
}

// OpenZones drops the closed zones, keeping the order; all of them when
// every zone is closed (then the create call says why).
func OpenZones(zones []*AvailabilityZone) []*AvailabilityZone {
	out := make([]*AvailabilityZone, 0, len(zones))
	for _, z := range zones {
		if !z.Closed() {
			out = append(out, z)
		}
	}
	if len(out) == 0 {
		return zones
	}
	return out
}

// ResolveZone is the listed zone ref names: by its UUID or by its name
// ("ru.AZ-2", in any case). It is the one rule for a zone a person or an
// agent names: the zone of a new VM (--region of evo vm create, the region
// of MCP provision, CLOUDRU_REGION), evo configure --region, a stack's
// vm.zone and the zone of a Kafka or Redis cluster. A ref no listed zone
// matches is USAGE with the names to choose from (the open zones).
func ResolveZone(zones []*AvailabilityZone, ref string) (*AvailabilityZone, error) {
	for _, z := range zones {
		if z.ID == ref {
			return z, nil
		}
	}
	for _, z := range zones {
		if z.Name != "" && strings.EqualFold(z.Name, ref) {
			return z, nil
		}
	}
	var names []string
	for _, z := range OpenZones(zones) {
		if z.Name != "" {
			names = append(names, z.Name)
		}
	}
	hint := i18n.T("The zones are listed by evo vm zones (list_zones).", "Зоны — в выводе evo vm zones (list_zones).")
	if len(names) > 0 {
		hint = i18n.Tf("Zones: %s — a name or its UUID (evo vm zones, list_zones).",
			"Зоны: %s — имя или UUID (evo vm zones, list_zones).", strings.Join(names, ", "))
	}
	return nil, clierrors.New(clierrors.CodeUsage,
		i18n.Tf("availability zone %q not found", "зона доступности %q не найдена", ref), hint)
}
