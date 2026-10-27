// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"errors"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// schemaRule is a static rule of an argument the schema declares (D36):
// a bound of a number or the format of a string.
type schemaRule struct {
	min, max any // nil for none
	format   string
}

// schemaRules are the rules that handlers checked themselves before D36
// (minIntArg, portArg, uuidArg, checkedDelete, ranges inline): each is
// in the schema now, and CheckArgs checks it before the handler for MCP
// and the CLI alike.
var schemaRules = map[string]schemaRule{
	"deploy.disk_size":                {min: 0},
	"provision.disk_size":             {min: 0},
	"billing_finops.days":             {min: 1, max: maxFinopsDays},
	"image_create.min_cpu":            {min: 0},
	"image_create.min_ram":            {min: 0},
	"image_create.min_disk":           {min: 0},
	"k8s_add_nodepool.count":          {min: 1},
	"k8s_add_nodepool.cpu":            {min: 1},
	"k8s_add_nodepool.ram_gb":         {min: 1},
	"k8s_add_nodepool.disk_gb":        {min: 10},
	"pg_create.storage_gb":            {min: 1},
	"pg_create.instances":             {min: 1},
	"pg_create.backup_retention_days": {min: 1, max: 30},
	"lb_create.port":                  {min: 1, max: 65535},
	"lb_create.target_port":           {min: 1, max: 65535},
	"lb_create.hc_port":               {min: 1, max: 65535},
	"kafka_get.cluster_id":            {format: "uuid"},
	"pg_get.id":                       {format: "uuid"},
	"pg_get.cluster_id":               {format: "uuid"},
	"redis_get.cluster_id":            {format: "uuid"},
	"vpc_get.id":                      {format: "uuid"},
	"vpc_routes.vpc_id":               {format: "uuid"},
	"destroy.vm_id":                   {format: "uuid"},
	"disk_delete.disk_id":             {format: "uuid"},
	"image_delete.image_id":           {format: "uuid"},
	"release_floating_ip.id":          {format: "uuid"},
	"snat_delete.id":                  {format: "uuid"},
	// G13-21, G13-22 (Grok, wave 2): what the specification of the
	// service states (the registry's id is a UUID by its description).
	"ar_delete.id": {format: "uuid"},
	// The other deletes of G13-21 (owner's decision 24.10, D36: the live
	// data of L-15 and of G13-21 — their get refuses another form with a
	// 400, their lists give UUIDs).
	"cert_delete.id":              {format: "uuid"},
	"dns_record_delete.record_id": {format: "uuid"},
	"dns_zone_delete.id":          {format: "uuid"},
	"k8s_delete.cluster_id":       {format: "uuid"},
	"kafka_delete.cluster_id":     {format: "uuid"},
	"lb_delete.id":                {format: "uuid"},
	"pg_delete.id":                {format: "uuid"},
	"pg_delete.cluster_id":        {format: "uuid"},
	"pg_db_delete.cluster_id":     {format: "uuid"},
	"pg_user_delete.cluster_id":   {format: "uuid"},
	"redis_delete.cluster_id":     {format: "uuid"},
	"tg_delete.id":                {format: "uuid"},
	"vpc_delete.id":               {format: "uuid"},
	"vpc_route_delete.route_id":   {format: "uuid"},
	"vpc_route_delete.vpc_id":     {format: "uuid"},
	// G13-3: a value no API takes made a preview, then failed after consent.
	"kafka_create.storage_gib": {min: 1},
	"redis_create.storage_gb":  {min: 1},
	"dns_record_create.ttl":    {min: 0},
	"open_ports.cidr":          {format: "cidr"},
	"vpc_route_create.subnet":  {format: "cidr"},
}

