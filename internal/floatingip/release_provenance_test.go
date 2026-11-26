// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package floatingip

import (
	"context"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// No request in this fixture returned a confirmed address ID. The reader can
// report its inventory, but no change to it is permitted.
type readOnlyReleaseInventory struct {
	fakeCloud
	t *testing.T
}

func (c *readOnlyReleaseInventory) DeleteFloatingIP(context.Context, string, string) error {
	c.t.Fatalf("release requested without an address ID confirmed for this attempt")
	return nil
}

func TestRelease_UnconfirmedAddressStaysUnresolved(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing []*provision.FloatingIP
	}{
		{name: "empty inventory"},
		{name: "earlier address", existing: []*provision.FloatingIP{{ID: "fixture-existing", Name: "fixture-address", State: "available"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Seed the inventory before recording this separate local request.
			c := &readOnlyReleaseInventory{fakeCloud: fakeCloud{fips: tc.existing}, t: t}
			sf := &state.File{}
			mark := state.LostFloatingIP{Name: "fixture-address", ProjectID: "fixture-project", VMID: "fixture-vm", RequestedAt: time.Unix(100, 0)}
			Note(sf, mark)
			released, unresolved, err := Release(context.Background(), c, sf, mark.ProjectID, "", mark.VMID, "")
			if len(released) != 0 {
				t.Fatalf("reported release without a confirmed ID: %v", released)
			}
			if err == nil && (len(unresolved) != 1 || unresolved[0].Name != mark.Name) {
				t.Fatalf("pending request not reported to caller: %v", unresolved)
			}
		})
	}
}
