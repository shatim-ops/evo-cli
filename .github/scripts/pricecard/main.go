// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Command pricecard holds the answers of the compute price-calculation API
// against evo's rate card, for probe P49 of api-probe (BIL-9, the VAT of
// the API; F10, the card's rates by vCPU share). Each line of stdin is one
// item: {"item", "kind": "flavor"|"floating_ip"|"ssd_gb", "name", "cpu",
// "ram", "share", "http", "answer": <the API's body>}. The answer is read by
// pricing.FromCalculation, as a preview reads it, and the card is
// pricing's own, so neither is copied here. Each line of stdout is the
// item with the ratio of the API's monthly price of its part to the card's
// — a number without unit: the log of the probe is public, and no price
// goes into it.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

type item struct {
	Item   string          `json:"item"`
	Kind   string          `json:"kind"`
	Name   string          `json:"name,omitempty"`
	CPU    int             `json:"cpu,omitempty"`
	RAM    int             `json:"ram,omitempty"`
	Share  string          `json:"share,omitempty"`
	HTTP   string          `json:"http"`
	Answer json.RawMessage `json:"answer"`
}

type out struct {
	Item   string   `json:"item"`
	Kind   string   `json:"kind"`
	Share  string   `json:"share,omitempty"`
	CPU    int      `json:"cpu,omitempty"`
	RAM    int      `json:"ram,omitempty"`
	HTTP   string   `json:"http"`
	Period string   `json:"period,omitempty"`
	Ratio  *float64 `json:"api_over_card,omitempty"`
	Why    string   `json:"why,omitempty"`
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var in item
		if err := json.Unmarshal(sc.Bytes(), &in); err != nil {
			fmt.Fprintln(os.Stderr, "pricecard: a line is not an item:", err)
			os.Exit(1)
		}
		_ = enc.Encode(compare(in))
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "pricecard:", err)
		os.Exit(1)
	}
}

// compare is the ratio of the API's monthly price of in's part to the
// card's; no ratio, with why, when either is not there.
func compare(in item) out {
	o := out{Item: in.Item, Kind: in.Kind, Share: in.Share, CPU: in.CPU, RAM: in.RAM, HTTP: in.HTTP}
	var c provision.PriceCalculation
	if in.HTTP != "200" || json.Unmarshal(in.Answer, &c) != nil {
		o.Why = "no answer of the API"
		return o
	}
	e := pricing.FromCalculation(c)
	if e.Breakdown != nil {
		o.Period = e.Breakdown.Period
	}
	var api, card float64
	switch in.Kind {
	case "flavor":
		api = e.ComputeMonthlyRub
		card = pricing.EstimateFor(in.Name, in.Share, in.CPU, in.RAM, 0, "").ComputeMonthlyRub
	case "floating_ip":
		api = e.FloatingIPMonthlyRub
		card = pricing.Estimate{}.WithFloatingIP().FloatingIPMonthlyRub
	case "ssd_gb":
		api = e.DiskMonthlyRub
		card = pricing.EstimateDisk(1, "ssd").DiskMonthlyRub
	default:
		o.Why = "unknown kind"
		return o
	}
	if api <= 0 || card <= 0 {
		o.Why = "no part of it in the answer or on the card"
		return o
	}
	r := math.Round(api/card*10000) / 10000
	o.Ratio = &r
	return o
}
