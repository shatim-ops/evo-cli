// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// One place for «the default zone» of every create (release plan 1.9, line
// «Зона по умолчанию у Kafka и Redis…»; «Риск», conclusion 254):
// engine.DefaultZoneFirst. A DBaaS create takes the zone its use declares
// there — ru.AZ-2, as before the rule had one place, until the owner
// decides (#2008) — and a VM the zone the zone list marks default.
func TestDbaasZone_TheZoneItsUseDeclares(t *testing.T) {
	yes := true
	for _, c := range []struct {
		name      string
		zones     []*provision.AvailabilityZone
		dbaas, vm string
	}{
		{"ru.AZ-1 marked, ru.AZ-2 listed", []*provision.AvailabilityZone{{ID: "z1", Name: "ru.AZ-1", Default: true, Enabled: &yes}, {ID: "z2", Name: "ru.AZ-2"}}, "ru.AZ-2", "ru.AZ-1"},
		{"no ru.AZ-2", []*provision.AvailabilityZone{{ID: "z1", Name: "ru.AZ-1"}, {ID: "z3", Name: "ru.AZ-3", Default: true}}, "ru.AZ-1", "ru.AZ-3"},
	} {
		_, shown, err := dbaasZone(context.Background(), &engine.Engine{Cloud: &listedZones{zones: c.zones}}, "")
		if err != nil || shown != c.dbaas {
			t.Errorf("%s: the DBaaS zone %q (%v), want %s", c.name, shown, err, c.dbaas)
		}
		if got := engine.DefaultZoneFirst(c.zones, engine.ZoneVM)[0].Name; got != c.vm {
			t.Errorf("%s: the VM zone %q, want %s", c.name, got, c.vm)
		}
	}
}

// A zone the list marks closed (enabled=false) takes no new resource: the
// default of a DBaaS create is never one, and with every zone closed the
// call is refused before any question («Риск», conclusion 269).
func TestDbaasZone_AClosedZoneIsNoDefault(t *testing.T) {
	no := false
	closed := func(z *provision.AvailabilityZone) *provision.AvailabilityZone { z.Enabled = &no; return z }
	zones := []*provision.AvailabilityZone{{ID: "z1", Name: "ru.AZ-1"}, closed(&provision.AvailabilityZone{ID: "z2", Name: "ru.AZ-2"})}
	if _, shown, err := dbaasZone(context.Background(), &engine.Engine{Cloud: &listedZones{zones: zones}}, ""); err != nil || shown != "ru.AZ-1" {
		t.Errorf("ru.AZ-2 closed: the zone %q (%v), want the open ru.AZ-1", shown, err)
	}
	all := []*provision.AvailabilityZone{closed(&provision.AvailabilityZone{ID: "z1", Name: "ru.AZ-1"}), closed(&provision.AvailabilityZone{ID: "z2", Name: "ru.AZ-2"})}
	if _, shown, err := dbaasZone(context.Background(), &engine.Engine{Cloud: &listedZones{zones: all}}, ""); err == nil {
		t.Errorf("every zone closed: the zone %q, want a refusal", shown)
	}
}

// listedZones lists the zones it is given.
type listedZones struct {
	cancelCloud
	zones []*provision.AvailabilityZone
}

func (c *listedZones) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return c.zones, nil
}
