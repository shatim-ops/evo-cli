// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The resources a project-bound registry (SetProjectBound: evo mcp
// --allow-project, H9) acts on by id. The factory refuses an engine for a
// project outside --allow-project, and so every call that names its project
// (project_id) or works in the server's. A resource addressed by its id is
// another matter: GET /v1/clusters/{id} carries no project, and with a key
// of the organization it reads, changes or deletes the cluster of any
// project the key sees (release audit 1.2, REL12-3). So every argument that
// names a resource by id is declared here (idArgs), and a bound registry
// checks, before the handler runs, that the resource is in an allowed
// project: its kind (ownerKind) finds the project, and the factory decides
// whether it is allowed. An argument that needs no check says why. Without
// SetProjectBound nothing here runs: no request, no answer changes.

// ownerKind is a kind of resource addressed by id, and how to tell the
// project that owns one.
type ownerKind struct {
	// what names the kind in messages ("PostgreSQL cluster").
	what string
	// byName: a value that is not a UUID is a name, which the handler
	// finds in the server's project (vm_get takes a VM's name in vm_id):
	// it addresses nothing by id, so the check passes it; a UUID must be
	// one of the project's (FB121-2).
	byName bool
	// listTool lists the resources of the project (the hint of a refusal).
	listTool string
	// check refuses id unless it names a resource of an allowed project.
	check func(ctx context.Context, c *ownerCall, k *ownerKind, id string) error
}

// idArg is an argument that names a resource by id: the kind whose owner a
// bound registry checks, or, with no kind, why it needs no check.
type idArg struct {
	name string
	kind *ownerKind
	why  string
	// ids reads the argument; nil reads it as one string.
	ids func(args map[string]any) []string
}

// Reasons of the arguments that need no check.
const (
	whyProjectArg = "the project itself: the server's engine factory admits only the projects of --allow-project (checkAllowedProject); " +
		"org_quotas and iam_service_accounts are the documented reads of other projects"
	whyCatalogue = "an entry of a catalogue shared by every project (zone, specification, version), not a project's resource"
	whyLocal     = "a local object of this host (plan, stack job), not a cloud resource; a stack job runs only in the server's project (checkJobProject)"
	whyListed    = "the handler resolves it through the project's own list (vm_list, list_floating_ips, list_security_groups, " +
		"list_subnets) and refuses one that is not there"
	whyInProject      = "the request carries the project (projectId in the query), and the API looks the resource up in that project"
	whyInParent       = "a part of the resource the call's other id argument names (checked): the API addresses it inside that resource"
	whyContainerImage = "a container image reference (registry/name:tag) the app pulls, not a Cloud.ru resource id"
	whyReference      = "a reference inside a request on a checked resource or a create in the call's project, which the service resolves within that project; " +
		"evo has no lookup of its project"
)

