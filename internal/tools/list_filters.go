// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
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
	// filterExact: the field is the value, in any case, and the values are
	// no closed list: an id.
	filterExact
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
	// card: the item of the list does not carry the field — the card of
	// the resource does, and card reads the field from it by the id of
	// the item (nil: the card names none). evo reads the card of each
	// item the other filters left (cardReads at most) and puts the field
	// into the row: the selection is checked by evo, and the answer shows
	// what it was selected by.
	card func(ctx context.Context, eng *engine.Engine, id string) (any, error)
	// many: the value is a list, and an item passes when its field is one
	// of the values (ids, states). An empty list and an empty value in it
	// are USAGE by the schema (prop), as an empty string of a filter is.
	many bool
	// ref is the read tool that lists what a filterRef names.
	ref string
	// en and ru describe the argument, in one line: the schema and the
	// flag of the command.
	en, ru string
	// cloudPath and cloudParam: the list of the API (its path) takes the
	// value — the id of a reference, the value of a closed list — as this
	// query parameter and answers what evo's rule answers, or more. Only a
	// parameter the probe found so in two runs (api-probe, release plan
	// 1.8.2); evo checks the items by its rule all the same.
	cloudPath, cloudParam string
}

// inCloud is f that the list of the API narrows too (cloudParam).
func (f listFilter) inCloud(path, param string) listFilter {
	f.cloudPath, f.cloudParam = path, param
	return f
}

