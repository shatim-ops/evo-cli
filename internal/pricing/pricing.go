// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package pricing prices a VM configuration for a preview.
//
// The price comes from the compute price-calculation API of Cloud.ru when it
// answers (FromCalculation; the caller makes the call). Otherwise it is a
// transparent rate-card ESTIMATE, not a billed price (EstimateFor). The rate
// card can be overridden by a JSON file at ~/.evo/pricing.json:
//
//	{
//	  "currency": "RUB",
//	  "flavor_hourly": { "lowcost10-2-4": 2.80 },
//	  "vcpu_hourly": 0.60,
//	  "ram_gb_hourly": 0.40,
//	  "disk_ssd_gb_monthly": 7.0,
//	  "disk_hdd_gb_monthly": 3.0
//	}
//
// Any field present in the file overrides the built-in default.
package pricing

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// hoursPerMonth is the conventional cloud billing month (730h).
const hoursPerMonth = 730.0

// Estimate is a cost breakdown for one VM. The fields are unambiguous on
// purpose: total_monthly_rub already includes the disk, so compute and disk are
// breakdown components — never add total + disk.
//
// Source says where the price comes from: SourceAPI, or one of the rate-card
// estimates ("estimate: rate-card ..."). The fields after Source are set by
// the API price only, except PriceAPIError, which says why a rate-card
// estimate is shown although the API was asked, and LicenseNote.
type Estimate struct {
	Currency          string  `json:"currency"`
	HourlyRub         float64 `json:"hourly_rub"`          // compute, per hour
	ComputeMonthlyRub float64 `json:"compute_monthly_rub"` // compute only
	DiskMonthlyRub    float64 `json:"disk_monthly_rub"`    // disk only
	// TotalMonthlyRub is compute + disk; from the API it is the API's total:
	// the floating IP (FloatingIPMonthlyRub) and the image components of the
	// Breakdown as well.
	TotalMonthlyRub float64 `json:"total_monthly_rub"`
	Source          string  `json:"source"`

	// TotalHourlyRub is the API's total per hour (compute, disk, floating IP).
	TotalHourlyRub       float64 `json:"total_hourly_rub,omitempty"`
	FloatingIPMonthlyRub float64 `json:"floating_ip_monthly_rub,omitempty"`
	// Breakdown is the API's answer as given.
	Breakdown *Breakdown `json:"breakdown,omitempty"`
	// PriceAPIError is why the price-calculation API gave no price (an HTTP
	// status with its message, no answer in time): the rate-card estimate is
	// shown instead.
	PriceAPIError string `json:"price_api_error,omitempty"`
	// LicenseNote is set for a marketplace image: its license is not in
	// the price, from either source.
	LicenseNote string `json:"license_note,omitempty"`
}

// SourceAPI is the Source of a price from the price-calculation API. The API
// answers 0 for the image of every image (marketplace ones included, seen
// live 16.10.2026): a license is not in the price.
const SourceAPI = "price-calculation API (Cloud.ru); image license not included"

// SourceVCPURAM is the Source of an estimate by the card's rates of a vCPU
// and a GB of RAM: one rate of a vCPU, whatever share of a core it
// guarantees.
const SourceVCPURAM = "estimate: rate-card (vCPU+RAM); not a billed price"

// Breakdown is the components of an API price, in roubles per Period. The
// API does not say the period of its components: Period is "month" or "hour"
// when they add up to total_price_month or total_price_hour, "unknown"
// otherwise (then the compute, disk and floating IP fields of the Estimate
// stay 0 and only the totals are known).
type Breakdown struct {
	Period          string  `json:"period"`
	Flavor          float64 `json:"flavor"`
	Disks           float64 `json:"disks"`
	FloatingIPs     float64 `json:"floating_ips"`
	Image           float64 `json:"image"`
	MkpImage        float64 `json:"mkp_image"`
	CustomImageSize float64 `json:"custom_image_size"`
	NATGateways     float64 `json:"nat_gateways"`
}

