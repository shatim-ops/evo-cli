// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pricing

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The API does not say the period of its components: FromCalculation takes
// the one whose total they add up to.
func TestFromCalculation(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		in                    provision.PriceCalculation
		period                string
		compute, disk, fip, h float64
	}{
		{"month", provision.PriceCalculation{Flavor: 1022, Disks: 140.16, FloatingIPs: 146, TotalPriceHour: 1.792, TotalPriceMonth: 1308.16},
			"month", 1022, 140.16, 146, 1.4},
		{"hour", provision.PriceCalculation{Flavor: 1.4, Disks: 0.192, FloatingIPs: 0.2, TotalPriceHour: 1.792, TotalPriceMonth: 1308.16},
			"hour", 1022, 140.16, 146, 1.4},
		{"unknown", provision.PriceCalculation{Flavor: 5, Disks: 1, TotalPriceHour: 1.792, TotalPriceMonth: 1308.16},
			"unknown", 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := FromCalculation(tc.in)
			if e.Source != SourceAPI || e.Currency != "RUB" || e.TotalMonthlyRub != 1308.16 || e.TotalHourlyRub != 1.792 {
				t.Errorf("totals %+v", e)
			}
			if e.Breakdown == nil || e.Breakdown.Period != tc.period || e.Breakdown.Flavor != tc.in.Flavor {
				t.Fatalf("breakdown %+v", e.Breakdown)
			}
			if e.ComputeMonthlyRub != tc.compute || e.DiskMonthlyRub != tc.disk || e.FloatingIPMonthlyRub != tc.fip || e.HourlyRub != tc.h {
				t.Errorf("compute %v disk %v fip %v hourly %v, want %v %v %v %v",
					e.ComputeMonthlyRub, e.DiskMonthlyRub, e.FloatingIPMonthlyRub, e.HourlyRub, tc.compute, tc.disk, tc.fip, tc.h)
			}
		})
	}
}
