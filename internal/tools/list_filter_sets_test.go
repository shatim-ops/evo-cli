// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Release plan 1.9, Вп-10: a list selects by several ids and by several
// states — «ids» and «states» (the plural of the released «state», which
// stays as it is). The pack of ids another API gave picks its VMs in one
// call; several filters are «and», evo checks the selection itself and
// names what it applied.
func TestVMList_SelectsByIDsAndStates(t *testing.T) {
	r, _ := targetRegistry(t, &filterCloud{})
	tl, _ := r.Lookup("vm_list")
	call := func(args map[string]any) (map[string]any, bool, string) {
		t.Helper()
		text, isErr := tl.Handler(context.Background(), args)
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		return m, isErr, text
	}
	names := func(m map[string]any) string {
		vms, _ := m["vms"].([]any)
		var out []string
		for _, v := range vms {
			if row, ok := v.(map[string]any); ok {
				out = append(out, stringArg(row, "name"))
			}
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	const web1, web2, db1, none = "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-0000000000ff"
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"ids": []any{web1, db1}}, "db-1,web-1"},
		{map[string]any{"ids": []any{strings.ToUpper(web2)}}, "web-2"},
		{map[string]any{"ids": []any{web1, none}}, "web-1"},
		{map[string]any{"ids": []any{none}}, ""},
		{map[string]any{"states": []any{"stopped", "ERROR"}}, "web-2"},
		{map[string]any{"states": []any{"running", "stopped"}}, "db-1,web-1,web-2"},
		// Several filters are «and», the released «state» among them.
		{map[string]any{"ids": []any{web1, web2, db1}, "states": []any{"running"}, "zone": "ru.AZ-1"}, "web-1"},
		{map[string]any{"ids": []any{web1, web2}, "state": "stopped"}, "web-2"},
		{map[string]any{"state": "running", "states": []any{"stopped"}}, ""},
	} {
		m, isErr, text := call(c.args)
		if isErr {
			t.Errorf("%v: an error, want the selection %q: %s", c.args, c.want, text)
			continue
		}
		if got := names(m); got != c.want {
			t.Errorf("%v: %q, want %q", c.args, got, c.want)
		}
		filters, _ := m["filters"].(map[string]any)
		for k := range c.args {
			if filters[k] == nil {
				t.Errorf("%v: the answer does not name the filter %s it applied: %s", c.args, k, text)
			}
		}
		if c.want == "" && !strings.Contains(stringArg(m, "hint"), "not «none in the project»") && !strings.Contains(stringArg(m, "hint"), "may exist") {
			t.Errorf("%v: an empty selection does not say it is not «none»: %s", c.args, text)
		}
	}
	// The values are in the spelling of the schema in the answer.
	m, _, text := call(map[string]any{"states": []any{" RUNNING "}})
	if f, _ := m["filters"].(map[string]any); !reflect.DeepEqual(f["states"], []any{"running"}) {
		t.Errorf("states in the answer are not in the spelling of the schema: %s", text)
	}

	// Not a list, an empty list, an empty value, a state evo does not
	// know: USAGE before the request, never «not set» and the whole list.
	for _, c := range []struct {
		args  map[string]any
		named string
	}{
		{map[string]any{"ids": []any{}}, "ids"},
		{map[string]any{"ids": []any{web1, ""}}, "ids[1]"},
		{map[string]any{"ids": []any{"  "}}, "ids[0]"},
		{map[string]any{"ids": web1}, "list"},
		{map[string]any{"ids": []any{1}}, "ids[0]"},
		{map[string]any{"states": []any{}}, "states"},
		{map[string]any{"states": []any{"running", "sleeping"}}, "stopped"},
		{map[string]any{"states": "running"}, "list"},
	} {
		m, isErr, text := call(c.args)
		if !isErr || stringArg(m, "reason") != "USAGE" || !strings.Contains(text, c.named) {
			t.Errorf("%v: want USAGE naming %q, got %s", c.args, c.named, text)
		}
	}
}

