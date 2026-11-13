// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// beginIntent writes the intent of a consented call before it goes to the
// cloud, at the one gate of consent (D35): a process cut between the
// consent and the cloud's answer leaves it, and the next run names it
// (release plan 1.8, (и); «Риск», conclusion 46). Every consented call
// writes one — a create, a delete, a change: each can leave the cloud in a
// state no record names. end takes the answer: an answer with an outcome
// removes the intent and settles the earlier ones of the same tool and
// name; a cut call (its context done, INTERRUPTED) keeps it. A record of
// the same call another key left is not settled by this one
// (intent.Settle), and the answer names it (namedKept). An intent that
// cannot be written refuses the call before it goes out.
func (r *Registry) beginIntent(ctx context.Context, name string, args map[string]any) (end func(answer string), err error) {
	if r.newEngine == nil {
		return func(string) {}, nil
	}
	// The directory is the registry's, declared at its build: a record
	// that cannot be placed refuses the call before it goes out.
	root, ok := r.stateDir()
	if !ok {
		return nil, errNoStateDir()
	}
	in := intent.Intent{Tool: name, Surface: "mcp", PID: os.Getpid(), StartedAt: clock.Now(), Canon: r.canonArgs(name, args)}
	// The project and the key are those the call goes with: the engine of
	// its own arguments — a server bound to projects (--allow-project) has
	// none without them, and the call with project_id goes out all the
	// same. A call no engine is built for is recorded without them: its
	// handler may yet reach the cloud, and no repeat settles a record
	// whose key is not known (the interim delta audit of 1.9, FB19I-32;
	// «Риск», conclusion 177, У9).
	eng, noEngine := r.engineOfCall(args)
	if eng != nil {
		in.Zone, in.Key = eng.Config.Region, eng.Maker()
	}
	// The surface as the journal (D46) has it: a call of the CLI carries
	// its Caller, a call over MCP none.
	if callerOf(ctx) != nil {
		in.Surface = "cli"
	}
	for k := range args {
		if k != "confirmed" {
			in.Args = append(in.Args, k)
		}
	}
	// Named as the tool resolves the resource; the read that failed
	// refuses the call before it goes out (intended), so here it has not.
	if in.Name, in.Target, err = r.resourceOf(ctx, name, args); err != nil {
		return nil, err
	}
	in.ProjectID = projectOfCall(eng, in, args)
	if z, _ := args["zone"].(string); z != "" {
		in.Zone = z
	}
	dir := intent.Dir(root)
	rec, err := intent.Open(dir, in)
	if err != nil {
		// The call would be refused for its own reason — no key, a
		// project the server does not allow: that is what the caller
		// is told, not the directory of a record it would not need.
		if noEngine != nil {
			return nil, noEngine
		}
		return nil, err
	}
	if own, ok := ctx.Value(callRecordsKey{}).(*callRecords); ok {
		own.ids = append(own.ids, rec.ID)
	}
	forgetPending(ctx)
	return func(answer string) {
		// Whatever the end does to the record, the call reads the
		// directory anew after it.
		defer forgetPending(ctx)
		if ctx.Err() != nil || intent.OutcomeUnknown(answerReason(answer), answerHTTPStatus(answer)) {
			// No write went out (changeRule.viaHTTP): the call changed
			// nothing, whatever cut it.
			if n, counted := httpretry.Writes(ctx); counted && n == 0 && everySendIsAChange[name].viaHTTP {
				intent.Close(rec)
				return
			}
			// The call returned, the record stays: a later call of the
			// same may settle it (intent.Settle).
			intent.End(rec)
			return
		}
		// The call ended and the operation goes on: its answer names the
		// same tool as the next step. The record stays, named as that
		// step names the resource, until a call of the step ends the
		// operation (settleHeld) — and its StartedAt is the start the
		// limit of the repeats counts from.
		if next, goesOn := sameToolNext(name, answer); goesOn {
			// Named as a call of the step would be (the same rule of the
			// project of a call addressed by id).
			rec.Name, rec.Target = intentResource(name, next)
			if _, _, byID := intentTarget(rec); byID && stringArg(next, "project_id") == "" {
				rec.ProjectID = ""
			}
			intent.Hold(rec)
			rec.Held = true
			intent.Settle(dir, rec)
			return
		}
		intent.Close(rec)
		intent.Settle(dir, rec)
	}, nil
}

// engineOfCall is the engine a call with args goes with: that of its own
// project_id, when it names one. nil with the error when none is built.
func (r *Registry) engineOfCall(args map[string]any) (*engine.Engine, error) {
	var over map[string]any
	if p := stringArg(args, "project_id"); p != "" {
		over = map[string]any{"ProjectID": p}
	}
	eng, err := r.newEngine(over)
	if err != nil {
		return nil, err
	}
	return eng, nil
}

// projectOfCall is the project the record of a call names: the one its
// engine goes with, or the project_id it gives. The write of a call
// addressed by id lands in the project of the resource, which the call's
// project does not tell (namedProject): its record names no project rather
// than the profile's (R-82). The one rule for the record of a call and for
// the search of the cut calls of a repeat: a resource of the same name in
// another project is another resource.
func projectOfCall(eng *engine.Engine, in intent.Intent, args map[string]any) string {
	project := ""
	if eng != nil {
		project = eng.Config.ProjectID
	}
	if _, _, byID := intentTarget(in); byID {
		project = ""
	}
	if p := stringArg(args, "project_id"); p != "" {
		project = p
	}
	return project
}

