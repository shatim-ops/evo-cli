// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// D67 (#597, п. 35–36; agent run C2 on rc.2, Ц-12): a VM of a flavor the
// catalogue names zones for, none of them the zone of the call, is refused
// before the question. A flavor that names no zones, or none it is
// available in, is as before (D54: no decision without the field).
func TestFlavorOutOfZone(t *testing.T) {
	az1 := &deployplan.RefSpec{ID: "00000000-0000-4000-8000-0000000000a1", Name: "ru.AZ-1"}
	byID := &deployplan.RefSpec{ID: az1.ID}
	in := []provision.FlavorZone{{ID: az1.ID, Name: "ru.AZ-1", Enabled: true}, {ID: "z2", Name: "ru.AZ-2", Enabled: true}}
	other := []provision.FlavorZone{{ID: "z2", Name: "ru.AZ-2", Enabled: true}, {ID: az1.ID, Name: "ru.AZ-1", Enabled: false}}
	for _, c := range []struct {
		name  string
		zones []provision.FlavorZone
		zone  *deployplan.RefSpec
		want  bool
	}{
		{"no zones in the answer", nil, az1, false},
		{"zones, none available", []provision.FlavorZone{{ID: "z2", Name: "ru.AZ-2"}}, az1, false},
		{"the zone by name", in, az1, false},
		{"the zone by id", in, byID, false},
		{"another zone", other, az1, true},
		{"another zone, by id", other, byID, true},
		{"no zone of the call", other, &deployplan.RefSpec{}, false},
	} {
		f := &provision.Flavor{ID: "f1", Name: "gpa100-40", Zones: c.zones}
		err := FlavorOutOfZone(f, c.zone)
		var ce *clerrors.CLIError
		switch {
		case !c.want && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case c.want && (!errors.As(err, &ce) || ce.Code != clerrors.CodeUsage || !strings.Contains(err.Error(), "ru.AZ-2")):
			t.Errorf("%s: want USAGE naming the flavor's zones, got %v", c.name, err)
		}
	}
}
