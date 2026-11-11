// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Filters of the lists (release plan 1.8.2, briefs/list-filters.md): a list
// declares its filters here, and one wrapper (filtered) serves them all —
// the schema MCP sends, the flags of the command, the help and the docs
// come from the same declaration. The rule of the selection is evo's: a
// filter is named as the field of the item it reads («what you see is what
// you filter by»), several filters are «and», and evo checks every item by
// that rule itself — the handler reads the whole list, and what the cloud
// narrowed on the way (none yet: a parameter of the API goes in after its
// probe) is only ever a speed-up.
//
// Filters are for reads. A path that writes, or decides before a write —
// the free name of a create, «already deleted» (D26), a cleanup, destroy,
// a stack, an interrupted call — reads the whole list: a filter cannot
// hide a resource from it.

// filterKind is how a filter compares its value with the item.
type filterKind int

const (
	// filterText: the value is a part of the field, in any case.
	filterText filterKind = iota
	// filterEnum: the field is the value, in any case; the values are the
	// closed list of the published specification (enum of the schema).
	filterEnum
	// filterRef: the value names an entry of another list (a zone, a VPC,
	// a flavor) by id or by name; an entry the list lacks is refused.
	filterRef
	// filterBool: the field is set (true, not empty, above zero), or not.
	filterBool
)

// listFilter is one filter of a list.
type listFilter struct {
	// arg is the argument, named as the field of the item.
	arg  string
	kind filterKind
	// fields are the fields of the item the value is compared with; none:
	// the field named as arg.
	fields []string
	// values are the values of a filterEnum.
	values []string
	// ref is the read tool that lists what a filterRef names.
	ref string
	// en and ru describe the argument, in one line: the schema and the
	// flag of the command.
	en, ru string
}

// listSpec is a list tool and its filters.
type listSpec struct {
	// rows is the key of the items in the answer.
	rows    string
	filters []listFilter
	// own are the filters the handler applies itself, as it did before
	// 1.8.2 (their names and the answer stay); they are declared in the
	// schema of the tool. An argument that adds to the answer (rules of
	// list_security_groups, all of image_list) is no filter.
	own []string
	// none is why a list takes no filter.
	none string
}

func textFilter(arg, en, ru string, fields ...string) listFilter {
	return listFilter{arg: arg, kind: filterText, fields: fields, en: en, ru: ru}
}

func nameFilter() listFilter {
	return textFilter("name", "Only with this in the name, any case.", "Только с этой частью имени, регистр не важен")
}

func enumFilter(arg string, values ...string) listFilter {
	en, ru := "Only in this state.", "Только в этом состоянии"
	if arg == "type" {
		en, ru = "Only of this type.", "Только этого типа"
	}
	return listFilter{arg: arg, kind: filterEnum, values: values, en: en, ru: ru}
}

func zoneFilter(fields ...string) listFilter {
	return listFilter{arg: "zone", kind: filterRef, ref: "list_zones", fields: fields,
		en: "Only in this zone: name or UUID.", ru: "Только в этой зоне: имя или UUID"}
}

func vpcFilter() listFilter {
	return listFilter{arg: "vpc_id", kind: filterRef, ref: "vpc_list",
		en: "Only in this VPC: UUID or name.", ru: "Только в этой VPC: UUID или имя"}
}

func boolFilter(arg, en, ru string, fields ...string) listFilter {
	return listFilter{arg: arg, kind: filterBool, fields: fields, en: en, ru: ru}
}

