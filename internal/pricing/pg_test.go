// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pricing

import (
	"strings"
	"testing"
)

// Ц-10 (owner's decision 29.10, #570, option «b»): a PostgreSQL cluster is
// priced before consent by the public tariff, with VAT: vCPU and RAM per
// hour of each node, the SSD per GB-hour of each node; the source names
// the tariff's date and version, so a stale rate reads as stale.
func TestEstimatePG_ByTheTariff(t *testing.T) {
	e := EstimatePG(PGSize{CPU: 2, RAMGB: 8, StorageGB: 50, Instances: 2})
	// node: 2×1.49145 + 8×0.4026 = 6.2037; disk 50×0.016836 = 0.8418;
	// two nodes: 14.091 an hour, ×730 a month.
	if e.TotalHourlyRub != 14.091 || e.TotalMonthlyRub != 10286.43 {
		t.Errorf("hour %v, month %v; want 14.091 and 10286.43", e.TotalHourlyRub, e.TotalMonthlyRub)
	}
	if e.ComputeMonthlyRub+e.DiskMonthlyRub != e.TotalMonthlyRub {
		t.Errorf("compute %v + disk %v ≠ total %v", e.ComputeMonthlyRub, e.DiskMonthlyRub, e.TotalMonthlyRub)
	}
	if !strings.Contains(e.Source, PGTariffDate) || !strings.Contains(e.Source, PGTariffVersion) {
		t.Errorf("source %q names no date and version of the tariff", e.Source)
	}
	if one := EstimatePG(PGSize{CPU: 2, RAMGB: 8, StorageGB: 50}); one.TotalHourlyRub != 7.0455 {
		t.Errorf("no instances is one node: hour %v, want 7.0455", one.TotalHourlyRub)
	}
}