// waitArgs are the arguments that say how long the call waits, not what
// it does: two calls that differ in them are the same call.
var waitArgs = map[string]bool{"wait": true, "wait_seconds": true}

// canonArgs is the canonical form of the arguments that make a call of
// tool name what it is, for the print of its record (intent.Intent.Print):
// its arguments as JSON with sorted keys, without the consent and the wait
// (waitArgs) and without the arguments that hold a secret
// (CLISpec.SecretFiles) — a secret does not enter the print at all, so two
// calls that differ only in it are one call, as they were before the
// print («Риск», conclusion 177, У4).
func (r *Registry) canonArgs(name string, args map[string]any) []byte {
	t, _ := r.Lookup(name)
	sig := make(map[string]any, len(args))
	for k, v := range args {
		if k != "confirmed" && !waitArgs[k] && !t.CLI.SecretFiles[k] {
			sig[k] = v
		}
	}
	canon, err := json.Marshal(sig)
	if err != nil {
		return []byte{}
	}
	return canon
}

// changeRule is what a tool every send of which is a change declares to
// the layer of intents.
type changeRule struct {
	// read is the tool that shows the resource after a call whose outcome
	// is not known.
	read string
	// target resolves the resource a call names to the one way the layer
	// tells it by — the record's name and target («argument=id») — when
	// the tool takes it spelled more than one way (a VM by its id or its
	// name): a cut call is found however the repeat spells the resource
	// («Риск», conclusion 177, У11). nil: the call names it one way
	// (intentResource). It reads the cloud as the plan of the tool does,
	// through the same resolver, so the call reads once.
	target func(ctx context.Context, r *Registry, args map[string]any) (name, target string, err error)
	// made: the tool makes a resource its list shows by name (createdIDs).
	// A cut call whose resource is listed has an outcome that can be read:
	// its record closes and the repeat is answered as any call on a name
	// that is taken — the question is asked only while the list does not
	// show it («Риск», conclusion 179, У1).
	made bool
	// viaHTTP: every write of the tool goes out through evo's HTTP
	// transport (httpretry), which counts them for the call. A call that
	// ended with no outcome and sent no write changed nothing: its record
	// closes, and no repeat is asked about a request that never went
	// («Риск», conclusion 179, У2).
	viaHTTP bool
}

// paidCreate is the rule of a create of a resource billed from its first
// minute at the price of a cluster: the repeat of a call whose answer was
// lost would make a second one while the list does not show the first yet
// (release plan 1.9, «Повтор платного создания после потерянного ответа
// спрашивает заново»; D107). The resource is told by its name — and by
// the parent it is made in, parent, when it has one — whatever the other
// arguments of the repeat are.
func paidCreate(tool, parent string, name func(args map[string]any) string) changeRule {
	return changeRule{read: createdIDs[tool].list, made: true, viaHTTP: true,
		target: func(_ context.Context, _ *Registry, args map[string]any) (string, string, error) {
			target := ""
			if id := stringArg(args, parent); parent != "" && id != "" {
				target = parent + "=" + id
			}
			return name(args), target, nil
		}}
}

// nameArg is the name of what a create makes when the call must give it.
func nameArg(args map[string]any) string { return stringArg(args, "name") }

// everySendIsAChange are the tools with no repeat that is a success: every
// call that reaches the cloud is one more change (a reboot). A tool
// declares itself here; the layer names no tool. For such a tool the
// record of a cut call is not settled by «the same call again» — that
// call would be a second change — so a consented call that finds one asks
// again (askAgainAfterACut), and the record's words send to the read, not
// to a repeat (release plan 1.9, «Слепой повтор действия, где каждый
// вызов — новое изменение»; «Риск», conclusion 138). The list is of new
// tools: a shipped tool does not join it without a decision — its
// consented call would start to answer a question.
var everySendIsAChange = map[string]changeRule{
	// A reboot has no «already»: the VM runs before it and after it
	// (docs/internal/briefs/ops-wave-1.md §5).
	"vm_reboot": {read: "vm_get", target: vmTarget("vm_reboot")},
	// A backup by the same name is found before the question, but a lost
	// answer may leave it not yet listed: a repeat would make a second,
	// billed with no term («Риск», conclusion 164, У5; the audit of #1544,
	// Б2).
	"backup_create": {read: "backup_list"},
	// A restore makes a new disk each time it is sent: the disk of a cut
	// one may come and be billed though the list does not show it yet
	// («Риск», conclusion 164, У5; the probe of #1580).
	"backup_restore": {read: "disk_list"},
	// The creates billed at the price of a cluster (D107).
	// The name is the one the plan of the tool makes the resource by:
	// the same function, where the tool has a default of its own.
	"pg_create":        paidCreate("pg_create", "", nameArg),
	"kafka_create":     paidCreate("kafka_create", "", nameArg),
	"redis_create":     paidCreate("redis_create", "", nameArg),
	"k8s_create":       paidCreate("k8s_create", "", k8sClusterNameArg),
	"k8s_add_nodepool": paidCreate("k8s_add_nodepool", "cluster_id", nodePoolName),
	"lb_create":        paidCreate("lb_create", "", nameArg),
}

// spelledArgs are the arguments that name the resource of the record in
// as its call did: its target («argument=id») and its name, by the
// argument the tool names a resource by (intentResource).
func spelledArgs(in intent.Intent) map[string]any {
	args := map[string]any{}
	if arg, id, ok := strings.Cut(in.Target, "="); ok && id != "" {
		args[arg] = id
	}
	if in.Name != "" {
		arg := "name"
		if c, ok := createdIDs[in.Tool]; ok && c.arg != "" {
			arg = c.arg
		}
		args[arg] = in.Name
	}
	return args
}

