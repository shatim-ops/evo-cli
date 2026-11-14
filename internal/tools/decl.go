// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "iter"

// Decl is the declaration of a tool: what the registry, the server and the
// invariants need to know of it besides its name, description and schema.
// Every rule of a tool is read from it, not from a list keyed by the tool's
// name (modules of other domains, step 0: docs/internal/briefs/modules.md,
// §5). The tools of evo declare it in decls; New sets Tool.Decl.
type Decl struct {
	// Group is the tool's group when its name does not start with the
	// group (groupPrefixes assign the rest).
	Group string
	// Project: the tool calls a project-scoped API on every call: a list
	// or a create of the project (project_id in the query or the body, or
	// the project in the path: monitoring, audit), or resources addressed
	// by name or path inside the project (Container Apps, Secret Manager),
	// and the mk8s catalogues (projectId is required there). Without a
	// project it answers CONFIG_INVALID before any request (release audit,
	// R-06): the API said 422 "query.project_id: Field required", and that
	// was API_ERROR, exit 5. Tools addressing a resource by its id,
	// catalogues (zones, flavors, images, Marketplace, disk types), org_*,
	// billing_*, Object Storage and the local state work without one; a
	// name a tool resolves through a compute list hits the same check
	// there (provision). The check is engineFor's: the builders of the
	// handlers' engines and clients use it (requireProject).
	Project bool
	// RequiredOneOf are argument sets one argument of each the call must
	// have, which "required" cannot say: each set is the arguments one of
	// which the call must have (vm_get: vm_id or name). The handler
	// answers USAGE without one.
	//
	// The input schema does not carry this as a top-level anyOf (QA 20.10,
	// QA-4): the Anthropic API refuses a tool whose input_schema has
	// anyOf, oneOf or allOf at the top level (400 «input_schema does not
	// support oneOf, allOf, or anyOf at the top level»), and an MCP client
	// hands it the schema as it is — one such tool fails every request of
	// the session. The property descriptions say it instead; RequiredOneOf
	// carries it to docs/tools.md and the empty-arguments test.
	RequiredOneOf [][]string
	// AlwaysListed: a server lists and serves the tool whatever its
	// --toolsets, for this reason.
	AlwaysListed string

	// Sensitive: the tool reveals a secret in plaintext (a decrypted
	// secret value, a private key). It takes confirmed, so the agent asks
	// the person first, but it changes nothing, so the rule "a tool is
	// mutating when it takes confirmed" does not apply: it is not Mutating
	// (no destructiveHint, no mutating label in the CLI). It is not marked
	// readOnlyHint either: a client may auto-approve read-only tools, and
	// the plaintext would then land in the agent's context (and its logs)
	// without the person seeing the call (security audit 19.10, SEC-4).
	Sensitive bool
	// SideEffects: the tool changes nothing the call asks to change, so it
	// takes no confirmed and is not Mutating, but it is not a read either
	// (A9, REL12-4): stack_status advances the stack it shows — its next
	// steps create resources — and k8s_kubeconfig mints a token of the
	// cluster. It is not marked readOnlyHint: a client that approves reads
	// by itself would run it unseen. The value is what the effect is: the
	// group card says it (cardDoes), and 'tools list/describe' mark the
	// tool side_effects — one decision point for MCP, the card and the
	// catalog (external review 11.11).
	SideEffects string
	// Destructive: a mutating tool, besides *_delete, that may delete or
	// overwrite data: it gets destructiveHint in MCP, so a client asks the
	// person every time. kubectl, apply and helm run whatever they are
	// given (delete, uninstall); s3_put overwrites an existing key;
	// k8s_add_addon with resolve_mode OVERWRITE replaces objects already in
	// the cluster.
	Destructive bool
	// PartialConsent: the tool takes confirmed but needs it only for the
	// calls this rule of its handler's gate says: k8s_kubectl for a command
	// that is not a read (kubectlNeedsConsent), k8s_helm for install,
	// upgrade and uninstall (helmNeedsConsent). Every call of any other
	// tool that takes confirmed needs it.
	PartialConsent func(args map[string]any) bool

	// Created: the tool is a create, and this is what names what it makes
	// (created.go).
	Created created
	// CreateName: the create names what it makes by these arguments, by
	// this rule (create_names.go, D30).
	CreateName createName
	// Nameless: the create makes a resource whose name the call does not
	// give, and why.
	Nameless string
}

// createdOf is the declaration of what tool creates, and whether it is a
// create.
func createdOf(tool string) (created, bool) {
	c := decls[tool].Created
	return c, c.declared()
}

