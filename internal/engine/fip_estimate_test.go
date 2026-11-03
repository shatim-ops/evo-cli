// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The rate card's estimate of a VM names every billable part the preview
// creates: a floating IP with a public address (agent run M0 on 1.6.0: the
// estimate without the API's answer left it out). The same in a create
// preview and in the fallback of a plan's price.
func TestEstimateVM_FloatingIPWithPublicAddress(t *testing.T) {
	vm := deployplan.VMSpec{Flavor: deployplan.FlavorSpec{Name: "f", VCPUs: 2, RAMGB: 4}, Disk: deployplan.DiskSpec{SizeGB: 20, Type: "SSD"}}
	no := false
	for _, c := range []struct {
		name   string
		public *bool
		fip    bool
	}{{"public by default", nil, true}, {"no public address", &no, false}} {
		p := CreatePreview{Resource: ResourceVM, VM: &vm, PublicIP: c.public}
		p.Price()
		got := p.Cost
		if got == nil {
			t.Fatalf("%s: no cost", c.name)
		}
		sum := got.ComputeMonthlyRub + got.DiskMonthlyRub + got.FloatingIPMonthlyRub
		if (got.FloatingIPMonthlyRub > 0) != c.fip || got.TotalMonthlyRub < sum-0.01 || got.TotalMonthlyRub > sum+0.01 {
			t.Errorf("%s: cost %+v: want the floating IP %v and a total of its parts", c.name, got, c.fip)
		}
		line := strings.Join(createCostLines(p), "\n")
		if strings.Contains(line, "floating IP") != c.fip {
			t.Errorf("%s: cost line %q", c.name, line)
		}
	}

	fail := &mockCloudAPI{priceFn: func(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
		return nil, errors.New("down")
	}}
	vm.Flavor.ID = "f-small"
	cost := deployplan.PriceVM(context.Background(), fail, deployplan.PriceInput{ProjectID: "p", VM: vm, PublicIP: true})
	if cost.FloatingIPMonthlyRub <= 0 || cost.PriceAPIError == "" {
		t.Errorf("plan price without the API: %+v, want the floating IP in the estimate", cost)
	}
}
