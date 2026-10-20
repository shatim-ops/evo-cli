// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

// CLISpec is how a tool is presented in the CLI: its command, which of its
// arguments are positional, how its flags are named, and how its answer is
// shown. It is part of the tool's description, like its schema: the command
// trees of internal/cli are built from it (docs/internal/cli-mcp-parity,
// REQ-1). Every other argument of the schema becomes a flag with the type
// from the schema.
//
// A tool has a Verb (its command), or None (no command, with the reason),
// or is in CLIPending (no command yet); TestRegistry_EveryToolHasCLISpec
// keeps it so.
type CLISpec struct {
	// Group is the top-level command ("vm"); "" puts the command at the
	// root (status, deploy).
	Group string
	// Verb is the command inside the group ("create", "list").
	Verb string
	// Args lists the positional arguments as tool argument names:
	//
	//	"disk_id"     required
	//	"id?"         optional
	//	"args..."     the rest of the command line, joined with spaces
	//	"vm_id|name"  required; goes to vm_id when it is a UUID, to name otherwise
	Args []string
	// Rename maps a tool argument to its flag name, when the default
	// (snake_case → kebab-case) clashes or reads badly.
	Rename map[string]string
	// Omit are tool arguments that get no flag: an alias of a positional
	// argument (pg get <cluster-id> beside the tool's id). With both, the
	// flag won and the positional was lost: `pg delete A --id B` deleted B
	// (QA 20.10, QA-1).
	Omit map[string]bool
	// Pairs are object arguments given as a repeatable key=value flag
	// (--meta admin_password=…) instead of one JSON object.
	Pairs map[string]bool
	// Files are tool arguments whose flag names a file holding the value
	// ("-": stdin), read and checked against the argument's type:
	// k8s add-addon --config settings.json.
	Files map[string]bool
	// Columns are the table columns of a list answer (default: picked from
	// the rows).
	Columns []string
	// Render names the view that shows the answer instead of the generic
	// table ("image-list"); the CLI holds the closed set of views. --json
	// still prints the tool's answer as is.
	Render string
	// Raw prints the answer's "output" (what kubectl or helm printed) as
	// is, not as a FIELD/VALUE table; on a tool error it goes to stderr
	// before the error.
	Raw bool
	// None: the tool has no command, for this reason. No tool has one now,
	// and none should.
	None string
}