// The rules a handler checked itself are in the schema (D36).
func TestArgs_SchemaRulesDeclared(t *testing.T) {
	r := New(nil)
	for key, want := range schemaRules {
		tool, arg, _ := strings.Cut(key, ".")
		tl, ok := r.Lookup(tool)
		if !ok {
			t.Errorf("%s: no tool %s", key, tool)
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		p, _ := props[arg].(map[string]any)
		if p == nil {
			t.Errorf("%s: %s takes no %s", key, tool, arg)
			continue
		}
		if want.min != nil && p["minimum"] != want.min || want.max != nil && p["maximum"] != want.max {
			t.Errorf("%s: minimum %v maximum %v, want %v %v", key, p["minimum"], p["maximum"], want.min, want.max)
		}
		if p["format"] != nil || want.format != "" {
			if f, _ := p["format"].(string); f != want.format {
				t.Errorf("%s: format %q, want %q", key, f, want.format)
			}
		}
	}
}

// CheckArgs refuses a value out of the schema's bounds or of another
// format than the schema's, as USAGE naming the argument, before any
// handler; a value at the bound passes (D36). Every rule of every tool,
// not only those above.
func TestArgs_SchemaConstraintsEnforced(t *testing.T) {
	good := map[string]any{"uuid": "00000000-0000-4000-8000-000000000001", "cidr": "10.0.0.0/16"}
	bad := map[string][]any{"uuid": {"bad", "vm-1", "00000000-0000-4000-8000-00000000000g"}, "cidr": {"bad", "10.0.0.0", "10.0.0.0/33"}}
	refused := func(t *testing.T, tl Tool, arg string, v any) {
		t.Helper()
		err := CheckArgs(tl, map[string]any{arg: v})
		var ce *clierrors.CLIError
		if !errors.As(err, &ce) || ce.Code != clierrors.CodeUsage || !strings.Contains(ce.Message, arg) {
			t.Errorf("%s %s=%v: %v, want USAGE naming %s", tl.Name, arg, v, err, arg)
		}
	}
	passed := func(t *testing.T, tl Tool, arg string, v any) {
		t.Helper()
		if err := CheckArgs(tl, map[string]any{arg: v}); err != nil {
			t.Errorf("%s %s=%v at the bound: %v", tl.Name, arg, v, err)
		}
	}
	rules := 0
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for arg, pv := range props {
			p, _ := pv.(map[string]any)
			if n, ok := p["minimum"].(int); ok {
				rules++
				refused(t, tl, arg, float64(n-1))
				passed(t, tl, arg, float64(n))
			}
			if n, ok := p["maximum"].(int); ok {
				rules++
				refused(t, tl, arg, float64(n+1))
				passed(t, tl, arg, float64(n))
			}
			if f, ok := p["format"].(string); ok && good[f] != nil {
				rules++
				for _, v := range bad[f] {
					refused(t, tl, arg, v)
				}
				passed(t, tl, arg, good[f])
			}
		}
	}
	if rules < len(schemaRules) {
		t.Errorf("%d rules in the schemas, fewer than the %d declared", rules, len(schemaRules))
	}
}

// The reasons an id argument of idWithoutFormat gives for having no
// format of the schema.
const (
	// The description says UUID, the schema does not: CheckArgs lets
	// "vm-1" or a typo through, and the cloud answers it after the call
	// with a raw 400 or 422 — for a mutating tool, after the question of
	// consent. The fix is uuidProp, by «разработка» (D36). It checks the
	// form of a UUID, not its version: disk_delete declares the format and
	// still got a raw 422 on a UUID that is not version 4 (live run 27.10);
	// which version a service takes is for the api-probe of 1.4.
	idUndeclared = "description says UUID, schema does not"
	// evo's own id, not the cloud's: plan-<hex> (deployplan), stk-<hex>
	// (stack, checked by its store).
	idOfEvo = "evo's own id, not a UUID"
	// The description does not say what the id is, though cert_delete.id
	// is a UUID (G13-21): which form the service takes is not checked.
	idUnknown = "form of the id not checked"
)

