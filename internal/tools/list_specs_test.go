// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The declaration of the filters holds together (briefs/list-filters.md,
// «Механизм»): a tool named as a list is in listSpecs, with filters or
// with why it has none; filters are for reads; every filter is an argument
// of the schema, a closed list has its values, a reference names a list
// that takes no filter itself.
func TestListSpecs_Declared(t *testing.T) {
	r := New(nil)
	for _, tl := range r.Tools() {
		s, in := listSpecs[tl.Name]
		listName := tl.Name == "list" || strings.HasPrefix(tl.Name, "list_") || strings.HasSuffix(tl.Name, "_list")
		if listName && !in {
			t.Errorf("%s: a list not in listSpecs — declare its filters, or why it has none", tl.Name)
		}
		if !in {
			continue
		}
		if tl.Mutating || HasSideEffects(tl) || IsSensitive(tl) {
			t.Errorf("%s: filters are for reads only", tl.Name)
		}
		if (s.none != "") == (s.args() != nil) {
			t.Errorf("%s: wants filters or a reason for none, not both and not neither", tl.Name)
		}
		if s.args() != nil && s.rows == "" {
			t.Errorf("%s: no key of the items (rows)", tl.Name)
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, a := range s.args() {
			if _, ok := props[a]; !ok {
				t.Errorf("%s: filter %s is not an argument of the schema", tl.Name, a)
			}
		}
		for _, f := range s.filters {
			if f.en == "" || f.ru == "" || strings.Contains(f.en, "\n") {
				t.Errorf("%s.%s: wants a description of one line in both languages", tl.Name, f.arg)
			}
			if (f.kind == filterEnum) != (len(f.values) > 0) {
				t.Errorf("%s.%s: a closed list has its values, another filter has none", tl.Name, f.arg)
			}
			if f.kind == filterRef {
				ref, ok := listSpecs[f.ref]
				if _, tool := r.Lookup(f.ref); !ok || !tool || ref.rows == "" {
					t.Errorf("%s.%s: its list %q is not a list of listSpecs with rows", tl.Name, f.arg, f.ref)
				}
			}
		}
	}
	for name := range listSpecs {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("listSpecs names %q, which is not a tool", name)
		}
	}
}

// enumSource is where the values of a filter by state come from: the enum
// of the item in the published specification, as scripts/api-contract.json
// names it at the request of the list (job contract compares that with the
// specification itself).
var enumSource = map[string]struct{ service, path, field string }{
	"vm_list.state":             {"compute", "/api/v1/vms", "items[].state"},
	"disk_list.state":           {"compute", "/api/v1/disks", "items[].state"},
	"backup_list.state":         {"compute", "/api/v1/backups", "items[].state"},
	"backup_list.resource_type": {"compute", "/api/v1/backups", "items[].resource.resource_type"},
	"list_floating_ips.state":   {"compute", "/api/v1/floating-ips", "items[].state"},
	"list_subnets.state":        {"compute", "/api/v1/subnets", "items[].state"},
	"snat_list.status":          {"compute", "/api/v1/nat-gateways", "items[].state"},
	"k8s_list.status":           {"mk8s", "/v3/clusters", "clusters[].status"},
	"k8s_nodepools.status":      {"mk8s", "/v3/nodePools", "nodePools[].status"},
	"pg_list.status":            {"postgresql", "/v1/clusters", "clusters[].status"},
	"kafka_list.status":         {"kafka", "/v1/clusters", "clusters[].status"},
	"redis_list.status":         {"redis", "/v1/clusters", "clusters[].status"},
	"lb_list.status":            {"nlb", "/v2/networkLoadBalancers", "balancers[].status"},
	"tg_list.status":            {"nlb", "/v2/targetGroups", "target_groups[].status"},
	"ar_list.status":            {"ar", "/v1/registries", "registries[].status"},
	"ar_list.type":              {"ar", "/v1/registries", "registries[].registryType"},
	"vpc_routes.status":         {"vpc", "/v1/vpcs/{vpcId}/routes/static", "routes[].status"},
	"pg_backups.status":         {"postgresql", "/v1/clusters/{cluster_id}/backups", "backups[].status"},
}

