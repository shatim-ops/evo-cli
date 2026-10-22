// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Why a tool that asks for consent shows neither a price nor a tariff in
// its preview.
const (
	priceNoNewResource = "changes or removes what exists: nothing new to pay for"
	// The resource is billable and the preview says nothing of its cost:
	// the gap of dogfood №4 that 1.2.1 closed for Kubernetes, PostgreSQL,
	// buckets and node pools. Each goes with tariff_url in 1.2.3.
	priceKnownGap = "known gap: billable, no tariff_url yet (release-plan, 1.2.3)"
	// Whether Cloud.ru bills it is not settled; the owner decides by the
	// start of 1.3, else it counts as billable and gets tariff_url
	// (owner's decision 22.10, release-plan).
	priceToConfirm = "to confirm: is it billable"
)

// noPriceInPreview is the closed list of the tools whose preview carries no
// cost and no tariff_url, with the reason.
var noPriceInPreview = map[string]string{
	"ar_delete": priceNoNewResource, "ca_delete": priceNoNewResource, "ca_start": priceNoNewResource,
	"ca_stop": priceNoNewResource, "cert_delete": priceNoNewResource, "cert_import": priceNoNewResource,
	"cert_issue_le": priceNoNewResource, "cert_renew": priceNoNewResource, "destroy": priceNoNewResource,
	"disk_attach": priceNoNewResource, "disk_delete": priceNoNewResource, "disk_detach": priceNoNewResource,
	"dns_record_create": priceNoNewResource, "dns_record_delete": priceNoNewResource, "dns_zone_delete": priceNoNewResource,
	"fip_attach": priceNoNewResource, "image_delete": priceNoNewResource, "k8s_apply": priceNoNewResource,
	"k8s_delete": priceNoNewResource, "k8s_helm": priceNoNewResource, "k8s_kubectl": priceNoNewResource,
	"kafka_delete": priceNoNewResource, "lb_delete": priceNoNewResource, "open_ports": priceNoNewResource,
	"pg_db_create": priceNoNewResource, "pg_db_delete": priceNoNewResource, "pg_delete": priceNoNewResource,
	"pg_user_create": priceNoNewResource, "pg_user_delete": priceNoNewResource, "redis_delete": priceNoNewResource,
	"release_floating_ip": priceNoNewResource, "s3_bucket_delete": priceNoNewResource, "s3_rm": priceNoNewResource,
	"secret_add_version": priceNoNewResource, "secret_delete": priceNoNewResource, "snat_delete": priceNoNewResource,
	"stack_destroy": priceNoNewResource, "tg_delete": priceNoNewResource, "vpc_delete": priceNoNewResource,
	"vpc_route_delete": priceNoNewResource,

	"ar_create": priceKnownGap, "ca_deploy": priceKnownGap, "image_create": priceKnownGap,
	"kafka_create": priceKnownGap, "lb_create": priceKnownGap, "redis_create": priceKnownGap,
	"snat_create": priceKnownGap,

	"dns_zone_create": priceToConfirm, "pg_backup_create": priceToConfirm, "s3_put": priceToConfirm,
	"secret_create": priceToConfirm, "stack_apply": priceToConfirm, "tg_create": priceToConfirm,
	"vpc_create": priceToConfirm, "vpc_route_create": priceToConfirm,
}

// A preview the person consents to says what the call costs (cost) or where
// its rates are (tariff_url): an agent never approves a billable resource
// blind (dogfood №4, D17). A tool that shows neither is in the closed list
// noPriceInPreview with its reason, so a new tool cannot join silently, and
// one that gains a price leaves the list (process audit 1.2.1, PR 5).
func TestContract_EveryPreviewPricedOrListed(t *testing.T) {
	world := withParityWorld(t)
	fakeKubeBinaries(t)
	srv := parityServers(t)
	var bad []string
	gaps := map[string][]string{}
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
		_, cost := p["cost"]
		tariff, _ := p["tariff_url"].(string)
		priced := cost || tariff != ""
		why, listed := noPriceInPreview[tl.Name]
		switch {
		case priced && listed:
			bad = append(bad, tl.Name+": the preview has a price now; take it out of noPriceInPreview")
		case !priced && !listed:
			bad = append(bad, tl.Name+": the preview shows neither cost nor tariff_url; price it or list it in noPriceInPreview with the reason")
		case listed:
			gaps[why] = append(gaps[why], tl.Name)
		}
	}
	reg := tools.New(nil)
	for name := range noPriceInPreview {
		if tl, ok := reg.Lookup(name); !ok || !tl.Mutating {
			bad = append(bad, name+": in noPriceInPreview, but no tool that asks for consent")
		}
	}
	for _, why := range []string{priceKnownGap, priceToConfirm} {
		sort.Strings(gaps[why])
		t.Logf("%s: %s", why, strings.Join(gaps[why], ", "))
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}
