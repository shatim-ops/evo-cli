// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

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
}

// decls are the declarations of the tools of evo, in the order of
// registerTools. A tool with nothing to declare has no entry.
var decls = map[string]Decl{
	"deploy":               {Group: "deploy"},
	"provision":            {Group: "vm", Project: true},
	"status":               {Group: "deploy"},
	"list":                 {Group: "deploy"},
	"vm_get":               {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}},
	"vm_start":             {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}},
	"vm_stop":              {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}},
	"vm_reboot":            {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}},
	"vm_resize":            {Group: "vm", RequiredOneOf: [][]string{{"vm_id", "name"}}},
	"vm_list":              {Project: true},
	"logs":                 {Group: "deploy"},
	"destroy":              {Group: "deploy"},
	"list_zones":           {Group: "vm"},
	"marketplace_list":     {Group: "image"},
	"list_images":          {Group: "image"},
	"image_create":         {Project: true},
	"fip_attach":           {Group: "vpc"},
	"image_get":            {RequiredOneOf: [][]string{{"image", "image_id"}}},
	"deploy_plan":          {Group: "deploy", Project: true},
	"deploy_apply":         {Group: "deploy"},
	"list_flavors":         {Group: "vm"},
	"list_disk_types":      {Group: "disk"},
	"list_subnets":         {Group: "vpc", Project: true},
	"subnet_create":        {Group: "vpc", Project: true},
	"subnet_delete":        {Group: "vpc"},
	"list_security_groups": {Group: "vpc", Project: true},
	"list_floating_ips":    {Group: "vpc", Project: true},
	"open_ports":           {Group: "deploy"},
	"close_ports":          {Group: "deploy"},
	"sg_rule_create":       {Group: "vpc"},
	"sg_rule_delete":       {Group: "vpc"},
	"k8s_list":             {Project: true},
	// mk8s catalogues take projectId too: GET /v3/addons without it is a
	// live 400, the spec requires it on /v3/addons and productConfiguration
	// (Cursor review of PR #89).
	"k8s_options":          {Project: true},
	"k8s_plan":             {Project: true},
	"k8s_create":           {Project: true},
	"k8s_addons":           {Project: true},
	"k8s_addon_versions":   {Project: true},
	"k8s_add_addon":        {Project: true},
	"backup_list":          {Project: true},
	"backup_create":        {Project: true, RequiredOneOf: [][]string{{"vm_id", "disk_id"}}},
	"disk_list":            {Project: true},
	"disk_create":          {Project: true},
	"ca_list":              {Project: true},
	"ca_get":               {Project: true},
	"ca_deploy":            {Project: true},
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
	"release_floating_ip":  {Group: "vpc", RequiredOneOf: [][]string{{"id", "ip_address"}}},
	"snat_list":            {Project: true},
	"snat_create":          {Project: true},
	"pg_list":              {Project: true},
	"pg_get":               {RequiredOneOf: [][]string{{"id", "cluster_id"}}},
	"pg_create":            {Project: true},
	"pg_delete":            {RequiredOneOf: [][]string{{"id", "cluster_id"}}},
	"kafka_list":           {Project: true},
	"kafka_create":         {Project: true},
	"redis_list":           {Project: true},
	"redis_create":         {Project: true},
	"lb_list":              {Project: true},
	"tg_list":              {Project: true},
	"tg_create":            {Project: true, RequiredOneOf: [][]string{{"vm_id", "targets"}}},
	"lb_create":            {Project: true},
	"vpc_list":             {Project: true},
	"vpc_create":           {Project: true},
	"vpc_route_create":     {Project: true, RequiredOneOf: [][]string{{"next_hop_vip_id", "next_hop_vm_id", "next_hop_magic_router_id"}}},
	"dns_zones":            {Project: true},
	"dns_zone_create":      {Project: true},
	"dns_record_create":    {RequiredOneOf: [][]string{{"value", "values"}}},
	"secret_list":          {Project: true},
	"secret_get":           {Project: true},
	"secret_value":         {Project: true},
	"secret_create":        {Project: true},
	"secret_add_version":   {Project: true},
	"secret_delete":        {Project: true},
	"ar_list":              {Project: true},
	"ar_get":               {Project: true},
	"ar_create":            {Project: true},
	"ar_delete":            {Project: true},
	"metrics_history":      {Project: true},
	"metrics_scrape":       {Project: true},
	"audit_events":         {Project: true},
	"cert_list":            {Project: true},
	"cert_issue_le":        {Project: true},
	"cert_import":          {Project: true},
}