// cliSpecs are the commands of the tools, by tool name, grouped as the
// command trees are. New sets Tool.CLI from it.
var cliSpecs = map[string]CLISpec{
	// vm
	"vm_list":      {Group: "vm", Verb: "list", Columns: []string{"id", "name", "state", "zone", "created_time"}},
	"vm_get":       {Group: "vm", Verb: "get", Args: []string{"vm_id|name"}},
	"provision":    {Group: "vm", Verb: "create", Args: []string{"name"}, Rename: map[string]string{"image_metadata": "meta", "ports": "port"}, Pairs: map[string]bool{"image_metadata": true}},
	"destroy":      {Group: "vm", Verb: "delete", Args: []string{"vm_id|name"}},
	"list_zones":   {Group: "vm", Verb: "zones"},
	"list_flavors": {Group: "vm", Verb: "flavors", Columns: []string{"name", "vcpus", "ram_gb", "est_monthly_rub", "free_tier"}},

	// image
	"image_list": {Group: "image", Verb: "list", Args: []string{"query?"}, Render: "image-list"},
	"image_get":  {Group: "image", Verb: "get", Args: []string{"image_id|image"}, Render: "image-card"},

	// marketplace
	"marketplace_list": {Group: "marketplace", Verb: "list", Args: []string{"query?"}, Render: "marketplace-list"},

	// disk
	"disk_list":       {Group: "disk", Verb: "list", Columns: []string{"id", "name", "size_gb", "state", "disk_type", "zone", "attached_to"}},
	"list_disk_types": {Group: "disk", Verb: "types"},
	"disk_create":     {Group: "disk", Verb: "create", Args: []string{"name"}},
	"disk_attach":     {Group: "disk", Verb: "attach", Args: []string{"disk_id"}},
	"disk_detach":     {Group: "disk", Verb: "detach", Args: []string{"disk_id"}},
	"disk_resize":     {Group: "disk", Verb: "resize", Args: []string{"disk_id"}},
	"disk_delete":     {Group: "disk", Verb: "delete", Args: []string{"disk_id"}},

	// vpc
	"vpc_list":             {Group: "vpc", Verb: "list"},
	"vpc_get":              {Group: "vpc", Verb: "get", Args: []string{"id"}},
	"vpc_create":           {Group: "vpc", Verb: "create", Args: []string{"name"}},
	"vpc_delete":           {Group: "vpc", Verb: "delete", Args: []string{"id"}},
	"vpc_operation":        {Group: "vpc", Verb: "operation", Args: []string{"operation_id"}},
	"vpc_routes":           {Group: "vpc", Verb: "routes", Args: []string{"vpc_id"}},
	"vpc_route_create":     {Group: "vpc", Verb: "route-create", Args: []string{"vpc_id"}},
	"vpc_route_delete":     {Group: "vpc", Verb: "route-delete", Args: []string{"vpc_id", "route_id"}},
	"list_subnets":         {Group: "vpc", Verb: "subnets"},
	"list_security_groups": {Group: "vpc", Verb: "security-groups"},
	"list_floating_ips":    {Group: "vpc", Verb: "fips"},
	"fip_attach":           {Group: "vpc", Verb: "fip-attach", Args: []string{"fip"}},
	"release_floating_ip":  {Group: "vpc", Verb: "fip-release", Args: []string{"id?"}},

	// k8s
	"k8s_list":           {Group: "k8s", Verb: "list"},
	"k8s_get":            {Group: "k8s", Verb: "get", Args: []string{"cluster_id"}},
	"k8s_options":        {Group: "k8s", Verb: "options"},
	"k8s_plan":           {Group: "k8s", Verb: "plan"},
	"k8s_create":         {Group: "k8s", Verb: "create"},
	"k8s_delete":         {Group: "k8s", Verb: "delete", Args: []string{"cluster_id"}},
	"k8s_kubeconfig":     {Group: "k8s", Verb: "kubeconfig", Args: []string{"cluster_id"}},
	"k8s_nodepools":      {Group: "k8s", Verb: "nodepools", Args: []string{"cluster_id"}},
	"k8s_add_nodepool":   {Group: "k8s", Verb: "add-nodepool", Args: []string{"cluster_id"}},
	"k8s_addons":         {Group: "k8s", Verb: "addons", Args: []string{"query?"}, Render: "addon-list"},
	"k8s_addon_versions": {Group: "k8s", Verb: "addon-versions", Args: []string{"name"}, Render: "addon-versions"},
	"k8s_add_addon":      {Group: "k8s", Verb: "add-addon", Args: []string{"cluster_id", "name"}, Files: map[string]bool{"config": true}},
	"k8s_kubectl":        {Group: "k8s", Verb: "kubectl", Args: []string{"cluster_id", "args..."}, Raw: true},
	"k8s_apply":          {Group: "k8s", Verb: "apply", Args: []string{"cluster_id"}, Raw: true},
	"k8s_helm":           {Group: "k8s", Verb: "helm", Args: []string{"cluster_id", "action"}, Raw: true},

	// pg
	"pg_list":          {Group: "pg", Verb: "list"},
	"pg_get":           {Group: "pg", Verb: "get", Args: []string{"cluster_id"}, Omit: map[string]bool{"id": true}},
	"pg_versions":      {Group: "pg", Verb: "versions"},
	"pg_specs":         {Group: "pg", Verb: "specs"},
	"pg_create":        {Group: "pg", Verb: "create", Args: []string{"name"}},
	"pg_delete":        {Group: "pg", Verb: "delete", Args: []string{"cluster_id"}, Omit: map[string]bool{"id": true}},
	"pg_operation":     {Group: "pg", Verb: "operation", Args: []string{"operation_id"}},
	"pg_databases":     {Group: "pg", Verb: "databases", Args: []string{"cluster_id"}},
	"pg_db_create":     {Group: "pg", Verb: "db-create", Args: []string{"cluster_id", "name"}},
	"pg_db_delete":     {Group: "pg", Verb: "db-delete", Args: []string{"cluster_id", "name"}},
	"pg_users":         {Group: "pg", Verb: "users", Args: []string{"cluster_id"}},
	"pg_user_create":   {Group: "pg", Verb: "user-create", Args: []string{"cluster_id", "name"}},
	"pg_user_delete":   {Group: "pg", Verb: "user-delete", Args: []string{"cluster_id", "name"}},
	"pg_backups":       {Group: "pg", Verb: "backups", Args: []string{"cluster_id"}},
	"pg_backup_create": {Group: "pg", Verb: "backup-create", Args: []string{"cluster_id", "name"}},

	// s3
	"s3_buckets":       {Group: "s3", Verb: "buckets"},
	"s3_ls":            {Group: "s3", Verb: "ls", Args: []string{"bucket"}},
	"s3_get":           {Group: "s3", Verb: "get", Args: []string{"bucket", "key"}},
	"s3_put":           {Group: "s3", Verb: "put", Args: []string{"bucket", "key"}},
	"s3_rm":            {Group: "s3", Verb: "rm", Args: []string{"bucket", "key"}},
	"s3_bucket_create": {Group: "s3", Verb: "bucket-create", Args: []string{"bucket"}, Rename: map[string]string{"region": "s3-region"}},
	"s3_bucket_delete": {Group: "s3", Verb: "bucket-delete", Args: []string{"bucket"}},

	// secret
	"secret_list":        {Group: "secret", Verb: "list"},
	"secret_get":         {Group: "secret", Verb: "get", Args: []string{"path"}},
	"secret_value":       {Group: "secret", Verb: "value", Args: []string{"path"}},
	"secret_create":      {Group: "secret", Verb: "create", Args: []string{"path"}},
	"secret_add_version": {Group: "secret", Verb: "add-version", Args: []string{"path"}},
	"secret_delete":      {Group: "secret", Verb: "delete", Args: []string{"path"}},
}

