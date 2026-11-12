// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Why a tool that asks for consent shows neither a price nor a tariff in
// its preview.
const priceNoNewResource = "changes or removes what exists: nothing new to pay for"

// noPriceInPreview is the closed list of the tools whose preview carries no
// cost and no tariff_url, with the reason.
var noPriceInPreview = map[string]string{
	"ar_delete": priceNoNewResource, "ca_delete": priceNoNewResource, "ca_start": priceNoNewResource,
	"ca_stop": priceNoNewResource, "cert_delete": priceNoNewResource, "cert_import": priceNoNewResource,
	"cert_issue_le": priceNoNewResource, "cert_renew": priceNoNewResource, "destroy": priceNoNewResource,
	"disk_attach": priceNoNewResource, "disk_delete": priceNoNewResource, "disk_detach": priceNoNewResource,
	"dns_record_create": priceNoNewResource, "dns_record_delete": priceNoNewResource, "dns_zone_delete": priceNoNewResource,
	"fip_attach": priceNoNewResource, "image_delete": priceNoNewResource, "k8s_apply": priceNoNewResource,
	"k8s_delete": priceNoNewResource, "k8s_nodepool_delete": priceNoNewResource, "backup_delete": priceNoNewResource, "k8s_helm": priceNoNewResource, "k8s_kubectl": priceNoNewResource,
	"kafka_delete": priceNoNewResource, "lb_delete": priceNoNewResource, "close_ports": priceNoNewResource, "open_ports": priceNoNewResource, "sg_rule_delete": priceNoNewResource,
	"pg_db_create": priceNoNewResource, "pg_db_delete": priceNoNewResource, "pg_delete": priceNoNewResource,
	"pg_user_create": priceNoNewResource, "pg_user_delete": priceNoNewResource, "redis_delete": priceNoNewResource,
	"release_floating_ip": priceNoNewResource, "s3_bucket_delete": priceNoNewResource, "s3_rm": priceNoNewResource,
	"secret_add_version": priceNoNewResource, "secret_delete": priceNoNewResource, "snat_delete": priceNoNewResource, "subnet_delete": priceNoNewResource,
	"stack_destroy": priceNoNewResource, "tg_delete": priceNoNewResource, "vpc_delete": priceNoNewResource,
	"vpc_route_delete": priceNoNewResource,
	// A reboot starts and stops no charge (the brief, §5.2).
	"vm_reboot": priceNoNewResource,
}

