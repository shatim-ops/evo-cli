// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"sort"
	"testing"
)

// bare404Kinds are the 404 answers the product did not write: a gateway's
// HTML page, an empty body — a route or an address that is not there, not
// a resource that is gone (FB13-3, E1).
var bare404Kinds = []string{"404html", "404empty"}

// bare404Gone are the deletes that read a 404 without the product's body as
// the resource gone and answer already_deleted. E1 (1.5) makes it
// API_ERROR, code 5, with the hint «the route or the address of the
// service», in one point (isGone); the list only shrinks — a delete not
// here fails the test, and so does a line whose delete no longer does it.
var bare404Gone = map[string]bool{
	"ar_delete": true, "ca_delete": true, "cert_delete": true, "disk_delete": true,
	"dns_record_delete": true, "dns_zone_delete": true, "image_delete": true,
	"k8s_delete": true, "kafka_delete": true, "lb_delete": true, "pg_delete": true,
	"redis_delete": true, "s3_bucket_delete": true, "secret_delete": true,
	"snat_delete": true, "tg_delete": true, "vpc_delete": true,
}

// D26 answers already_deleted when the service says the resource is not
// there. A 404 whose body is not the service's — HTML, or nothing — says
// that only of a route: a wrong address or a moved API would read as a done
// delete, and «deleted» would not mean the resource is gone (CLAUDE.md §6,
// axis 3). Every delete of deleteTargets, confirmed, in the contract world
// answering each kind of bare404Kinds, answers anything but
// already_deleted, outside bare404Gone (1.4.0, E4).
func TestAlreadyDeleted_Bare404IsNotGone(t *testing.T) {
	rt := &contractRT{}
	reg := contractReg(t, rt)
	found := map[string]string{}
	names := make([]string, 0, len(deleteTargets))
	for name := range deleteTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tl, ok := reg.Lookup(name)
		if !ok || contractHTTPSkip[name] {
			continue
		}
		for _, kind := range bare404Kinds {
			rt.kind = kind
			args := contractArgs(tl)
			args["confirmed"] = true
			text, _, pan := contractCall(tl, args, contractCallLimit)
			rt.take()
			if pan != "" {
				t.Errorf("%s %s panicked", name, kind)
				continue
			}
			if _, status := contractReason(text); status == "already_deleted" {
				found[name] = kind
			}
		}
	}
	for _, name := range names {
		if kind := found[name]; kind != "" && !bare404Gone[name] {
			t.Errorf("%s: a 404 without the service's body (%s) answers already_deleted — a missing route is not a deleted resource (E1)", name, kind)
		}
	}
	for name := range bare404Gone {
		if found[name] == "" {
			t.Errorf("bare404Gone names %s, which no longer reads a bare 404 as gone: remove the line", name)
		}
	}
}
