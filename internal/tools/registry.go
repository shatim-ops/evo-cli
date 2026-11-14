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
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
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
	// UntrustedOutput: the answer carries what a workload or a stored
	// object wrote (logs, kubectl output, S3 objects), which the agent must
	// not take for instructions (untrustedFields).
	UntrustedOutput bool `json:"-"`
	// CLI is the tool's command in the CLI (cliSpecs).
	CLI CLISpec `json:"-"`
	// Decl is the tool's declaration (decls).
	Decl    Decl    `json:"-"`
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
	// unchecked are the handlers under the check of the arguments
	// (checkedArgs): Wrap wraps them, so the check stays the first layer of
	// every call whatever a transport adds (G13-1).
	unchecked map[string]Handler

	// served reports whether the server serves the tool named (evo mcp
	// --toolsets, --read-only); nil: every tool. agent_skill marks by it
	// the tools of a card that are not on the server (SetServed).
	served func(name string) bool

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
	// callBudget: SetCallBudget; 0 — a call waits as long as its work
	// takes (the CLI).
	callBudget time.Duration
	// pendingNoted: an answer of this process over MCP carried the intents
	// earlier runs left (withPending) — the first JSON object; the CLI says
	// them on stderr.
	// stateDirOf: SetStateDir.
	stateDirOf string

	pendingMu    sync.Mutex
	pendingNoted bool
}

// MCPCallBudget is how long a call of evo mcp may wait for the cloud before
// it answers with the state of what it started (D69): MCP clients often
// break a call off at 60 s (Claude Code's default MCP_TOOL_TIMEOUT).
const MCPCallBudget = 45 * time.Second

// SetCallBudget bounds the wait of every call of the registry for the
// cloud: a call that has not seen its work end answers within d of its
// start with the state so far and the next step (D69). The CLI sets none
// and waits as before (H14); a call's wait argument overrides it. evo mcp
// sets MCPCallBudget. Call it before the registry serves calls.
func (r *Registry) SetCallBudget(d time.Duration) { r.callBudget = d }

// CallBudget is the budget SetCallBudget set, 0 without one.
func (r *Registry) CallBudget() time.Duration { return r.callBudget }

type callUntilKey struct{}

// withCallStart is ctx with the end of the budget of the outermost call: a
// tool a tool calls (a step of a stack) spends the budget of the call it is
// a step of. Without a budget ctx is as it was.
func (r *Registry) withCallStart(ctx context.Context) context.Context {
	if _, ok := ctx.Value(callUntilKey{}).(time.Time); ok || r.callBudget <= 0 {
		return ctx
	}
	// wall-clock: the end of D69's budget of the call.
	return context.WithValue(ctx, callUntilKey{}, time.Now().Add(r.callBudget))
}

// waitLeft is how long the call may still wait for the cloud, and whether
// it is bounded at all: the argument wait (false — not at all, true — as
// long as the work takes), else what is left of the budget of the
// outermost call (budgetLeft).
func (r *Registry) waitLeft(ctx context.Context, args map[string]any) (time.Duration, bool) {
	if waitOff(args) {
		return 0, true
	}
	if _, given := args["wait"].(bool); given {
		return 0, false
	}
	return budgetLeft(ctx)
}

// waitOff reports whether the call asked not to wait for the cloud at all
// (wait=false): the one reading of the argument wait.
func waitOff(args map[string]any) bool {
	w, given := args["wait"].(bool)
	return given && !w
}

// budgetLeft is what is left of the budget of the call ctx belongs to, and
// whether it has one (withCallStart): the bound of every wait for the
// cloud inside the call, the waits of operations included.
func budgetLeft(ctx context.Context) (time.Duration, bool) {
	until, ok := ctx.Value(callUntilKey{}).(time.Time)
	if !ok {
		return 0, false
	}
	// wall-clock: D69's budget is a bound on the wall clock of the call.
	return max(time.Until(until), 0), true
}

// callBound is how long a step of this call may run: its own limit, or —
// in an MCP call (D69) — what is left of the call's budget, less a tenth
// (at most 2 s) to answer; capped says the budget set it. One point for the
// steps that run their own clock: kubectl, helm, the waits for a disk.
func callBound(ctx context.Context, own time.Duration) (time.Duration, bool) {
	left, ok := budgetLeft(ctx)
	if !ok {
		return own, false
	}
	t := left - min(left/10, 2*time.Second)
	if t >= own {
		return own, false
	}
	return t, true
}

