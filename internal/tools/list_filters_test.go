// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// listFilters are the filters every list takes (brief list-filters.md,
// release plan 1.8.2): one vocabulary on both surfaces, named as the field
// of the item in the answer — state or status, as the item carries it
// (checked against the handlers on main, 11.11). A list here takes each of them as an argument
// of its schema; a list not here declares its filters or why it has none
// (the class invariant, once the registry declares them).
var listFilters = map[string][]string{
	"vm_list":              {"zone", "state", "name", "flavor", "public_ip"},
	"disk_list":            {"zone", "state", "name", "attached", "type", "bootable"},
	"list_floating_ips":    {"zone", "state", "name", "free"},
	"list_subnets":         {"zone", "state", "name", "vpc_id"},
	"list_security_groups": {"zone", "name"},
	"snat_list":            {"zone", "status", "name"},
	"list_flavors":         {"zone", "name", "cpu", "ram", "gpu", "spot"},
	"image_list":           {"query", "type", "zone", "all"},
	"vpc_list":             {"name"},
	"dns_zones":            {"name", "domain"},
	"dns_records":          {"zone_id", "type", "name"},
	"k8s_list":             {"status", "name"},
	"k8s_nodepools":        {"cluster_id", "status", "name"},
	"pg_list":              {"status", "name"},
	"kafka_list":           {"status", "name"},
	"redis_list":           {"status", "name"},
	"lb_list":              {"status", "name", "vpc_id"},
	"tg_list":              {"status", "name", "vpc_id"},
	"ar_list":              {"status", "name", "type"},
	"ca_list":              {"name"}, // status: no enum in the spec — the reason is in the registry's declaration
	"secret_list":          {"name"},
	"cert_list":            {"name", "type", "dns_name", "common_name", "state"},
	"s3_buckets":           {"name"},
}

// ownStateValues are the state or status filters a list had before 1.8.2
// whose value goes to the API as is: an enum would turn the API's answer to
// a wrong value into USAGE, exit 1 — a breaking change the brief keeps out
// ("у cert_list прежнее остаётся"). A closed list, not a list of misses: no
// line is added, and a line leaves with the breaking change that gives its
// filter an enum.
var ownStateValues = map[string]string{
	"cert_list": "state: the API constants CERTIFICATE_STATE_*, passed as is since 1.6; an enum is a breaking change",
}

// Every list of the brief takes its filters as arguments of its schema,
// and a state or status filter declares the values evo knows (enum): an
// unknown value is USAGE before the request, the same on both surfaces,
// not an empty list (brief list-filters.md, «Семантика»).
func TestListFilters_EveryListTakesItsFilters(t *testing.T) {
	r := New(nil)
	var bad []string
	for name, want := range listFilters {
		tl, ok := r.Lookup(name)
		if !ok {
			bad = append(bad, name+": no such tool")
			continue
		}
		if tl.Mutating || HasSideEffects(tl) {
			bad = append(bad, name+": filters are for reads only")
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, f := range want {
			p, ok := props[f].(map[string]any)
			if !ok {
				bad = append(bad, name+": no filter "+f)
				continue
			}
			if (f == "state" || f == "status") && ownStateValues[name] == "" {
				if vals, _ := p["enum"].([]any); len(vals) == 0 {
					bad = append(bad, name+": filter "+f+" declares no values (enum): an unknown value would be an empty list, not USAGE")
				}
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// filterCloud answers the VMs of two zones whatever the request asks: a
// cloud that ignores a filter, or narrows it otherwise than evo, gives
// evo a superset.
type filterCloud struct {
	targetCloud
}

func (*filterCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{
		{ID: "00000000-0000-4000-8000-0000000000a1", Name: "ru.AZ-1"},
		{ID: "00000000-0000-4000-8000-0000000000a2", Name: "ru.AZ-2"},
	}, nil
}

func (*filterCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	vm := func(id, name, state, zone, zoneID string) *provision.VM {
		return &provision.VM{ID: id, Name: name, Status: state, AvailabilityZone: &provision.ZoneRef{ID: zoneID, Name: zone}}
	}
	return []*provision.VM{
		vm("00000000-0000-4000-8000-000000000001", "web-1", "running", "ru.AZ-1", "00000000-0000-4000-8000-0000000000a1"),
		vm("00000000-0000-4000-8000-000000000002", "web-2", "stopped", "ru.AZ-1", "00000000-0000-4000-8000-0000000000a1"),
		vm("00000000-0000-4000-8000-000000000003", "db-1", "running", "ru.AZ-2", "00000000-0000-4000-8000-0000000000a2"),
	}, nil
}

// evo checks the selection by its own rule even when the cloud filtered:
// the cloud here ignores every filter, and the answer holds only what the
// filters name. Several filters are "and"; name is a part of the name,
// case-insensitive; an empty selection is a success; an unknown zone or
// state is USAGE naming what is known (brief list-filters.md).
func TestVMList_EvoChecksTheSelectionItself(t *testing.T) {
	r, _ := targetRegistry(t, &filterCloud{})
	call := func(args map[string]any) (map[string]any, bool, string) {
		t.Helper()
		text, isErr := r.handlers["vm_list"](context.Background(), args)
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		return m, isErr, text
	}
	names := func(m map[string]any) []string {
		vms, _ := m["vms"].([]any)
		var out []string
		for _, v := range vms {
			if row, ok := v.(map[string]any); ok {
				out = append(out, stringArg(row, "name"))
			}
		}
		sort.Strings(out)
		return out
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"zone": "ru.AZ-1"}, "web-1,web-2"},
		{map[string]any{"zone": "00000000-0000-4000-8000-0000000000a2"}, "db-1"},
		{map[string]any{"zone": "ru.AZ-1", "state": "RUNNING"}, "web-1"},
		{map[string]any{"name": "WEB"}, "web-1,web-2"},
		{map[string]any{"zone": "ru.AZ-2", "state": "stopped"}, ""},
	} {
		m, isErr, text := call(c.args)
		if isErr {
			t.Errorf("%v: an error, want the selection %q: %s", c.args, c.want, text)
			continue
		}
		if got := strings.Join(names(m), ","); got != c.want {
			t.Errorf("%v: %q, want %q (evo checks the selection itself)", c.args, got, c.want)
		}
	}
	for _, c := range []struct {
		args  map[string]any
		named string
	}{
		{map[string]any{"zone": "ru.AZ-9"}, "ru.AZ-1"},
		{map[string]any{"state": "sleeping"}, "running"},
	} {
		m, isErr, text := call(c.args)
		if !isErr || stringArg(m, "reason") != "USAGE" || !strings.Contains(text, c.named) {
			t.Errorf("%v: want USAGE naming %q among the known values, got %s", c.args, c.named, text)
		}
	}
}

// An enum filter says «state» only of an argument of a state: «Only in this
// state» was the text of resource_type of backup_list (FB19I-24 (г)).
func TestListFilters_EnumSaysStateOnlyOfAState(t *testing.T) {
	for tool, spec := range listSpecs {
		for _, f := range spec.filters {
			if f.kind != filterEnum {
				continue
			}
			state := strings.HasPrefix(f.arg, "state") || strings.HasPrefix(f.arg, "status")
			if !state && (strings.Contains(f.en, "state") || strings.Contains(f.ru, "состоян")) {
				t.Errorf("%s: filter %s is not of a state and says so: %q / %q", tool, f.arg, f.en, f.ru)
			}
		}
	}
}
