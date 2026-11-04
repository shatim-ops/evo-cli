// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"testing"
)

// flavorShareMisses are the vCPU shares the list of flavors prices without
// the share (its row has est_note): cpu_share → why. It only shrinks.
// Empty since 1.7 (release plan 1.7, F10: the card has the rates of each
// share, probe P49 of 04.11.2026).
var flavorShareMisses = map[string]string{}

// F10 (agent runs of Grok on 1.6.0; release plan 1.7): the price of a
// flavor in the list of flavors (vm flavors, list_flavors) is the price the
// preview of the VM names for it — one card, not two. For every flavor of
// the catalogue of the parity world: est_monthly_rub of its row, which
// counts a 10 GB SSD, is the compute of provision's preview with that
// flavor plus its disk at 10 GB. A row that prices without the vCPU share
// (est_note) is a line of flavorShareMisses.
func TestFlavors_ListPriceIsThePreviewPrice(t *testing.T) {
	_, _ = withJSONWorld(t)
	reg := newRegistry()
	list, _ := reg.Lookup("list_flavors")
	prov, _ := reg.Lookup("provision")
	newProcess()
	text, isErr := list.Handler(context.Background(), copyArgs(parityArgs["list_flavors"]))
	if isErr {
		t.Fatalf("list_flavors: %s", text)
	}
	var ans struct {
		Flavors []map[string]any `json:"flavors"`
	}
	if err := json.Unmarshal([]byte(text), &ans); err != nil || len(ans.Flavors) == 0 {
		t.Fatalf("list_flavors: no flavors (%v): %.500s", err, text)
	}
	shares := map[string]bool{}
	compared := 0
	var bad []string
	for _, row := range ans.Flavors {
		name, _ := row["name"].(string)
		est, ok := row["est_monthly_rub"].(float64)
		if name == "" || !ok {
			continue
		}
		share, _ := row["cpu_share"].(string)
		if _, noted := row["est_note"]; noted {
			shares[share] = true
			if _, ok := flavorShareMisses[share]; !ok {
				bad = append(bad, name+": priced without its vCPU share "+share+" (est_note), not a line of flavorShareMisses")
			}
		}
		t.Setenv("CLOUDRU_FLAVOR", name)
		newProcess()
		pv, _ := prov.Handler(context.Background(), copyArgs(parityArgs["provision"]))
		var p struct {
			Preview struct {
				VM struct {
					Disk struct {
						SizeGB float64 `json:"size_gb"`
					} `json:"disk"`
				} `json:"vm"`
				Cost struct {
					Compute float64 `json:"compute_monthly_rub"`
					Disk    float64 `json:"disk_monthly_rub"`
					Source  string  `json:"source"`
				} `json:"cost"`
			} `json:"preview"`
		}
		if err := json.Unmarshal([]byte(pv), &p); err != nil || p.Preview.VM.Disk.SizeGB == 0 {
			bad = append(bad, name+": provision's preview has no cost and disk to compare: "+clipLine(pv))
			continue
		}
		if !strings.Contains(p.Preview.Cost.Source, "rate-card") {
			continue // the price API's figure, checked live in the job contract
		}
		compared++
		want := math.Round(p.Preview.Cost.Compute + p.Preview.Cost.Disk*10/p.Preview.VM.Disk.SizeGB)
		if math.Abs(est-want) > 1 {
			bad = append(bad, name+": the list says "+jsonNum(est)+" ₽/month (with 10 GB SSD), the preview "+jsonNum(want)+" (compute "+jsonNum(p.Preview.Cost.Compute)+" + disk at 10 GB)")
		}
	}
	if compared < 2 {
		bad = append(bad, "compared the price of "+jsonNum(float64(compared))+" flavor(s): the world gives the test nothing to compare")
	}
	t.Logf("flavors compared: %d of %d", compared, len(ans.Flavors))
	for share := range flavorShareMisses {
		if !shares[share] {
			bad = append(bad, share+": in flavorShareMisses, but no flavor of the share is priced without it; take the line out (the list only shrinks)")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

func jsonNum(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
