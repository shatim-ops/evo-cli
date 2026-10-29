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
	"k8s_delete": priceNoNewResource, "k8s_helm": priceNoNewResource, "k8s_kubectl": priceNoNewResource,
	"kafka_delete": priceNoNewResource, "lb_delete": priceNoNewResource, "close_ports": priceNoNewResource, "open_ports": priceNoNewResource,
	"pg_db_create": priceNoNewResource, "pg_db_delete": priceNoNewResource, "pg_delete": priceNoNewResource,
	"pg_user_create": priceNoNewResource, "pg_user_delete": priceNoNewResource, "redis_delete": priceNoNewResource,
	"release_floating_ip": priceNoNewResource, "s3_bucket_delete": priceNoNewResource, "s3_rm": priceNoNewResource,
	"secret_add_version": priceNoNewResource, "secret_delete": priceNoNewResource, "snat_delete": priceNoNewResource, "subnet_delete": priceNoNewResource,
	"stack_destroy": priceNoNewResource, "tg_delete": priceNoNewResource, "vpc_delete": priceNoNewResource,
	"vpc_route_delete": priceNoNewResource,
}

// A preview the person consents to says what the call costs (cost) or where
// its rates are (tariff_url): an agent never approves a billable resource
// blind (dogfood №4, D17). A tool that shows neither is in the closed list
// noPriceInPreview with its reason, so a new tool cannot join silently, and
// one that gains a price leaves the list (process audit 1.2.1, PR 5).
// A preview never carries the value of a secret argument
// (CLISpec.SecretFiles): secret_create's names the path only.
func TestContract_EveryPreviewPricedOrListed(t *testing.T) {
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
	}
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