// FromCalculation is the Estimate of an answer of the price-calculation API.
func FromCalculation(c provision.PriceCalculation) Estimate {
	b := Breakdown{Flavor: c.Flavor, Disks: c.Disks, FloatingIPs: c.FloatingIPs, Image: c.Image,
		MkpImage: c.MkpImage, CustomImageSize: c.CustomImageSize, NATGateways: c.NATGateways, Period: "unknown"}
	e := Estimate{
		Currency:        "RUB",
		TotalMonthlyRub: round2(c.TotalPriceMonth),
		TotalHourlyRub:  round4(c.TotalPriceHour),
		Source:          SourceAPI,
	}
	// Hours in the API's month: its own ratio of the totals, else 730.
	hours := hoursPerMonth
	if c.TotalPriceHour > 0 && c.TotalPriceMonth > 0 {
		hours = c.TotalPriceMonth / c.TotalPriceHour
	}
	sum := c.Flavor + c.Disks + c.FloatingIPs + c.Image + c.MkpImage + c.CustomImageSize + c.NATGateways
	toMonth := 0.0
	switch {
	case near(sum, c.TotalPriceMonth):
		b.Period, toMonth = "month", 1
	case near(sum, c.TotalPriceHour):
		b.Period, toMonth = "hour", hours
	}
	if toMonth > 0 {
		compute := c.Flavor * toMonth
		e.ComputeMonthlyRub = round2(compute)
		e.HourlyRub = round4(compute / hours)
		e.DiskMonthlyRub = round2(c.Disks * toMonth)
		e.FloatingIPMonthlyRub = round2(c.FloatingIPs * toMonth)
	}
	e.Breakdown = &b
	return e
}

// near reports whether a sum of components is the given total, to a cent
// or 1%.
func near(sum, total float64) bool {
	if total <= 0 {
		return false
	}
	return math.Abs(sum-total) <= math.Max(0.01, total*0.01)
}

// card holds the (overridable) rate card.
type card struct {
	Currency         string             `json:"currency"`
	FlavorHourly     map[string]float64 `json:"flavor_hourly"`
	VCPUHourly       float64            `json:"vcpu_hourly"`
	RAMGBHourly      float64            `json:"ram_gb_hourly"`
	DiskSSDGBMonthly float64            `json:"disk_ssd_gb_monthly"`
	DiskHDDGBMonthly float64            `json:"disk_hdd_gb_monthly"`
}

func defaultCard() card {
	return card{
		Currency:         "RUB",
		FlavorHourly:     map[string]float64{},
		VCPUHourly:       0.60,
		RAMGBHourly:      0.40,
		DiskSSDGBMonthly: 7.0,
		DiskHDDGBMonthly: 3.0,
	}
}

// load merges the optional override file onto the default card.
func load() card {
	c := defaultCard()
	home, err := os.UserHomeDir()
	if err != nil {
		return c
	}
	data, err := os.ReadFile(filepath.Join(config.DataDir(home), "pricing.json"))
	if err != nil {
		return c
	}
	var override card
	if err := json.Unmarshal(data, &override); err != nil {
		return c
	}
	if override.Currency != "" {
		c.Currency = override.Currency
	}
	for k, v := range override.FlavorHourly {
		c.FlavorHourly[k] = v
	}
	if override.VCPUHourly > 0 {
		c.VCPUHourly = override.VCPUHourly
	}
	if override.RAMGBHourly > 0 {
		c.RAMGBHourly = override.RAMGBHourly
	}
	if override.DiskSSDGBMonthly > 0 {
		c.DiskSSDGBMonthly = override.DiskSSDGBMonthly
	}
	if override.DiskHDDGBMonthly > 0 {
		c.DiskHDDGBMonthly = override.DiskHDDGBMonthly
	}
	return c
}

// Estimate computes a cost estimate for a flavor + disk. flavorName may be
// empty (then vCPU/RAM rates are used); diskType is matched case-insensitively
// against "ssd"/"hdd".
func EstimateFor(flavorName string, vcpus, ramGB int, diskGB int, diskType string) Estimate {
	c := load()

	var hourly float64
	source := SourceVCPURAM
	if v, ok := c.FlavorHourly[flavorName]; ok {
		hourly = v
		source = "estimate: rate-card (flavor); not a billed price"
	} else {
		hourly = float64(vcpus)*c.VCPUHourly + float64(ramGB)*c.RAMGBHourly
	}

	perGB := c.DiskSSDGBMonthly
	if strings.Contains(strings.ToLower(diskType), "hdd") {
		perGB = c.DiskHDDGBMonthly
	}
	diskMonthly := float64(diskGB) * perGB

	computeMonthly := hourly * hoursPerMonth
	return Estimate{
		Currency:          c.Currency,
		HourlyRub:         round2(hourly),
		ComputeMonthlyRub: round2(computeMonthly),
		DiskMonthlyRub:    round2(diskMonthly),
		TotalMonthlyRub:   round2(computeMonthly + diskMonthly),
		Source:            source,
	}
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// round4 keeps the hourly price readable: kopecks of an hour are below a cent.
func round4(f float64) float64 {
	return math.Round(f*10000) / 10000
}