// The states of the published specifications of Cloud.ru (the enum of the
// item of each list; scripts/api-contract.json names them at the request,
// and job contract compares them with the specification).
var (
	vmStates      = []string{"running", "stopped", "error", "error_creating", "error_deleting", "creating", "starting", "stopping", "rebooting", "deleting", "updating"}
	diskStates    = []string{"available", "in_use", "reserved", "error", "error_creating", "error_deleting", "creating", "updating", "deleting", "attaching", "detaching"}
	fipStates     = []string{"available", "in_use", "error", "error_creating", "error_deleting", "creating", "deleting", "updating", "attaching", "detaching"}
	networkStates = []string{"created", "error", "error_creating", "error_deleting", "creating", "deleting", "updating"}
	k8sStatuses   = []string{"OBJECT_STATUS_PENDING", "OBJECT_STATUS_PROVISIONING", "OBJECT_STATUS_RUNNING", "OBJECT_STATUS_RESUMING", "OBJECT_STATUS_DELETING", "OBJECT_STATUS_ERROR", "OBJECT_STATUS_SCALING_UP", "OBJECT_STATUS_SCALING_DOWN", "OBJECT_STATUS_STOPPING", "OBJECT_STATUS_STOPPED", "OBJECT_STATUS_SUSPENDING", "OBJECT_STATUS_SUSPENDED", "OBJECT_STATUS_UPGRADING", "OBJECT_STATUS_EDITING", "OBJECT_STATUS_ADDING", "OBJECT_STATUS_DELETED"}
	dbaasStatuses = []string{"STATUS_UNSPECIFIED", "PENDING", "DEPLOYING", "FAILED", "UPDATING", "READY", "SUSPENDED", "SUSPENDING", "ERASED", "ERASING", "ROLLING_BACK"}
	lbStatuses    = []string{"NLB_STATUS_UNSPECIFIED", "NLB_STATUS_CREATING", "NLB_STATUS_ERROR_CREATING", "NLB_STATUS_RUNNING", "NLB_STATUS_UPDATING", "NLB_STATUS_ERROR_UPDATING", "NLB_STATUS_DELETING", "NLB_STATUS_ERROR_DELETING", "NLB_STATUS_DELETED", "NLB_STATUS_SUSPENDING", "NLB_STATUS_SUSPENDED", "NLB_STATUS_ERROR_SUSPENDING", "NLB_STATUS_RESUMING", "NLB_STATUS_ERROR_RESUMING"}
	tgStatuses    = []string{"TARGET_GROUP_STATUS_UNSPECIFIED", "TARGET_GROUP_STATUS_CREATING", "TARGET_GROUP_STATUS_ERROR_CREATING", "TARGET_GROUP_STATUS_CREATED", "TARGET_GROUP_STATUS_UPDATING", "TARGET_GROUP_STATUS_ERROR_UPDATING", "TARGET_GROUP_STATUS_DELETING", "TARGET_GROUP_STATUS_ERROR_DELETING", "TARGET_GROUP_STATUS_DELETED"}
	arStatuses    = []string{"CREATING", "ACTIVE", "ERROR"}
	arTypes       = []string{"DOCKER", "DEBIAN", "RPM", "GENERIC", "PYPI", "NPM"}
)

