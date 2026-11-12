// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// What evo passes to the cloud is probed on every candidate: the step of
// job readonly of cloud-e2e runs list_filter_probe.sh with STRICT=1, and a
// parameter of IN_CLOUD the cloud no longer selects by as evo does is red
// (release plan 1.8.2, «проба отбора в облаке — на каждом кандидате»;
// R-76, conclusion 106 of «Риск»). IN_CLOUD is the declarations of
// inCloud: a parameter passed to the cloud and not probed would leave a
// list short without a word.
func TestListFilters_CloudParamsAreProbed(t *testing.T) {
	declared := map[string]bool{}
	for _, s := range listSpecs {
		for _, f := range s.filters {
			if f.cloudParam == "" {
				continue
			}
			path, ok := strings.CutPrefix(f.cloudPath, "/api/v1/")
			if !ok {
				t.Errorf("%s: cloud path %q is not of Compute /api/v1 — the probe reads Compute only", f.arg, f.cloudPath)
				continue
			}
			declared[path+":"+f.cloudParam] = true
		}
	}
	src, err := os.ReadFile("../../.github/scripts/list_filter_probe.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^IN_CLOUD="([^"]*)"$`).FindSubmatch(src)
	if m == nil {
		t.Fatal(`list_filter_probe.sh: no line IN_CLOUD="…"`)
	}
	probed := map[string]bool{}
	for _, p := range strings.Fields(string(m[1])) {
		probed[p] = true
		// A parameter of IN_CLOUD is one the script checks.
		path, param, _ := strings.Cut(p, ":")
		if !regexp.MustCompile(`(?m)^check +\S+ +` + regexp.QuoteMeta(path) + ` +` + regexp.QuoteMeta(param) + ` `).Match(src) {
			t.Errorf("IN_CLOUD names %s, which no check of list_filter_probe.sh probes", p)
		}
	}
	var missing, extra []string
	for p := range declared {
		if !probed[p] {
			missing = append(missing, p)
		}
	}
	for p := range probed {
		if !declared[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("passed to the cloud (inCloud) and not in IN_CLOUD of list_filter_probe.sh: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("IN_CLOUD of list_filter_probe.sh names what evo does not pass to the cloud: %v", extra)
	}
	if len(declared) == 0 {
		t.Fatal("no filter passed to the cloud: the rule reads nothing")
	}
}
