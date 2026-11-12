// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

var (
	// saysBillable: the description says the call spends money.
	saysBillable = regexp.MustCompile(`(?i)\bbillable\b|\bbilled\b|\bpaid\b`)
	// saysPublic: the description says the call gives a public address
	// (public_ip, the field of the answer, counts).
	saysPublic = regexp.MustCompile(`(?i)\bpublic(\b|_)`)
)

// descMoneyMisses are the tools whose description in tools/list does not
// say yet what their preview says (TestDescription_SaysWhatThePreviewBills):
// the list only shrinks, each line with what the description lacks.
// Closes by 1.8.
var descMoneyMisses = map[string]string{
	"ca_deploy":        "billable",
	"dns_zone_create":  "billable",
	"k8s_add_addon":    "billable",
	"pg_backup_create": "billable",
	"s3_put":           "billable",
	"secret_create":    "billable",
	"subnet_create":    "billable",
	"tg_create":        "billable",
	"vpc_create":       "billable",
	"vpc_route_create": "billable",
}

// A model reads tools/list before any preview: a create that spends money
// or gives the VM a public address says so in its description, or the
// model plans with it as if free and private (retro of «1.7 —
// development», #843: shortening the descriptions, #834, took «billable»
// and «public floating IP» out of deploy, and people found both). What
// the description owes is read from the preview on the parity world, not
// from a list: a resource of class priced or paid_unpriced — the
// description says billable; a floating_ip line — it says public.
func TestDescription_SaysWhatThePreviewBills(t *testing.T) {
	var bad []string
	seen := map[string]bool{}
	eachPreview(t, func(tl tools.Tool, p map[string]any) {
		var owes []string
		lines, _ := p["price_classes"].([]any)
		billable, public := false, false
		for _, l := range lines {
			m, _ := l.(map[string]any)
			switch m["class"] {
			case "priced", "paid_unpriced":
				billable = true
			}
			if m["resource"] == "floating_ip" {
				public = true
			}
		}
		if billable && !saysBillable.MatchString(tl.Description) {
			owes = append(owes, "billable")
		}
		if public && !saysPublic.MatchString(tl.Description) {
			owes = append(owes, "public address")
		}
		got := strings.Join(owes, ", ")
		want, listed := descMoneyMisses[tl.Name]
		seen[tl.Name] = true
		switch {
		case got != "" && !listed:
			bad = append(bad, tl.Name+": its preview bills, the description does not say "+got+" (tools/list is read before any preview)")
		case listed && got != want:
			bad = append(bad, tl.Name+": descMoneyMisses says «"+want+"», the description lacks «"+got+"» now: update or take the line out")
		}
	})
	for name := range descMoneyMisses {
		if !seen[name] {
			bad = append(bad, name+": in descMoneyMisses, but no preview of it on the parity world")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}
