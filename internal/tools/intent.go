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

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
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
	// No engine: the call cannot reach the cloud, its handler answers that
	// itself.
	eng, err := r.newEngine(nil)
	if err != nil || eng == nil {
		return func(string) {}, nil
	}
	root := eng.StateDir
	in := intent.Intent{Tool: name, Surface: "mcp", PID: os.Getpid(), StartedAt: clock.Now(),
		ProjectID: eng.Config.ProjectID, Zone: eng.Config.Region}
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
	in.Name, in.Target = intentResource(name, args)
	in.Key = eng.Maker()
	// The write of a call addressed by id lands in the project of the
	// resource, which the call's project does not tell (namedProject): the
	// record names no project rather than the profile's (R-82).
	if _, _, byID := intentTarget(in); byID {
		in.ProjectID = ""
	}
	if p, _ := args["project_id"].(string); p != "" {
		in.ProjectID = p
	}
	if z, _ := args["zone"].(string); z != "" {
		in.Zone = z
	}
	// The engine of evo always has its directory (buildEngine fails
	// without the working directory); one without is a test's fake.
	if root == "" {
		return func(string) {}, nil
	}
	dir := intent.Dir(root)
	rec, err := intent.Open(dir, in)
	if err != nil {
		return nil, err
	}
	return func(answer string) {
		if ctx.Err() != nil || intent.OutcomeUnknown(answerReason(answer), 0) {
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
func (r *Registry) settleHeld(name string, args map[string]any, answer string) {
	resource, target := intentResource(name, args)
	root, err := os.Getwd()
	if err != nil {
		return
	}
	var held []intent.Intent
	for _, in := range intent.Pending(intent.Dir(root)) {
		if !in.Held || in.Tool != name {
			continue
		}
		if (in.Name == resource && in.Target == target && (resource != "" || target != "")) || answerNames(answer, in.Target) {
			held = append(held, in)
		}
	}
	if len(held) == 0 || intent.OutcomeUnknown(answerReason(answer), 0) || answerStatus(answer) == "confirmation_required" {
		return
	}
	if _, goesOn := sameToolNext(name, answer); goesOn {
		return
	}
	cur := r.maker()
	for _, in := range held {
		if !in.Key.Other(cur) {
			intent.Close(in)
		}
	}
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
func (r *Registry) namedKept(name string, args map[string]any, answer string) string {
	resource, target := intentResource(name, args)
	root, err := os.Getwd()
	if err != nil || (resource == "" && target == "") {
		return answer
	}
	// The records of this directory, as withPending reads them; the key
	// of the call is asked for only when one of them is of this call — a
	// call with nothing left builds no engine.
	var same []intent.Intent
	for _, in := range intent.Pending(intent.Dir(root)) {
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

// answerReason is the reason of a tool's answer, "" when it has none.
func answerReason(answer string) string {
	var a struct {
		Reason string `json:"reason"`
		Error  struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(answer), &a) != nil {
		return ""
	}
	if a.Reason != "" {
		return a.Reason
	}
	return a.Error.Reason
}

// PendingNotes are the intents root's earlier runs left, in words: what
// was sent, where, when, and where to look — no advice to delete: the
// resource may be the person's now (R-56). The lines are the same in
// doctor, in the first answer of a tool and in the CLI's stderr.
func (r *Registry) PendingNotes(root string) []string {
	var out []string
	for _, in := range pendingIntents(root) {
		out = append(out, r.pendingNote(in, r.maker()))
	}
	return out
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
	// A delete addressed by id is not looked up in the list of the call's
	// project, which does not tell a deleted resource from one of another
	// project (R-82): doctor reads it by id and says what to do.
	if _, k, id, byID := deleteByID(in); byID {
		if k.card == nil {
			// No read by id closes it: doctor says the same and no more.
			return head + i18n.Tf(" — evo has no read by id that tells a deleted %s from one of another project: check %s where it was — the same %s on it settles the record",
				" — у evo нет чтения по id, которое отличит удалённый ресурс «%s» от ресурса другого проекта: проверьте %s там, где он был, — тот же %s на нём закроет запись", k.what, quoteCloud(id), tool)
		}
		return head + i18n.T(" — evo doctor checks it by id", " — evo doctor проверит по id")
	}
	return head + i18n.Tf(" — check %s for %s; the same %s on it settles it",
		" — проверьте %s по %s; тот же %s на нём закроет запись", quoteCloud(r.listToolOf(in.Tool)), name, tool)
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
	if c.list == "" || c.field == "" || len(c.listArgs) > 0 || !ok || h == nil || (isCreate && in.Name == "") || (!isCreate && !isDelete) {
		return "", false
	}
	listArgs := map[string]any{}
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
func (r *Registry) withPending(answer string) (string, bool) {
	root, err := os.Getwd()
	if err != nil {
		return answer, true
	}
	return withNotes(answer, r.PendingNotes(root))
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
func (r *Registry) notePending(answer string) string {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if !r.pendingNoted {
		answer, r.pendingNoted = r.withPending(answer)
	}
	return answer
}
