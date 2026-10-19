// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package tools is the registry of evo operations: name, group,
// description, JSON schema of the arguments, handler, and whether the
// operation changes anything in the cloud.
//
// The registry does not know about transports. The MCP server
// (internal/mcp) and the CLI ('evo tools …' and the command trees)
// are two clients of the same handlers: arguments come in as
// map[string]any, the result goes out as a JSON string plus an isError flag.
//
// Handlers never write to stdout: engine output is redirected to in-memory
// buffers, because for MCP stdout is the protocol channel.
package tools

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// EngineFactory builds an *engine.Engine from a set of config overrides.
// Overrides use the same field names as config flags (e.g. "ComposeFile",
// "Region", "ProjectID"). A nil or empty map yields the default config.
type EngineFactory func(overrides map[string]any) (*engine.Engine, error)

// Handler executes a single tool call. It returns the textual (JSON) result
// and a flag telling whether the result is a tool-level error.
type Handler func(ctx context.Context, args map[string]any) (text string, isError bool)

// toolHandler is the name the handler files use.
type toolHandler = Handler

// Tool is one registry entry.
type Tool struct {
	Name        string         `json:"name"`
	Group       string         `json:"group"`
	Mutating    bool           `json:"mutating"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	// RequiredOneOf are argument sets one argument of each the call must
	// have, beside the schema's "required" (see requiredOneOf).
	RequiredOneOf [][]string `json:"-"`
	Handler       Handler    `json:"-"`
}

// OpWaitMaxSeconds is the longest wait_seconds the *_operation tools honour.
const OpWaitMaxSeconds = opWaitMaxSeconds

// Registry holds every tool and its handler. Handlers build an engine per
// call through the factory, so each call sees the current config, profile
// and working directory.
type Registry struct {
	newEngine EngineFactory
	tools     []Tool
	handlers  map[string]toolHandler

	// jobLocks serialize the stack tools per job_id (see lockJob).
	jobMu    sync.Mutex
	jobLocks map[string]*sync.Mutex
}

// New builds the registry. factory is called by handlers that need an
// engine (credentials, project, region).
func New(factory EngineFactory) *Registry {
	r := &Registry{newEngine: factory}
	r.registerTools()
	for i := range r.tools {
		t := &r.tools[i]
		t.Group = groupOf(t.Name)
		t.Mutating = isMutating(*t)
		t.RequiredOneOf = requiredOneOf[t.Name]
		if h := r.handlers[t.Name]; h != nil {
			if projectTools[t.Name] {
				h = r.requireProject(h)
			}
			r.handlers[t.Name] = checkedArgs(t.Name, t.InputSchema, h)
		}
		t.Handler = r.handlers[t.Name]
	}
	return r
}

// Tools returns the tools in registration order (the order MCP advertises).
func (r *Registry) Tools() []Tool {
	return r.tools
}

// Lookup returns the tool with the given name.
func (r *Registry) Lookup(name string) (Tool, bool) {
	for _, t := range r.tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// Wrap replaces the handler of the named tool with wrap(current handler).
// Every caller sees the new handler: transports, Tools/Lookup, and the
// in-process calls the stack orchestrator makes. It reports whether the
// tool exists. Transport policies (MCP over HTTP) use it; the CLI builds its
// own registry and is not affected.
func (r *Registry) Wrap(name string, wrap func(next Handler) Handler) bool {
	next, ok := r.handlers[name]
	if !ok {
		return false
	}
	h := wrap(next)
	r.handlers[name] = h
	for i := range r.tools {
		if r.tools[i].Name == name {
			r.tools[i].Handler = h
		}
	}
	return true
}

// ErrResult formats an error as a JSON tool result, as the handlers do.
func ErrResult(err error) string { return errResult(err) }

// BoolArg reads a boolean argument, def when it is absent.
func BoolArg(args map[string]any, key string, def bool) bool { return boolArg(args, key, def) }

// requiredOneOf names the tools that need one argument of a set, which
// "required" cannot say: each set is the arguments one of which the call
// must have (vm_get: vm_id or name). The handler answers USAGE without
// one.
//
// The input schema does not carry this as a top-level anyOf (QA 20.10,
// QA-4): the Anthropic API refuses a tool whose input_schema has anyOf,
// oneOf or allOf at the top level (400 «input_schema does not support
// oneOf, allOf, or anyOf at the top level»), and an MCP client hands it
// the schema as it is — one such tool fails every request of the session.
// The property descriptions say it instead; Tool.RequiredOneOf carries
// it to docs/tools.md and the empty-arguments test.
var requiredOneOf = map[string][][]string{
	"pg_get":              {{"id", "cluster_id"}},
	"pg_delete":           {{"id", "cluster_id"}},
	"vm_get":              {{"vm_id", "name"}},
	"image_get":           {{"image", "image_id"}},
	"release_floating_ip": {{"id", "ip_address"}},
	"tg_create":           {{"vm_id", "targets"}},
	"dns_record_create":   {{"value", "values"}},
	"vpc_route_create":    {{"next_hop_vip_id", "next_hop_vm_id", "next_hop_magic_router_id"}},
}

// Groups returns the group names in alphabetical order.
func Groups() []string {
	seen := map[string]bool{}
	for _, g := range groupByName {
		seen[g] = true
	}
	for _, p := range groupPrefixes {
		seen[p.group] = true
	}
	out := make([]string, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// groupByName assigns tools whose name does not start with their group.
var groupByName = map[string]string{
	"deploy":       "deploy",
	"deploy_plan":  "deploy",
	"deploy_apply": "deploy",
	"status":       "deploy",
	"list":         "deploy",
	"logs":         "deploy",
	"destroy":      "deploy",
	"open_ports":   "deploy",

	"provision":    "vm",
	"vm_get":       "vm",
	"list_zones":   "vm",
	"list_flavors": "vm",

	"list_images":      "image",
	"marketplace_list": "image",

	"list_disk_types": "disk",

	"list_subnets":         "vpc",
	"list_security_groups": "vpc",
	"list_floating_ips":    "vpc",
	"release_floating_ip":  "vpc",
	"fip_attach":           "vpc",

	"iam_service_accounts": "iam",
	"org_projects":         "org",
	"org_quotas":           "org",
}

// groupPrefixes assigns the rest by name prefix.
var groupPrefixes = []struct{ prefix, group string }{
	{"vm_", "vm"},
	{"image_", "image"},
	{"disk_", "disk"},
	{"vpc_", "vpc"},
	{"snat_", "snat"},
	{"k8s_", "k8s"},
	{"pg_", "pg"},
	{"kafka_", "kafka"},
	{"redis_", "redis"},
	{"lb_", "nlb"},
	{"tg_", "nlb"},
	{"dns_", "dns"},
	{"secret_", "secret"},
	{"ar_", "ar"},
	{"ca_", "ca"},
	{"cert_", "cert"},
	{"s3_", "s3"},
	{"billing_", "billing"},
	{"metrics_", "monitoring"},
	{"audit_", "audit"},
	{"stack_", "stack"},
}

// groupOf returns the group of a tool, or "" when none matches (a test
// keeps that from happening).
func groupOf(name string) string {
	if g, ok := groupByName[name]; ok {
		return g
	}
	for _, p := range groupPrefixes {
		if strings.HasPrefix(name, p.prefix) {
			return p.group
		}
	}
	return ""
}

// projectTools call a project-scoped API on every call: a list or a create
// of the project (project_id in the query or the body, or the project in the
// path: monitoring, audit), or resources addressed by name or path inside
// the project (Container Apps, Secret Manager), and the mk8s catalogues
// (projectId is required there). Without a project they
// answer CONFIG_INVALID before any request (release audit, R-06): the API
// said 422 "query.project_id: Field required", and that was API_ERROR,
// exit 5. Tools addressing a resource by its id, catalogues (zones,
// flavors, images, Marketplace, disk types), org_*, billing_*, Object
// Storage and the local state work without one; a name a tool resolves
// through a compute list hits the same check there (provision). The check
// is engineFor's: the builders of the handlers' engines and clients use it.
var projectTools = map[string]bool{
	"vm_list": true, "list_subnets": true, "list_security_groups": true, "list_floating_ips": true,
	"disk_list": true, "disk_create": true, "image_create": true, "snat_list": true, "snat_create": true,
	"deploy_plan": true,
	"vpc_list":    true, "vpc_create": true, "vpc_route_create": true,
	"k8s_list": true, "k8s_plan": true, "k8s_create": true,
	// mk8s catalogues take projectId too: GET /v3/addons without it is a
	// live 400, the spec requires it on /v3/addons and productConfiguration
	// (Cursor review of PR #89).
	"k8s_options": true, "k8s_addons": true, "k8s_addon_versions": true, "k8s_add_addon": true,
	"ca_list": true, "ca_get": true, "ca_deploy": true, "ca_delete": true, "ca_logs": true, "ca_start": true, "ca_stop": true,
	"iam_service_accounts": true,
	"pg_list":              true, "pg_create": true, "kafka_list": true, "kafka_create": true, "redis_list": true, "redis_create": true,
	"lb_list": true, "lb_create": true, "tg_list": true, "tg_create": true,
	"dns_zones": true, "dns_zone_create": true,
	"secret_list": true, "secret_get": true, "secret_value": true, "secret_create": true, "secret_add_version": true, "secret_delete": true,
	"ar_list": true, "ar_get": true, "ar_create": true, "ar_delete": true,
	"metrics_history": true, "metrics_scrape": true, "audit_events": true,
	"cert_list": true, "cert_issue_le": true, "cert_import": true,
}

// needsProject marks the context of a projectTools call that names no
// project itself (project_id).
type needsProject struct{}

// requireProject marks the call: engineFor then refuses an engine without
// a project. The check is where the handler builds its engine, so a
// preview or an argument refused before any engine stays as it was.
func (r *Registry) requireProject(next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		if stringArg(args, "project_id") == "" {
			ctx = context.WithValue(ctx, needsProject{}, true)
		}
		return next(ctx, args)
	}
}

// engineFor builds the engine of a handler, as newEngine does; for a
// projectTools call with no project configured it is CONFIG_INVALID
// (errors.ProjectRequired), before any token or API request. With no key
// either, the missing key is reported first (AUTH_REQUIRED, the same exit
// code 2): it is the first step of a new setup, and 'evo configure' sets
// both, while the project hint alone led to a second refusal.
func (r *Registry) engineFor(ctx context.Context, overrides map[string]any) (*engine.Engine, error) {
	eng, err := r.newEngine(overrides)
	if err != nil || eng == nil {
		return eng, err
	}
	if marked, _ := ctx.Value(needsProject{}).(bool); marked && eng.Config.ProjectID == "" {
		if (eng.Config.KeyID == "" || eng.Config.Secret == "") && eng.Auth != nil {
			// Without a key the IAM provider refuses at once, with no request.
			if _, err := eng.Auth.Token(ctx); err != nil {
				return nil, err
			}
		}
		return nil, clierrors.ProjectRequired()
	}
	return eng, nil
}

// sensitiveTools reveal a secret in plaintext (a decrypted secret value, a
// private key). They take confirmed, so the agent asks the person first, but
// they change nothing, so the rule "a tool is mutating when it takes
// confirmed" does not apply: they are not Mutating (no destructiveHint, no
// mutating label in the CLI). They are not marked readOnlyHint either: a
// client may auto-approve read-only tools, and the plaintext would then land
// in the agent's context (and its logs) without the person seeing the call
// (security audit 19.10, SEC-4).
var sensitiveTools = map[string]bool{
	"cert_private_key": true,
	"secret_value":     true,
}

// IsSensitive reports whether a tool reveals a secret and asks for consent
// although it changes nothing (sensitiveTools).
func IsSensitive(t Tool) bool { return sensitiveTools[t.Name] }

// destructiveTools are the mutating tools, besides *_delete, that may delete
// or overwrite data: they get destructiveHint in MCP, so a client asks the
// person every time. kubectl, apply and helm run whatever they are given
// (delete, uninstall); s3_put overwrites an existing key; k8s_add_addon with
// resolve_mode OVERWRITE replaces objects already in the cluster.
var destructiveTools = map[string]bool{
	"destroy":             true,
	"stack_destroy":       true,
	"release_floating_ip": true,
	"s3_rm":               true,
	"s3_put":              true,
	"k8s_kubectl":         true,
	"k8s_apply":           true,
	"k8s_helm":            true,
	"k8s_add_addon":       true,
}

// IsDestructive reports whether a mutating tool may delete or overwrite
// data; the other mutating tools only add or change in place.
func IsDestructive(t Tool) bool {
	return t.Mutating && (strings.HasSuffix(t.Name, "_delete") || destructiveTools[t.Name])
}

// isMutating reports whether the tool changes state in the cloud.
func isMutating(t Tool) bool {
	if sensitiveTools[t.Name] {
		return false
	}
	props, _ := t.InputSchema["properties"].(map[string]any)
	_, gated := props["confirmed"]
	return gated
}