// idArgs are the arguments of every tool that name a resource by id
// (TestProjectBound_IDArgsDeclared keeps the list closed: a new tool with
// such an argument fails it until the argument is declared here).
var idArgs = map[string][]idArg{
	// Compute.
	"vm_get":       {{name: "vm_id", kind: kindVM}},
	"destroy":      {{name: "vm_id", why: whyListed}},
	"image_get":    {{name: "image", kind: kindImage}, {name: "image_id", kind: kindImage}},
	"image_delete": {{name: "image_id", kind: kindImage}},
	"image_create": {{name: "disk_id", kind: kindDisk}},
	"deploy":       {{name: "image", kind: kindImage}, {name: "image_id", kind: kindImage}, {name: "project_id", why: whyProjectArg}},
	"provision": {{name: "image", kind: kindImage}, {name: "image_id", kind: kindImage}, {name: "subnet_id", why: whyListed},
		{name: "project_id", why: whyProjectArg}},
	"deploy_plan":         {{name: "image", kind: kindImage}, {name: "base_plan_id", why: whyLocal}},
	"deploy_apply":        {{name: "plan_id", why: whyLocal}},
	"fip_attach":          {{name: "fip", why: whyListed}, {name: "vm", why: whyListed}},
	"release_floating_ip": {{name: "id", kind: kindFIP}},
	"open_ports":          {{name: "security_group", why: whyListed}},
	"close_ports":         {{name: "security_group", why: whyListed}},
	"disk_create":         {{name: "attach_to", why: whyListed}},
	"disk_attach":         {{name: "disk_id", kind: kindDisk}, {name: "vm", why: whyListed}},
	"disk_detach":         {{name: "disk_id", kind: kindDisk}, {name: "vm", why: whyListed}},
	"disk_resize":         {{name: "disk_id", kind: kindDisk}},
	"disk_delete":         {{name: "disk_id", kind: kindDisk}},
	"snat_create":         {{name: "availability_zone_id", why: whyCatalogue}},
	"snat_delete":         {{name: "id", kind: kindNAT}},
	"subnet_create":       {{name: "vpc_id", kind: kindVPC}, {name: "availability_zone_id", why: whyCatalogue}},
	"subnet_delete":       {{name: "subnet_id", kind: kindSubnet}},

	// Managed Kubernetes.
	"k8s_get":          {{name: "cluster_id", kind: kindK8s}},
	"k8s_kubeconfig":   {{name: "cluster_id", kind: kindK8s}},
	"k8s_delete":       {{name: "cluster_id", kind: kindK8s}},
	"k8s_nodepools":    {{name: "cluster_id", kind: kindK8s}},
	"k8s_add_nodepool": {{name: "cluster_id", kind: kindK8s}, {name: "nodes_subnet_id", why: whyListed}, {name: "security_group_id", kind: kindSG}},
	"k8s_add_addon":    {{name: "cluster_id", kind: kindK8s}},
	"k8s_kubectl":      {{name: "cluster_id", kind: kindK8s}},
	"k8s_apply":        {{name: "cluster_id", kind: kindK8s}},
	"k8s_helm":         {{name: "cluster_id", kind: kindK8s}},
	"k8s_plan":         {{name: "subnet_id", why: whyListed}, {name: "cluster_sa_id", why: whyReference}},
	"k8s_create":       {{name: "subnet_id", why: whyListed}, {name: "cluster_sa_id", why: whyReference}},

	// Managed PostgreSQL, Kafka, Redis.
	"pg_get":           {{name: "id", kind: kindPG}, {name: "cluster_id", kind: kindPG}},
	"pg_delete":        {{name: "id", kind: kindPG}, {name: "cluster_id", kind: kindPG}},
	"pg_databases":     {{name: "cluster_id", kind: kindPG}},
	"pg_db_create":     {{name: "cluster_id", kind: kindPG}},
	"pg_db_delete":     {{name: "cluster_id", kind: kindPG}},
	"pg_users":         {{name: "cluster_id", kind: kindPG}},
	"pg_user_create":   {{name: "cluster_id", kind: kindPG}},
	"pg_user_delete":   {{name: "cluster_id", kind: kindPG}},
	"pg_backups":       {{name: "cluster_id", kind: kindPG}},
	"pg_backup_create": {{name: "cluster_id", kind: kindPG}},
	"pg_create":        {{name: "specification_id", why: whyCatalogue}, {name: "subnet_id", kind: kindSubnet}},
	"pg_operation":     {{name: "operation_id", kind: kindOperation}},
	"kafka_get":        {{name: "cluster_id", kind: kindKafka}},
	"kafka_delete":     {{name: "cluster_id", kind: kindKafka}},
	"kafka_specs":      {{name: "version_id", why: whyCatalogue}},
	"kafka_create":     {{name: "specification_id", why: whyCatalogue}, {name: "version_id", why: whyCatalogue}, {name: "subnet_id", kind: kindSubnet}},
	"kafka_operation":  {{name: "operation_id", kind: kindOperation}},
	"redis_get":        {{name: "cluster_id", kind: kindRedis}},
	"redis_delete":     {{name: "cluster_id", kind: kindRedis}},
	"redis_specs":      {{name: "version_id", why: whyCatalogue}},
	"redis_create":     {{name: "specification_id", why: whyCatalogue}, {name: "version_id", why: whyCatalogue}, {name: "subnet_id", kind: kindSubnet}},
	"redis_operation":  {{name: "operation_id", kind: kindOperation}},

	// Network Load Balancer.
	"lb_get":       {{name: "id", kind: kindNLB}},
	"lb_delete":    {{name: "id", kind: kindNLB}},
	"lb_create":    {{name: "target_group_id", kind: kindTG}, {name: "vpc_id", kind: kindVPC}, {name: "subnet_id", why: whyListed}},
	"lb_operation": {{name: "operation_id", kind: kindOperation}},
	"tg_get":       {{name: "id", kind: kindTG}},
	"tg_delete":    {{name: "id", kind: kindTG}},
	"tg_create":    {{name: "vm_id", kind: kindVM}, {name: "targets", kind: kindVM, ids: targetIDs}, {name: "interface_id", why: whyInParent}},

	// VPC.
	"vpc_get":       {{name: "id", kind: kindVPC}},
	"vpc_delete":    {{name: "id", kind: kindVPC}},
	"vpc_operation": {{name: "operation_id", kind: kindOperation}},
	"vpc_routes":    {{name: "vpc_id", kind: kindVPC}},
	"vpc_route_create": {{name: "vpc_id", kind: kindVPC}, {name: "next_hop_vm_id", kind: kindVM}, {name: "next_hop_interface_id", why: whyInParent},
		{name: "next_hop_vip_id", why: whyReference}, {name: "next_hop_magic_router_id", why: whyReference}},
	"vpc_route_delete": {{name: "vpc_id", kind: kindVPC}, {name: "route_id", why: whyInParent}},

	// DNS.
	"dns_zone_create":   {{name: "vpc_id", kind: kindVPC}},
	"dns_zone_get":      {{name: "id", kind: kindDNSZone}},
	"dns_zone_delete":   {{name: "id", kind: kindDNSZone}},
	"dns_records":       {{name: "zone_id", kind: kindDNSZone}},
	"dns_record_create": {{name: "zone_id", kind: kindDNSZone}},
	"dns_record_get":    {{name: "record_id", kind: kindDNSRecord}},
	"dns_record_delete": {{name: "record_id", kind: kindDNSRecord}},
	"dns_operation":     {{name: "operation_id", kind: kindOperation}},

	// Certificate Manager.
	"cert_get":         {{name: "id", kind: kindCert}},
	"cert_delete":      {{name: "id", kind: kindCert}},
	"cert_usage":       {{name: "id", kind: kindCert}},
	"cert_renew":       {{name: "certificate_id", kind: kindCert}},
	"cert_versions":    {{name: "certificate_id", kind: kindCert}},
	"cert_private_key": {{name: "certificate_id", kind: kindCert}, {name: "version_id", why: whyInParent}},
	"cert_operation":   {{name: "operation_id", kind: kindOperation}},

	// Artifact Registry: GET and DELETE /v1/registries/{id} take projectId
	// (required by ar/openapi.yaml); the operations do not.
	"ar_get":       {{name: "id", why: whyInProject}},
	"ar_delete":    {{name: "id", why: whyInProject}},
	"ar_operation": {{name: "operation_id", kind: kindOperation}},

	// Secret Manager: the create carries projectId.
	"secret_create": {{name: "kms_key_id", why: whyReference}},

	// Container Apps: an app is addressed by name with projectId in the
	// query (containers/openapi.yaml); its image is a container image.
	"ca_deploy": {{name: "image", why: whyContainerImage}},

	// The project itself.
	"billing_consumption":  {{name: "project_id", why: whyProjectArg}},
	"billing_finops":       {{name: "project_id", why: whyProjectArg}},
	"org_quotas":           {{name: "project_id", why: whyProjectArg}},
	"iam_service_accounts": {{name: "project_id", why: whyProjectArg}},

	// Stack jobs.
	"stack_apply":   {{name: "job_id", why: whyLocal}},
	"stack_status":  {{name: "job_id", why: whyLocal}},
	"stack_destroy": {{name: "job_id", why: whyLocal}},
}