// listSpecs are the lists and their filters, by tool. A list tool is here
// with its filters or with why it has none (TestListSpecs_*).
var listSpecs = map[string]listSpec{
	// Compute
	"vm_list": {rows: "vms", filters: []listFilter{
		zoneFilter(), enumFilter("state", vmStates...), nameFilter(),
		{arg: "flavor", kind: filterRef, ref: "list_flavors", en: "Only of this flavor: name.", ru: "Только с этим flavor: имя"},
		boolFilter("public_ip", "true: only with a public IP; false: only without.", "true — только с публичным IP, false — только без него"),
	}},
	"disk_list": {rows: "disks", filters: []listFilter{
		zoneFilter(), enumFilter("state", diskStates...), nameFilter(),
		boolFilter("attached", "true: only attached to a VM; false: only free.", "true — только подключённые к ВМ, false — только свободные", "attached_to"),
		{arg: "type", kind: filterRef, ref: "list_disk_types", fields: []string{"disk_type"}, en: "Only of this disk type: name.", ru: "Только этого типа диска: имя"},
		boolFilter("bootable", "true: only bootable; false: only the others.", "true — только загрузочные, false — только остальные"),
	}},
	"list_floating_ips": {rows: "floating_ips", filters: []listFilter{
		zoneFilter(), enumFilter("state", fipStates...), nameFilter(),
		boolFilter("free", "true: only free to attach; false: only taken.", "true — только свободные для привязки, false — только занятые", "available"),
	}},
	"list_subnets": {rows: "subnets", filters: []listFilter{
		zoneFilter("availability_zone_id", "availability_zone_name"), enumFilter("state", networkStates...), nameFilter(), vpcFilter(),
	}},
	"list_security_groups": {rows: "security_groups", filters: []listFilter{
		zoneFilter("availability_zone_id", "availability_zone_name"), nameFilter(),
	}},
	"snat_list": {rows: "nat_gateways", filters: []listFilter{
		zoneFilter("availability_zone_id", "availability_zone"), enumFilter("status", networkStates...), nameFilter(),
	}},
	"list_flavors": {rows: "flavors", own: []string{"cpu", "ram", "share", "zone", "gpu", "spot"}, filters: []listFilter{nameFilter()}},
	"image_list":   {rows: "images", own: []string{"query", "type", "zone"}},

	// VPC, DNS
	"vpc_list": {rows: "vpcs", filters: []listFilter{nameFilter()}},
	"dns_zones": {rows: "zones", filters: []listFilter{
		nameFilter(),
		textFilter("domain", "Only with this in the domain, any case.", "Только с этой частью домена, регистр не важен"),
	}},
	"dns_records": {rows: "records", own: []string{"zone_id", "type"}, filters: []listFilter{nameFilter()}},

	// Managed Kubernetes, the databases
	"k8s_list":      {rows: "clusters", filters: []listFilter{enumFilter("status", k8sStatuses...), nameFilter()}},
	"k8s_nodepools": {rows: "node_pools", own: []string{"cluster_id"}, filters: []listFilter{enumFilter("status", k8sStatuses...), nameFilter()}},
	"pg_list":       {rows: "clusters", filters: []listFilter{enumFilter("status", dbaasStatuses...), nameFilter()}},
	"kafka_list":    {rows: "clusters", filters: []listFilter{enumFilter("status", dbaasStatuses...), nameFilter()}},
	"redis_list":    {rows: "clusters", filters: []listFilter{enumFilter("status", dbaasStatuses...), nameFilter()}},

	// The balancer
	"lb_list": {rows: "load_balancers", filters: []listFilter{enumFilter("status", lbStatuses...), nameFilter(), vpcFilter()}},
	"tg_list": {rows: "target_groups", filters: []listFilter{enumFilter("status", tgStatuses...), nameFilter(), vpcFilter()}},

	// Artifact Registry, Container Apps, the secrets, the certificates, S3
	"ar_list": {rows: "registries", filters: []listFilter{enumFilter("status", arStatuses...), nameFilter(), enumFilter("type", arTypes...)}},
	// No status: the published specification of Container Apps gives the
	// status as a string with no list of values (condition 5 of the risk
	// conclusion 104: the values are the specification's, not evo's).
	"ca_list": {rows: "containers", filters: []listFilter{nameFilter()}},
	"secret_list": {rows: "secrets", filters: []listFilter{
		textFilter("name", "Only with this in the name or the path, any case.", "Только с этой частью имени или пути, регистр не важен", "name", "path"),
	}},
	"cert_list":  {rows: "certificates", own: []string{"name", "type", "dns_name", "common_name", "state"}},
	"s3_buckets": {rows: "buckets", filters: []listFilter{nameFilter()}},

	// The catalogues the filters themselves resolve by, and the short
	// lists: no filter, with why.
	"list_zones":       {rows: "zones", none: "a catalogue of a few zones"},
	"list_disk_types":  {rows: "disk_types", none: "a catalogue of a few types"},
	"list":             {none: "the records of this directory, read from the local state: a few"},
	"list_images":      {none: "deprecated (v2.0): image_list searches the catalogue"},
	"marketplace_list": {none: "a search of the catalogue by its own query and kind, not a list of the project"},
}