// callCtx is ctx ended where callBound ends this call's steps: the reads
// of the cloud a call makes while it waits end with the call, leaving it
// time to answer, not past it (D69, H14; agent run of 1.7.0: a read after
// the budget had no bound, and a call took 56 s and more). Without a
// budget it is ctx.
func callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if t, capped := callBound(ctx, time.Duration(math.MaxInt64)); capped {
		return context.WithTimeout(ctx, t)
	}
	return ctx, func() {}
}

// callEndCtx is ctx ended with the budget of the call itself, without the
// time to answer callCtx leaves: a last read in that time. Without a
// budget it is ctx.
func callEndCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if left, capped := budgetLeft(ctx); capped {
		return context.WithTimeout(ctx, left)
	}
	return ctx, func() {}
}

// callRecords are the records the call itself opened (beginIntent), by
// id: its own answer says how it ended and does not name them as what an
// earlier run left (notePending).
//
// It also holds what the call has read of the directory of the records:
// the layers of one call (the question after a cut call, the held record,
// the records of another key) read it once and again only after the call
// itself wrote there — not once each.
// The holder is of one call and is not guarded: the layers of a call run
// one after another, in its goroutine.
type callRecords struct {
	ids []string

	root    string
	pending []intent.Intent
	read    bool

	// open is the record the call opened (beginIntent), with hasOpen.
	open    intent.Intent
	hasOpen bool

	// resolved is what the call has resolved of the cloud by a read that
	// more than one of its layers needs (onceACall).
	resolved map[string]resolvedOnce
}

// resolvedOnce is one result of onceACall.
type resolvedOnce struct {
	value any
	err   error
}

// onceACall is read() for the call of ctx, made once for key: the layer
// of intents resolves the resource a call names before the plan of the
// tool does, by the same resolver, and the call reads the cloud once for
// both. A call with no holder (a layer called on its own) reads each time.
func onceACall[T any](ctx context.Context, key string, read func() (T, error)) (T, error) {
	c, ok := ctx.Value(callRecordsKey{}).(*callRecords)
	if !ok {
		return read()
	}
	if was, ok := c.resolved[key]; ok {
		v, _ := was.value.(T)
		return v, was.err
	}
	v, err := read()
	if c.resolved == nil {
		c.resolved = map[string]resolvedOnce{}
	}
	c.resolved[key] = resolvedOnce{value: v, err: err}
	return v, err
}

// pendingIn are the records of root as the call of ctx sees them: read
// once for the call, and again after forgetPending. A call with no holder
// (a layer called on its own) reads the directory.
func pendingIn(ctx context.Context, root string) []intent.Intent {
	c, ok := ctx.Value(callRecordsKey{}).(*callRecords)
	if !ok {
		return readPending(intent.Dir(root))
	}
	if !c.read || c.root != root {
		c.root, c.pending, c.read = root, readPending(intent.Dir(root)), true
	}
	return c.pending
}

// readPending reads the records of a directory (tests count the reads of
// a call).
var readPending = intent.Pending

// forgetPending makes the next pendingIn of the call read the directory:
// the call wrote or closed a record there.
func forgetPending(ctx context.Context) {
	if c, ok := ctx.Value(callRecordsKey{}).(*callRecords); ok {
		c.read = false
	}
}

// markSent is told by the counter of the call's writes
// (httpretry.CountWrites) right before a write goes out, until it says the
// call is marked: the record the call opened is marked at once — a process
// killed after the write leaves the mark. A write before the record is
// opened marks nothing and does not spend the mark (false). An error: the
// mark is not written, and the write does not go out.
func markSent(ctx context.Context) (marked bool, err error) {
	c, ok := ctx.Value(callRecordsKey{}).(*callRecords)
	if !ok || !c.hasOpen {
		// Nothing to mark yet: the next write asks again.
		return false, nil
	}
	if c.open.Sent {
		return true, nil
	}
	if err := intent.MarkSent(c.open); err != nil {
		return false, err
	}
	c.open.Sent = true
	return true, nil
}

