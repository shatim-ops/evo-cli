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
// A tool has a Verb (its command) or None (no command, with the reason);
// TestRegistry_EveryToolHasCLISpec keeps it so. Every tool has a command
// (the parity plan, docs/internal/cli-mcp-parity/spec.md, gave the last
// ones theirs in PR 5).
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

	// dns
	"dns_zones":         {Group: "dns", Verb: "zones", Columns: []string{"id", "name", "domain", "visibility", "state", "delegated"}},
	"dns_zone_get":      {Group: "dns", Verb: "zone-get", Args: []string{"id"}},
	"dns_zone_create":   {Group: "dns", Verb: "zone-create", Args: []string{"domain"}},
	"dns_zone_delete":   {Group: "dns", Verb: "zone-delete", Args: []string{"id"}},
	"dns_records":       {Group: "dns", Verb: "records", Args: []string{"zone_id"}},
	"dns_record_get":    {Group: "dns", Verb: "record-get", Args: []string{"record_id"}},
	"dns_record_create": {Group: "dns", Verb: "record-create", Args: []string{"zone_id", "name"}},
	"dns_record_delete": {Group: "dns", Verb: "record-delete", Args: []string{"record_id"}},
	"dns_operation":     {Group: "dns", Verb: "operation", Args: []string{"operation_id"}},

	// cert
	"cert_list":        {Group: "cert", Verb: "list"},
	"cert_get":         {Group: "cert", Verb: "get", Args: []string{"id"}},
	"cert_issue_le":    {Group: "cert", Verb: "issue-le", Args: []string{"name"}},
	"cert_import":      {Group: "cert", Verb: "import", Args: []string{"name"}},
	"cert_renew":       {Group: "cert", Verb: "renew", Args: []string{"certificate_id"}},
	"cert_delete":      {Group: "cert", Verb: "delete", Args: []string{"id"}},
	"cert_usage":       {Group: "cert", Verb: "usage", Args: []string{"id"}},
	"cert_versions":    {Group: "cert", Verb: "versions", Args: []string{"certificate_id"}},
	"cert_private_key": {Group: "cert", Verb: "private-key", Args: []string{"certificate_id"}},
	"cert_operation":   {Group: "cert", Verb: "operation", Args: []string{"operation_id"}},

	// lb, tg: the command is the prefix of the tool's name, not its group
	// (nlb, which --toolsets keeps): owner's decision Q4.
	"lb_list":      {Group: "lb", Verb: "list"},
	"lb_get":       {Group: "lb", Verb: "get", Args: []string{"id"}},
	"lb_create":    {Group: "lb", Verb: "create", Args: []string{"name"}},
	"lb_delete":    {Group: "lb", Verb: "delete", Args: []string{"id"}},
	"lb_operation": {Group: "lb", Verb: "operation", Args: []string{"operation_id"}},
	"tg_list":      {Group: "tg", Verb: "list"},
	"tg_get":       {Group: "tg", Verb: "get", Args: []string{"id"}},
	"tg_create":    {Group: "tg", Verb: "create", Args: []string{"name"}},
	"tg_delete":    {Group: "tg", Verb: "delete", Args: []string{"id"}},

	// snat
	"snat_list":   {Group: "snat", Verb: "list", Columns: []string{"id", "name", "status", "availability_zone", "nat_type"}},
	"snat_create": {Group: "snat", Verb: "create", Args: []string{"name"}},
	"snat_delete": {Group: "snat", Verb: "delete", Args: []string{"id"}},

	// kafka
	"kafka_list":      {Group: "kafka", Verb: "list"},
	"kafka_get":       {Group: "kafka", Verb: "get", Args: []string{"cluster_id"}},
	"kafka_versions":  {Group: "kafka", Verb: "versions"},
	"kafka_specs":     {Group: "kafka", Verb: "specs"},
	"kafka_create":    {Group: "kafka", Verb: "create", Args: []string{"name"}},
	"kafka_delete":    {Group: "kafka", Verb: "delete", Args: []string{"cluster_id"}},
	"kafka_operation": {Group: "kafka", Verb: "operation", Args: []string{"operation_id"}},

	// redis
	"redis_list":      {Group: "redis", Verb: "list"},
	"redis_get":       {Group: "redis", Verb: "get", Args: []string{"cluster_id"}},
	"redis_versions":  {Group: "redis", Verb: "versions"},
	"redis_specs":     {Group: "redis", Verb: "specs"},
	"redis_create":    {Group: "redis", Verb: "create", Args: []string{"name"}},
	"redis_delete":    {Group: "redis", Verb: "delete", Args: []string{"cluster_id"}},
	"redis_operation": {Group: "redis", Verb: "operation", Args: []string{"operation_id"}},

	// ar
	"ar_list":      {Group: "ar", Verb: "list"},
	"ar_get":       {Group: "ar", Verb: "get", Args: []string{"id"}},
	"ar_create":    {Group: "ar", Verb: "create", Args: []string{"name"}},
	"ar_delete":    {Group: "ar", Verb: "delete", Args: []string{"id"}},
	"ar_operation": {Group: "ar", Verb: "operation", Args: []string{"operation_id"}},

	// ca
	"ca_list":   {Group: "ca", Verb: "list"},
	"ca_get":    {Group: "ca", Verb: "get", Args: []string{"name"}},
	"ca_deploy": {Group: "ca", Verb: "deploy", Args: []string{"name"}, Pairs: map[string]bool{"env": true}},
	"ca_delete": {Group: "ca", Verb: "delete", Args: []string{"name"}},
	"ca_logs":   {Group: "ca", Verb: "logs", Args: []string{"name"}, Render: "log-lines"},
	"ca_start":  {Group: "ca", Verb: "start", Args: []string{"name"}},
	"ca_stop":   {Group: "ca", Verb: "stop", Args: []string{"name"}},

	// billing, audit, org, iam. project_id of these tools is a filter of
	// what is read, not the project of the configuration: its flag is
	// --project-id of the command, which shadows the root one (as deploy's
	// does), so that 'evo org quotas --project-id X' passes X to the tool.
	"billing_balance":      {Group: "billing", Verb: "balance"},
	"billing_consumption":  {Group: "billing", Verb: "consumption", Rename: map[string]string{"project_id": "project-id"}},
	"billing_finops":       {Group: "billing", Verb: "finops", Rename: map[string]string{"project_id": "project-id"}},
	"audit_events":         {Group: "audit", Verb: "events"},
	"org_quotas":           {Group: "org", Verb: "quotas", Rename: map[string]string{"project_id": "project-id"}},
	"iam_service_accounts": {Group: "iam", Verb: "service-accounts", Rename: map[string]string{"project_id": "project-id"}},

	// metrics: the command is the prefix of the tool's name, not its group
	// (monitoring, which --toolsets keeps): owner's decision Q4.
	"metrics_history": {Group: "metrics", Verb: "history", Args: []string{"match"}},
	"metrics_scrape":  {Group: "metrics", Verb: "scrape", Args: []string{"match?"}, Render: "exposition"},

	// stack
	"stack_plan":    {Group: "stack", Verb: "plan", Files: map[string]bool{"manifest": true}},
	"stack_apply":   {Group: "stack", Verb: "apply", Args: []string{"job_id"}},
	"stack_status":  {Group: "stack", Verb: "status", Args: []string{"job_id"}},
	"stack_destroy": {Group: "stack", Verb: "destroy", Args: []string{"job_id"}},

	// The engine commands (spec REQ-4, REQ-11): commands of v1.0 that keep
	// their help and flags and call these tools; internal/cli builds them
	// by hand (engineCommands), the trees do not. evo list-images is the
	// command of list_images: hidden and deprecated (evo image list) until
	// v2.0.
	"deploy":      {Verb: "deploy"},
	"status":      {Verb: "status"},
	"logs":        {Verb: "logs"},
	"list_images": {Verb: "list-images"},
	"whoami":      {Verb: "whoami"},
	"doctor":      {Verb: "doctor"},
	// The deploy wizard and the records: subcommands of the engine command
	// evo deploy, which is a command with an action of its own and a group
	// at once (spec.md R7): 'evo deploy' deploys, 'evo deploy plan' plans.
	// The trees add them to evo deploy instead of a second root deploy.
	"deploy_plan":  {Group: "deploy", Verb: "plan", Rename: map[string]string{"image_metadata": "meta", "ports": "port"}, Pairs: map[string]bool{"image_metadata": true}},
	"deploy_apply": {Group: "deploy", Verb: "apply", Args: []string{"plan_id"}},
	"list":         {Group: "deploy", Verb: "list", Columns: []string{"id", "vm_name", "vm_ip", "status", "flavor_name", "created_at"}},
	// org projects; evo project list is its alias (internal/cli).
	"org_projects": {Group: "org", Verb: "projects", Render: "project-list"},

	// image, vpc: commands added to the trees of v1.1.
	"image_create": {Group: "image", Verb: "create", Args: []string{"name"}},
	"image_delete": {Group: "image", Verb: "delete", Args: []string{"image_id"}},
	"open_ports":   {Group: "vpc", Verb: "open-ports", Args: []string{"security_group"}, Rename: map[string]string{"ports": "port"}},
}
