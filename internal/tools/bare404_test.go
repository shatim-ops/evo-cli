// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"sort"
	"strings"
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
	"redis_delete": true, "release_floating_ip": true, "s3_bucket_delete": true, "secret_delete": true,
	"snat_delete": true, "subnet_delete": true, "tg_delete": true, "vpc_delete": true,
}

// bare404Unreached are the deletes of deleteTargets whose confirmed call
// with the contract arguments reaches no request in the contract world, so
// this test does not check them: each with why. The list only shrinks — a
// delete that reaches no request and is not here fails the test, and so
// does a line whose delete now reaches one.
var bare404Unreached = map[string]string{}

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
	reached := map[string]bool{}
	names := make([]string, 0, len(deleteTargets))
	for name := range deleteTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tl, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		if contractHTTPSkip[name] {
			// A delete outside the contract world would leave E4 unchecked.
			t.Errorf("%s: a delete of deleteTargets in contractHTTPSkip — E4 does not reach it", name)
			continue
		}
		for _, kind := range bare404Kinds {
			rt.kind = kind
			args := byTargetOnly(tl, contractArgs(tl))
			args["confirmed"] = true
			text, _, pan := contractCall(tl, args, contractCallLimit)
			if len(rt.take()) > 0 {
				reached[name] = true
			}
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
		if _, ok := reg.Lookup(name); !ok {
			continue
		}
		switch {
		case !reached[name] && bare404Unreached[name] == "":
			t.Errorf("%s reaches no request in the contract world: a bare 404 is not checked — fix its contract arguments or name it in bare404Unreached", name)
		case reached[name] && bare404Unreached[name] != "":
			t.Errorf("bare404Unreached names %s, which now reaches a request: remove the line", name)
		}
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

// byTargetOnly leaves the resource of the delete tl named one way, by the
// argument of deleteTargets: the contract arguments fill every argument,
// and a delete refuses two names of one resource (vm_id and name, id and
// ip_address) before any request. The ways are what the registry declares
// — Tool.RequiredOneOf and the "a|b" positional of the CLI.
func byTargetOnly(tl Tool, args map[string]any) map[string]any {
	target := deleteTargets[tl.Name].arg
	sets := slices.Clone(tl.RequiredOneOf)
	for _, a := range tl.CLI.Args {
		if strings.Contains(a, "|") {
			sets = append(sets, strings.Split(a, "|"))
		}
	}
	for _, set := range sets {
		if !slices.Contains(set, target) {
			continue
		}
		for _, other := range set {
			if other != target {
				delete(args, other)
			}
		}
	}
	return args
}