// named is resourceOf for a layer that reads the answer of a call already
// made: a resolver that fails leaves the resource as the call spelled it.
func (r *Registry) named(ctx context.Context, name string, args map[string]any) (resource, target string) {
	if resource, target, err := r.resourceOf(ctx, name, args); err == nil {
		return resource, target
	}
	return intentResource(name, args)
}

// resourceOf is what the layer tells the resource of a call of tool name
// by: as the tool resolves it (changeRule.target), else as the call names
// it (intentResource). An error is the read of the resolver that failed.
func (r *Registry) resourceOf(ctx context.Context, name string, args map[string]any) (resource, target string, err error) {
	if rule := everySendIsAChange[name]; rule.target != nil {
		return rule.target(ctx, r, args)
	}
	resource, target = intentResource(name, args)
	return resource, target, nil
}

// cutCalls are the records of this caller's own cut calls of the tool name
// on the resource the call names, for a tool of everySendIsAChange: the
// same tool, the same resource, the same key, not held. A record of
// another key is not this caller's cut call, and a held one is an
// operation going on.
func (r *Registry) cutCalls(ctx context.Context, name string, args map[string]any) []intent.Intent {
	if everySendIsAChange[name].read == "" {
		return nil
	}
	// A resource that cannot be resolved names no cut call here: the call
	// with consent was refused for that before (intended), and a call
	// without consent gets the same refusal from the plan of the tool,
	// which reads through the same resolver.
	resource, target, err := r.resourceOf(ctx, name, args)
	root, ok := r.stateDir()
	if err != nil || !ok || (resource == "" && target == "") {
		return nil
	}
	eng, _ := r.engineOfCall(args)
	project := projectOfCall(eng, intent.Intent{Tool: name, Name: resource, Target: target}, args)
	var cut []intent.Intent
	for _, in := range pendingIn(ctx, root) {
		if in.Held || in.Tool != name || in.Key == nil || in.ProjectID != project {
			continue
		}
		if in.Name != resource || in.Target != target {
			// A record written before the target was resolved names the
			// resource as its call spelled it: it is resolved the same
			// way. One that cannot be resolved now is not this call's.
			was, wasTarget, err := r.resourceOf(ctx, name, spelledArgs(in))
			if err != nil || was != resource || wasTarget != target {
				continue
			}
		}
		cut = append(cut, in)
	}
	if len(cut) == 0 {
		return nil
	}
	// The key the call goes with — that of its own project_id on a server
	// bound to projects, which has no engine without one.
	var cur *state.Maker
	if eng != nil {
		cur = eng.Maker()
	}
	var mine []intent.Intent
	for _, in := range cut {
		if cur == nil || in.Key.Other(cur) {
			continue
		}
		// What the cut call made is listed: its outcome is read, the
		// record closes, and nobody is asked about it.
		if everySendIsAChange[name].made {
			if _, listed := r.checkByList(ctx, in, in.ProjectID); listed {
				intent.Close(in)
				forgetPending(ctx)
				continue
			}
		}
		mine = append(mine, in)
	}
	return mine
}

// cutCallWarning is what the question says of the cut calls mine of tool
// name, and the field that names them: the last one, how many there were —
// each may have taken effect — and the read that shows the resource
// (read_tool: a command of the CLI names its own command by it). It sends to the person, not to
// «confirmed again»: the consent given was to one change («Риск»,
// conclusion 145, У1).
func cutCallWarning(name string, mine []intent.Intent) (message string, interrupted map[string]any) {
	last := mine[len(mine)-1]
	what := firstNonEmptyStr(last.Name, last.Target)
	rule := everySendIsAChange[name]
	interrupted = map[string]any{"tool": name, "read_tool": rule.read, "started_at": last.StartedAt.UTC().Format(time.RFC3339), "calls": len(mine)}
	if rule.made {
		// A create: what is at stake is a second billed resource, and
		// the list is where the first one shows — when it shows, which
		// evo does not say of the platform (a hypothesis, no run).
		return i18n.Tf("An earlier %s of %s, started %s, got no answer from the cloud: the request may have been accepted, and what it made may not be listed yet — %s does not show it now. Nothing was sent. A yes now makes a second billed resource of the name if the first request was taken: check %s again first, ask the person whether to send one more, and with their yes call %s with confirmed=true.",
			"Предыдущий вызов %s для %s, начатый %s, не получил ответа облака: запрос мог быть принят, а созданное может ещё не быть в списке — %s сейчас его не показывает. Ничего не отправлено. «Да» сейчас создаст второй платный ресурс с этим именем, если первый запрос был принят: сначала проверьте %s ещё раз, спросите человека, отправлять ли ещё один, и с его «да» вызовите %s с confirmed=true.",
			name, quoteCloud(what), last.StartedAt.Local().Format("02.01.2006 15:04"), rule.read, rule.read, name), interrupted
	}
	return i18n.Tf("An earlier %s of %s, started %s, was cut before the cloud answered: its outcome is unknown, and it may have taken effect. A call now is one more change, not a repeat of that one, so nothing was sent. %s shows the resource now; ask the person whether to send one more, and with their yes call %s with confirmed=true.",
			"Предыдущий вызов %s для %s, начатый %s, оборван до ответа облака: исход неизвестен, изменение могло произойти. Вызов сейчас — ещё одно изменение, а не повтор того, поэтому ничего не отправлено. Ресурс сейчас показывает %s; спросите человека, отправлять ли ещё одно, и с его «да» вызовите %s с confirmed=true.",
			name, quoteCloud(what), last.StartedAt.Local().Format("02.01.2006 15:04"), rule.read, name),
		interrupted
}

