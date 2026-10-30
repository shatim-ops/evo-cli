// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pricing

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// pgSnapshotRate is the rate with VAT of the snapshot's row whose name ends
// with suffix; every row of it (the two deployment modes) has that rate.
func pgSnapshotRate(t *testing.T, s tariffSnapshot, suffix string) float64 {
	t.Helper()
	rate := -1.0
	for _, r := range s.Rates {
		if !strings.HasSuffix(r.Name, suffix) {
			continue
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(r.WithVAT, ",", "."), 64)
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		if rate >= 0 && v != rate {
			t.Fatalf("%q: the modes differ (%v, %v) — the card has one rate", suffix, rate, v)
		}
		rate = v
	}
	if rate < 0 {
		t.Fatalf("no row of the snapshot ends with %q", suffix)
	}
	return rate
}

// Ц-10 (owner's decision 29.10, #570, option «b»): a PostgreSQL cluster is
// priced before consent by the public tariff, with VAT — the card of evo
// is the snapshot of the tariff (#644), its version and date with it: a
// tariff that moves fails the live check of the snapshot first, then this
// test until the card follows.
func TestEstimatePG_ByTheTariffSnapshot(t *testing.T) {
	s := loadTariffSnapshot(t, pgTariffSnapshot)
	if s.Version != PGTariffVersion || s.InForce != "28 сентября 2026 г." || PGTariffDate != "28.09.2026" {
		t.Errorf("snapshot %s of %q, card %s of %s: the card is not of the snapshot's tariff", s.Version, s.InForce, PGTariffVersion, PGTariffDate)
	}
	vcpu := pgSnapshotRate(t, s, "1 vCPU")
	ram := pgSnapshotRate(t, s, "1 ГБ RAM")
	ssd := pgSnapshotRate(t, s, "SSD дисках Evolution Managed PostgreSQL")
	if pgVCPUHourly != vcpu || pgRAMGBHourly != ram || pgSSDGBHourly != ssd {
		t.Errorf("card vCPU %v, RAM %v, SSD %v; the snapshot %v, %v, %v", pgVCPUHourly, pgRAMGBHourly, pgSSDGBHourly, vcpu, ram, ssd)
	}

	// Two nodes of 2 vCPU and 8 GB with 50 GB each: per node and hour
	// vCPU and RAM, and the SSD of each node (storage counted per node).
	e := EstimatePG(PGSize{CPU: 2, RAMGB: 8, StorageGB: 50, Instances: 2})
	hour := 2 * (2*vcpu + 8*ram + 50*ssd)
	if math.Abs(e.TotalHourlyRub-hour) > 0.0001 || math.Abs(e.TotalMonthlyRub-hour*hoursPerMonth) > 0.01 {
		t.Errorf("hour %v, month %v; want %v and %v", e.TotalHourlyRub, e.TotalMonthlyRub, hour, hour*hoursPerMonth)
	}
	if e.ComputeMonthlyRub+e.DiskMonthlyRub != e.TotalMonthlyRub {
		t.Errorf("compute %v + disk %v ≠ total %v", e.ComputeMonthlyRub, e.DiskMonthlyRub, e.TotalMonthlyRub)
	}
	if !strings.Contains(e.Source, PGTariffDate) || !strings.Contains(e.Source, PGTariffVersion) {
		t.Errorf("source %q names no date and version of the tariff", e.Source)
	}
	one := EstimatePG(PGSize{CPU: 2, RAMGB: 8, StorageGB: 50})
	if math.Abs(one.TotalHourlyRub-hour/2) > 0.0001 {
		t.Errorf("no instances is one node: hour %v, want %v", one.TotalHourlyRub, hour/2)
	}
}
