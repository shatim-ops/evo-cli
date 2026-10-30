// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// jsonText is s as it reads inside a JSON string.
func jsonText(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// bare404Kinds are the 404 answers the product did not write — a route or
// an address that is not there, not a resource that is gone (FB13-3, E1):
// a gateway's HTML page, an empty body, plain text (compute), {"error_msg"}
// (VPC, NLB, Container Apps) and grpc-gateway's routing error (DNS, mk8s,
// AR; api-probe 33 and 34).
var bare404Kinds = []string{"404html", "404empty", "404text", "404errormsg", "404route"}

// bare404Gone are the deletes that read a 404 without the product's body as
// the resource gone and answer already_deleted. D63 makes it API_ERROR,
// code 5 (httpretry.RouteNotFoundAsBadGateway, one point for the clients of
// the services); the list only shrinks — a delete not here fails the test,
// and so does a line whose delete no longer does it.
// Closes by 1.8: a line lives at most two versions (release plan 1.6,
// «храповики со сроком»; TestMisses_HaveADeadline).
var bare404Gone = map[string]bool{"s3_bucket_delete": true}

// bare404Outside are the deletes D63 does not reach, each with why: their
// 404 is not answered API_ERROR with the hint of the address.
var bare404Outside = map[string]string{
	"s3_bucket_delete": s3Outside,
	"s3_rm":            s3Outside,
}

// s3Outside: S3 is not a client of httpretry.Transport — minio reads the
// XML of the answer and gives a 404 without it a code itself (NoSuchBucket),
// and its HEAD has no body to tell a route by. Not in the probe of D63.
const s3Outside = "S3: minio, not httpretry.Transport; not in the probe of D63"

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
			reason, status := contractReason(text)
			if status == "already_deleted" {
				found[name] = kind
			} else if bare404Outside[name] == "" && (reason != clierrors.CodeAPIError || !strings.Contains(text, jsonText(clierrors.RouteNotFoundHint()))) {
				// D63: code 5 and the hint of the address, not of the
				// service's state.
				t.Errorf("%s %s: reason %s, want API_ERROR with the hint of the address: %.300s", name, kind, reason, text)
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
	for name := range bare404Outside {
		if _, ok := deleteTargets[name]; !ok {
			t.Errorf("bare404Outside names %s, which deleteTargets does not declare: remove the line", name)
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