// askAgainAfterACut answers a consented call of a tool of
// everySendIsAChange that finds the record of its own cut call: the call
// is not sent. A client that timed out and sends the same call again,
// with the consent of the first one, would make a second change the
// person consented to once. The answer is the question again, saying what
// is known; the record is closed (closeAsked), so the next consented call
// is sent as any other.
func (r *Registry) askAgainAfterACut(ctx context.Context, name string, args map[string]any) (string, bool) {
	mine := r.cutCalls(ctx, name, args)
	if len(mine) == 0 {
		return "", false
	}
	r.closeAsked(ctx, mine)
	message, interrupted := cutCallWarning(name, mine)
	return toJSON(map[string]any{"status": "confirmation_required", "interrupted": interrupted, "message": message}), true
}

// closeAsked closes the records mine of the cut calls a question has just
// named — when the question is the answer of the call: over MCP it goes to
// the model, which is told to ask the person. A command of the CLI makes
// the call without consent itself and then answers the question — with
// --yes, by a script, by nobody under --dry-run — so its question closes
// nothing: the command says when a person answered it (CutCallAnswered).
// Until then every run finds the record and asks again (the interim delta
// audit of 1.9, high; «Риск», conclusion 176, У1 and У2).
func (r *Registry) closeAsked(ctx context.Context, mine []intent.Intent) {
	if callerOf(ctx) != nil {
		return
	}
	for _, in := range mine {
		intent.Close(in)
	}
	forgetPending(ctx)
}

// CutCallAnswered closes the records of the caller's own cut calls of the
// tool name on the resource args name: a person read the question that
// named them (the answer's "interrupted") and answered it at the terminal,
// yes or no. The call with consent that follows a yes is sent as any
// other; after a no nothing is sent, and the cut call is asked about no
// more.
func (r *Registry) CutCallAnswered(ctx context.Context, name string, args map[string]any) {
	for _, in := range r.cutCalls(ctx, name, args) {
		intent.Close(in)
	}
	forgetPending(ctx)
}

// questionNamesTheCut is the question of a call without consent, with the
// caller's own cut call named in it: one question that says everything —
// the preview and the cut call — and the record closed (closeAsked), so
// the consented call that follows is sent as any other, not asked a second
// time («Риск», conclusion 145, У2). An answer that is not a question (a
// refusal, an answer with nothing to consent to) stays as it is, and so
// does the record.
func (r *Registry) questionNamesTheCut(ctx context.Context, name string, args map[string]any, answer string) string {
	if answerStatus(answer) != "confirmation_required" {
		return answer
	}
	mine := r.cutCalls(ctx, name, args)
	if len(mine) == 0 {
		return answer
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return answer
	}
	message, interrupted := cutCallWarning(name, mine)
	if was, _ := m["message"].(string); was != "" {
		message += " " + was
	}
	m["message"], m["interrupted"] = message, interrupted
	r.closeAsked(ctx, mine)
	return toJSON(m)
}

// sameToolNext reads the next step of answer when it is a call of tool
// itself (next_tool, next_args): the operation goes on, and that call
// goes on with it.
func sameToolNext(tool, answer string) (args map[string]any, ok bool) {
	var a struct {
		NextTool string         `json:"next_tool"`
		NextArgs map[string]any `json:"next_args"`
	}
	if json.Unmarshal([]byte(answer), &a) != nil || a.NextTool != tool {
		return nil, false
	}
	return a.NextArgs, true
}

// settleHeld closes the held records of this call of tool name once its
// answer is an outcome with no further step of the same tool: evo itself
// goes on with the operation no more — it ended, or the call handed the
// wait to a read. The step of a wait carries no consent (nextSameCall), so
// it writes no intent of its own, and nothing else would close the record
// of the call that started the operation. A question is not an outcome,
// and a record another key left is not this call's to close
// (intent.Settle). The record is of this call when the call names its
// resource as the record does — or when its answer does: a held record is
// named by id (its step's), and a call that names the VM by its name
// answers with that id («Риск», conclusion 137, У1).
func (r *Registry) settleHeld(ctx context.Context, name string, args map[string]any, answer string) {
	resource, target := r.named(ctx, name, args)
	root, ok := r.stateDir()
	if !ok {
		return
	}
	var held []intent.Intent
	for _, in := range pendingIn(ctx, root) {
		if !in.Held || in.Tool != name {
			continue
		}
		// The answer names the record's target for a call that named
		// the resource another way; a record that names what the
		// operation makes (a restore: the copy and the new disk) is of
		// the call that names the same — another disk of the same copy
		// is another operation («Риск», conclusion 176, У4).
		if (in.Name == resource && in.Target == target && (resource != "" || target != "")) ||
			(in.Name == "" && answerNames(answer, in.Target)) {
			held = append(held, in)
		}
	}
	if len(held) == 0 || intent.OutcomeUnknown(answerReason(answer), answerHTTPStatus(answer)) || answerStatus(answer) == "confirmation_required" ||
		refusedTheCall[answerReason(answer)] {
		return
	}
	if _, goesOn := sameToolNext(name, answer); goesOn {
		return
	}
	cur := r.maker()
	for _, in := range held {
		if !in.Key.Other(cur) {
			intent.Close(in)
			forgetPending(ctx)
		}
	}
}