// filterHintRows is how many items a list answers without a filter before
// it says, in one line, which filters narrow it: one number for every
// list. A shorter list says nothing — the line would cost more than it
// saves.
const filterHintRows = 20

// itemFields are the fields of the item f reads.
func (f listFilter) itemFields() []string {
	if len(f.fields) > 0 {
		return f.fields
	}
	return []string{f.arg}
}

// prop is the property of f in the schema of its tool.
func (f listFilter) prop() map[string]any {
	switch f.kind {
	case filterEnum:
		return enumProp(f.en, f.values...)
	case filterBool:
		return boolProp(f.en)
	}
	return strProp(f.en)
}

// declare puts the filters of the tool into its schema and its Russian
// argument descriptions: the one place a filter becomes an argument.
func (s listSpec) declare(t *Tool) {
	if len(s.filters) == 0 {
		return
	}
	props, _ := t.InputSchema["properties"].(map[string]any)
	for _, f := range s.filters {
		props[f.arg] = f.prop()
		if t.ArgDescriptionsRU[f.arg] == "" {
			t.ArgDescriptionsRU[f.arg] = f.ru
		}
	}
}

// ListFilters are the filter arguments of tool t, the handler's own
// included, in the order of the declaration; nil for a tool that is no
// list or takes none.
func ListFilters(t Tool) []string {
	return listSpecs[t.Name].args()
}

// ListFilterValues are the values filter arg of list t takes, when they
// are a closed list.
func ListFilterValues(t Tool, arg string) []string {
	for _, f := range listSpecs[t.Name].filters {
		if f.arg == arg {
			return f.values
		}
	}
	return nil
}

// FilterSample is a filter of a list with a value for an example.
type FilterSample struct{ Arg, Value string }

// sampleStates are the states an example filters by, the first a filter
// has.
var sampleStates = []string{"running", "available", "created", "OBJECT_STATUS_RUNNING", "READY", "NLB_STATUS_RUNNING", "TARGET_GROUP_STATUS_CREATED", "ACTIVE"}

// ListFilterSamples are the first filters of t (two at most, of the ones
// the registry serves) with a value each: the example of the command's
// help and of the docs.
func ListFilterSamples(t Tool) []FilterSample {
	var out []FilterSample
	for _, f := range listSpecs[t.Name].filters {
		v := ""
		switch {
		case f.arg == "zone":
			v = "ru.AZ-2"
		case f.kind == filterEnum && f.arg != "type":
			for _, st := range sampleStates {
				for _, known := range f.values {
					if v == "" && known == st {
						v = st
					}
				}
			}
		case f.kind == filterText:
			v = "web"
		}
		if v != "" && len(out) < 2 {
			out = append(out, FilterSample{f.arg, v})
		}
	}
	return out
}

// given reports whether the call sets argument k: a string of spaces, or
// nothing, sets none.
func given(args map[string]any, k string) bool {
	switch v := args[k].(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	}
	return true
}

// condition is a filter with the value of a call.
type condition struct {
	f listFilter
	// want are the values an item's field may be (filterEnum: the value;
	// filterRef: the id and the name of the entry; filterText: the part).
	want []string
	on   bool
	// shown is the filter as the answer names it: a reference as
	// «name (id)».
	shown any
}

// matches reports whether the item passes the condition.
func (c condition) matches(row map[string]any) bool {
	for _, k := range c.f.itemFields() {
		v := row[k]
		switch c.f.kind {
		case filterBool:
			if isSet(v) == c.on {
				return true
			}
		case filterText:
			if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), c.want[0]) {
				return true
			}
		default:
			for _, s := range fieldValues(v) {
				for _, w := range c.want {
					if strings.EqualFold(s, w) {
						return true
					}
				}
			}
		}
	}
	return false
}

