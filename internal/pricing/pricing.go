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
//	  "share_hourly": { "1:10": { "vcpu": 0.33, "ram_gb": 0.08 } },
//	  "vcpu_hourly": 0.60,
//	  "ram_gb_hourly": 0.40,
//	  "disk_ssd_gb_monthly": 11.42,
//	  "disk_hdd_gb_monthly": 3.0,
//	  "floating_ip_monthly": 149.33
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

// SourceAPIDisk is the Source of a price of a disk alone from the
// price-calculation API: no image, so no image license to leave out.
const SourceAPIDisk = "price-calculation API (Cloud.ru)"

// SourceDiskCard is the Source of an estimate of a disk alone by the card's
// rate of a GB.
const SourceDiskCard = "estimate: rate-card (disk GB); not a billed price"

// SourceVCPURAM is the Source of an estimate by the card's rates of a vCPU
// and a GB of RAM: one rate of a vCPU, whatever share of a core it
// guarantees (a flavor of a share the card has no rates of).
const SourceVCPURAM = "estimate: rate-card (vCPU+RAM); not a billed price"

// SourceVCPUShare is the Source of an estimate by the card's rates of a
// vCPU of the flavor's share of a core and of a GB of RAM.
const SourceVCPUShare = "estimate: rate-card (vCPU by its share + RAM, price-calculation of 04.11.2026); not a billed price"

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
	Currency          string             `json:"currency"`
	FlavorHourly      map[string]float64 `json:"flavor_hourly"`
	ShareHourly       map[string]rates   `json:"share_hourly"`
	VCPUHourly        float64            `json:"vcpu_hourly"`
	RAMGBHourly       float64            `json:"ram_gb_hourly"`
	DiskSSDGBMonthly  float64            `json:"disk_ssd_gb_monthly"`
	DiskHDDGBMonthly  float64            `json:"disk_hdd_gb_monthly"`
	FloatingIPMonthly float64            `json:"floating_ip_monthly"`
}

// rates are the hourly rates of a vCPU and of a GB of RAM of one share of a
// core.
type rates struct {
	VCPU  float64 `json:"vcpu"`
	RAMGB float64 `json:"ram_gb"`
}

func defaultCard() card {
	return card{
		Currency:     "RUB",
		FlavorHourly: map[string]float64{},
		// The rates of each share of a core (the guaranteed share of a
		// vCPU, FlavorSchema's oversubscription): the monthly compute the
		// price-calculation API answers for the flavors of 1 vCPU and 1 or
		// 2 GB of the share, read back to a vCPU and a GB an hour of a
		// 730-hour month (probe P49, two runs of 04.11.2026; release plan
		// 1.7, F10). Two flavors a share: the rates fit them, a size
		// further away is an extrapolation. The public tariff names no
		// rate by share.
		ShareHourly: map[string]rates{
			"1:1":  {VCPU: 0.9898, RAMGB: 0.2376},
			"1:3":  {VCPU: 0.3801, RAMGB: 0.2534},
			"1:10": {VCPU: 0.3299, RAMGB: 0.0792},
		},
		// A flavor of a share the card has no rates of (or of none).
		VCPUHourly:  0.60,
		RAMGBHourly: 0.40,
		// The SSD rate the price-calculation API answers: 0.0156 per GB
		// and hour, a month of 732 hours (seen in a live run of 17.10.2026
		// and in the agent run C2 of 30.10.2026; the card had 7.0, ~39 %
		// under). The HDD rate is not seen live.
		DiskSSDGBMonthly: 11.42,
		DiskHDDGBMonthly: 3.0,
		// The floating IP rate the price-calculation API answers: 0.204
		// per hour, a month of 732 hours (seen in the same runs).
		FloatingIPMonthly: 149.33,
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
	for k, v := range override.ShareHourly {
		if v.VCPU > 0 && v.RAMGB > 0 {
			c.ShareHourly[k] = v
		}
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
	if override.FloatingIPMonthly > 0 {
		c.FloatingIPMonthly = override.FloatingIPMonthly
	}
	return c
}

// EstimateFor computes a cost estimate for a flavor + disk: by the card's
// rate of the flavor's name, else by the rates of its share of a core
// ("1:10", as the catalogue writes it), else by the rates of a vCPU and a
// GB of RAM. flavorName and share may be empty; diskType is matched
// case-insensitively against "ssd"/"hdd".
func EstimateFor(flavorName, share string, vcpus, ramGB int, diskGB int, diskType string) Estimate {
	c := load()

	var hourly float64
	source := SourceVCPURAM
	if v, ok := c.FlavorHourly[flavorName]; ok {
		hourly = v
		source = "estimate: rate-card (flavor); not a billed price"
	} else if r, ok := c.ShareHourly[share]; ok {
		hourly = float64(vcpus)*r.VCPU + float64(ramGB)*r.RAMGB
		source = SourceVCPUShare
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

// WithFloatingIP is estimate e with a floating IP created with the VM, at
// the card's rate: its own field and in the total.
func (e Estimate) WithFloatingIP() Estimate {
	fip := load().FloatingIPMonthly
	e.FloatingIPMonthlyRub = round2(fip)
	e.TotalMonthlyRub = round2(e.TotalMonthlyRub + fip)
	return e
}

// FloatingIPMonthlyRub is the card's rate of one floating IP a month: the
// price a question about reserving one more address names.
func FloatingIPMonthlyRub() float64 { return round2(load().FloatingIPMonthly) }

// EstimateDisk is the rate card's estimate of a disk alone: diskGB at the
// card's rate of its type.
func EstimateDisk(diskGB int, diskType string) Estimate {
	e := EstimateFor("", "", 0, 0, diskGB, diskType)
	e.Source = SourceDiskCard
	return e
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// round4 keeps the hourly price readable: kopecks of an hour are below a cent.
func round4(f float64) float64 {
	return math.Round(f*10000) / 10000
}