// One mechanism for every list (§2), not a case of vm_list: a list that
// has filters selects by «ids» when its item carries an id, or says why it
// does not (noIDs); and every filter by the state has its plural with the
// same values. The item is the one of everyArrayAPI: the list is known by
// its answer.
func TestListFilters_EveryListSelectsBySeveralValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	api := newEveryArrayAPI(t)
	cloud := provision.NewCloudRuAdapter(&http.Client{Transport: api}, "")
	cloud.SetToken("t")
	r := raRegistry(t, api, cloud, nil)

	plural := map[string]string{"state": "states", "status": "statuses"}
	seen := 0
	for name, s := range listSpecs {
		if len(s.filters) == 0 {
			if s.noIDs != "" {
				t.Errorf("%s: noIDs at a list with no filters says nothing", name)
			}
			continue
		}
		tl, ok := r.Lookup(name)
		if !ok {
			t.Errorf("%s: no such tool", name)
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		byArg := map[string]listFilter{}
		for _, f := range s.filters {
			byArg[f.arg] = f
		}
		for _, f := range s.filters {
			p, _ := props[f.arg].(map[string]any)
			if !f.many {
				if to := plural[f.arg]; to != "" && f.kind == filterEnum {
					if g, ok := byArg[to]; !ok || !g.many || !reflect.DeepEqual(g.values, f.values) {
						t.Errorf("%s: filter %s has no %s of the same values", name, f.arg, to)
					}
				}
				continue
			}
			items, _ := p["items"].(map[string]any)
			if p["type"] != "array" || p["minItems"] != 1 || items["minLength"] != 1 {
				t.Errorf("%s: the schema of %s does not refuse an empty list and an empty value: %v", name, f.arg, p)
			}
			if f.kind != filterEnum {
				continue
			}
			// The closed list is the one of the filter by one value: the
			// description names that argument, and a value outside it is
			// USAGE naming the values, with no request.
			one := f.itemFields()[0]
			if desc, _ := p["description"].(string); !strings.Contains(desc, "values of "+one) {
				t.Errorf("%s: the description of %s does not say where its values are: %q", name, f.arg, desc)
			}
			args := filterCallArgs(tl)
			args[f.arg] = []any{f.values[0], "NO_SUCH_STATE"}
			api.requests.Store(0)
			text, isErr := tl.Handler(context.Background(), args)
			if !isErr || !strings.Contains(text, `"USAGE"`) || !strings.Contains(text, f.values[len(f.values)-1]) || api.requests.Load() != 0 {
				t.Errorf("%s with %s of an unknown value: want USAGE naming the values, before any request (%d sent): %s", name, f.arg, api.requests.Load(), text)
			}
		}

		_, ids := byArg["ids"]
		if ids == (s.noIDs != "") {
			t.Errorf("%s: «ids» and noIDs together, or neither: one of them", name)
		}
		if unreached[name] != "" {
			continue
		}
		text, isErr := tl.Handler(context.Background(), filterCallArgs(tl))
		_, rows, err := listRows(text, s.rows)
		if isErr || err != nil || len(rows) == 0 {
			t.Errorf("%s: the API of the test answers it no item: %s", name, text)
			continue
		}
		item, _ := rows[0].(map[string]any)
		id, _ := item["id"].(string)
		if !ids {
			continue
		}
		if id == "" {
			t.Errorf("%s: takes «ids», and its item carries no id — declare noIDs", name)
			continue
		}
		seen++
		// The world may answer several items of the one id (the zones
		// of two kinds of dns_zones).
		same := 0
		for _, row := range rows {
			if it, _ := row.(map[string]any); it["id"] == id {
				same++
			}
		}
		for want, list := range map[int][]any{same: {id}, 0: {"99999999-9999-4999-8999-999999999999"}} {
			args := filterCallArgs(tl)
			args["ids"] = list
			text, isErr := tl.Handler(context.Background(), args)
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			got, _ := m[s.rows].([]any)
			if isErr || len(got) != want || m["filtered_by"] != "evo" {
				t.Errorf("%s with ids %v: %d items, want %d, selected by evo: %s", name, list, len(got), want, text)
			}
		}
	}
	if seen < 20 {
		t.Fatalf("%d lists select by ids: the rule reads few", seen)
	}
}