// The values of a filter by state are the specification's, not evo's
// (condition 5 of the risk conclusion 104): each closed list of listSpecs
// is the enum the contract names at the list's request, value for value.
func TestListSpecs_StatesAreTheSpecifications(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/api-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Services []struct {
			Name     string `json:"name"`
			Requests []struct {
				Method string              `json:"method"`
				Path   string              `json:"path"`
				Enums  map[string][]string `json:"enums"`
			} `json:"requests"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	enum := func(service, path, field string) []string {
		for _, s := range contract.Services {
			for _, rq := range s.Requests {
				if s.Name == service && rq.Method == "GET" && rq.Path == path {
					return rq.Enums[field]
				}
			}
		}
		return nil
	}
	seen := map[string]bool{}
	for name, s := range listSpecs {
		for _, f := range s.filters {
			if f.kind != filterEnum {
				continue
			}
			// The filter by several values is the closed list of the
			// field it reads (states of state): one source.
			key := name + "." + f.itemFields()[0]
			seen[key] = true
			src, ok := enumSource[key]
			if !ok {
				t.Errorf("%s: a closed list with no source in enumSource — name the enum of the specification it is", key)
				continue
			}
			if want := enum(src.service, src.path, src.field); !reflect.DeepEqual(f.values, want) {
				t.Errorf("%s: values %v, the contract's %s %s %s: %v", key, f.values, src.service, src.path, src.field, want)
			}
		}
	}
	for key := range enumSource {
		if !seen[key] {
			t.Errorf("enumSource names %s, which is no closed list of listSpecs", key)
		}
	}
}

// manyCloud answers n VMs of two zones and two flavors; zonesErr fails the
// read of the zones.
type manyCloud struct {
	filterCloud
	n        int
	zonesErr error
	// asked are the parameters each read of the VMs was narrowed by.
	asked []map[string]string
}

func (c *manyCloud) ListAvailabilityZones(ctx context.Context) ([]*provision.AvailabilityZone, error) {
	if c.zonesErr != nil {
		return nil, c.zonesErr
	}
	return c.filterCloud.ListAvailabilityZones(ctx)
}

func (c *manyCloud) ListVMsInProject(ctx context.Context, _ string) ([]*provision.VM, error) {
	c.asked = append(c.asked, provision.NarrowOf(ctx, "/api/v1/vms"))
	out := make([]*provision.VM, 0, c.n)
	for i := 0; i < c.n; i++ {
		zone, state := "1", "running"
		if i%2 == 1 {
			zone = "2"
		}
		if i%5 == 0 {
			state = "stopped"
		}
		out = append(out, &provision.VM{
			ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), Name: fmt.Sprintf("vm-%02d", i), Status: state,
			AvailabilityZone: &provision.ZoneRef{ID: "00000000-0000-4000-8000-0000000000a" + zone, Name: "ru.AZ-" + zone},
		})
	}
	return out, nil
}

func (*manyCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{
		{Name: "low-2-4", VCPUs: 2, RAMGB: 4},
		{Name: "low-4-8", VCPUs: 4, RAMGB: 8},
		{Name: "gen-2-8", VCPUs: 2, RAMGB: 8},
	}, nil
}

func callList(t *testing.T, r *Registry, tool string, args map[string]any) (map[string]any, string, bool) {
	t.Helper()
	text, isErr := r.handlers[tool](context.Background(), args)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("%s %v: not JSON: %s", tool, args, text)
	}
	return m, text, isErr
}

// An empty selection is a success that says what it is (condition 2 of
// the risk conclusion 104): the filters as applied — a zone as «name
// (id)» —, count 0, the total before the selection, who selected, and a
// hint that this is not «none in the project».
func TestListFilters_EmptySelectionNamesFiltersAndTotal(t *testing.T) {
	r, _ := targetRegistry(t, &filterCloud{})
	m, text, isErr := callList(t, r, "vm_list", map[string]any{"name": " DB ", "public_ip": true})
	if isErr {
		t.Fatalf("an empty selection is a success: %s", text)
	}
	filters, _ := m["filters"].(map[string]any)
	if filters["name"] != "DB" || filters["public_ip"] != true {
		t.Errorf("filters as applied: %v", m["filters"])
	}
	if m["count"] != float64(0) || m["total"] != float64(3) || m["filtered_by"] != "evo" {
		t.Errorf("count %v, total %v, filtered_by %v; want 0, 3, evo", m["count"], m["total"], m["filtered_by"])
	}
	if vms, ok := m["vms"].([]any); !ok || len(vms) != 0 {
		t.Errorf("vms: %v, want an empty list", m["vms"])
	}
	hint, _ := m["hint"].(string)
	if !strings.Contains(hint, "name, public_ip") || !strings.Contains(hint, "3") {
		t.Errorf("the hint names the filters to drop and the total: %q", hint)
	}

	// A filter the cloud narrows by as well: the zone is named «name
	// (id)», and there is no total — evo did not see the whole list
	// (the risk conclusion 104, condition 2: never total = count).
	m, text, isErr = callList(t, r, "vm_list", map[string]any{"zone": "ru.az-2", "state": "stopped"})
	filters, _ = m["filters"].(map[string]any)
	if isErr || filters["zone"] != "ru.AZ-2 (00000000-0000-4000-8000-0000000000a2)" || filters["state"] != "stopped" {
		t.Errorf("filters as applied: %s", text)
	}
	hint, _ = m["hint"].(string)
	if _, has := m["total"]; has || m["count"] != float64(0) || m["filtered_by"] != "cloud, evo" || !strings.Contains(hint, "outside the selection may exist") {
		t.Errorf("a selection the cloud narrowed: want count 0, no total, filtered_by «cloud, evo» and the hint of it, got %s", text)
	}
}

// A call without filters answers what the handler answers, to the byte: a
// path that reads the whole list (the free name of a create, the cleanup,
// Tab) sees no difference. A list over filterHintRows items adds one line
// that names the filters; a selection is shorter than the list it is from.
func TestListFilters_NoFilterIsTheWholeList(t *testing.T) {
	small, _ := targetRegistry(t, &filterCloud{})
	m, _, _ := callList(t, small, "vm_list", map[string]any{})
	for _, k := range []string{"filters", "count", "total", "hint", "filtered_by"} {
		if _, has := m[k]; has {
			t.Errorf("a short list without filters carries %q", k)
		}
	}
	if vms, _ := m["vms"].([]any); len(vms) != 3 {
		t.Errorf("want the 3 VMs of the project, got %d", len(vms))
	}

	big, _ := targetRegistry(t, &manyCloud{n: filterHintRows + 5})
	all, whole, _ := callList(t, big, "vm_list", map[string]any{})
	if vms, _ := all["vms"].([]any); len(vms) != filterHintRows+5 {
		t.Fatalf("want every VM, got %d", len(vms))
	}
	hint, _ := all["hint"].(string)
	for _, f := range []string{"zone", "state", "name"} {
		if !strings.Contains(hint, f) {
			t.Errorf("the hint of a long list names filter %s: %q", f, hint)
		}
	}
	sel, part, isErr := callList(t, big, "vm_list", map[string]any{"zone": "ru.AZ-1", "state": "running"})
	if _, has := sel["total"]; isErr || sel["count"] != float64(10) || has {
		t.Fatalf("selection: count %v, total %v (the cloud narrowed: none): %s", sel["count"], sel["total"], part)
	}
	byName, text, _ := callList(t, big, "vm_list", map[string]any{"name": "vm-1"})
	if byName["count"] != float64(10) || byName["total"] != float64(filterHintRows+5) {
		t.Fatalf("selection by evo alone: count %v of %v: %s", byName["count"], byName["total"], text)
	}
	if _, has := sel["hint"]; has {
		t.Errorf("a selection that is not empty needs no hint: %v", sel["hint"])
	}
	if len(part) >= len(whole) {
		t.Errorf("the selection (%d bytes) is not shorter than the list (%d bytes)", len(part), len(whole))
	}
}

// A reference the filter cannot resolve is a refusal, never an empty list
// (condition 4 of the risk conclusion 104): the zones could not be read —
// the error of that read; a flavor the catalogue lacks — USAGE.
func TestListFilters_UnresolvedReferenceIsARefusal(t *testing.T) {
	down, _ := targetRegistry(t, &manyCloud{n: 3, zonesErr: errors.New("the zones are down")})
	m, text, isErr := callList(t, down, "vm_list", map[string]any{"zone": "ru.AZ-1"})
	if !isErr || !strings.Contains(text, "the zones are down") || m["vms"] != nil {
		t.Errorf("zones unreadable: want the error of the read and no list, got %s", text)
	}
	r, _ := targetRegistry(t, &manyCloud{n: 3})
	m, text, isErr = callList(t, r, "vm_list", map[string]any{"flavor": "no-such-flavor"})
	if !isErr || m["reason"] != "USAGE" || !strings.Contains(text, "list_flavors") {
		t.Errorf("an unknown flavor: want USAGE naming list_flavors, got %s", text)
	}
}

// The handler's own filters and the registry's go together: the answer
// names both, and gives no total — what the handler narrowed is not in
// the list evo selected from.
func TestListFilters_WithTheHandlersOwn(t *testing.T) {
	r, _ := targetRegistry(t, &manyCloud{n: 3})
	m, text, isErr := callList(t, r, "list_flavors", map[string]any{"cpu": 2, "name": "LOW"})
	if isErr {
		t.Fatalf("list_flavors: %s", text)
	}
	filters, _ := m["filters"].(map[string]any)
	if filters["name"] != "LOW" || filters["cpu"] != float64(2) {
		t.Errorf("filters: %v, want name and cpu", m["filters"])
	}
	if _, has := m["total"]; has {
		t.Errorf("total %v: unknown when the handler narrowed the list itself", m["total"])
	}
	rows, _ := m["flavors"].([]any)
	var names []string
	for _, row := range rows {
		item, _ := row.(map[string]any)
		names = append(names, stringArg(item, "name"))
		if !strings.Contains(strings.ToLower(stringArg(item, "name")), "low") || item["vcpus"] != float64(2) {
			t.Errorf("flavor %v passes neither filter", item)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "low-2-4" {
		t.Errorf("flavors %v, want low-2-4 alone", names)
	}
	if m["count"] != float64(len(rows)) {
		t.Errorf("count %v for %d flavors (%v)", m["count"], len(rows), names)
	}

	// Nothing passed: no total either, and the hint says what evo cannot
	// know — resources outside the selection may exist.
	m, text, isErr = callList(t, r, "list_flavors", map[string]any{"cpu": 2, "name": "no-such"})
	hint, _ := m["hint"].(string)
	if _, has := m["total"]; isErr || has || m["count"] != float64(0) || !strings.Contains(hint, "outside the selection may exist") {
		t.Errorf("an empty selection the handler narrowed too: want count 0, no total and the hint of it, got %s", text)
	}
}

// cloudProbed are the parameters of the Compute lists the probe confirmed
// to answer what evo's rule answers, in two runs of two (api-probe; release
// plan 1.8.2): the only ones a filter may hand to the cloud (condition 3
// of the risk conclusion 104). A parameter the probe did not confirm stays
// evo's; the results are in the closed repository.
var cloudProbed = map[string]bool{
	"/api/v1/vms availability_zone_ids":            true,
	"/api/v1/vms statuses":                         true,
	"/api/v1/disks availability_zone_id":           true,
	"/api/v1/floating-ips availability_zone_id":    true,
	"/api/v1/subnets availability_zone_ids":        true,
	"/api/v1/security-groups availability_zone_id": true,
}

func TestListSpecs_CloudParametersAreProbed(t *testing.T) {
	seen := map[string]bool{}
	for name, s := range listSpecs {
		for _, f := range s.filters {
			if (f.cloudPath == "") != (f.cloudParam == "") {
				t.Errorf("%s.%s: a cloud parameter wants its path and its name", name, f.arg)
			}
			if f.cloudParam == "" {
				continue
			}
			key := f.cloudPath + " " + f.cloudParam
			seen[key] = true
			if !cloudProbed[key] {
				t.Errorf("%s.%s: %s goes to the cloud unprobed — two runs of the probe first", name, f.arg, key)
			}
			if f.kind != filterRef && f.kind != filterEnum {
				t.Errorf("%s.%s: only an exact value (a reference, a closed list) goes to the cloud", name, f.arg)
			}
		}
	}
	for key := range cloudProbed {
		if !seen[key] {
			t.Errorf("cloudProbed names %s, which no filter hands to the cloud", key)
		}
	}
}

// The list a selection reads is narrowed by the cloud with the probed
// parameters — the id of the zone, the state — and never by a filter the
// cloud does not take; a list read without filters is asked whole.
func TestListFilters_NarrowsTheCloudRead(t *testing.T) {
	cloud := &manyCloud{n: 4}
	r, _ := targetRegistry(t, cloud)
	callList(t, r, "vm_list", map[string]any{"zone": "ru.AZ-2", "state": "RUNNING", "name": "vm"})
	callList(t, r, "vm_list", map[string]any{"name": "vm"})
	callList(t, r, "vm_list", map[string]any{})
	want := []map[string]string{
		{"availability_zone_ids": "00000000-0000-4000-8000-0000000000a2", "statuses": "running"}, nil, nil,
	}
	if !reflect.DeepEqual(cloud.asked, want) {
		t.Errorf("the reads of the VMs were narrowed by %v, want %v", cloud.asked, want)
	}
}