// fieldValues are the strings a field of an item holds: the string, or
// the id and the name of an object.
func fieldValues(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case map[string]any:
		var out []string
		for _, k := range []string{"id", "name"} {
			if s, ok := x[k].(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// isSet reports whether a field of an item says «yes»: true, a string or
// a list that is not empty, a number above zero.
func isSet(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case json.Number:
		f, _ := x.Float64()
		return f > 0
	}
	return false
}

// filtered is the handler of a list under its filters: the values are
// checked and the references resolved before the list is read (a value
// evo does not know, or a reference that is not there or cannot be read,
// is refused — never an empty list); the handler reads the whole list;
// evo keeps the items that pass every filter and says what it applied,
// how many passed and of how many. A call without filters answers as the
// handler does, with one line of a hint when the list is long.
func (r *Registry) filtered(s listSpec, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		var conds []condition
		for _, f := range s.filters {
			if !given(args, f.arg) {
				continue
			}
			if f.kind == filterRef && len(conds) == 0 {
				// The refusals of the handler's engine come first, with
				// no request: no key, no project (engineFor).
				if _, err := r.engineFor(ctx, nil); err != nil {
					return errResult(err), true
				}
			}
			c, refusal, err := r.condition(ctx, f, args[f.arg])
			if err != nil {
				return errResult(err), true
			}
			if refusal != "" {
				return refusal, true
			}
			conds = append(conds, c)
		}
		ownGiven := false
		for _, a := range s.own {
			ownGiven = ownGiven || given(args, a)
		}
		rest := args
		if len(conds) > 0 {
			rest = make(map[string]any, len(args))
			for k, v := range args {
				rest[k] = v
			}
			for _, f := range s.filters {
				delete(rest, f.arg)
			}
		}
		text, isErr := next(ctx, rest)
		if isErr {
			return text, true
		}
		if len(conds) == 0 && ownGiven {
			return text, false
		}
		m, rows, err := listRows(text, s.rows)
		if err != nil {
			if len(conds) == 0 {
				return text, false
			}
			// A part of a list is never answered as the selection.
			return errResult(err), true
		}
		if len(conds) == 0 {
			if len(rows) <= filterHintRows {
				return text, false
			}
			m["hint"] = i18n.Tf("%d items; narrow the list with: %s.", "Элементов: %d; сузить список: %s.",
				len(rows), strings.Join(s.args(), ", "))
			return toJSON(m), false
		}
		kept := make([]any, 0, len(rows))
		for _, row := range rows {
			item, _ := row.(map[string]any)
			pass := item != nil
			for _, c := range conds {
				pass = pass && c.matches(item)
			}
			if pass {
				kept = append(kept, row)
			}
		}
		applied := make(map[string]any, len(conds))
		names := make([]string, 0, len(conds))
		for _, c := range conds {
			applied[c.f.arg] = c.shown
			names = append(names, c.f.arg)
		}
		for _, a := range s.own {
			if given(args, a) {
				applied[a] = args[a]
			}
		}
		m[s.rows] = kept
		m["filters"] = applied
		m["count"] = len(kept)
		m["filtered_by"] = "evo"
		// What the handler narrowed itself is not in rows: the total is
		// known only when evo alone selected.
		if !ownGiven {
			m["total"] = len(rows)
		}
		if len(kept) == 0 {
			m["hint"] = emptySelectionHint(names, len(rows), ownGiven)
		}
		return toJSON(m), false
	}
}

// emptySelectionHint says what an empty selection is and is not: nothing
// passed the filters — which is not «the project has none».
func emptySelectionHint(filters []string, total int, ownGiven bool) string {
	drop := strings.Join(filters, ", ")
	if ownGiven {
		return i18n.Tf("Nothing passed the filters; resources outside the selection may exist. Drop a filter (%s) or list without filters before deciding there is none.",
			"Под отбор ничего не подошло; вне отбора ресурсы могут быть. Снимите фильтр (%s) или запросите список без фильтров, прежде чем считать, что ресурса нет.", drop)
	}
	return i18n.Tf("Nothing passed the filters; %d without them. This is not «none in the project»: drop a filter (%s) or list without filters.",
		"Под отбор ничего не подошло; без фильтров элементов: %d. Это не «в проекте нет»: снимите фильтр (%s) или запросите список без фильтров.", total, drop)
}

// args are the filter arguments of the list, the handler's own included.
func (s listSpec) args() []string {
	if len(s.filters)+len(s.own) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.filters)+len(s.own))
	for _, f := range s.filters {
		out = append(out, f.arg)
	}
	return append(out, s.own...)
}

