// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"regexp"
	"strings"
	"testing"
)

var (
	probeMethod = regexp.MustCompile(`-X\s+"?([A-Z$a-z]+)"?`)
	probeGone   = regexp.MustCompile(`^\s*gone\s+\S+\s+(.+)$`)
	probeTry    = regexp.MustCompile(`^\s*try\s+\S+\s+([A-Z]+)\s+(\S+)`)
)

// api-probe runs without the owner's approval on the observer's key: it
// changes nothing by what it sends, not only by the key's rights. No PUT or
// PATCH; a POST is the exchange of the key for a token or the price
// calculation; a DELETE (404 of the products, D63, and the rights of the
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
				if !strings.Contains(l, "auth/token") && !strings.Contains(l, "price-calculation") && !strings.Contains(l, "x-www-form-urlencoded") {
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