// CLIPending are the tools without a command of their own yet, with the
// PR of the parity plan (docs/internal/cli-mcp-parity/spec.md §5) that
// gives them one; until then they are reached with 'evo tools call', and
// five of them through an engine command with its own code. The list only
// shrinks: TestRegistry_EveryToolHasCLISpec and TestParity_CommandsAndTools
// fail on a tool that is neither here nor in cliSpecs, and on one that is in
// both. Delete it once it is empty.
var CLIPending = map[string]string{
	// PR 3a: dns, cert, lb/tg, snat.
	"dns_zones": "PR 3a", "dns_zone_get": "PR 3a", "dns_zone_create": "PR 3a", "dns_zone_delete": "PR 3a",
	"dns_records": "PR 3a", "dns_record_get": "PR 3a", "dns_record_create": "PR 3a", "dns_record_delete": "PR 3a",
	"dns_operation": "PR 3a",
	"cert_list":     "PR 3a", "cert_get": "PR 3a", "cert_issue_le": "PR 3a", "cert_import": "PR 3a", "cert_delete": "PR 3a",
	"cert_private_key": "PR 3a", "cert_renew": "PR 3a", "cert_usage": "PR 3a", "cert_versions": "PR 3a",
	"cert_operation": "PR 3a",
	"lb_list":        "PR 3a", "lb_get": "PR 3a", "lb_create": "PR 3a", "lb_delete": "PR 3a", "lb_operation": "PR 3a",
	"tg_list": "PR 3a", "tg_get": "PR 3a", "tg_create": "PR 3a", "tg_delete": "PR 3a",
	"snat_list": "PR 3a", "snat_create": "PR 3a", "snat_delete": "PR 3a",

	// PR 3b: kafka, redis, ar, ca.
	"kafka_list": "PR 3b", "kafka_get": "PR 3b", "kafka_create": "PR 3b", "kafka_delete": "PR 3b",
	"kafka_operation": "PR 3b", "kafka_versions": "PR 3b", "kafka_specs": "PR 3b",
	"redis_list": "PR 3b", "redis_get": "PR 3b", "redis_create": "PR 3b", "redis_delete": "PR 3b",
	"redis_operation": "PR 3b", "redis_versions": "PR 3b", "redis_specs": "PR 3b",
	"ar_list": "PR 3b", "ar_get": "PR 3b", "ar_create": "PR 3b", "ar_delete": "PR 3b", "ar_operation": "PR 3b",
	"ca_list": "PR 3b", "ca_get": "PR 3b", "ca_deploy": "PR 3b", "ca_delete": "PR 3b", "ca_logs": "PR 3b",
	"ca_start": "PR 3b", "ca_stop": "PR 3b",

	// PR 3c: billing, audit, org, iam, metrics, stack, image, vpc open-ports,
	// deploy plan/apply/list.
	"billing_balance": "PR 3c", "billing_consumption": "PR 3c", "billing_finops": "PR 3c",
	"audit_events": "PR 3c",
	"org_quotas":   "PR 3c", "iam_service_accounts": "PR 3c",
	"metrics_history": "PR 3c", "metrics_scrape": "PR 3c",
	"stack_plan": "PR 3c", "stack_apply": "PR 3c", "stack_status": "PR 3c", "stack_destroy": "PR 3c",
	"image_create": "PR 3c", "image_delete": "PR 3c",
	"open_ports":  "PR 3c",
	"deploy_plan": "PR 3c", "deploy_apply": "PR 3c", "list": "PR 3c",

	// PR 5: the engine commands with their own code (evo deploy, status,
	// logs, project list, list-images) become commands of these tools.
	"deploy":       "PR 5: evo deploy (engine.Deploy)",
	"status":       "PR 5: evo status (engine.Status)",
	"logs":         "PR 5: evo logs (engine.Logs)",
	"org_projects": "PR 5: evo project list (listProjects)",
	"list_images":  "PR 5: evo list-images (engine.ListImages)",
}