// listRows is the answer of a list tool and its items.
func listRows(text, key string) (map[string]any, []any, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, nil, fmt.Errorf(i18n.T("read the list to select from: %w", "чтение списка для отбора: %w"), err)
	}
	rows, ok := m[key].([]any)
	if !ok {
		return nil, nil, fmt.Errorf(i18n.T("the list to select from has no %q", "в списке для отбора нет %q"), key)
	}
	return m, rows, nil
}

// condition is filter f with value v, checked: an enum is in the spelling
// of the schema already (checkArgs); a reference is resolved by its list,
// and refusal is the error answer of that list when it could not be read.
func (r *Registry) condition(ctx context.Context, f listFilter, v any) (_ condition, refusal string, _ error) {
	c := condition{f: f, shown: v}
	switch f.kind {
	case filterBool:
		c.on, _ = v.(bool)
	case filterText:
		s := strings.TrimSpace(stringOf(v))
		c.want, c.shown = []string{strings.ToLower(s)}, s
	case filterEnum:
		c.want = []string{stringOf(v)}
	case filterRef:
		id, name, refusal, err := r.listed(ctx, f, strings.TrimSpace(stringOf(v)))
		if err != nil || refusal != "" {
			return c, refusal, err
		}
		c.want = nonEmpty(id, name)
		c.shown = addrLabel(name, id)
	}
	return c, "", nil
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// knownShown is how many entries of a reference's list a refusal names;
// a longer list is named by its tool.
const knownShown = 12

// listed is the entry of f's list that ref names, by id or by name (any
// case): its id and its name. The list is read for the call; a list that
// cannot be read (refusal: its own error answer, as it is), an entry it
// lacks and a name several entries share are refusals — a filter by
// something evo cannot find is not an empty list (condition 4 of the risk
// conclusion 104).
func (r *Registry) listed(ctx context.Context, f listFilter, ref string) (id, name, refusal string, _ error) {
	text, isErr := r.unchecked[f.ref](ctx, map[string]any{})
	if isErr {
		return "", "", text, nil
	}
	_, rows, err := listRows(text, listSpecs[f.ref].rows)
	if err != nil {
		return "", "", "", err
	}
	type entry struct{ id, name string }
	var found []entry
	var known []string
	for _, row := range rows {
		item, _ := row.(map[string]any)
		e := entry{stringOf(item["id"]), stringOf(item["name"])}
		known = append(known, firstNonEmptyStr(e.name, e.id))
		if (e.id != "" && strings.EqualFold(e.id, ref)) || (e.name != "" && strings.EqualFold(e.name, ref)) {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 1:
		return found[0].id, found[0].name, "", nil
	case 0:
		sort.Strings(known)
		hint := i18n.Tf("Known: %s.", "Известные: %s.", strings.Join(known, ", "))
		if len(known) == 0 || len(known) > knownShown {
			hint = i18n.Tf("See %s for the known ones.", "Известные — в %s.", f.ref)
		}
		return "", "", "", clierrors.New(clierrors.CodeUsage,
			i18n.Tf("filter %s: %s is not in %s; nothing was listed", "фильтр %s: %s нет в %s; список не прочитан", f.arg, quoteCloud(ref), f.ref), hint)
	}
	ids := make([]string, len(found))
	for i, e := range found {
		ids[i] = e.id
	}
	return "", "", "", clierrors.New(clierrors.CodeUsage,
		i18n.Tf("filter %s: %d entries of %s are named %s: %s", "фильтр %s: в %s имя %[4]s носят %[2]d: %[5]s", f.arg, len(found), f.ref, quoteCloud(ref), strings.Join(ids, ", ")),
		i18n.Tf("Pass %s as an id.", "Передайте %s как id.", f.arg))
}
