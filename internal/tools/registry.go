// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package tools is the registry of evo operations: name, group,
// description, JSON schema of the arguments, handler, and whether the
// operation changes anything in the cloud.
//
// The registry does not know about the MCP transports (stdio, HTTP); how a
// tool is presented in the CLI (its command, positional arguments, flags,
// table: CLISpec) is part of the tool's description, like its schema. The
// MCP server (internal/mcp) and the CLI ('evo tools …' and the command
// trees built from CLISpec) are two clients of the same handlers: arguments
// come in as map[string]any, the result goes out as a JSON string plus an
// isError flag.
//
// Handlers never write to stdout: engine output is redirected to in-memory
// buffers, because for MCP stdout is the protocol channel.
package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// EngineFactory builds an *engine.Engine from a set of config overrides.
// Overrides use the same field names as config flags (e.g. "ComposeFile",
// "Region", "ProjectID"), and WarningsShown. A nil or empty map yields the
// default config.
type EngineFactory func(overrides map[string]any) (*engine.Engine, error)

// WarningsShown (true) is the override of a tool whose answer shows the
// configuration's warnings (Config.Warnings: doctor's checks): the factory
// does not print them as well. It is no config field.
const WarningsShown = "WarningsShown"

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
	// ShortRU, DescriptionRU and ArgDescriptionsRU describe the tool and
	// its arguments in Russian, for the CLI (toolsRU): ShortRU is the
	// summary, DescriptionRU the rest. Description and the schema stay the
	// English text MCP sends.
	ShortRU           string            `json:"-"`
	DescriptionRU     string            `json:"-"`
	ArgDescriptionsRU map[string]string `json:"-"`
	// RequiredOneOf are argument sets one argument of each the call must
	// have, beside the schema's "required" (see requiredOneOf).
	RequiredOneOf [][]string `json:"-"`
	// CLI is the tool's command in the CLI (cliSpecs).
	CLI     CLISpec `json:"-"`
	Handler Handler `json:"-"`
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

	// readOnly: SetReadOnly was called; stack_status advances no stack,
	// deploy_plan writes no deploy key and no plan.
	readOnly bool
	// projectBound: SetProjectBound was called; a read that names no
	// project reads the configuration's project, not the agreement or the
	// organization, and a resource named by id must be of an allowed
	// project (boundToProjects).
	projectBound bool
	// ops are the operations this bound registry started (kindOperation).
	ops operationSet
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
		t.CLI = cliSpecs[t.Name]
		ru := toolsRU[t.Name]
		t.ShortRU, t.DescriptionRU = ru.Short, ru.Long
		t.ArgDescriptionsRU = argDescriptionsRU(*t, ru.Args)
		if h := r.handlers[t.Name]; h != nil {
			// Inside requireProject: the check's engine sees its mark.
			h = r.boundToProjects(*t, h)
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

// SetReadOnly makes the registry act on no consent given before: a running
// stack is reported by stack_status, not advanced, since its steps would
// create resources on the consent stack_apply was given. Nor does it
// prepare a call it will not make: deploy_plan shows the plan without
// creating the deploy key or saving the plan, so it hands out no plan_id
// (break 1.2, B12-9). evo mcp --read-only sets it, and refuses every call
// that needs consent (NeedsConsent) itself (internal/mcp, H11). Call it
// before the registry serves calls.
func (r *Registry) SetReadOnly() { r.readOnly = true }

// SetProjectBound binds the registry to the project of its engines'
// configuration: a read that names no project_id and would read more than
// a project otherwise reads that project only — billing_consumption and
// billing_finops the consumption of the project, not of the whole billing
// agreement (billingClient); org_projects lists that project, not the
// organization's. A call in another project is the factory's to refuse,
// and so is a resource a tool names by id (idArgs): before the handler,
// the registry finds the resource's project and asks the factory for an
// engine there (boundToProjects); a *_operation tool reads only the
// operations this registry started. evo mcp --allow-project sets it (H9),
// where the factory admits allowed projects only; org_quotas and
// iam_service_accounts with project_id stay the documented reads of other
// projects. Without it the CLI and MCP read as documented: the agreement,
// the organization, a resource by id with no check. Call it before the
// registry serves calls.
func (r *Registry) SetProjectBound() { r.projectBound = true }

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

// ParseToolsets reads a --toolsets list (evo mcp, evo agent setup): the
// groups it names, lower-cased, without repeats, in the order given; nil
// for every group ("all", or no group named). An unknown group is an error.
func ParseToolsets(groups []string) ([]string, error) {
	known := map[string]bool{}
	for _, g := range Groups() {
		known[g] = true
	}
	var want []string
	seen := map[string]bool{}
	for _, g := range groups {
		g = strings.ToLower(strings.TrimSpace(g))
		switch {
		case g == "", seen[g]:
		case g == "all":
			return nil, nil
		case !known[g]:
			return nil, fmt.Errorf("unknown toolset %q; known: all, %s", g, strings.Join(Groups(), ", "))
		default:
			seen[g] = true
			want = append(want, g)
		}
	}
	return want, nil
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

	"whoami": "auth",
	"doctor": "auth",
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
	"deploy_plan": true, "provision": true,
	"vpc_list": true, "vpc_create": true, "vpc_route_create": true,
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
	return takesConsent(t) && !sensitiveTools[t.Name]
}

// takesConsent reports whether some call of the tool needs the person's
// consent: its schema has confirmed. Those are the mutating tools and
// sensitiveTools; the schema is where the agent learns it, so it is where
// the registry reads it too.
func takesConsent(t Tool) bool {
	props, _ := t.InputSchema["properties"].(map[string]any)
	_, gated := props["confirmed"]
	return gated
}

// partialConsent are the tools that take confirmed but need it only for
// some calls, by the rule their handler gates on: k8s_kubectl for a
// command that is not a read (kubectlNeedsConsent), k8s_helm for install,
// upgrade and uninstall (helmNeedsConsent). Every call of any other tool
// that takes confirmed needs it.
var partialConsent = map[string]func(args map[string]any) bool{
	"k8s_kubectl": kubectlNeedsConsent,
	"k8s_helm":    helmNeedsConsent,
}

// NeedsConsent reports whether this call of t needs confirmed=true: the
// call changes the cloud or reveals a secret, and the handler answers
// confirmation_required without it. This is the one decision behind the
// gates of the handlers and evo mcp --read-only, which refuses such a
// call whether confirmed is set or not.
func NeedsConsent(t Tool, args map[string]any) bool {
	if !takesConsent(t) {
		return false
	}
	if needs, partial := partialConsent[t.Name]; partial {
		return needs(args)
	}
	return true
}

// AlwaysNeedsConsent reports whether every call of t needs confirmed=true
// (NeedsConsent whatever the arguments).
func AlwaysNeedsConsent(t Tool) bool {
	_, partial := partialConsent[t.Name]
	return takesConsent(t) && !partial
}