// A preview the person consents to says what the call costs (cost) or where
// its rates are (tariff_url): an agent never approves a billable resource
// blind (dogfood №4, D17). A tool that shows neither is in the closed list
// noPriceInPreview with its reason, so a new tool cannot join silently, and
// one that gains a price leaves the list (process audit 1.2.1, PR 5).
// A preview never carries the value of a secret argument
// (CLISpec.SecretFiles): secret_create's names the path only.
func TestContract_EveryPreviewPricedOrListed(t *testing.T) {
	var bad []string
	eachPreview(t, func(tl tools.Tool, p map[string]any) {
		bad = append(bad, priceClassProblems(tl.Name, p)...)
		_, cost := p["cost"]
		tariff, _ := p["tariff_url"].(string)
		priced := cost || tariff != ""
		_, listed := noPriceInPreview[tl.Name]
		switch {
		case priced && listed:
			bad = append(bad, tl.Name+": the preview has a price now; take it out of noPriceInPreview")
		case !priced && !listed:
			bad = append(bad, tl.Name+": the preview shows neither cost nor tariff_url; price it or list it in noPriceInPreview with the reason")
		}
	})
	reg := tools.New(nil)
	for name := range noPriceInPreview {
		if tl, ok := reg.Lookup(name); !ok || !tl.Mutating {
			bad = append(bad, name+": in noPriceInPreview, but no tool that asks for consent")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// eachPreview calls fn with the preview of each tool that asks for
// consent, on the parity world (parityArgs, paritySetup): one pass over
// the registry for the invariants that read what a preview says. A tool
// without a preview there fails the test; a preview that holds the value
// of a secret argument does too.
func eachPreview(t *testing.T, fn func(tl tools.Tool, p map[string]any)) {
	t.Helper()
	world := withParityWorld(t)
	fakeKubeBinaries(t)
	srv := parityServers(t)
	var bad []string
	for _, tl := range tools.New(nil).Tools() {
		if !tl.Mutating {
			continue
		}
		args := map[string]any{}
		for k, v := range parityArgs[tl.Name] {
			args[k] = v
		}
		done := func() {}
		if setup := paritySetup[tl.Name]; setup != nil {
			extra, d := setup(t, world)
			for k, v := range extra {
				args[k] = v
			}
			done = d
		}
		got := stdioDetails(t, srv.stdio, tl.Name, args)
		done()
		if got["status"] != "confirmation_required" {
			bad = append(bad, tl.Name+": no preview on the fake cloud (parityArgs): "+clipLine(fmt.Sprint(got)))
			continue
		}
		p, _ := got["preview"].(map[string]any)
		blob, _ := json.Marshal(p)
		for k := range tl.CLI.SecretFiles {
			if v, _ := args[k].(string); v != "" && strings.Contains(string(blob), v) {
				bad = append(bad, tl.Name+": the preview holds the value of "+k)
			}
		}
		fn(tl, p)
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// priceClassProblems is how the preview p of tool name fails release plan
// 1.7, C: each resource the preview creates has a class of its price
// (price_classes), and the class holds what it says — priced has its own
// part of the cost above zero (D17: a silent «0» is money spent blind),
// zero_at_create names what the tariff bills by use and where (note,
// tariff_url), and paid_unpriced says why evo has no price (note). A
// preview of a tool that creates nothing (noPriceInPreview) has no
// classes; any other has at least one.
func priceClassProblems(name string, p map[string]any) []string {
	var bad []string
	lines, _ := p["price_classes"].([]any)
	if _, listed := noPriceInPreview[name]; listed {
		if len(lines) > 0 {
			bad = append(bad, name+": creates nothing (noPriceInPreview), but its preview has price_classes")
		}
		return bad
	}
	if len(lines) == 0 {
		return append(bad, name+": the preview names no class of the price of what it creates (price_classes)")
	}
	tariff, _ := p["tariff_url"].(string)
	for _, l := range lines {
		m, _ := l.(map[string]any)
		r, _ := m["resource"].(string)
		note, _ := m["note"].(string)
		switch m["class"] {
		case "priced":
			c, _ := p["cost"].(map[string]any)
			if v, _ := c[costPart(r)].(float64); v <= 0 {
				bad = append(bad, fmt.Sprintf("%s: %s is priced, but the preview's cost has no %s above zero: %v", name, r, costPart(r), p["cost"]))
			}
		case "zero_at_create":
			if note == "" || tariff == "" {
				bad = append(bad, name+": "+r+" costs nothing at creation, but the preview does not say what the tariff bills (note) or where (tariff_url)")
			}
		case "paid_unpriced":
			// The note says why evo has no price (priceClasses' why). A kind
			// with no line there answers paid_unpriced too — with no note,
			// so a kind nobody declared fails here, through the answer.
			if note == "" {
				bad = append(bad, name+": "+r+" is billable without a price, but the preview does not say why (note): declare its kind in priceClasses")
			}
		default:
			bad = append(bad, fmt.Sprintf("%s: %s has the class %v, not one of priced, zero_at_create, paid_unpriced", name, r, m["class"]))
		}
		if r == "" {
			bad = append(bad, name+": a line of price_classes names no resource")
		}
	}
	return bad
}

// costPart is the field of a preview's cost that holds the monthly price of
// resource r: a priced resource has its own part, so a VM priced with no
// price of its floating IP fails, not passes on the VM's sum.
func costPart(r string) string {
	switch r {
	case "vm", "pg_cluster":
		return "compute_monthly_rub"
	}
	return r + "_monthly_rub"
}
