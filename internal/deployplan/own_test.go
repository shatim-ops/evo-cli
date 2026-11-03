// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// ownedOld is the folder's record of vm-old: the VM of the retry tests is
// the folder's own (D73), recorded by the apply its answer was lost to.
var ownedOld = []string{"vm-old"}

// ownCloud lists one VM with the plan's name, made at made.
type ownCloud struct {
	existingCloud
	made time.Time
}

func (c *ownCloud) ListVMsInProject(_ context.Context, project string) ([]*provision.VM, error) {
	c.project = project
	return []*provision.VM{{ID: "vm-old", Name: "x", Status: "stopped", CreatedAt: c.made}}, nil
}

func (c *ownCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{}, nil
}

// D73 (owner's decision 31.10): deploy_apply adopts only its own VM — one
// the folder's state records (plan.Owned), or one made after the plan (a
// lost answer of this plan's create, provision.MadeSince). A VM with the
// plan's name made before it is someone else's: refused with its id, not
// adopted, nothing created (agent runs of Grok on 1.6.0, G2-1).
func TestApply_AdoptsOnlyItsOwnVM(t *testing.T) {
	planned := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		made  time.Time
		owned []string
		adopt bool
	}{
		{"made before the plan, not recorded", planned.Add(-time.Hour), nil, false},
		{"made before the plan, made by no one's clock", time.Time{}, nil, false},
		{"recorded in the folder's state", planned.Add(-time.Hour), []string{"vm-old"}, true},
		{"made after the plan: a lost answer", planned.Add(time.Minute), nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &ownCloud{made: tc.made}
			plan := &Plan{ProjectID: "p-1", CreatedAt: planned, VM: VMSpec{Name: "x"}, Owned: tc.owned}
			res, err := ApplyWithTimeout(context.Background(), c, plan, 0, time.Time{}, nil)
			var foreign *ForeignVMError
			switch {
			case c.creates != 0:
				t.Fatalf("CreateVM called %d times", c.creates)
			case tc.adopt && (err != nil || res == nil || res.VMID != "vm-old"):
				t.Fatalf("own VM not adopted: %+v, %v", res, err)
			case !tc.adopt && (!errors.As(err, &foreign) || foreign.ID != "vm-old" || res != nil):
				t.Fatalf("someone else's VM: %+v, %v; want ForeignVMError vm-old", res, err)
			}
		})
	}
}
