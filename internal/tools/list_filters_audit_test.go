// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The delta audit of 1.8.2 (docs/internal/audits/delta-1.8.2.md; release
// plan 1.8.2, «Правки дельта-аудита 1.8.2 до тега»), on every list of
// listSpecs — the class, not one test a filter.

// filterRegistry is the registry on the API of the test that answers every
// array with one item (list_filter_or_reason_test.go).
func filterRegistry(t *testing.T) *Registry {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	api := newEveryArrayAPI(t)
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	return raRegistry(t, api, cloud, nil)
}

// filterCallArgs are a value of each required argument of tl.
func filterCallArgs(tl Tool) map[string]any {
	args := map[string]any{}
	props, _ := tl.InputSchema["properties"].(map[string]any)
	for _, a := range schemaRequired(tl) {
		args[a] = sampleArg(props[a])
	}
	return args
}

// FB182-3: an empty value of a filter is USAGE before the request — the
// rule of «zone» in the CLI (D89), for every filter of listSpecs, in one
// point (tools.CheckArgs). Before: MCP took "" for «not given» and answered
// the whole list, with no filters in the answer.
func TestListFilters_EmptyFilterIsUsage(t *testing.T) {
	r := filterRegistry(t)
	seen := 0
	for name, s := range listSpecs {
		tl, ok := r.Lookup(name)
		if !ok {
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, f := range s.filters {
			p, _ := props[f.arg].(map[string]any)
			if p["type"] == "boolean" {
				continue
			}
			seen++
			for _, empty := range []string{"", "  "} {
				args := filterCallArgs(tl)
				args[f.arg] = empty
				text, isErr := tl.Handler(context.Background(), args)
				var m map[string]any
				_ = json.Unmarshal([]byte(text), &m)
				if !isErr || m["reason"] != "USAGE" {
					t.Errorf("%s with %s %q: %s %v — want USAGE before the request", name, f.arg, empty, map[bool]string{true: "error", false: "an answer"}[isErr], m["reason"])
				}
			}
		}
	}
	if seen < 30 {
		t.Fatalf("%d filters of strings: the rule reads few", seen)
	}
}

// FB182-1 (b): a list narrowed by evo alone answers how many there were
// (total, condition 2 of the risk conclusion 104), and filters names only
// what selected. An argument of scope (cluster_id of k8s_nodepools,
// zone_id of dns_records) is not a filter: evo read every item of the
// scope, so total stands under «name» too. The help, the hint of a long
// list and the card of the group name the filters from one source
// (ListFilters, listSpec.args): TestListFilters_HelpNamesOnlyFlags
// (internal/cli) holds the three.
func TestListFilters_NameFilterAnswersTotal(t *testing.T) {
	r := filterRegistry(t)
	seen := 0
	for name, s := range listSpecs {
		named := false
		for _, f := range s.filters {
			named = named || f.arg == "name"
		}
		tl, ok := r.Lookup(name)
		if !named || !ok || unreached[name] != "" {
			continue
		}
		args := filterCallArgs(tl)
		args["name"] = everyItem["name"]
		text, isErr := tl.Handler(context.Background(), args)
		if isErr {
			t.Errorf("%s under name: %.200s", name, text)
			continue
		}
		seen++
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		if _, ok := m["total"]; !ok {
			t.Errorf("%s under name: no total (filters %v, filtered_by %v) — evo read the whole list of its scope", name, m["filters"], m["filtered_by"])
		}
		// filters names what selected: the filter, not the scope.
		if f, _ := m["filters"].(map[string]any); len(f) != 1 || f["name"] == nil {
			t.Errorf("%s under name: filters %v — want name alone, an argument of scope is not a filter", name, m["filters"])
		}
	}
	if seen < 15 {
		t.Fatalf("%d lists with «name»: the rule reads few", seen)
	}
}