// goesOn tells a call of tool name on the resource args name whose
// operation this caller began and evo still goes on with: its held record,
// named as the call names the resource.
func (r *Registry) goesOn(ctx context.Context, name string, args map[string]any) bool {
	resource, target, err := r.resourceOf(ctx, name, args)
	root, ok := r.stateDir()
	if err != nil || !ok || (resource == "" && target == "") {
		return false
	}
	for _, in := range pendingIn(ctx, root) {
		if in.Held && in.Tool == name && in.Name == resource && in.Target == target && !in.Key.Other(r.maker()) {
			return true
		}
	}
	return false
}

// heldSince is the start of the operation of tool on target («argument=id»)
// that a call with this key began and evo still goes on with — its held
// record; ok false when there is none: the operation is somebody else's, or
// another key's, or evo stopped waiting for it. The limit of the repeats
// of the operation counts from there, not from each repeat.
func (r *Registry) heldSince(eng *engine.Engine, tool, target string) (since time.Time, ok bool) {
	root, ok := r.stateDir()
	if eng == nil || !ok {
		return time.Time{}, false
	}
	cur := eng.Maker()
	for _, in := range intent.Pending(intent.Dir(root)) {
		if in.Held && in.Tool == tool && in.Target == target && !in.Key.Other(cur) {
			return in.StartedAt, true
		}
	}
	return time.Time{}, false
}

// refusedTheCall are the reasons of a refusal that speaks of the call, not
// of the operation it asked about: the request was throttled, or the key
// or the setup did not let it through. The operation may go on all the
// same, so a held record stays. (For the call that sends the write they
// are an outcome — nothing was sent: intent.OutcomeUnknown.)
var refusedTheCall = map[string]bool{
	clierrors.CodeAPIRateLimited: true, clierrors.CodeAuthRequired: true, clierrors.CodeAuthExpired: true,
	clierrors.CodeAuthFailed: true, clierrors.CodeConfigInvalid: true,
}

// answerNames tells an answer that names the resource of target
// («argument=id») in the field of that argument: vm_id of a VM's answer.
func answerNames(answer, target string) bool {
	arg, id, ok := strings.Cut(target, "=")
	if !ok || id == "" {
		return false
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return false
	}
	got, _ := m[arg].(string)
	return got == id
}

// answerStatus is the status of a tool's answer, "" when it has none.
func answerStatus(answer string) string {
	var a struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal([]byte(answer), &a)
	return a.Status
}

// namedKept is the answer of a call of tool name with the records of the
// same call on the same resource that another key left
// (pending_operations): this call does not settle them, with consent or
// without — an already_deleted of this key says what this key sees — so
// every answer of it names them, not only the first answer of the process
// («Риск», conclusions 128 and 135).
func (r *Registry) namedKept(ctx context.Context, name string, args map[string]any, answer string) string {
	resource, target := r.named(ctx, name, args)
	root, ok := r.stateDir()
	if !ok || (resource == "" && target == "") {
		return answer
	}
	// The records of this directory, as withPending reads them; the key
	// of the call is asked for only when one of them is of this call — a
	// call with nothing left builds no engine.
	var same []intent.Intent
	for _, in := range pendingIn(ctx, root) {
		if in.Tool == name && in.Name == resource && in.Target == target && in.Key != nil {
			same = append(same, in)
		}
	}
	if len(same) == 0 {
		return answer
	}
	cur := r.maker()
	var notes []string
	for _, in := range same {
		if in.Key.Other(cur) {
			notes = append(notes, r.pendingNote(in, cur))
		}
	}
	if named, ok := withNotes(answer, notes); ok {
		return named
	}
	return answer
}

// intentResource is what a call names its resource by: the argument the create
// declares (createdIDs: name, bucket, path…), else name; and, for a call on
// an existing resource, its first declared id argument (idArgs) as
// «argument=id» («Риск», conclusion 46, condition 2).
func intentResource(tool string, args map[string]any) (name, target string) {
	arg := "name"
	if c, ok := createdIDs[tool]; ok && c.arg != "" {
		arg = c.arg
	}
	name, _ = args[arg].(string)
	for _, a := range idArgs[tool] {
		if a.why == whyProjectArg {
			continue
		}
		if v, _ := args[a.name].(string); v != "" {
			return name, a.name + "=" + v
		}
	}
	return name, ""
}

// intentTarget is the resource in addresses by id when its write lands
// there: the declared target of its tool (idArg.target) that the record
// names (Target, «argument=id»).
func intentTarget(in intent.Intent) (a idArg, id string, ok bool) {
	arg, id, found := strings.Cut(in.Target, "=")
	if !found || id == "" {
		return idArg{}, "", false
	}
	for _, a := range targetArgs(in.Tool) {
		if a.name == arg {
			return a, id, true
		}
	}
	return idArg{}, "", false
}

// apiStatusField carries the status of the API's answer from errAnswer to
// the record of the call, inside the handler; outer takes it out of the
// answer (withoutAPIStatus).
const apiStatusField = "_api_status"

// answerHTTPStatus is the status of the API's answer a tool's error answer
// carries (apiStatusField), 0 when none: a refusal of the platform (4xx)
// is an outcome known, though its reason is API_ERROR as a 5xx's is.
func answerHTTPStatus(answer string) int {
	var a struct {
		Status float64 `json:"_api_status"`
	}
	if json.Unmarshal([]byte(answer), &a) != nil {
		return 0
	}
	return int(a.Status)
}

