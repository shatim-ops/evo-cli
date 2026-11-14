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

// One idea of «the default zone» for every create (release plan 1.9, line
// «Зона по умолчанию у Kafka и Redis…»; «Риск», conclusion 254): the zone
// the platform's zone list marks default and open — as evo init and evo vm
// zones take it — not a zone named in the code. A list that marks none
// keeps its order.
func TestDbaasZone_TheZoneThePlatformMarksDefault(t *testing.T) {
	yes := true
	for _, c := range []struct {
		name  string
		zones []*provision.AvailabilityZone
		want  string
	}{
		{"marked ru.AZ-1", []*provision.AvailabilityZone{{ID: "z1", Name: "ru.AZ-1", Default: true, Enabled: &yes}, {ID: "z2", Name: "ru.AZ-2"}}, "ru.AZ-1"},
		{"marked ru.AZ-3", []*provision.AvailabilityZone{{ID: "z2", Name: "ru.AZ-2"}, {ID: "z3", Name: "ru.AZ-3", Default: true}}, "ru.AZ-3"},
		{"none marked", []*provision.AvailabilityZone{{ID: "z1", Name: "ru.AZ-1"}, {ID: "z2", Name: "ru.AZ-2"}}, "ru.AZ-1"},
	} {
		eng := dbaasZoneEngine(t, c.zones)
		_, shown, err := dbaasZone(context.Background(), eng, "")
		if err != nil || shown != c.want {
			t.Errorf("%s: the zone %q (%v), want %s", c.name, shown, err, c.want)
		}
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

func dbaasZoneEngine(t *testing.T, zones []*provision.AvailabilityZone) *engine.Engine {
	t.Helper()
	return &engine.Engine{Cloud: &listedZones{zones: zones}}
}