// allCreated are the creates and what names what they make.
func allCreated() iter.Seq2[string, created] {
	return func(yield func(string, created) bool) {
		for name, d := range decls {
			if d.Created.declared() && !yield(name, d.Created) {
				return
			}
		}
	}
}

// createNameOf is the rule of the name of what tool makes, and whether it
// declares one.
func createNameOf(tool string) (createName, bool) {
	c := decls[tool].CreateName
	return c, len(c.args) > 0
}

// allCreateNames are the creates and the rule of the name of what they
// make.
func allCreateNames() iter.Seq2[string, createName] {
	return func(yield func(string, createName) bool) {
		for name, d := range decls {
			if len(d.CreateName.args) > 0 && !yield(name, d.CreateName) {
				return
			}
		}
	}
}

// allNameless are the creates that make a resource whose name the call
// does not give, and why.
func allNameless() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for name, d := range decls {
			if d.Nameless != "" && !yield(name, d.Nameless) {
				return
			}
		}
	}
}

// decls are the declarations of the tools of evo, in the order of
// registerTools. A tool with nothing to declare has no entry.
var decls = map[string]Decl{
	"deploy":    {Group: "deploy", Created: created{id: "vm_id", delete: "destroy"}, Nameless: "the VM is named by the project (.evo.yaml, the directory), checked by provision.ValidName"},
	"provision": {Group: "vm", Project: true, Created: created{id: "vm_id", list: "vm_list", field: "vms", delete: "destroy"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}},
	"status":    {Group: "deploy"},
	"list":      {Group: "deploy"},
	"vm_get":    {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}},
	"vm_start":  {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}},
	// vm_stop deletes nothing on a disk, but it takes the service down and
	// may lose what the VM held in memory: a client asks the person every
	// time (D99, §4.3).
	"vm_stop": {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}, Destructive: true},
	// vm_reboot takes the service down for the time of the reboot (§5.2).
	"vm_reboot": {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}, Destructive: true},
	// vm_resize of a running VM: whether the guest OS restarts in it the
	// probe does not see (Г-10), so the client asks the person every time
	// (§6.5, В3: unknown is taken as a restart).
	"vm_resize":            {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}, Destructive: true},
	"vm_list":              {Project: true},
	"logs":                 {Group: "deploy"},
	"destroy":              {Group: "deploy", Destructive: true},
	"list_zones":           {Group: "vm"},
	"marketplace_list":     {Group: "image"},
	"list_images":          {Group: "image"},
	"image_create":         {Project: true, Created: created{id: "image.id", lost: "image_id", list: "image_list", field: "images", delete: "image_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}},
	"fip_attach":           {Group: "vpc"},
	"image_get":            {RequiredOneOf: [][]string{{"image", "image_id"}}},
	"deploy_plan":          {Group: "deploy", Project: true},
	"deploy_apply":         {Group: "deploy", Created: created{id: "vm_id", delete: "destroy"}, Nameless: "the VM is named by the saved plan, checked by deploy_plan"},
	"list_flavors":         {Group: "vm"},
	"list_disk_types":      {Group: "disk"},
	"list_subnets":         {Group: "vpc", Project: true},
	"subnet_create":        {Group: "vpc", Project: true, Created: created{id: "subnet_id", list: "list_subnets", field: "subnets", delete: "subnet_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}},
	"subnet_delete":        {Group: "vpc"},
	"list_security_groups": {Group: "vpc", Project: true},
	"list_floating_ips":    {Group: "vpc", Project: true},
	"open_ports":           {Group: "deploy"},
	"close_ports":          {Group: "deploy"},
	"sg_rule_create":       {Group: "vpc", Created: created{id: "rule_id", delete: "sg_rule_delete"}, Nameless: "a rule has no name: it is known by its fields, and the same rule there answers already_exists"},
	"sg_rule_delete":       {Group: "vpc"},
	"k8s_list":             {Project: true},
	// mk8s catalogues take projectId too: GET /v3/addons without it is a
	// live 400, the spec requires it on /v3/addons and productConfiguration
	// (Cursor review of PR #89).
	"k8s_options":    {Project: true},
	"k8s_plan":       {Project: true},
	"k8s_create":     {Project: true, Created: created{id: "cluster_id", list: "k8s_list", field: "clusters", delete: "k8s_delete"}, CreateName: createName{args: []string{"name"}, rule: "k8sNameRe: the mk8s specification"}},
	"k8s_kubeconfig": {SideEffects: "returns a kubeconfig; mints a token of the cluster"},
	// A scale down deletes nodes with the data on their disks; the hint is
	// the tool's, not the call's, so a scale up asks too (D99; conclusion
	// 112 of «Риск»).
	"k8s_nodepool_scale":   {Destructive: true},
	"k8s_add_nodepool":     {Created: created{id: "nodepool_id", list: "k8s_nodepools", field: "node_pools", listArgs: []string{"cluster_id"}, delete: "k8s_nodepool_delete"}, CreateName: createName{args: []string{"name"}, what: "node pool", whRU: "группа узлов"}},
	"k8s_addons":           {Project: true},
	"k8s_addon_versions":   {Project: true},
	"k8s_add_addon":        {Project: true, Destructive: true, Created: created{none: "an addon of a cluster, known by its name; it goes with the cluster"}, Nameless: "the addon is named by the catalogue (k8s_addons)"},
	"k8s_kubectl":          {PartialConsent: kubectlNeedsConsent, Destructive: true},
	"k8s_apply":            {Destructive: true},
	"k8s_helm":             {PartialConsent: helmNeedsConsent, Destructive: true},
	"backup_list":          {Project: true},
	"backup_create":        {Project: true, RequiredOneOf: [][]string{{"vm_id", "disk_id"}}, Created: created{id: "backup_id", list: "backup_list", field: "backups", delete: "backup_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}},
	"backup_restore":       {Created: created{id: "disk_id", list: "disk_list", field: "disks", delete: "disk_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}},
	"disk_list":            {Project: true},
	"disk_create":          {Project: true, Created: created{id: "disk.id", lost: "disk_id", list: "disk_list", field: "disks", delete: "disk_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}},
	"ca_list":              {Project: true},
	"ca_get":               {Project: true},
	"ca_deploy":            {Project: true, Created: created{arg: "name", lost: "id", list: "ca_list", field: "containers", delete: "ca_delete"}, CreateName: createName{args: []string{"name"}, rule: "caNameRe: an RFC 1123 label"}},
	"ca_delete":            {Project: true},
	"ca_logs":              {Project: true},
	"ca_start":             {Project: true},
	"ca_stop":              {Project: true},
	"iam_service_accounts": {Group: "iam", Project: true},
	"org_projects":         {Group: "org"},
	"org_quotas":           {Group: "org"},
	"agent_skill":          {Group: "auth", AlwaysListed: "the card of a group is read by the agent that works with that group, whichever groups the server serves"},
	"whoami":               {Group: "auth"},
	"doctor":               {Group: "auth"},
	"release_floating_ip":  {Group: "vpc", RequiredOneOf: [][]string{{"id", "ip_address"}}, Destructive: true},
	"snat_list":            {Project: true},
	"snat_create":          {Project: true, Created: created{id: "nat_gateway.id", lost: "nat_gateway_id", list: "snat_list", field: "nat_gateways", delete: "snat_delete"}, CreateName: createName{args: []string{"name"}, rule: "provision.ValidName: the compute name pattern"}},
	"pg_list":              {Project: true},
	"pg_get":               {RequiredOneOf: [][]string{{"id", "cluster_id"}}},
	"pg_create":            {Project: true, Created: created{id: "cluster_id", list: "pg_list", field: "clusters", delete: "pg_delete"}, CreateName: createName{args: []string{"name"}, what: "PostgreSQL cluster", whRU: "кластер PostgreSQL"}},
	"pg_delete":            {RequiredOneOf: [][]string{{"id", "cluster_id"}}},
	"kafka_list":           {Project: true},
	"kafka_create":         {Project: true, Created: created{id: "cluster_id", list: "kafka_list", field: "clusters", delete: "kafka_delete"}, CreateName: createName{args: []string{"name"}, what: "Kafka cluster", whRU: "кластер Kafka"}},
	"redis_list":           {Project: true},
	"redis_create":         {Project: true, Created: created{id: "cluster_id", list: "redis_list", field: "clusters", delete: "redis_delete"}, CreateName: createName{args: []string{"name"}, what: "Redis cluster", whRU: "кластер Redis"}},
	"pg_db_create":         {Created: created{arg: "name", list: "pg_databases", field: "databases", listArgs: []string{"cluster_id"}, delete: "pg_db_delete"}, CreateName: createName{args: []string{"name"}, what: "database", whRU: "база данных"}},
	"pg_user_create":       {Created: created{arg: "name", list: "pg_users", field: "users", listArgs: []string{"cluster_id"}, delete: "pg_user_delete"}, CreateName: createName{args: []string{"name"}, what: "database user", whRU: "пользователь базы данных"}},
	"pg_backup_create":     {Created: created{id: "backup_id", list: "pg_backups", field: "backups", listArgs: []string{"cluster_id"}}, CreateName: createName{args: []string{"name"}, what: "backup", whRU: "резервная копия"}},
	"lb_list":              {Project: true},
	"tg_list":              {Project: true},
	"tg_create":            {Project: true, RequiredOneOf: [][]string{{"vm_id", "targets"}}, Created: created{id: "tg_id", list: "tg_list", field: "target_groups", delete: "tg_delete"}, CreateName: createName{args: []string{"name"}, what: "target group", whRU: "целевая группа"}},
	"lb_create":            {Project: true, Created: created{id: "lb_id", list: "lb_list", field: "load_balancers", delete: "lb_delete"}, CreateName: createName{args: []string{"name"}, what: "load balancer", whRU: "балансировщик"}},
	"vpc_list":             {Project: true},
	"vpc_create":           {Project: true, Created: created{id: "vpc_id", list: "vpc_list", field: "vpcs", delete: "vpc_delete"}, CreateName: createName{args: []string{"name"}, what: "VPC", whRU: "VPC"}},
	"vpc_route_create":     {Project: true, RequiredOneOf: [][]string{{"next_hop_vip_id", "next_hop_vm_id", "next_hop_magic_router_id"}}, Created: created{id: "route_id", list: "vpc_routes", field: "routes", listArgs: []string{"vpc_id"}, delete: "vpc_route_delete"}, Nameless: "a route is known by its destination, named by no argument"},
	"dns_zones":            {Project: true},
	"dns_zone_create":      {Project: true, Created: created{id: "zone_id", list: "dns_zones", field: "zones", delete: "dns_zone_delete"}, CreateName: createName{args: []string{"name", "domain"}, what: "DNS zone", whRU: "DNS-зона"}},
	"dns_record_create":    {RequiredOneOf: [][]string{{"value", "values"}}, Created: created{id: "record_id", list: "dns_records", field: "records", listArgs: []string{"zone_id"}, delete: "dns_record_delete"}, CreateName: createName{args: []string{"name"}, what: "DNS record", whRU: "DNS-запись"}},
	"secret_list":          {Project: true},
	"secret_get":           {Project: true},
	"secret_value":         {Project: true, Sensitive: true},
	"secret_create":        {Project: true, Created: created{arg: "path", lost: "secret_id", list: "secret_list", field: "secrets", delete: "secret_delete"}, CreateName: createName{args: []string{"path"}, what: "secret", whRU: "секрет"}},
	"secret_add_version":   {Project: true, Nameless: "a version of a secret that is there"},
	"secret_delete":        {Project: true},
	"ar_list":              {Project: true},
	"ar_get":               {Project: true},
	"ar_create":            {Project: true, Created: created{id: "registry_id", list: "ar_list", field: "registries", delete: "ar_delete"}, CreateName: createName{args: []string{"name"}, what: "registry", whRU: "реестр"}},
	"ar_delete":            {Project: true},
	"metrics_history":      {Project: true},
	"metrics_scrape":       {Project: true},
	"audit_events":         {Project: true},
	"cert_list":            {Project: true},
	"cert_issue_le":        {Project: true, Created: created{id: "certificate_id", list: "cert_list", field: "certificates", delete: "cert_delete"}, CreateName: createName{args: []string{"name"}, what: "certificate", whRU: "сертификат"}},
	"cert_import":          {Project: true, Created: created{id: "certificate.id", lost: "certificate_id", list: "cert_list", field: "certificates", delete: "cert_delete"}, CreateName: createName{args: []string{"name"}, what: "certificate", whRU: "сертификат"}},
	"cert_private_key":     {Sensitive: true},
	"s3_put":               {Destructive: true, Created: created{arg: "key", delete: "s3_rm"}, Nameless: "an object key may hold spaces anywhere: S3 takes any UTF-8"},
	"s3_rm":                {Destructive: true},
	"s3_bucket_create":     {Created: created{arg: "bucket", delete: "s3_bucket_delete"}, CreateName: createName{args: []string{"bucket"}, rule: "bucketNameRe and the S3 client's rule"}},
	"stack_status":         {SideEffects: "advances the running stack: its next steps create the resources approved in stack_apply; on a --read-only server only shows it"},
	"stack_destroy":        {Destructive: true},
}