// withoutAPIStatus is answer without apiStatusField: the status is the
// record's, not the caller's — the answer stays the same whichever 4xx
// the platform gave (CMP-7).
func withoutAPIStatus(answer string) string {
	if !strings.Contains(answer, `"`+apiStatusField+`"`) {
		return answer
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return answer
	}
	delete(m, apiStatusField)
	return toJSON(m)
}

// answerReason is the reason of a tool's answer, "" when it has none.
func answerReason(answer string) string {
	// "error" is a string in a tool's answer and an object in the CLI's
	// --json one: one struct of both failed to decode a tool's error, and
	// every such answer read as one with no reason — an outcome — so the
	// record of a call whose answer was lost was closed (the audit of
	// #1544, Б2).
	var a map[string]any
	if json.Unmarshal([]byte(answer), &a) != nil {
		return ""
	}
	if r, _ := a["reason"].(string); r != "" {
		return r
	}
	if e, _ := a["error"].(map[string]any); e != nil {
		r, _ := e["reason"].(string)
		return r
	}
	return ""
}

// PendingNotes are the intents root's earlier runs left, in words: what
// was sent, where, when, and where to look — no advice to delete: the
// resource may be the person's now (R-56). The lines are the same in
// doctor, in the first answer of a tool and in the CLI's stderr.
func (r *Registry) PendingNotes(root string) []string {
	notes, _ := r.pendingNotes(root, nil)
	return notes
}

// pendingNotes are PendingNotes without the records of the ids own, and
// whether any was left out.
func (r *Registry) pendingNotes(root string, own []string) (notes []string, leftOut bool) {
	for _, in := range pendingIntents(root) {
		if slices.Contains(own, in.ID) {
			leftOut = true
			continue
		}
		notes = append(notes, r.pendingNote(in, r.maker()))
	}
	return notes, leftOut
}

// pendingIntents are the intents a run in root reads: the folder's and the
// configuration directory's (evo login's key, № 37), oldest first in each.
func pendingIntents(root string) []intent.Intent {
	var out []intent.Intent
	for _, dir := range intent.Dirs(root) {
		out = append(out, intent.Pending(dir)...)
	}
	return out
}

// pendingNote is one intent in words. A record is a file of the project
// directory, which comes with git clone as well as evo wrote it: each of
// its strings goes into evo's sentence on one line and in quotes, as a
// name from the cloud does (quoteCloud; FB180-2, R-67).
func (r *Registry) pendingNote(in intent.Intent, cur *state.Maker) string {
	started := in.StartedAt.Local().Format("02.01.2006 15:04")
	if in.Note != "" {
		// Its own words; the record stays until the person removes it:
		// no list of a tool tells its outcome.
		return i18n.Tf("%s, started %s (%s, pid %d); once checked, delete the record %s",
			"%s, начат %s (%s, pid %d); после проверки удалите запись %s",
			quoteCloud(in.Note), started, quoteCloud(in.Surface), in.PID, filepath.Join(in.Dir, in.ID+".json"))
	}
	where := ""
	if in.ProjectID != "" {
		where = i18n.Tf(" in project %s", " в проекте %s", quoteCloud(in.ProjectID))
	}
	if in.Zone != "" {
		where += i18n.Tf(", zone %s", ", зона %s", quoteCloud(in.Zone))
	}
	// A call on an existing resource is named by its id (Target).
	name := in.Name
	if name == "" {
		name = in.Target
	}
	if name == "" {
		name = i18n.T("(no name)", "(без имени)")
	} else {
		name = quoteCloud(name)
	}
	tool := quoteCloud(in.Tool)
	head := i18n.Tf("%s %s%s, started %s (%s, pid %d): the cloud did not answer, the outcome is unknown",
		"%s %s%s, начат %s (%s, pid %d): облако не ответило, исход неизвестен",
		tool, name, where, started, quoteCloud(in.Surface), in.PID)
	// An operation that goes on: the call that started it ended at its
	// time limit, and the step its answer named goes on with it.
	if in.Held && !in.Key.Other(cur) {
		return i18n.Tf("%s %s%s, started %s (%s, pid %d): the call ended at its time limit and the operation was still going — the same %s on it goes on; the record stays while evo goes on with the operation, and closes when a call of %s ends it or stops waiting for it",
			"%s %s%s, начат %s (%s, pid %d): вызов закончился на пределе времени, операция ещё шла — тот же %s на нём продолжит; запись остаётся, пока evo сам продолжает операцию, и закрывается, когда вызов %s её завершит или перестанет её ждать",
			tool, name, where, started, quoteCloud(in.Surface), in.PID, tool, tool)
	}
	// A record another key left: a call with this key does not settle it
	// (intent.Settle), and this key may not see what that one changed.
	if in.Key.Other(cur) {
		return head + i18n.Tf(" — sent with another key (%s): a call with this key does not close the record; run evo doctor with that key",
			" — отправлено другим ключом (%s): вызов этим ключом запись не закроет; запустите evo doctor с тем ключом", quoteCloud(in.Key.KeyID))
	}
	// Every call of the tool is one more change: the same call again does
	// not settle the record, it changes the resource a second time.
	if read := everySendIsAChange[in.Tool].read; read != "" {
		return head + i18n.Tf(" — %s shows it; the same %s again is one more change, not a repeat: whether to send it is the person's decision",
			" — его показывает %s; тот же %s ещё раз — это ещё одно изменение, а не повтор: слать ли его — решает человек", quoteCloud(read), tool)
	}
	// A delete addressed by id is not looked up in the list of the call's
	// project, which does not tell a deleted resource from one of another
	// project (R-82): doctor reads it by id and says what to do.
	if _, k, id, byID := deleteByID(in); byID {
		if k.card == nil {
			// No read by id closes it: doctor says the same and no more.
			return head + i18n.Tf(" — evo has no read by id that tells a deleted %s from one of another project: check %s where it was — the same %s with the same arguments settles the record",
				" — у evo нет чтения по id, которое отличит удалённый ресурс «%s» от ресурса другого проекта: проверьте %s там, где он был, — тот же %s с теми же аргументами закроет запись", k.what, quoteCloud(id), tool)
		}
		return head + i18n.T(" — evo doctor checks it by id", " — evo doctor проверит по id")
	}
	// The same call is the same arguments (intent.Settle): another call
	// of the tool on the resource leaves the record.
	return head + i18n.Tf(" — check %s for %s; the same %s with the same arguments settles it",
		" — проверьте %s по %s; тот же %s с теми же аргументами закроет запись", quoteCloud(r.listToolOf(in.Tool)), name, tool)
}

