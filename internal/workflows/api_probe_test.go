// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

var (
	probeMethod = regexp.MustCompile(`-X\s+"?([A-Z$a-z]+)"?`)
	probeGone   = regexp.MustCompile(`^\s*gone\s+\S+\s+(.+)$`)
	probeTry    = regexp.MustCompile(`^\s*try\s+\S+\s+([A-Z]+)\s+(\S+)`)
)

// api-probe runs without the owner's approval on the observer's key: it
// changes nothing by what it sends, not only by the key's rights. No PUT or
// PATCH; a POST is the exchange of the key for a token or a price
// calculation (Compute price-calculation, Managed Kubernetes
// calculate-price: both only count); a DELETE (404 of the products, D63, and the rights of the
// key) goes to an id that does not exist ($NOID) or a path that does not
// exist (evo-probe-…), and a write the key's rights must refuse is only in
// try of the rights step.
func TestWorkflows_APIProbeChangesNothing(t *testing.T) {
	j, ok := readWorkflows(t)["api-probe.yml"].Jobs["probe"]
	if !ok {
		t.Fatal("api-probe: no job probe")
	}
	gones := 0
	for _, s := range j.Steps {
		for _, l := range strings.Split(s.Run, "\n") {
			if m := probeGone.FindStringSubmatch(l); m != nil {
				gones++
				if !strings.Contains(m[1], "$NOID") && !strings.Contains(m[1], "evo-probe-") {
					t.Errorf("api-probe, %s: DELETE of an id that may exist: %s", s.Name, strings.TrimSpace(l))
				}
			}
			if m := probeTry.FindStringSubmatch(l); m != nil && m[1] == "DELETE" && !strings.Contains(m[2], "$NOID") {
				t.Errorf("api-probe, %s: DELETE of an id that may exist: %s", s.Name, strings.TrimSpace(l))
			}
			m := probeMethod.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			switch m[1] {
			case "GET":
			case "POST":
				if !strings.Contains(l, "auth/token") && !strings.Contains(l, "price-calculation") && !strings.Contains(l, "/billing/calculate-price") && !strings.Contains(l, "x-www-form-urlencoded") {
					t.Errorf("api-probe, %s: a POST that is not the token or the price: %s", s.Name, strings.TrimSpace(l))
				}
			case "DELETE":
				if !strings.Contains(l, `"$url"`) {
					t.Errorf("api-probe, %s: a DELETE outside gone: %s", s.Name, strings.TrimSpace(l))
				}
			case "$method":
				if !strings.Contains(s.Name, "Права ключа") {
					t.Errorf("api-probe, %s: a method from a variable outside the rights step: %s", s.Name, strings.TrimSpace(l))
				}
			default:
				t.Errorf("api-probe, %s: -X %s: %s", s.Name, m[1], strings.TrimSpace(l))
			}
		}
	}
	if gones == 0 {
		t.Error("api-probe: no gone call — the 404 of the products (D63) left the workflow, update the test")
	}
}

var (
	probeEvoCall = regexp.MustCompile(`\bevo\s+(?:--\S+\s+)*tools\s+call\s+("?\$?[A-Za-z_]+"?)(?:\s|$)`)
	probeForList = regexp.MustCompile(`^\s*for\s+([a-z_]+)\s+in\s+([a-z_ ]+);`)
)

// api-probe runs evo itself too (P23; the cause of the refusal of
// image_get in the probe CMP-7, 10.11): on the observer's key, with no
// approval, a call of evo changes nothing — each tool it calls is one of
// reading (not Mutating in the registry), named or listed in the for loop
// it runs in, and no call carries --yes.
func TestWorkflows_APIProbeEvoCallsOnlyRead(t *testing.T) {
	j, ok := readWorkflows(t)["api-probe.yml"].Jobs["probe"]
	if !ok {
		t.Fatal("api-probe: no job probe")
	}
	reg := tools.New(nil)
	calls := 0
	for _, s := range j.Steps {
		loops := map[string][]string{}
		for _, l := range strings.Split(s.Run, "\n") {
			if m := probeForList.FindStringSubmatch(l); m != nil {
				loops[m[1]] = strings.Fields(m[2])
			}
			m := probeEvoCall.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			if strings.Contains(l, "--yes") || strings.Contains(l, " -y ") {
				t.Errorf("api-probe, %s: evo with --yes: %s", s.Name, strings.TrimSpace(l))
			}
			name := strings.Trim(m[1], `"`)
			names := []string{name}
			if v, isVar := strings.CutPrefix(name, "$"); isVar {
				if names = loops[v]; names == nil {
					t.Errorf("api-probe, %s: evo tools call of $%s, which no for loop of the step lists: %s", s.Name, v, strings.TrimSpace(l))
				}
			}
			for _, n := range names {
				calls++
				tool, ok := reg.Lookup(n)
				switch {
				case !ok:
					t.Errorf("api-probe, %s: evo tools call %s — no such tool", s.Name, n)
				case tool.Mutating:
					t.Errorf("api-probe, %s: evo tools call %s — a tool that changes the cloud", s.Name, n)
				}
			}
		}
	}
	if calls == 0 {
		t.Error("api-probe: no evo tools call — P23 left the workflow, update the test")
	}
}