// idWithoutFormat are the string arguments named id or *_id that declare
// no format in the schema, each with its reason. The list only shrinks: a
// new id argument with no format fails the test, and so does a line whose
// argument declares one now or is gone (CLAUDE.md §6, axis 1: what can be
// known without a write is checked before the question).
var idWithoutFormat = map[string]string{
	"cert_get.id":                               idUnknown,
	"cert_operation.operation_id":               idUnknown,
	"cert_private_key.certificate_id":           idUnknown,
	"cert_renew.certificate_id":                 idUnknown,
	"cert_usage.id":                             idUnknown,
	"cert_versions.certificate_id":              idUnknown,
	"secret_create.kms_key_id":                  idUnknown,
	"deploy_apply.plan_id":                      idOfEvo,
	"deploy_plan.base_plan_id":                  idOfEvo,
	"stack_apply.job_id":                        idOfEvo,
	"stack_destroy.job_id":                      idOfEvo,
	"stack_status.job_id":                       idOfEvo,
	"ar_get.id":                                 idUndeclared,
	"ar_operation.operation_id":                 idUndeclared,
	"billing_consumption.project_id":            idUndeclared,
	"billing_finops.project_id":                 idUndeclared,
	"deploy.image_id":                           idUndeclared,
	"deploy.project_id":                         idUndeclared,
	"disk_attach.disk_id":                       idUndeclared,
	"disk_detach.disk_id":                       idUndeclared,
	"disk_resize.disk_id":                       idUndeclared,
	"dns_operation.operation_id":                idUndeclared,
	"dns_record_create.zone_id":                 idUndeclared,
	"dns_record_get.record_id":                  idUndeclared,
	"dns_records.zone_id":                       idUndeclared,
	"dns_zone_get.id":                           idUndeclared,
	"iam_service_accounts.project_id":           idUndeclared,
	"image_create.disk_id":                      idUndeclared,
	"image_get.image_id":                        idUndeclared,
	"k8s_add_addon.cluster_id":                  idUndeclared,
	"k8s_add_nodepool.cluster_id":               idUndeclared,
	"k8s_add_nodepool.nodes_subnet_id":          idUndeclared,
	"k8s_add_nodepool.security_group_id":        idUndeclared,
	"k8s_apply.cluster_id":                      idUndeclared,
	"k8s_create.cluster_sa_id":                  idUndeclared,
	"k8s_create.subnet_id":                      idUndeclared,
	"k8s_get.cluster_id":                        idUndeclared,
	"k8s_helm.cluster_id":                       idUndeclared,
	"k8s_kubeconfig.cluster_id":                 idUndeclared,
	"k8s_kubectl.cluster_id":                    idUndeclared,
	"k8s_nodepools.cluster_id":                  idUndeclared,
	"k8s_plan.cluster_sa_id":                    idUndeclared,
	"k8s_plan.subnet_id":                        idUndeclared,
	"kafka_create.specification_id":             idUndeclared,
	"kafka_create.subnet_id":                    idUndeclared,
	"kafka_create.version_id":                   idUndeclared,
	"kafka_operation.operation_id":              idUndeclared,
	"kafka_specs.version_id":                    idUndeclared,
	"lb_create.subnet_id":                       idUndeclared,
	"lb_create.target_group_id":                 idUndeclared,
	"lb_create.vpc_id":                          idUndeclared,
	"lb_get.id":                                 idUndeclared,
	"lb_operation.operation_id":                 idUndeclared,
	"org_quotas.project_id":                     idUndeclared,
	"pg_backup_create.cluster_id":               idUndeclared,
	"pg_backups.cluster_id":                     idUndeclared,
	"pg_create.specification_id":                idUndeclared,
	"pg_create.subnet_id":                       idUndeclared,
	"pg_databases.cluster_id":                   idUndeclared,
	"pg_db_create.cluster_id":                   idUndeclared,
	"pg_operation.operation_id":                 idUndeclared,
	"pg_user_create.cluster_id":                 idUndeclared,
	"pg_users.cluster_id":                       idUndeclared,
	"provision.image_id":                        idUndeclared,
	"provision.project_id":                      idUndeclared,
	"provision.subnet_id":                       idUndeclared,
	"redis_create.specification_id":             idUndeclared,
	"redis_create.subnet_id":                    idUndeclared,
	"redis_create.version_id":                   idUndeclared,
	"redis_operation.operation_id":              idUndeclared,
	"redis_specs.version_id":                    idUndeclared,
	"snat_create.availability_zone_id":          idUndeclared,
	"tg_create.interface_id":                    idUndeclared,
	"tg_create.vm_id":                           idUndeclared,
	"tg_get.id":                                 idUndeclared,
	"vm_get.vm_id":                              idUndeclared,
	"vpc_operation.operation_id":                idUndeclared,
	"vpc_route_create.next_hop_interface_id":    idUndeclared,
	"vpc_route_create.next_hop_magic_router_id": idUndeclared,
	"vpc_route_create.next_hop_vip_id":          idUndeclared,
	"vpc_route_create.next_hop_vm_id":           idUndeclared,
	"vpc_route_create.vpc_id":                   idUndeclared,
}

// An id the description calls a UUID is a UUID in the schema too, so
// CheckArgs refuses another form before the call (D36): every string
// argument named id or *_id declares a format, outside idWithoutFormat.
func TestArgs_IDsDeclareTheirFormat(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for arg, pv := range props {
			p, _ := pv.(map[string]any)
			if typ, _ := p["type"].(string); typ != "string" || arg != "id" && !strings.HasSuffix(arg, "_id") {
				continue
			}
			key := tl.Name + "." + arg
			if p["format"] != nil {
				continue
			}
			seen[key] = true
			if idWithoutFormat[key] == "" {
				t.Errorf("%s: an id with no format in the schema — uuidProp, so CheckArgs refuses another form before the call", key)
			}
		}
	}
	for key := range idWithoutFormat {
		if !seen[key] {
			t.Errorf("idWithoutFormat names %s, which declares a format now or is gone: remove the line", key)
		}
	}
}
