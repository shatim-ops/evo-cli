// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pricing

// The public tariff «Evolution Managed PostgreSQL» (Appendix 7.EVO.4,
// cloud.ru/documents/tariffs/evolution/managed-postgresql), version
// 260918, in force from 28.09.2026, prices with VAT. Both deployment modes,
// business and standard, have the same rates. The process checks the
// tariff once a version (release plan 1.6, «Цена PostgreSQL до согласия»):
// a rate that moves is a line here and its date.
const (
	PGTariffDate    = "28.09.2026"
	PGTariffVersion = "260918"

	pgVCPUHourly    = 1.49145  // 1 vCPU of a node, an hour
	pgRAMGBHourly   = 0.4026   // 1 GB of RAM of a node, an hour
	pgSSDGBHourly   = 0.016836 // 1 GB of network SSD, an hour
	pgTariffNameRaw = "Evolution Managed PostgreSQL"
)

// PGSize is what the PostgreSQL tariff prices: the size of a node, its
// storage and the number of nodes (0 is one).
type PGSize struct {
	CPU, RAMGB, StorageGB, Instances int
}

// EstimatePG prices a PostgreSQL cluster by the tariff: vCPU, RAM and the
// SSD of each node. The tariff prices storage per GB and does not say
// whether each node has its own: evo assumes so (a replica keeps its own
// copy of the data), the higher of the two, and the source says it.
func EstimatePG(s PGSize) Estimate {
	n := float64(max(s.Instances, 1))
	compute := n * (float64(s.CPU)*pgVCPUHourly + float64(s.RAMGB)*pgRAMGBHourly)
	disk := n * float64(s.StorageGB) * pgSSDGBHourly
	computeMonthly := round2(compute * hoursPerMonth)
	diskMonthly := round2(disk * hoursPerMonth)
	return Estimate{
		Currency:          "RUB",
		HourlyRub:         round4(compute),
		ComputeMonthlyRub: computeMonthly,
		DiskMonthlyRub:    diskMonthly,
		TotalMonthlyRub:   round2(computeMonthly + diskMonthly),
		TotalHourlyRub:    round4(compute + disk),
		Source: "estimate: tariff «" + pgTariffNameRaw + "» of " + PGTariffDate + " (version " + PGTariffVersion +
			"), with VAT; storage counted on each node; not a billed price",
	}
}