// maker is the key the registry now runs with; nil when there is none.
func (r *Registry) maker() *state.Maker {
	if r.newEngine == nil {
		return nil
	}
	eng, err := r.newEngine(nil)
	if err != nil || eng == nil {
		return nil
	}
	return eng.Maker()
}

// deleteByID tells an intent of a delete of the resource it addresses by
// id: its kind and the id. The target of a delete inside a parent is the
// parent, and reading it tells nothing of the child: not one.
func deleteByID(in intent.Intent) (a idArg, k *ownerKind, id string, ok bool) {
	a, id, ok = intentTarget(in)
	if !ok {
		return idArg{}, nil, "", false
	}
	if d, isDelete := deleteTargets[in.Tool]; !isDelete || d.parent == a.name {
		return idArg{}, nil, "", false
	}
	return a, a.kind, id, true
}

// checkPending is what doctor finds out about the intent in and whether
// its outcome is now known (the record closes): a delete of a resource
// addressed by id is checked by a read of that id (checkByID), any other
// by the list of what it names (checkByList).
func (r *Registry) checkPending(ctx context.Context, in intent.Intent, eng *engine.Engine) (found string, settled bool) {
	if _, k, id, ok := deleteByID(in); ok {
		return r.checkByID(ctx, in, k, id, eng.Maker())
	}
	return r.checkByList(ctx, in, eng.Config.ProjectID)
}

// checkByID checks a cut delete of the resource id by reading it by id:
// the list of the call's project does not tell a deleted resource from
// one of another project, which a key of the organization deletes as
// well (R-82; «Риск», conclusion 114). Only «not there», answered to the
// key that sent the delete, closes the record: another key may not see
// the resource, and a read that failed says nothing. A kind with no read
// by id is never closed here — the same delete on it settles the record.
// found is "" when doctor has nothing to add to the record's words.
func (r *Registry) checkByID(ctx context.Context, in intent.Intent, k *ownerKind, id string, cur *state.Maker) (found string, settled bool) {
	what, tool := quoteCloud(id), quoteCloud(in.Tool)
	if k.card == nil {
		// Never closed here; the record's own words say why (pendingNote).
		return "", false
	}
	if in.Key.Other(cur) {
		// The record's own words say it (pendingNote).
		return "", false
	}
	if in.Key == nil || cur == nil {
		return i18n.Tf("not checked: the record or this run names no key, and another key may not see %s — run doctor with the key that sent the delete",
			"не проверено: запись или этот запуск не называет ключ, а другой ключ может не видеть %s — запустите doctor с ключом, который отправил удаление", what), false
	}
	m, err := k.card(ctx, &ownerCall{r: r, args: map[string]any{}}, id)
	switch {
	case err == nil:
		status := firstNonEmptyStr(stringArg(m, "status"), stringArg(m, "state"))
		return i18n.Tf("%s %s is still there (status %s): the delete did not take effect, or is still going — run doctor again, or the same %s",
			"%s %s ещё есть (статус %s): удаление не прошло или ещё идёт — повторите doctor или тот же %s", k.what, what, quoteCloud(status), tool), false
	case notThere(err):
		return i18n.Tf("the read of %s %s by id, with the key that sent the delete, answers that it is not there: the delete took effect; the record is closed",
			"чтение ресурса «%s» %s по id ключом, который отправил удаление, отвечает, что его нет: удаление прошло; запись закрыта", k.what, what), true
	}
	reason, _, _ := errReason(err, nil)
	return i18n.Tf("could not read %s %s by id (%s): not checked — run doctor again",
		"не удалось прочитать ресурс «%s» %s по id (%s): не проверено — повторите doctor", k.what, what, reason), false
}