// listSpec is a list tool and its filters.
type listSpec struct {
	// rows is the key of the items in the answer.
	rows    string
	filters []listFilter
	// own are the filters the handler applies itself, as it did before
	// 1.8.2 (their names and the answer stay); they are declared in the
	// schema of the tool. An argument that adds to the answer (rules of
	// list_security_groups, all of image_list) is no filter, nor is the
	// resource a list is read in (the cluster of k8s_nodepools, the zone
	// of dns_records, FB182-1): it does not narrow the list it names.
	own []string
	// noIDs is why a list with filters takes no «ids»: its item carries
	// no id (withSets).
	noIDs string
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

// idsFilter selects by several ids: the pack of ids another API gave.
func idsFilter() listFilter {
	return listFilter{arg: "ids", kind: filterExact, many: true, fields: []string{"id"},
		en: "Only these ids.", ru: "Только с одним из этих id"}
}

// anyOf is the filter by several values of the closed list of f, named as
// f in the plural: «state» stays as it is released, «states» takes a list.
func (f listFilter) anyOf() listFilter {
	arg := f.arg + "s"
	if f.arg == "status" {
		arg = "statuses"
	}
	return listFilter{arg: arg, kind: filterEnum, many: true, fields: []string{f.arg}, values: f.values,
		en: "Only these states: values of " + f.arg + ".", ru: "Только в одном из этих состояний: значения — как у " + f.arg}
}

// withSets gives every list that has filters its filters by several
// values (release plan 1.9, Вп-10), in one place: «ids» where the item
// carries an id (noIDs says why not), and the plural of each filter by
// the state. A new list gets them by being declared.
func withSets(specs map[string]listSpec) map[string]listSpec {
	for name, s := range specs {
		if len(s.filters) == 0 {
			continue
		}
		filters := append([]listFilter(nil), s.filters...)
		if s.noIDs == "" {
			filters = append(filters, idsFilter())
		}
		for _, f := range s.filters {
			if f.kind == filterEnum && (f.arg == "state" || f.arg == "status") {
				filters = append(filters, f.anyOf())
			}
		}
		s.filters = filters
		specs[name] = s
	}
	return specs
}

func boolFilter(arg, en, ru string, fields ...string) listFilter {
	return listFilter{arg: arg, kind: filterBool, fields: fields, en: en, ru: ru}
}

// The states of the published specifications of Cloud.ru (the enum of the
// item of each list; scripts/api-contract.json names them at the request,
// and job contract compares them with the specification).
var (
	vmStates         = []string{"running", "stopped", "error", "error_creating", "error_deleting", "creating", "starting", "stopping", "rebooting", "deleting", "updating"}
	diskStates       = []string{"available", "in_use", "reserved", "error", "error_creating", "error_deleting", "creating", "updating", "deleting", "attaching", "detaching"}
	fipStates        = []string{"available", "in_use", "error", "error_creating", "error_deleting", "creating", "deleting", "updating", "attaching", "detaching"}
	networkStates    = []string{"created", "error", "error_creating", "error_deleting", "creating", "deleting", "updating"}
	k8sStatuses      = []string{"OBJECT_STATUS_PENDING", "OBJECT_STATUS_PROVISIONING", "OBJECT_STATUS_RUNNING", "OBJECT_STATUS_RESUMING", "OBJECT_STATUS_DELETING", "OBJECT_STATUS_ERROR", "OBJECT_STATUS_SCALING_UP", "OBJECT_STATUS_SCALING_DOWN", "OBJECT_STATUS_STOPPING", "OBJECT_STATUS_STOPPED", "OBJECT_STATUS_SUSPENDING", "OBJECT_STATUS_SUSPENDED", "OBJECT_STATUS_UPGRADING", "OBJECT_STATUS_EDITING", "OBJECT_STATUS_ADDING", "OBJECT_STATUS_DELETED"}
	dbaasStatuses    = []string{"STATUS_UNSPECIFIED", "PENDING", "DEPLOYING", "FAILED", "UPDATING", "READY", "SUSPENDED", "SUSPENDING", "ERASED", "ERASING", "ROLLING_BACK"}
	lbStatuses       = []string{"NLB_STATUS_UNSPECIFIED", "NLB_STATUS_CREATING", "NLB_STATUS_ERROR_CREATING", "NLB_STATUS_RUNNING", "NLB_STATUS_UPDATING", "NLB_STATUS_ERROR_UPDATING", "NLB_STATUS_DELETING", "NLB_STATUS_ERROR_DELETING", "NLB_STATUS_DELETED", "NLB_STATUS_SUSPENDING", "NLB_STATUS_SUSPENDED", "NLB_STATUS_ERROR_SUSPENDING", "NLB_STATUS_RESUMING", "NLB_STATUS_ERROR_RESUMING"}
	tgStatuses       = []string{"TARGET_GROUP_STATUS_UNSPECIFIED", "TARGET_GROUP_STATUS_CREATING", "TARGET_GROUP_STATUS_ERROR_CREATING", "TARGET_GROUP_STATUS_CREATED", "TARGET_GROUP_STATUS_UPDATING", "TARGET_GROUP_STATUS_ERROR_UPDATING", "TARGET_GROUP_STATUS_DELETING", "TARGET_GROUP_STATUS_ERROR_DELETING", "TARGET_GROUP_STATUS_DELETED"}
	arStatuses       = []string{"CREATING", "ACTIVE", "ERROR"}
	routeStatuses    = []string{"ROUTE_STATUS_UNSPECIFIED", "ROUTE_STATUS_NEW", "ROUTE_STATUS_CREATING", "ROUTE_STATUS_CREATED", "ROUTE_STATUS_FAILED_CREATE", "ROUTE_STATUS_DELETING", "ROUTE_STATUS_DELETED", "ROUTE_STATUS_FAILED_DELETE"}
	pgBackupStatuses = []string{"BACKUP_STATUS_UNSPECIFIED", "BACKUP_STATUS_PENDING", "BACKUP_STATUS_STARTED", "BACKUP_STATUS_COMPLETED", "BACKUP_STATUS_FAILED"}
	arTypes          = []string{"DOCKER", "DEBIAN", "RPM", "GENERIC", "PYPI", "NPM"}
)

// listSpecs are the lists and their filters, by tool. A list tool is here
// with its filters or with why it has none (TestListSpecs_*).
var listSpecs = withSets(map[string]listSpec{
	// Compute
	"vm_list": {rows: "vms", filters: []listFilter{
		zoneFilter().inCloud("/api/v1/vms", "availability_zone_ids"),
		enumFilter("state", vmStates...).inCloud("/api/v1/vms", "statuses"), nameFilter(),
		{arg: "flavor", kind: filterRef, ref: "list_flavors", en: "Only of this flavor: name.", ru: "Только с этим flavor: имя"},
		boolFilter("public_ip", "true: only with a public IP; false: only without.", "true — только с публичным IP, false — только без него"),
		// The item of GET /api/v1/vms carries no image (the published
		// specification of Compute); the card does.
		{arg: "images", kind: filterExact, many: true, fields: []string{"image"}, card: vmImageByID,
			en: "Only of these images: id or name. Reads each VM left by the other filters; narrow with them.",
			ru: "Только из этих образов: id или имя. Читает каждую ВМ, оставшуюся после остальных фильтров, — сузьте ими"},
	}},
	"disk_list": {rows: "disks", filters: []listFilter{
		zoneFilter().inCloud("/api/v1/disks", "availability_zone_id"), enumFilter("state", diskStates...), nameFilter(),
		boolFilter("attached", "true: only attached to a VM; false: only free.", "true — только подключённые к ВМ, false — только свободные", "attached_to"),
		{arg: "type", kind: filterRef, ref: "list_disk_types", fields: []string{"disk_type"}, en: "Only of this disk type: name.", ru: "Только этого типа диска: имя"},
		boolFilter("bootable", "true: only bootable; false: only the others.", "true — только загрузочные, false — только остальные"),
	}},
	"backup_list": {rows: "backups", filters: []listFilter{
		enumFilter("state", backupStates...), nameFilter(),
		enumFilter("resource_type", "virtual_machine", "disk"),
		{arg: "resource_id", kind: filterExact, fields: []string{"resource_id"}, en: "Only the backups of this VM or disk: its id.", ru: "Только копии этой ВМ или диска: её id"},
	}},
	"list_floating_ips": {rows: "floating_ips", filters: []listFilter{
		zoneFilter().inCloud("/api/v1/floating-ips", "availability_zone_id"), enumFilter("state", fipStates...), nameFilter(),
		boolFilter("free", "true: only free to attach; false: only taken.", "true — только свободные для привязки, false — только занятые", "available"),
	}},
	"list_subnets": {rows: "subnets", filters: []listFilter{
		zoneFilter("availability_zone_id", "availability_zone_name").inCloud("/api/v1/subnets", "availability_zone_ids"),
		enumFilter("state", networkStates...), nameFilter(), vpcFilter(),
	}},
	"list_security_groups": {rows: "security_groups", filters: []listFilter{
		zoneFilter("availability_zone_id", "availability_zone_name").inCloud("/api/v1/security-groups", "availability_zone_id"), nameFilter(),
	}},
	"snat_list": {rows: "nat_gateways", filters: []listFilter{
		zoneFilter("availability_zone_id", "availability_zone"), enumFilter("status", networkStates...), nameFilter(),
	}},
	"list_flavors": {rows: "flavors", own: []string{"cpu", "ram", "share", "zone", "gpu", "spot"}, filters: []listFilter{nameFilter()},
		noIDs: "a flavor is named, in the answer and in every argument that takes one"},
	"image_list": {rows: "images", own: []string{"query", "type", "zone"}},

	// VPC, DNS
	"vpc_list": {rows: "vpcs", filters: []listFilter{nameFilter()}},
	"dns_zones": {rows: "zones", filters: []listFilter{
		nameFilter(),
		textFilter("domain", "Only with this in the domain, any case.", "Только с этой частью домена, регистр не важен"),
	}},
	"dns_records": {rows: "records", own: []string{"type"}, filters: []listFilter{nameFilter()}},

	// Managed Kubernetes, the databases
	"k8s_list":      {rows: "clusters", filters: []listFilter{enumFilter("status", k8sStatuses...), nameFilter()}},
	"k8s_nodepools": {rows: "node_pools", filters: []listFilter{enumFilter("status", k8sStatuses...), nameFilter()}},
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
	"s3_buckets": {rows: "buckets", filters: []listFilter{nameFilter()}, noIDs: "a bucket has a name and no id"},

	// The catalogues the filters themselves resolve by, and the short
	// lists: no filter, with why.
	"list_zones":       {rows: "zones", none: "a catalogue of a few zones"},
	"list_disk_types":  {rows: "disk_types", none: "a catalogue of a few types"},
	"list":             {none: "the records of this directory, read from the local state: a few"},
	"list_images":      {none: "deprecated (v2.0): image_list searches the catalogue"},
	"marketplace_list": {none: "a search of the catalogue by its own query and kind, not a list of the project"},

	// The lists inside one resource and of the organization: by the name
	// and, where the specification closes the list of them, the status.
	"pg_databases":         {rows: "databases", filters: []listFilter{nameFilter()}, noIDs: "the published specification gives a database a name and no id"},
	"pg_users":             {rows: "users", filters: []listFilter{nameFilter()}, noIDs: "the published specification gives a user a name and no id"},
	"pg_backups":           {rows: "backups", filters: []listFilter{enumFilter("status", pgBackupStatuses...), nameFilter()}},
	"vpc_routes":           {rows: "routes", filters: []listFilter{enumFilter("status", routeStatuses...)}},
	"org_projects":         {rows: "projects", filters: []listFilter{nameFilter()}},
	"iam_service_accounts": {rows: "service_accounts", filters: []listFilter{nameFilter()}},

	// The catalogues with a search of their own.
	"k8s_addons":  {rows: "addons", own: []string{"query", "category", "billable"}},
	"s3_ls":       {rows: "objects", own: []string{"prefix"}},
	"pg_specs":    {rows: "specifications", own: []string{"version"}},
	"kafka_specs": {rows: "specifications", own: []string{"version_id", "zone"}},
	"redis_specs": {rows: "specifications", own: []string{"version_id", "zone"}},

	// Short lists and what is no list of resources: no filter, with why.
	"pg_versions":        {none: "a catalogue of a few versions"},
	"kafka_versions":     {none: "a catalogue of a few versions"},
	"redis_versions":     {none: "a catalogue of a few versions"},
	"k8s_addon_versions": {none: "the versions of one addon: a few"},
	"cert_versions":      {none: "the versions of one certificate: a few"},
	"org_quotas":         {none: "a table of quotas read whole: a row is a quota, not a resource"},
	"audit_events":       {none: "a log read by time and by page (since, page_token), not a list of resources"},
	"ca_logs":            {none: "the lines of a log, read by its own arguments, not a list of resources"},
	"k8s_options":        {none: "a composite catalogue (versions, flavors, zones) read whole to plan a cluster"},
	"doctor":             {none: "the checks of this machine and this key, each of which the person reads"},
	"agent_skill":        {none: "the groups of the registry: a couple of dozen, read whole"},
})

// cardReads is how many cards one call reads for a filter by a field of
// the card (listFilter.card): a request each. More items than that left by
// the other filters is a refusal that says to narrow, not a part of the
// selection and not minutes of requests.
const cardReads = 50

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

// prop is the property of f in the schema of its tool. A string filter
// takes no empty value (minLength, D36): an empty string, or one of
// spaces, is USAGE before the request on both surfaces — not «no filter»
// and the whole list (FB182-3).
func (f listFilter) prop() map[string]any {
	var p map[string]any
	if f.many {
		// The closed list of a filter by several states is not written
		// out again: the schema has it at the filter by one (the budget
		// of tools/list), the description names that argument, and evo
		// checks each value by it before the list is read (condition).
		return map[string]any{"type": "array", "description": f.en, "minItems": 1,
			"items": map[string]any{"type": "string", "minLength": 1}}
	}
	switch f.kind {
	case filterEnum:
		p = enumProp(f.en, f.values...)
	case filterBool:
		return boolProp(f.en)
	default:
		p = strProp(f.en)
	}
	p["minLength"] = 1
	return p
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
		// A filter by several values names the argument that lists
		// them (anyOf): they are written out once.
		if f.arg == arg && !f.many {
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
		case f.many:
			// An example shows the filter of one value.
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
	// cloud is the value the list of the API takes for the filter
	// (listFilter.cloudParam): the id of a reference, the value of a
	// closed list.
	cloud string
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
		// The filters that read nothing are checked first: a value they
		// refuse is USAGE before any request, the list of a reference
		// included. The conditions stay in the order of the declaration.
		byFilter := make([]*condition, len(s.filters))
		engineChecked := false
		for _, reads := range []bool{false, true} {
			for i, f := range s.filters {
				if !given(args, f.arg) || (f.kind == filterRef) != reads {
					continue
				}
				if reads && !engineChecked {
					// The refusals of the handler's engine come before
					// the request of a reference: no key, no project
					// (engineFor).
					if _, err := r.engineFor(ctx, nil); err != nil {
						return errResult(err), true
					}
					engineChecked = true
				}
				c, refusal, err := r.condition(ctx, f, args[f.arg])
				if err != nil {
					return errResult(err), true
				}
				if refusal != "" {
					return refusal, true
				}
				byFilter[i] = &c
			}
		}
		var conds []condition
		for _, c := range byFilter {
			if c != nil {
				conds = append(conds, *c)
			}
		}
		// narrowed: something besides evo selected — the handler by its
		// own filters, or the cloud by a parameter.
		narrowed, by := false, "evo"
		for _, a := range s.own {
			narrowed = narrowed || given(args, a)
		}
		for _, c := range conds {
			if c.f.cloudParam != "" && c.cloud != "" {
				ctx = provision.Narrowed(ctx, c.f.cloudPath, c.f.cloudParam, c.cloud)
				narrowed, by = true, "cloud, evo"
			}
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
		if len(conds) == 0 && narrowed {
			return text, false
		}
		m, rows, err := listRows(text, s.rows)
		if err != nil {
			// No list at all (null: the handler says why in its answer —
			// a key that cannot list) is the handler's answer, not an
			// empty selection.
			if len(conds) == 0 || noList(text, s.rows) {
				return text, false
			}
			// A part of a list is never answered as the selection.
			return errResult(err), true
		}
		if len(conds) == 0 {
			if len(rows) <= filterHintRows {
				return text, false
			}
			addHint(m, i18n.Tf("%d items; narrow the list with: %s.", "Элементов: %d; сузить список: %s.",
				len(rows), strings.Join(s.args(), ", ")))
			return toJSON(m), false
		}
		// The filters of the item first; then the filters of the card,
		// over what those left.
		var ofItem, ofCard []condition
		for _, c := range conds {
			if c.f.card != nil {
				ofCard = append(ofCard, c)
			} else {
				ofItem = append(ofItem, c)
			}
		}
		kept := selected(rows, ofItem)
		if len(ofCard) > 0 {
			if err := r.readCards(ctx, s, kept, ofCard); err != nil {
				return errResult(err), true
			}
			kept = selected(kept, ofCard)
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
		m["filtered_by"] = by
		// What the handler or the cloud narrowed is not in rows: the
		// total is known only when evo alone selected.
		if !narrowed {
			m["total"] = len(rows)
		}
		if len(kept) == 0 {
			addHint(m, emptySelectionHint(names, len(rows), narrowed))
		}
		return toJSON(m), false
	}
}

// selected are the rows that pass every condition.
func selected(rows []any, conds []condition) []any {
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
	return kept
}

// readCards puts into each of rows the fields the conditions read from
// the card of its resource (listFilter.card). More rows than cardReads is
// USAGE naming the filters of the item that narrow; a card that cannot be
// read, or names no such field, is an error — a row is never «did not
// pass» because evo could not tell.
func (r *Registry) readCards(ctx context.Context, s listSpec, rows []any, conds []condition) error {
	if len(rows) == 0 {
		return nil
	}
	var narrow []string
	for _, f := range s.filters {
		if f.card == nil {
			narrow = append(narrow, f.arg)
		}
	}
	if len(rows) > cardReads {
		return clierrors.New(clierrors.CodeUsage,
			i18n.Tf("filter %s reads the card of each item, and %d are left to read: %d at most in one call; nothing was selected",
				"фильтр %s читает карточку каждого элемента, а читать осталось %d: за вызов — не больше %d; отбор не выполнен", conds[0].f.arg, len(rows), cardReads),
			i18n.Tf("Narrow the list with: %s.", "Сузьте список: %s.", strings.Join(narrow, ", ")))
	}
	eng, err := r.authedEngine(ctx, nil)
	if err != nil {
		return err
	}
	for read, row := range rows {
		item, _ := row.(map[string]any)
		for _, c := range conds {
			field, id := c.f.itemFields()[0], stringOf(item["id"])
			v, err := c.f.card(ctx, eng, id)
			if err != nil {
				return cardError(ctx, c.f.arg, id, read, len(rows), narrow, err)
			}
			if v == nil {
				return fmt.Errorf(i18n.T("filter %s: the card of %s names no %s, so the selection cannot be answered", "фильтр %s: карточка %s не называет %s — отбор выполнить нельзя"),
					c.f.arg, id, field)
			}
			item[field] = v
		}
	}
	return nil
}

// cardError is the failure to read a card under a filter, with what the
// bare error of the read does not say («Риск», conclusion 149): which
// filter read it, of which item, how many cards were read of how many, and
// that nothing was selected. The reason stays the read's. The hint is the
// read's too, but for two failures whose cause is the selection itself: an
// item gone between the list and its card — the same call again reads the
// list anew; and a call out of its time — the same call runs out the same
// way, so the hint says to narrow, not to retry later.
func cardError(ctx context.Context, filter, id string, read, of int, narrow []string, err error) error {
	reason, msg, hint := errReason(err, nil)
	switch {
	case stderrors.Is(err, context.DeadlineExceeded) || stderrors.Is(ctx.Err(), context.DeadlineExceeded):
		msg = i18n.T("the call ran out of its time", "вызову не хватило времени")
		hint = i18n.Tf("The same call again will run out the same way: narrow the list with: %s.",
			"Тот же вызов ещё раз закончится так же: сузьте список: %s.", strings.Join(narrow, ", "))
	case notThere(err):
		msg = i18n.T("it is gone from the list since evo read it", "его уже нет в списке, который прочитал evo")
		hint = i18n.T("Send the same call again: it reads the list anew.", "Повторите тот же вызов: он прочитает список заново.")
	}
	return clierrors.Wrap(reason,
		i18n.Tf("filter %s: the card of %s was not read (%d of %d read): %s; nothing was selected",
			"фильтр %s: карточка %s не прочитана (прочитано %d из %d): %s; отбор не выполнен", filter, id, read, of, msg),
		hint, err)
}

// addHint puts hint into the answer, after the hint the list gave itself.
func addHint(m map[string]any, hint string) {
	if own := stringOf(m["hint"]); own != "" {
		hint = own + " " + hint
	}
	m["hint"] = hint
}

// emptySelectionHint says what an empty selection is and is not: nothing
// passed the filters — which is not «the project has none».
func emptySelectionHint(filters []string, total int, narrowed bool) string {
	drop := strings.Join(filters, ", ")
	if narrowed {
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

// noList reports an answer whose items are null: the list was not read.
func noList(text, key string) bool {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return false
	}
	v, has := m[key]
	return has && v == nil
}

// condition is filter f with value v, checked: an enum is in the spelling
// of the schema already (checkArgs); a reference is resolved by its list,
// and refusal is the error answer of that list when it could not be read.
func (r *Registry) condition(ctx context.Context, f listFilter, v any) (_ condition, refusal string, _ error) {
	c := condition{f: f, shown: v}
	if f.many {
		// The list is checked by the schema already (checkArgs): not
		// empty, of strings that are not empty. A closed list is checked
		// here, by the rule and in the words of an enum of the schema.
		c.want = stringList(v)
		if f.kind == filterEnum {
			closed := enumProp("", f.values...)
			for i, s := range c.want {
				canon, err := enumValue(fmt.Sprintf("%s[%d]", f.arg, i), closed, s)
				if err != nil {
					return c, "", err
				}
				c.want[i] = canon
			}
		}
		c.shown = c.want
		return c, "", nil
	}
	switch f.kind {
	case filterBool:
		c.on, _ = v.(bool)
	case filterText:
		s := strings.TrimSpace(stringOf(v))
		c.want, c.shown = []string{strings.ToLower(s)}, s
	case filterEnum:
		c.want = []string{stringOf(v)}
		c.cloud = stringOf(v)
	case filterRef:
		id, name, refusal, err := r.listed(ctx, f, strings.TrimSpace(stringOf(v)))
		if err != nil || refusal != "" {
			return c, refusal, err
		}
		c.want = nonEmpty(id, name)
		c.cloud = id
		c.shown = addrLabel(name, id)
	}
	return c, "", nil
}

// stringList are the strings of a list argument.
func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
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