// The reads of the schedule of live probes 1.9 (docs/internal/phase-0/1.9.md,
// Г-05, Г-06, Г-22) are in api-probe: the operations of the second day are
// written from what the platform answers to a read — backups and protection
// plans, the tag service, the Kubernetes versions to upgrade to, the flavors
// to resize to, the project in the cards, the request id in the answers,
// TLS by host, the price before and after. A read dropped from the workflow
// leaves its operation's preview a guess; the second reading on 21.11
// compares with the first by these names.
func TestWorkflows_APIProbeReadsTheSecondDay(t *testing.T) {
	j, ok := readWorkflows(t)["api-probe.yml"].Jobs["probe"]
	if !ok {
		t.Fatal("api-probe: no job probe")
	}
	var run strings.Builder
	for _, s := range j.Steps {
		run.WriteString(s.Run)
	}
	for _, name := range []string{
		"bk19_list", "bk19_card_keys", "pp19_list", "tags19_service", "k8s19_product_config",
		"k8s19_versions_v2", "flavor19_params", "proj19_", "rid19", "tls19", "price19_flavor_next",
		"price19_k8s", "price19_k8s_detail", "price19_k8s_ratio",
	} {
		if !strings.Contains(run.String(), name) {
			t.Errorf("api-probe: no read %s of the schedule of probes 1.9 (Г-05, Г-06, Г-22)", name)
		}
	}
}

// The price of a Kubernetes cluster is asked with the values of the
// published specification (mk8s, CalculatePriceRequest): the type of a disk
// is DISK_TYPE_SSD_NVME, the type of a node pool is one of FLAVOR_TYPE_*.
// The first run of the probe sent "SSD", got 400 on its own request and
// learned nothing about the price (api-probe run 52).
func TestWorkflows_APIProbeAsksThePriceOfKubernetesBySpec(t *testing.T) {
	j, ok := readWorkflows(t)["api-probe.yml"].Jobs["probe"]
	if !ok {
		t.Fatal("api-probe: no job probe")
	}
	var body string
	for _, s := range j.Steps {
		for _, l := range strings.Split(s.Run, "\n") {
			if strings.Contains(l, "nodePools: [") {
				body = l
			}
		}
	}
	if body == "" {
		t.Fatal("api-probe: no request of the price of Kubernetes (nodePools)")
	}
	for _, want := range []string{`type: "MASTER_TYPE_SMALL"`, `type: "DISK_TYPE_SSD_NVME"`, `type: "FLAVOR_TYPE_GENERAL"`} {
		if !strings.Contains(body, want) {
			t.Errorf("api-probe: the price of Kubernetes is asked without %s: %s", want, strings.TrimSpace(body))
		}
	}
	// Run 52 got 400 «one or more fields have invalid values» on every
	// body: the deprecated master.type may be the field the service
	// refuses. So the probe also asks with the flavours of the catalogue
	// (productConfiguration) and prints the details of a refusal — a
	// request that only says 400 teaches nothing about the price (Г-51:
	// the stand's Kubernetes has no rate of its own in any tariff).
	var run strings.Builder
	for _, s := range j.Steps {
		run.WriteString(s.Run)
	}
	for _, want := range []string{"$mflavor", "$nflavor"} {
		if !strings.Contains(run.String(), want) {
			t.Errorf("api-probe: the price of Kubernetes is never asked with %s of the catalogue", want)
		}
	}
}
