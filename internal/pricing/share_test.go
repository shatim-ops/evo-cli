// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pricing

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// F10 (agent runs of Grok on 1.6.0; release plan 1.7): the card prices a
// flavor by the share of a core its vCPU guarantees. The wants are the
// monthly compute the price-calculation API answered in probe P49
// (api-probe, two runs of 04.11.2026, equal to the fourth digit) for the
// flavors of 1 vCPU and 1 or 2 GB of each share: the ratio of the API to
// the card of before, 0.60 a vCPU and 0.40 a GB an hour, times that card.
func TestEstimateFor_PricesTheVCPUShare(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	before := func(vcpus, ramGB int) float64 { return (0.60*float64(vcpus) + 0.40*float64(ramGB)) * hoursPerMonth }
	for _, c := range []struct {
		share        string
		vcpus, ramGB int
		ratio        float64
	}{
		{"1:1", 1, 1, 1.2274}, {"1:1", 1, 2, 1.0464},
		{"1:3", 1, 1, 0.6335}, {"1:3", 1, 2, 0.6335},
		{"1:10", 1, 1, 0.4091}, {"1:10", 1, 2, 0.3488},
	} {
		e := EstimateFor("", c.share, c.vcpus, c.ramGB, 0, "")
		want := c.ratio * before(c.vcpus, c.ramGB)
		if math.Abs(e.ComputeMonthlyRub-want) > 1 {
			t.Errorf("%s %dx%d: compute %.2f ₽/month, the API answered %.2f", c.share, c.vcpus, c.ramGB, e.ComputeMonthlyRub, want)
		}
		if e.Source != SourceVCPUShare {
			t.Errorf("%s %dx%d: source %q, want %q", c.share, c.vcpus, c.ramGB, e.Source, SourceVCPUShare)
		}
	}
	// A share the card has no rates of, or none, is priced at the rates of
	// a vCPU whatever its share: the list of flavors says so (est_note).
	for _, share := range []string{"", "1:2"} {
		if e := EstimateFor("", share, 2, 4, 0, ""); e.Source != SourceVCPURAM || e.HourlyRub != 2.80 {
			t.Errorf("share %q: %v ₽/h by %q, want 2.80 by %q", share, e.HourlyRub, e.Source, SourceVCPURAM)
		}
	}
}

// The rates of a share are the card's: ~/.evo/pricing.json overrides them
// one share at a time, and a rate of a flavor by its name stays first.
func TestEstimateFor_ShareRatesOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := config.DataDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	card := `{"share_hourly": {"1:3": {"vcpu": 0.5, "ram_gb": 0.25}}, "flavor_hourly": {"named-1-1": 3}}`
	if err := os.WriteFile(filepath.Join(dir, "pricing.json"), []byte(card), 0o600); err != nil {
		t.Fatal(err)
	}
	if e := EstimateFor("", "1:3", 2, 4, 0, ""); e.HourlyRub != 2.00 {
		t.Errorf("1:3 2x4: %v ₽/h, want 2.00 by the override", e.HourlyRub)
	}
	if e := EstimateFor("", "1:10", 1, 1, 0, ""); e.Source != SourceVCPUShare || e.HourlyRub != 0.41 {
		t.Errorf("1:10 1x1: %v ₽/h by %q, want the built-in 0.41 by share", e.HourlyRub, e.Source)
	}
	if e := EstimateFor("named-1-1", "1:3", 1, 1, 0, ""); e.HourlyRub != 3 {
		t.Errorf("named-1-1: %v ₽/h, want its own 3", e.HourlyRub)
	}
}