// callSent tells whether the call of ctx sent a write (httpretry.Writes).
func callSent(ctx context.Context) bool {
	n, _ := httpretry.Writes(ctx)
	return n > 0
}

type callRecordsKey struct{}

// budgeted stamps a call with its start (withCallStart) and notes in its
// answer what earlier calls left.
func (r *Registry) budgeted(name string, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		own := &callRecords{}
		text, isErr := next(context.WithValue(r.withCallStart(ctx), callRecordsKey{}, own), args)
		if callerOf(ctx) == nil {
			text = r.notePending(text, own.ids, callOn(name, args))
		}
		return text, isErr
	}
}

// New builds the registry. factory is called by handlers that need an
// engine (credentials, project, region).
func New(factory EngineFactory) *Registry {
	r := &Registry{newEngine: factory}
	r.registerTools()
	for i := range r.tools {
		t := &r.tools[i]
		t.Decl = decls[t.Name]
		t.Group = groupOf(t.Name)
		t.Mutating = isMutating(*t)
		if takesConsent(*t) && t.Decl.PartialConsent == nil {
			t.Description += " " + consentText(t.Name)
		}
		if t.UntrustedOutput = untrustedFields[t.Name].workload; t.UntrustedOutput {
			t.Description += " " + untrustedOutputNote
		}
		t.CLI = cliSpecs[t.Name]
		ru := toolsRU[t.Name]
		t.ShortRU, t.DescriptionRU = ru.Short, ru.Long
		t.ArgDescriptionsRU = argDescriptionsRU(*t, ru.Args)
		spec, isList := listSpecs[t.Name]
		spec.declare(t)
		noEmptyValue(t, scopeArgs...)
		noEmptyValue(t, spec.own...)
		if h := r.handlers[t.Name]; h != nil {
			// Innermost: every policy of the call stands over the list
			// the selection is made from.
			if isList && spec.args() != nil {
				h = r.filtered(spec, h)
			}
			// Inside requireProject: the check's engine sees its mark.
			h = r.boundToProjects(*t, h)
			// Inside requireProject too: the list of a name is the call's
			// project's; outside boundToProjects: it checks the id.
			if refs := refArgs[t.Name]; refs != nil {
				h = r.resolveRefs(refs, h)
			}
			if t.Decl.Project {
				h = r.requireProject(h)
			}
			if _, ok := createdOf(t.Name); ok || targetArgs(t.Name) != nil {
				h = r.namedProject(*t, h)
			}
			if keys := echoedArgs[t.Name]; keys != nil {
				h = namedArgs(keys, h)
			}
			if u := untrustedFields[t.Name]; u.marked() {
				h = markUntrusted(u, h)
			}
			if r.unchecked == nil {
				r.unchecked = map[string]Handler{}
			}
			r.unchecked[t.Name] = checkedCreateName(t.Name, r.intended(*t, h))
			r.handlers[t.Name] = r.budgeted(t.Name, outer(t.Name, t.InputSchema, r.unchecked[t.Name]))
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

// Wrap replaces the handler of the named tool with wrap(current handler),
// under the check of its arguments: a call with arguments the tool does
// not take gets that USAGE first, never a refusal of a server mode
// (--read-only, the HTTP restrictions) that reads as if the call were
// otherwise right (Grok on rc.1, G13-1). Every caller sees the new
// handler: transports, Tools/Lookup, and the in-process calls the stack
// orchestrator makes. It reports whether the tool exists. Transport
// policies (MCP over HTTP) use it; the CLI builds its own registry and is
// not affected.
func (r *Registry) Wrap(name string, wrap func(next Handler) Handler) bool {
	next, ok := r.unchecked[name]
	if !ok {
		return false
	}
	t, _ := r.Lookup(name)
	r.unchecked[name] = wrap(next)
	h := r.budgeted(name, outer(name, t.InputSchema, r.unchecked[name]))
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
// agreement (billingClient), and the balance of the agreement is not given
// (balanceRefused); org_projects lists that project, not the
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

// ErrResult formats an error as a JSON tool result, as a handler's answer
// reads to its caller (outer: without the status of the record).
func ErrResult(err error) string { return withoutAPIStatus(errResult(err)) }

// BoolArg reads a boolean argument, def when it is absent.
func BoolArg(args map[string]any, key string, def bool) bool { return boolArg(args, key, def) }

// Groups returns the group names in alphabetical order.
func Groups() []string {
	seen := map[string]bool{}
	for _, d := range decls {
		if d.Group != "" {
			seen[d.Group] = true
		}
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

// groupPrefixes assigns the rest by name prefix.
var groupPrefixes = []struct{ prefix, group string }{
	{"vm_", "vm"},
	{"image_", "image"},
	{"disk_", "disk"},
	{"backup_", "backup"},
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
	if g := decls[name].Group; g != "" {
		return g
	}
	for _, p := range groupPrefixes {
		if strings.HasPrefix(name, p.prefix) {
			return p.group
		}
	}
	return ""
}

// needsProject marks the context of a call of a Decl.Project tool that names no
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
// Decl.Project call with no project configured it is CONFIG_INVALID
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

// IsSensitive reports whether a tool reveals a secret and asks for consent
// although it changes nothing (Decl.Sensitive).
func IsSensitive(t Tool) bool { return t.Decl.Sensitive }

// AlwaysListed reports whether a server serves t under any --toolsets.
func AlwaysListed(t Tool) bool { return t.Decl.AlwaysListed != "" }

// SetServed tells the registry which tools the server serves (nil: all):
// the cards of agent_skill mark the rest.
func (r *Registry) SetServed(served func(name string) bool) { r.served = served }

// SetStateDir declares the directory this registry works in: the one its
// engines are built with (engine.StateDir). Every registry built with an
// engine factory declares it — the records of the calls (intent) are
// written and read there and nowhere else, whatever directory the process
// stands in and whether an engine can be built. dir comes from the build
// of evo, never from an argument of a tool.
func (r *Registry) SetStateDir(dir string) { r.stateDirOf = dir }

// stateDir is the directory declared by SetStateDir; false when none is.
func (r *Registry) stateDir() (string, bool) { return r.stateDirOf, r.stateDirOf != "" }

// stateDirUndeclared reports the defect of a build: a registry that can
// reach the cloud and was told no directory for the records of its calls.
func (r *Registry) stateDirUndeclared() bool { return r.newEngine != nil && r.stateDirOf == "" }

// errNoStateDir refuses a call of a registry of stateDirUndeclared. The
// reason is the one evo has for «it cannot work as it was set up»; the
// words say whose defect it is — nothing the person sets repairs it.
func errNoStateDir() error {
	return clierrors.New(clierrors.CodeConfigInvalid,
		i18n.T("evo was built without the directory of its call records: this is a defect of evo, not of your configuration",
			"evo собран без каталога записей вызовов: это дефект evo, а не вашей конфигурации"),
		i18n.T("Nothing was sent to the cloud. Report it: github.com/shatim-ops/evo-cli/issues.",
			"В облако ничего не отправлено. Сообщите об этом: github.com/shatim-ops/evo-cli/issues."))
}

// HasSideEffects reports whether a tool that takes no consent is not a
// read (Decl.SideEffects).
func HasSideEffects(t Tool) bool { return t.Decl.SideEffects != "" }

// IsDestructive reports whether a mutating tool may delete or overwrite
// data; the other mutating tools only add or change in place.
func IsDestructive(t Tool) bool {
	return t.Mutating && (strings.HasSuffix(t.Name, "_delete") || t.Decl.Destructive)
}

// isMutating reports whether the tool changes state in the cloud.
func isMutating(t Tool) bool {
	return takesConsent(t) && !t.Decl.Sensitive
}

// takesConsent reports whether some call of the tool needs the person's
// consent: its schema has confirmed. Those are the mutating tools and
// Decl.Sensitive tools; the schema is where the agent learns it, so it is where
// the registry reads it too.
func takesConsent(t Tool) bool {
	props, _ := t.InputSchema["properties"].(map[string]any)
	_, gated := props["confirmed"]
	return gated
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
	if t.Decl.PartialConsent != nil {
		return t.Decl.PartialConsent(args)
	}
	return true
}

// AlwaysNeedsConsent reports whether every call of t needs confirmed=true
// (NeedsConsent whatever the arguments).
func AlwaysNeedsConsent(t Tool) bool {
	return takesConsent(t) && t.Decl.PartialConsent == nil
}