// targetIDs are the VM ids of tg_create's targets ([{id, interface_id}]).
func targetIDs(args map[string]any) []string {
	raw, _ := args["targets"].([]any)
	var out []string
	for _, t := range raw {
		if m, ok := t.(map[string]any); ok {
			if id, _ := m["id"].(string); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

// values reads the ids of the argument in args.
func (a idArg) values(args map[string]any) []string {
	if a.ids != nil {
		return a.ids(args)
	}
	if v := stringArg(args, a.name); strings.TrimSpace(v) != "" {
		return []string{v}
	}
	return nil
}

// checkedIDArgs are the arguments of the tool a bound registry checks.
func checkedIDArgs(tool string) []idArg {
	var out []idArg
	for _, a := range idArgs[tool] {
		if a.kind != nil {
			out = append(out, a)
		}
	}
	return out
}

// boundToProjects wraps the handler of t for a registry bound to its projects
// (SetProjectBound): before the handler, every id argument of the call
// must name a resource of an allowed project; after a mutating call, the
// operation it started is remembered for the group's *_operation tool. Not
// bound, the handler runs as it is. A tool with nothing to check or
// remember keeps its handler.
func (r *Registry) boundToProjects(t Tool, next Handler) Handler {
	checks := checkedIDArgs(t.Name)
	if len(checks) == 0 && !t.Mutating {
		return next
	}
	return func(ctx context.Context, args map[string]any) (string, bool) {
		if !r.projectBound {
			return next(ctx, args)
		}
		c := &ownerCall{r: r, args: args, group: t.Group}
		for _, a := range checks {
			for _, id := range a.values(args) {
				if err := a.kind.check(ctx, c, a.kind, id); err != nil {
					return errResult(err), true
				}
			}
		}
		text, isErr := next(ctx, args)
		if t.Mutating && !isErr {
			r.ops.remember(t.Group, answerOperationID(text))
		}
		return text, isErr
	}
}

// ownerCall is one call being checked: the registry, the arguments and the
// engine of the call's project, built once.
type ownerCall struct {
	r     *Registry
	args  map[string]any
	group string
	eng   *engine.Engine
}

// engine is the authenticated engine of the call's project: project_id of
// the call when it has one (deploy, provision), else the server's.
func (c *ownerCall) engine(ctx context.Context) (*engine.Engine, error) {
	if c.eng != nil {
		return c.eng, nil
	}
	var overrides map[string]any
	if p := stringArg(c.args, "project_id"); p != "" {
		overrides = map[string]any{"ProjectID": p}
	}
	eng, err := c.r.authedEngine(ctx, overrides)
	if err != nil {
		return nil, err
	}
	c.eng = eng
	return eng, nil
}

// ErrProjectNotAllowed marks the factory's refusal of a project outside
// the server's (evo mcp --allow-project, checkAllowedProject): admit
// answers it as notInProject, and any other error of the factory as it is.
var ErrProjectNotAllowed = stderrors.New("project not allowed on this server")

// admit refuses a resource whose project is not allowed: the factory
// decides, as for project_id (checkAllowedProject). Its refusal is answered
// as notInProject — NOT_FOUND, naming no project — so that a resource of
// another project reads as one that is gone: a client probing ids learns
// neither that it exists nor whose it is, and an agent that takes
// NOT_FOUND for "already deleted" stays right. A resource whose answer
// names no project is refused too, as CONFIG_INVALID: unknown is not
// allowed, and not absent either.
func (c *ownerCall) admit(k *ownerKind, id, project string) error {
	if project == "" {
		return clierrors.New(clierrors.CodeConfigInvalid,
			i18n.Tf("the answer for %s %s names no project, so this --allow-project server cannot tell whose it is; nothing was done",
				"в ответе о ресурсе «%s» %s нет проекта: сервер с --allow-project не может определить, чей он; ничего не сделано", k.what, id),
			i18n.T("Ask the server's operator to check the resource in the Cloud.ru console.",
				"Попросите оператора сервера проверить ресурс в консоли Cloud.ru."))
	}
	if _, err := c.r.newEngine(map[string]any{"ProjectID": project}); err != nil {
		if stderrors.Is(err, ErrProjectNotAllowed) {
			return notInProject(k, id)
		}
		// A broken setup (a profile, no key) is that, not "no such
		// resource" (audit Fable 1.2.1, FB121-5).
		return err
	}
	return nil
}

// notInProject is the refusal of a resource outside the server's projects:
// of another project, or gone — one answer for both, NOT_FOUND as the API
// gives for one that is gone, naming no project.
func notInProject(k *ownerKind, id string) error {
	// One answer for both a resource of another project and one that is
	// gone (D26, H9 б): naming both reasons keeps an agent from hunting
	// for another server when the resource was deleted (G130-16).
	return clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("%s %s is deleted or not in the projects of this server (--allow-project); nothing was done",
			"%s %s удалён или не в проектах этого сервера (--allow-project); ничего не сделано", k.what, id),
		i18n.Tf("Take the id from %s.", "id — в выводе %s.", k.listTool))
}

// gone is the error of a read by id in a check: a resource the API does
// not have is notInProject, the answer of one of another project, so the
// two read alike; any other error is as it is.
func gone(k *ownerKind, id string, err error) error {
	var ce *clierrors.CLIError
	if stderrors.As(err, &ce) && (ce.Code == clierrors.CodeNotFound || ce.Code == clierrors.CodeVMNotFound) ||
		apiStatus(err) == http.StatusNotFound {
		return notInProject(k, id)
	}
	return err
}

// projectField is the project of a resource as the API gives it:
// project_id (the snake_case services) or projectId (grpc-gateway JSON).
func projectField(m map[string]any) string {
	for _, k := range []string{"project_id", "projectId"} {
		if v, _ := m[k].(string); v != "" {
			return v
		}
	}
	return ""
}

// byProjectField is a kind whose answer by id names its project.
func byProjectField(what, listTool string, get func(ctx context.Context, c *ownerCall, id string) (map[string]any, error)) *ownerKind {
	return &ownerKind{what: what, listTool: listTool, check: func(ctx context.Context, c *ownerCall, k *ownerKind, id string) error {
		m, err := get(ctx, c, id)
		if err != nil {
			return gone(k, id, err)
		}
		return c.admit(k, id, projectField(m))
	}}
}

// inProjectList is a kind whose answer by id names no project: the list of
// the call's project (admitted by the factory when its engine was built)
// must hold the id.
func inProjectList(what, listTool string, list func(ctx context.Context, c *ownerCall) ([]string, error)) *ownerKind {
	return &ownerKind{what: what, listTool: listTool, check: func(ctx context.Context, c *ownerCall, k *ownerKind, id string) error {
		if k.byName && !looksUUID(id) {
			return nil
		}
		ids, err := list(ctx, c)
		if err != nil {
			return err
		}
		for _, x := range ids {
			if x == id {
				return nil
			}
		}
		return notInProject(k, id)
	}}
}

// byName marks k as taking a name where a UUID is not given (byName).
func byName(k *ownerKind) *ownerKind {
	k.byName = true
	return k
}

// computeList is the list of a Compute resource in the call's project:
// the Compute API's answer by id names no project the code reads
// (GET /vms/{id}, /floating-ips/{id}), and DELETE, PUT and POST by id take
// no project_id (the one evo sends is not in compute/openapi-v3.yaml).
func computeList(list func(ctx context.Context, eng *engine.Engine) ([]string, error)) func(context.Context, *ownerCall) ([]string, error) {
	return func(ctx context.Context, c *ownerCall) ([]string, error) {
		eng, err := c.engine(ctx)
		if err != nil {
			return nil, err
		}
		return list(ctx, eng)
	}
}

// mapIDs are the "id" fields of a raw list.
func mapIDs(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, m := range items {
		if id, _ := m["id"].(string); id != "" {
			out = append(out, id)
		}
	}
	return out
}

var (
	kindVM = byName(inProjectList("VM", "vm_list", computeList(func(ctx context.Context, eng *engine.Engine) ([]string, error) {
		vms, err := eng.Cloud.ListVMsInProject(ctx, eng.Config.ProjectID)
		ids := make([]string, 0, len(vms))
		for _, v := range vms {
			ids = append(ids, v.ID)
		}
		return ids, err
	})))
	kindDisk = inProjectList("disk", "disk_list", computeList(func(ctx context.Context, eng *engine.Engine) ([]string, error) {
		disks, err := eng.Cloud.ListDisks(ctx, eng.Config.ProjectID)
		return mapIDs(disks), err
	}))
	kindFIP = inProjectList("floating IP", "list_floating_ips", computeList(func(ctx context.Context, eng *engine.Engine) ([]string, error) {
		fips, err := eng.Cloud.ListFloatingIPs(ctx, eng.Config.ProjectID)
		ids := make([]string, 0, len(fips))
		for _, f := range fips {
			ids = append(ids, f.ID)
		}
		return ids, err
	}))
	kindNAT = inProjectList("SNAT gateway", "snat_list", computeList(func(ctx context.Context, eng *engine.Engine) ([]string, error) {
		gws, err := eng.Cloud.ListNATGateways(ctx, eng.Config.ProjectID)
		return mapIDs(gws), err
	}))
	kindSG = inProjectList("security group", "list_security_groups", computeList(func(ctx context.Context, eng *engine.Engine) ([]string, error) {
		sgs, err := eng.Cloud.ListSecurityGroups(ctx, eng.Config.ProjectID)
		ids := make([]string, 0, len(sgs))
		for _, sg := range sgs {
			ids = append(ids, sg.ID)
		}
		return ids, err
	}))
	kindSubnet = inProjectList("subnet", "list_subnets", computeList(func(ctx context.Context, eng *engine.Engine) ([]string, error) {
		subs, err := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
		ids := make([]string, 0, len(subs))
		for _, sn := range subs {
			ids = append(ids, sn.ID)
		}
		return ids, err
	}))

	// Kafka and Redis answer a cluster without its project
	// (openapi-kafka.yaml, openapi-redis.yaml): the list of the project.
	kindKafka = inProjectList("Kafka cluster", "kafka_list", func(ctx context.Context, c *ownerCall) ([]string, error) {
		cli, _, err := c.r.kafkaClientEng(ctx)
		if err != nil {
			return nil, err
		}
		items, err := cli.ListClusters(ctx, kafkaQuery())
		return mapIDs(items), err
	})
	kindRedis = inProjectList("Redis cluster", "redis_list", func(ctx context.Context, c *ownerCall) ([]string, error) {
		cli, _, err := c.r.redisClientEng(ctx)
		if err != nil {
			return nil, err
		}
		items, err := cli.ListClusters(ctx, url.Values{})
		return mapIDs(items), err
	})

	// A DNS zone: the project's public and private zones, as
	// dnsZoneVisibility looks a zone up.
	kindDNSZone = inProjectList("DNS zone", "dns_zones", dnsZoneIDs)
	// A DNS record names its zone (publicZoneId, privateZoneId in
	// dns/openapi.yaml), not its project: the zone must be the project's.
	// The refusal names the record, not the zone of another project.
	kindDNSRecord = &ownerKind{what: "DNS record", listTool: "dns_records", check: func(ctx context.Context, c *ownerCall, k *ownerKind, id string) error {
		cli, err := c.r.dnsClient(ctx)
		if err != nil {
			return err
		}
		vis, err := dnsRecordVisibility(ctx, cli, c.args, id)
		if err != nil {
			return gone(k, id, err)
		}
		m, err := dnsGetRecord(ctx, cli, vis, id)
		if err != nil {
			return gone(k, id, err)
		}
		zone := ""
		for _, f := range []string{"publicZoneId", "public_zone_id", "privateZoneId", "private_zone_id"} {
			if v, _ := m[f].(string); v != "" {
				zone = v
				break
			}
		}
		if zone == "" {
			return c.admit(k, id, "")
		}
		zones, err := dnsZoneIDs(ctx, c)
		if err != nil {
			return err
		}
		if !slices.Contains(zones, zone) {
			return notInProject(k, id)
		}
		return nil
	}}

	kindPG = byProjectField("PostgreSQL cluster", "pg_list", func(ctx context.Context, c *ownerCall, id string) (map[string]any, error) {
		cli, err := c.r.pgClient(ctx)
		if err != nil {
			return nil, err
		}
		return cli.GetCluster(ctx, id)
	})
	kindNLB = byProjectField("load balancer", "lb_list", func(ctx context.Context, c *ownerCall, id string) (map[string]any, error) {
		cli, err := c.r.nlbClient(ctx)
		if err != nil {
			return nil, err
		}
		return cli.GetNLB(ctx, id)
	})
	kindTG = byProjectField("target group", "tg_list", func(ctx context.Context, c *ownerCall, id string) (map[string]any, error) {
		cli, err := c.r.nlbClient(ctx)
		if err != nil {
			return nil, err
		}
		return cli.GetTargetGroup(ctx, id)
	})
	kindVPC = byProjectField("VPC", "vpc_list", func(ctx context.Context, c *ownerCall, id string) (map[string]any, error) {
		cli, err := c.r.vpcClient(ctx)
		if err != nil {
			return nil, err
		}
		return cli.GetVPC(ctx, id)
	})
	kindK8s = byProjectField("Kubernetes cluster", "k8s_list", func(ctx context.Context, c *ownerCall, id string) (map[string]any, error) {
		cli, _, _, err := c.r.k8sClient(ctx)
		if err != nil {
			return nil, err
		}
		return cli.GetCluster(ctx, id)
	})
	kindCert = byProjectField("certificate", "cert_list", func(ctx context.Context, c *ownerCall, id string) (map[string]any, error) {
		cli, err := c.r.certClient(ctx)
		if err != nil {
			return nil, err
		}
		return cli.Get(ctx, id)
	})

	// An image by UUID: a public or Marketplace image is everyone's; another
	// names its project (project_id); one that says neither must be in the
	// catalogue as the call's project sees it. A name is resolved within
	// that catalogue and the project's own images, where another project's
	// image is not.
	kindImage = &ownerKind{what: "image", listTool: "image_list", check: func(ctx context.Context, c *ownerCall, k *ownerKind, ref string) error {
		if !images.IsUUID(strings.TrimSpace(ref)) {
			return nil
		}
		eng, err := c.engine(ctx)
		if err != nil {
			return err
		}
		id := strings.TrimSpace(ref)
		d, err := eng.Cloud.GetImage(ctx, id)
		if err != nil {
			return gone(k, id, err)
		}
		switch {
		case d.Public || d.Type == provision.ImageTypePublic || d.Type == provision.ImageTypeMarketplace:
			return nil
		case d.ProjectID != "":
			return c.admit(k, id, d.ProjectID)
		}
		list, err := eng.Cloud.ListImagesInProject(ctx, eng.Config.ProjectID)
		if err != nil {
			return err
		}
		for _, img := range list {
			if img.ID == id {
				return nil
			}
		}
		return notInProject(k, id)
	}}

	// An operation names no project, and once its resource is deleted
	// nothing does: a bound registry polls only the operations it started
	// (a mutating call in an allowed project), or a stack job of the
	// server's project did (crankJob).
	kindOperation = &ownerKind{what: "operation", check: func(_ context.Context, c *ownerCall, _ *ownerKind, id string) error {
		if c.r.ops.known(c.group, id) {
			return nil
		}
		return clierrors.New(clierrors.CodeConfigInvalid,
			i18n.Tf("operation %s was not started by this server: with --allow-project an operation is read only when this server started it",
				"операцию %s запустил не этот сервер: с --allow-project операция читается, только если её запустил он", id),
			i18n.T("Read the resource itself (its *_get or *_list tool), or poll the operation where it was started.",
				"Прочитайте сам ресурс (инструмент *_get или *_list) или опрашивайте операцию там, где её запустили."))
	}}
)

// dnsZoneIDs are the project's public and private DNS zones, as
// dnsZoneVisibility looks a zone up.
func dnsZoneIDs(ctx context.Context, c *ownerCall) ([]string, error) {
	cli, err := c.r.dnsClient(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, vis := range []string{"public", "private"} {
		zones, err := cli.ListZones(ctx, vis)
		if err != nil {
			return nil, err
		}
		for _, z := range zones {
			ids = append(ids, fmt.Sprint(z["id"]))
		}
	}
	return ids, nil
}

// operationSet is the operations a bound registry started, by group.
type operationSet struct {
	mu  sync.Mutex
	ids map[string]bool
}

func (o *operationSet) remember(group, id string) {
	if id == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ids == nil {
		o.ids = map[string]bool{}
	}
	o.ids[group+"/"+id] = true
}

func (o *operationSet) known(group, id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ids[group+"/"+id]
}

// answerOperationID is the operation a tool's answer names: operation_id,
// or the id of operation (cert_issue_le, cert_renew).
func answerOperationID(text string) string {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return ""
	}
	if id, _ := m["operation_id"].(string); id != "" {
		return id
	}
	op, _ := m["operation"].(map[string]any)
	id, _ := op["id"].(string)
	return id
}
