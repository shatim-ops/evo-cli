// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/pricing"
)

// The ratio is the API's monthly price of the item's part, as the preview
// reads the answer, over the card's; no ratio — with its reason — when the
// API did not answer or the part is not in the answer. The output carries
// no price: the log of api-probe is public.
func TestCompare(t *testing.T) {
	fip := pricing.Estimate{}.WithFloatingIP().FloatingIPMonthlyRub
	ans := func(v map[string]float64) json.RawMessage { b, _ := json.Marshal(v); return b }
	// An hourly answer of exactly the card's rate: the ratio is 1.
	h := fip / 730
	got := compare(item{Item: "fip", Kind: "floating_ip", HTTP: "200",
		Answer: ans(map[string]float64{"floating_ips": h, "total_price_hour": h, "total_price_month": h * 730})})
	if got.Ratio == nil || *got.Ratio != 1 || got.Period != "hour" {
		t.Errorf("a floating IP at the card's rate: %+v", got)
	}
	for _, in := range []item{
		{Item: "down", Kind: "flavor", HTTP: "503", Answer: json.RawMessage(`null`)},
		{Item: "no part", Kind: "ssd_gb", HTTP: "200", Answer: ans(map[string]float64{"flavor": 1, "total_price_hour": 1, "total_price_month": 730})},
		{Item: "kind", Kind: "nat", HTTP: "200", Answer: ans(map[string]float64{"total_price_hour": 1, "total_price_month": 730})},
	} {
		if got := compare(in); got.Ratio != nil || got.Why == "" {
			t.Errorf("%s: want no ratio and its reason, got %+v", in.Item, got)
		}
	}
	b, _ := json.Marshal(compare(item{Item: "x", Kind: "floating_ip", HTTP: "200",
		Answer: ans(map[string]float64{"floating_ips": 123.45, "total_price_month": 123.45})}))
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for k := range m {
		switch k {
		case "item", "kind", "share", "cpu", "ram", "http", "period", "api_over_card", "why":
		default:
			t.Errorf("the output has the field %q: only the item, its size and the ratio go to the public log", k)
		}
	}
}
