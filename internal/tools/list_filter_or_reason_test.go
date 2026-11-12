// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A list is known by its answer, not by its name: pg_databases,
// org_projects, k8s_addons and others are lists whose names say nothing of
// it (release plan 1.8.2, «остальные списки — фильтры по полям элемента или
// причина»; briefs/list-filters.md, «Тесты»). Every read tool is called on
// an API that answers every array with one item that has everything; an
// answer of evo with an array of items that carry an id or a name is a
// list. A list whose items carry a name, a state or a zone declares its
// filters in listSpecs, or why it has none.

// everyItem is the one item of every array of the API: a name, a state, a
// zone, in the spellings the services use.
var everyItem = map[string]any{
	"id": "11111111-1111-4111-8111-111111111111", "name": "n", "displayName": "n",
	"state": "running", "status": "RUNNING",
	"availability_zone":    map[string]any{"id": "22222222-2222-4222-8222-222222222222", "name": "ru.AZ-1"},
	"availability_zone_id": "22222222-2222-4222-8222-222222222222", "zone": "ru.AZ-1", "zoneId": "ru.AZ-1",
}

// everyArrayAPI answers every request with every top-level array of the
// GET answers of the published specifications (scripts/api-contract-fields.json),
// each with everyItem.
type everyArrayAPI struct {
	body []byte
	// requests counts the requests the API was sent.
	requests atomic.Int64
}

func newEveryArrayAPI(t *testing.T) *everyArrayAPI {
	t.Helper()
	raw, err := os.ReadFile("../../scripts/api-contract-fields.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Requests map[string][]string `json:"requests"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{}
	for req, fields := range c.Requests {
		if !strings.Contains(req, " GET ") {
			continue
		}
		for _, f := range fields {
			top := strings.SplitN(f, ".", 2)[0]
			if k, ok := strings.CutSuffix(top, "[]"); ok {
				body[k] = []any{everyItem}
			}
		}
	}
	if len(body) < 20 {
		t.Fatalf("%d arrays of the specifications: the contract of fields no longer reads", len(body))
	}
	b, _ := json.Marshal(body)
	return &everyArrayAPI{body: b}
}

func (a *everyArrayAPI) RoundTrip(*http.Request) (*http.Response, error) {
	a.requests.Add(1)
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.Write(a.body)
	return rec.Result(), nil
}

// listItems are the item arrays of an answer: top-level arrays of objects
// with an id or a name, by key.
func listItems(m map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for k, v := range m {
		arr, ok := v.([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		it, ok := arr[0].(map[string]any)
		if !ok {
			continue
		}
		if it["id"] != nil || it["name"] != nil {
			out[k] = it
		}
	}
	return out
}

// maxListArrays is how many arrays of items a list answers at most; more
// is the API's answer passed through whole, where every array of the test's
// API comes back.
const maxListArrays = 3

// selectable are the fields of an item a filter of the dictionary selects
// by (briefs/list-filters.md, «Словарь»).
func selectable(it map[string]any) []string {
	var out []string
	for _, k := range []string{"name", "state", "status", "zone", "availability_zone", "availability_zone_id", "availability_zone_name"} {
		if v, ok := it[k]; ok && v != nil && v != "" {
			out = append(out, k)
		}
	}
	return out
}

func TestListFilters_EveryListHasFiltersOrAReason(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	api := newEveryArrayAPI(t)
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)

	found := map[string]bool{}
	for _, tl := range r.Tools() {
		if tl.Mutating || HasSideEffects(tl) || IsSensitive(tl) {
			continue
		}
		args := map[string]any{}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, a := range schemaRequired(tl) {
			args[a] = sampleArg(props[a])
		}
		text, isErr := tl.Handler(context.Background(), args)
		if isErr {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(text), &m) != nil {
			continue
		}
		items := listItems(m)
		if len(items) == 0 || len(items) > maxListArrays {
			continue
		}
		found[tl.Name] = true
		s, declared := listSpecs[tl.Name]
		if declared && s.none != "" {
			continue
		}
		for key, it := range items {
			fields := selectable(it)
			if len(fields) == 0 {
				continue
			}
			if !declared || len(s.args()) == 0 {
				t.Errorf("%s: a list (%s[] with %s) with no filter and no reason — declare them in listSpecs", tl.Name, key, strings.Join(fields, ", "))
			}
		}
	}
	// The detection reads: every list of listSpecs with rows is found by
	// its answer — a list this test does not find is one it cannot hold.
	// A tool whose answer carries many arrays of items passes the API's
	// answer through (a get, an operation): not a list.
	var missed []string
	for name, s := range listSpecs {
		if s.rows != "" && !found[name] && unreached[name] == "" {
			missed = append(missed, name)
		}
	}
	sort.Strings(missed)
	if len(missed) > 0 {
		t.Errorf("lists of listSpecs not found by their answer: %v — the API of the test answers them no item", missed)
	}
}

// unreached are the lists of listSpecs this API does not answer, and why:
// listSpecs holds them by name (TestListSpecs_Declared).
var unreached = map[string]string{
	"s3_buckets": "S3 answers XML with its own keys, not the JSON of the specifications",
	"s3_ls":      "S3 answers XML with its own keys, not the JSON of the specifications",
}

// sampleArg is a value of a required argument the schema takes: a UUID for
// a string, the first value of an enum.
func sampleArg(p any) any {
	m, _ := p.(map[string]any)
	if e, ok := m["enum"].([]any); ok && len(e) > 0 {
		return e[0]
	}
	switch m["type"] {
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	}
	// The id of everyItem: a reference the tool resolves through a list
	// (dns_records by its zone) finds it.
	return everyItem["id"]
}