// checkByList reads the list of what in names (createdIDs: list, field),
// as doctor does for each intent (release plan 1.8, (и), PR-2; «Риск»,
// conclusion 46): a create whose resource is listed by its name took
// effect; a delete whose target is not listed took effect — the outcome is
// known, and the record closes. Anything else — not listed (yet), still
// listed, a list that needs another argument or did not answer — keeps
// the record; found tells what the list said, "" when it was not read.
func (r *Registry) checkByList(ctx context.Context, in intent.Intent, active string) (found string, settled bool) {
	// The list is the active project's: an intent of another one is not
	// read in it — a target of a delete there is «not listed» here
	// whatever happened to it («Риск», conclusion 47; the lesson of L3).
	if in.ProjectID != "" && active != "" && in.ProjectID != active {
		return i18n.Tf("the record is of project %s, the active one is %s: check it there", "запись из проекта %s, активный — %s: проверьте в нём", quoteCloud(in.ProjectID), active), false
	}
	c, isCreate := createdIDs[in.Tool]
	_, targetID, isDelete := strings.Cut(in.Target, "=")
	if !isCreate {
		for _, cc := range createdIDs {
			if cc.delete == in.Tool && cc.list != "" {
				c = cc
				break
			}
		}
	}
	t, ok := r.Lookup(c.list)
	h := t.Handler
	// The list of a part of a parent needs the parent (listArgs): a
	// create inside it names the parent as its target.
	parentArg, parentID, _ := strings.Cut(in.Target, "=")
	inParent := isCreate && len(c.listArgs) == 1 && c.listArgs[0] == parentArg && parentID != ""
	if c.list == "" || c.field == "" || (len(c.listArgs) > 0 && !inParent) || !ok || h == nil || (isCreate && in.Name == "") || (!isCreate && !isDelete) {
		return "", false
	}
	listArgs := map[string]any{}
	if inParent {
		listArgs[parentArg] = parentID
	}
	if props, _ := t.InputSchema["properties"].(map[string]any); props["project_id"] != nil && in.ProjectID != "" {
		listArgs["project_id"] = in.ProjectID
	}
	text, isErr := h(ctx, listArgs)
	var m map[string]any
	if isErr || json.Unmarshal([]byte(text), &m) != nil {
		return "", false
	}
	items, _ := m[c.field].([]any)
	listed := ""
	for _, it := range items {
		o, _ := it.(map[string]any)
		id, _ := o["id"].(string)
		name, _ := o["name"].(string)
		if (isCreate && name == in.Name) || (!isCreate && id == targetID) {
			listed = id
			break
		}
	}
	switch {
	case isCreate && listed != "":
		return i18n.Tf("%s lists %s (id %s): the create took effect; the record is closed", "%s показывает %s (id %s): создание прошло; запись закрыта", c.list, quoteCloud(in.Name), listed), true
	case isCreate:
		return i18n.Tf("%s has no %s yet: the cloud did not make it, or still makes it — run doctor again, or the same %s", "в %s пока нет %s: облако его не создало или ещё создаёт — повторите doctor или тот же %s", c.list, quoteCloud(in.Name), quoteCloud(in.Tool)), false
	case listed == "":
		return i18n.Tf("%s no longer lists %s: the delete took effect; the record is closed", "%s больше не показывает %s: удаление прошло; запись закрыта", c.list, quoteCloud(targetID)), true
	}
	return i18n.Tf("%s still lists %s: the delete did not take effect (yet) — run doctor again, or the same %s", "%s ещё показывает %s: удаление не прошло (или идёт) — повторите doctor или тот же %s", c.list, quoteCloud(targetID), quoteCloud(in.Tool)), false
}

// listToolOf is the read that names what tool may have made: <service>_list
// of a <service>_create, vm_list of the VM tools, else the tool itself
// (its preview names the resource).
func (r *Registry) listToolOf(tool string) string {
	switch tool {
	case "provision", "deploy", "deploy_apply", "destroy":
		return "vm_list"
	case "stack_apply", "stack_destroy":
		return "stack_status"
	}
	if c, ok := createdIDs[tool]; ok && c.list != "" {
		return c.list
	}
	for _, suffix := range []string{"_create", "_delete"} {
		if base, ok := strings.CutSuffix(tool, suffix); ok {
			if _, found := r.Lookup(base + "_list"); found {
				return base + "_list"
			}
		}
	}
	return tool
}

// withPending adds pending_operations — the intents earlier runs in this
// directory left — to the first answer over MCP: the agent reads them
// before it creates the same thing again (release plan 1.8, (и)).
// The records of own — those the call of this answer opened — are not
// named in it: the answer itself says how the call ended. They are not
// noted yet either (false), so the next answer names them; every other
// reader of the records sees them as before.
func (r *Registry) withPending(answer string, own []string) (string, bool) {
	root, ok := r.stateDir()
	if !ok {
		if r.stateDirUndeclared() {
			// Not «nothing was left»: the records were not read.
			return withNotes(answer, []string{i18n.T(
				"the records of earlier calls were not read: evo was built without their directory (a defect of evo; report it)",
				"записи прежних вызовов не прочитаны: evo собран без их каталога (дефект evo; сообщите о нём)")})
		}
		return answer, true
	}
	notes, leftOut := r.pendingNotes(root, own)
	answer, noted := withNotes(answer, notes)
	return answer, noted && !leftOut
}

// withNotes is answer with notes in pending_operations; false when the
// answer is not an object.
func withNotes(answer string, notes []string) (string, bool) {
	if len(notes) == 0 {
		return answer, true
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return answer, false // not an object: the next answer carries them
	}
	m["pending_operations"] = notes
	// The records' text is the project directory's, not evo's (FB180-2).
	if u, _ := m["_untrusted"].([]any); slices.Contains(u, any("pending_operations")) {
		// Named already: the answer of a call that kept a record.
	} else if u != nil {
		m["_untrusted"] = append(u, "pending_operations")
	} else {
		m["_untrusted"] = []string{"pending_operations"}
	}
	return toJSON(m), true
}

// notePending is answer with the intents of earlier runs, on the first JSON
// answer of the process; the lock is released however withPending ends.
func (r *Registry) notePending(answer string, own []string) string {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if !r.pendingNoted {
		answer, r.pendingNoted = r.withPending(answer, own)
	}
	return answer
}
