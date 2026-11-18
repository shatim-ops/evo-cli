// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// madeCall is a create the platform took whose operation goes on: the same
// call again reads it instead of sending a second create (Астра #1963, the
// full audit of 14.11, finding 01; release plan 1.9, the line of #1992).
// It is kept beside the intents, not as one: the record of the call closes
// when the call answers, as every record does. One file per call (madeKey).
type madeCall struct {
	Tool      string         `json:"tool"`
	Name      string         `json:"name,omitempty"`
	Target    string         `json:"target,omitempty"`
	ProjectID string         `json:"project_id,omitempty"`
	Key       *state.Maker   `json:"key,omitempty"`
	ID        string         `json:"id"`
	Next      string         `json:"next"`
	NextArgs  map[string]any `json:"next_args,omitempty"`
	StartedAt time.Time      `json:"started_at"`
}

// madeMaxAge bounds the life of a file of a create going on: older, it is
// not read and goes, and the call is planned as any other («Риск»,
// conclusion 268, 2). The longest wait evo takes for a create of its own is
// 30 minutes (restoreHeldLimit): four times that. A bound of evo, not a
// statement of the platform.
const madeMaxAge = 2 * time.Hour

// madeListGrace is how long a read that lists nothing for the id is taken
// for a list that does not show the resource yet, not for a resource gone
// («Риск», conclusion 268, 1): the list and its lag look the same. A bound
// of evo, not a statement of the platform; past it, the empty read is an
// answer.
const madeListGrace = 10 * time.Minute

// MadeDir is the directory of the creates going on in root (rememberMade):
// a part of the intents' directory, so whatever clears the intents clears
// it too; the reader of the intents skips it (intent.Pending reads *.json).
func MadeDir(root string) string { return filepath.Join(intent.Dir(root), "made") }

// madeName is the form of a name madeKey makes.
var madeName = regexp.MustCompile(`^[0-9a-f]{24}\.json$`)

// madeKey is the file of the call of tool with the canonical arguments
// canon (canonArgs) on the resource name, target: the same call, the same
// file; its project and key are in it. Values are hashed, not written in
// the name.
func madeKey(tool string, canon []byte, name, target string) string {
	h := sha256.New()
	for _, part := range [][]byte{[]byte(tool), canon, []byte(name), []byte(target)} {
		h.Write(part)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:12]) + ".json"
}

// madeGoesOn tells the answer of a create whose resource the platform took
// (the answer names its id) and whose operation goes on in the read of
// another tool, its next step — or, with no step named, in the declared
// list of the resource while the answer says creating or waiting: what a
// repeat of the call reads instead of creating again. A refusal, an answer
// that names no id, and a step of the same tool (held: sameToolNext) are
// not.
func madeGoesOn(tool, answer string) (id, next string, nextArgs map[string]any, ok bool) {
	if _, isCreate := createdOf(tool); !isCreate || answerReason(answer) != "" {
		return "", "", nil, false
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return "", "", nil, false
	}
	next, _ = m["next_tool"].(string)
	nextArgs, _ = m["next_args"].(map[string]any)
	id = createdID(tool, m)
	if id == "" || next == tool {
		return "", "", nil, false
	}
	if next == "" {
		// An answer that says the resource is on its way and names no
		// step: the declared list of the resource is the read, when it
		// needs no parent.
		c, _ := createdOf(tool)
		if st, _ := m["status"].(string); (st != "creating" && st != "waiting") || c.list == "" || len(c.listArgs) > 0 {
			return "", "", nil, false
		}
		next, nextArgs = c.list, nil
	}
	return id, next, nextArgs, true
}

// madeEnded tells a read of the step of a create that says nothing goes
// on: the resource is not found by the read (NOT_FOUND), it or its
// operation is in a state of error, its operation is done with an error,
// or — past madeListGrace — a read by id lists none of it, the read of
// the operation (op) does not find it, or the operation is done and the
// resource, read in the list before the step, is not there: made, then
// gone (A19-8, #2428). An operation not found sooner is
// one the platform does not show yet, not a resource gone: it goes on, as
// a list that lags («Риск», conclusion 310, У1). A read that fails
// otherwise tells nothing.
func madeEnded(read string, isErr bool, age time.Duration, op bool) bool {
	if isErr {
		return madeNotFound(read) && (!op || age > madeListGrace)
	}
	var m map[string]any
	if json.Unmarshal([]byte(read), &m) != nil {
		return false
	}
	for _, k := range []string{"status", "state"} {
		if st, _ := m[k].(string); st != "" && (madeKind(st) == stateFailed || strings.EqualFold(st, "failed")) {
			return true
		}
	}
	// The read of an operation (*_operation): done with an error; done
	// with none past the grace of a list that lags.
	if done, _ := m["done"].(bool); done {
		if _, _, failed := opFailure(m["error"]); failed || (op && age > madeListGrace) {
			return true
		}
	}
	count, counted := m["count"].(float64)
	return counted && count == 0 && age > madeListGrace
}

// madeNotFound tells a refusal of a read that does not find what it reads.
func madeNotFound(read string) bool {
	reason := answerReason(read)
	return reason == clierrors.CodeNotFound || reason == clierrors.CodeVMNotFound
}

// rememberMade keeps the create of the call of record rec when its answer
// goes on (madeGoesOn). A file that cannot be written leaves the call as it
// was before: the repeat is planned as any call.
func (r *Registry) rememberMade(rec intent.Intent, answer string) {
	id, next, nextArgs, ok := madeGoesOn(rec.Tool, answer)
	root, rooted := r.stateDir()
	if !ok || !rooted || rec.Key == nil {
		return
	}
	// The consent of this call is not kept: a later call gives its own,
	// or none (A19-1).
	nextArgs = withoutConsent(nextArgs)
	mc := madeCall{Tool: rec.Tool, Name: rec.Name, Target: rec.Target, ProjectID: rec.ProjectID, Key: rec.Key,
		ID: id, Next: next, NextArgs: nextArgs, StartedAt: clock.Now()}
	b, err := json.Marshal(mc)
	dir := MadeDir(root)
	if err != nil || os.MkdirAll(dir, 0o700) != nil {
		return
	}
	_ = config.WriteFileAtomic(filepath.Join(dir, madeKey(rec.Tool, rec.Canon, rec.Name, rec.Target)), b, 0o600)
}

// madeStepReads tells that step, a read, is one the repeat of create runs
// from a file of made/: a closed list per create — its declared list, its
// declared read by id (created.read), or an operation of its group — and
// none that holds a secret or does something besides (Sensitive,
// SideEffects: k8s_kubeconfig mints a token). A file of the key of the
// call still names any tool: another is named, not run, and its answer
// does not stand for the repeat's (A19-42, #2470; «Риск», 393).
func (r *Registry) madeStepReads(create string, step Tool) bool {
	c, ok := createdOf(create)
	if !ok || step.Decl.Sensitive || step.Decl.SideEffects != "" {
		return false
	}
	if step.Name == c.list || step.Name == c.read {
		return true
	}
	ct, ok := r.Lookup(create)
	return ok && step.Group == ct.Group && slices.ContainsFunc(step.Decl.IDArgs, func(a idArg) bool { return a.kind == kindOperation })
}

// madeByKey tells that the key cur wrote the file of made/ whose key is m:
// a field both name — the account, else the Key ID — is the same. A file
// whose key names nothing cur knows too — empty, or another account the
// key of the call does not name — is not evo's own: what a cloned
// directory of the state, or anyone who writes in it, may bring. Unlike
// Maker.Other, «unknown» is not «the same» here: the file names a step to
// run (A19-1, #2418).
func madeByKey(m, cur *state.Maker) bool {
	switch {
	case m == nil || cur == nil:
		return false
	case m.ServiceAccountID != "" && cur.ServiceAccountID != "":
		return m.ServiceAccountID == cur.ServiceAccountID
	case m.KeyID != "" && cur.KeyID != "":
		return m.KeyID == cur.KeyID
	}
	return false
}

// repeatOfMade answers a call of tool name whose same call before was
// taken by the platform and goes on (rememberMade): it reads the step of
// that operation and sends no second create — the consent given was to one
// resource. What the list shows by the name is made: its file goes, and
// the call goes on as any other — the plan of the tool answers the name
// taken. A call by the same name with other arguments is another create,
// the person's to make: another file. ok false when there is none.
func (r *Registry) repeatOfMade(ctx context.Context, name string, args map[string]any) (text string, isErr, ok bool) {
	if _, isCreate := createdOf(name); !isCreate {
		return "", false, false
	}
	root, rooted := r.stateDir()
	if !rooted {
		return "", false, false
	}
	// No create goes on here: nothing is read and no engine is built — the
	// call is planned as any other.
	if entries, err := os.ReadDir(MadeDir(root)); err != nil || len(entries) == 0 {
		return "", false, false
	}
	resource, target, err := r.resourceOf(ctx, name, args)
	if err != nil || (resource == "" && target == "") {
		return "", false, false
	}
	file := filepath.Join(MadeDir(root), madeKey(name, r.canonArgs(name, args), resource, target))
	b, err := config.ReadFileNoFollow(file)
	var mc madeCall
	if err != nil || json.Unmarshal(b, &mc) != nil || mc.Key == nil {
		return "", false, false
	}
	eng, _ := r.engineOfCall(args)
	if eng == nil || !madeByKey(mc.Key, eng.Maker()) ||
		mc.ProjectID != projectOfCall(eng, intent.Intent{Tool: name, Name: resource, Target: target}, args) {
		return "", false, false
	}
	// forget drops the file: nothing goes on, or nothing is known.
	forget := func() { _ = os.Remove(file) }
	if clock.Since(mc.StartedAt) > madeMaxAge {
		forget()
		return "", false, false
	}
	if _, listed, _ := r.checkByList(ctx, intent.Intent{Tool: name, Name: mc.Name, Target: mc.Target, ProjectID: mc.ProjectID}, mc.ProjectID); listed {
		forget()
		return "", false, false
	}
	said := i18n.Tf("An earlier %s of %s was taken by the platform as %s and its outcome is not known yet: no second create was sent; this is its progress.",
		"Предыдущий вызов %s для %s принят платформой как %s, его исход ещё неизвестен: второе создание не отправлено; ниже — его ход.",
		name, quoteCloud(resource), quoteCloud(mc.ID))
	out := map[string]any{"message": said}
	// The step runs only as a read: the call that repeats has not
	// consented to anything, whatever the file holds. A step that changes
	// the resource (disk_attach of disk_create with attach_to) is named,
	// with no consent, for the caller to take (A19-1, #2418).
	step := withoutConsent(mc.NextArgs)
	t, found := r.Lookup(mc.Next)
	writes := found && (t.Mutating || NeedsConsent(t, step))
	reads := found && t.Handler != nil && !writes && r.madeStepReads(name, t)
	switch {
	case writes:
		// No consent is told here: the step asks for it with its own
		// preview («Риск», conclusion 379).
		out["message"] = said + " " + i18n.Tf("Its next step, %s, needs its own consent: it was not run.",
			"Его следующему шагу, %s, нужно своё согласие: он не выполнен.", mc.Next)
	case found && !reads:
		out["message"] = said + " " + i18n.Tf("Its next step, %s, does not read this resource: it was not run.",
			"Его следующий шаг, %s, не читает этот ресурс: он не выполнен.", mc.Next)
	}
	if reads {
		read, readErr := t.Handler(ctx, maps.Clone(step))
		// The read says the resource is not there, or its operation
		// failed: nothing goes on, the file goes, and the call is planned
		// as any other — a new create («Риск», conclusion 268, 1).
		if madeEnded(read, readErr, clock.Since(mc.StartedAt), strings.HasSuffix(mc.Next, "_operation")) {
			forget()
			return "", false, false
		}
		var m map[string]any
		switch {
		case readErr && madeNotFound(read):
			// An operation the platform does not show yet (madeEnded):
			// its NOT_FOUND would read as the resource gone — the answer
			// is waiting, the read the next step («Риск», conclusion 310).
			out["note"] = i18n.T("The platform does not show the operation yet; read it again in a minute.",
				"Платформа ещё не показывает операцию; прочитайте её снова через минуту.")
		case json.Unmarshal([]byte(read), &m) == nil:
			out = m
			if done, _ := m["done"].(bool); done {
				// Done with no error (madeEnded): the outcome is known,
				// the list does not show it yet — or it is gone.
				said = i18n.Tf("An earlier %s of %s was taken by the platform as %s, and its operation is done; the list does not show it yet: no second create was sent. Read the list again in a minute; if it is still not there, it was deleted, and the same call creates it anew.",
					"Предыдущий вызов %s для %s принят платформой как %s, и его операция завершена; список его пока не показывает: второе создание не отправлено. Прочитайте список снова через минуту; если его там всё ещё нет, он удалён, и тот же вызов создаст его заново.",
					name, quoteCloud(resource), quoteCloud(mc.ID))
			}
			if was, _ := m["message"].(string); was != "" {
				said += " " + was
			}
			out["message"] = said
			isErr = readErr
		default:
			isErr = readErr
		}
	}
	if out["status"] == nil {
		out["status"] = "waiting"
	}
	out["repeat_of"] = map[string]any{"tool": name, "id": mc.ID, "started_at": mc.StartedAt.UTC().Format(time.RFC3339)}
	nextStep(out, mc.Next, step)
	return toJSON(out), isErr, true
}

// forgetMadeOf takes away the files of creates going on whose resource the
// delete tool name, called with args, answered gone: the repeat of such a
// create would wait on what is no more (A19-8, #2428). A file names its
// resource by the id the platform gave (madeCall.ID).
func forgetMadeOf(root, name string, args map[string]any, answer string) {
	d := decls[name].Delete
	id := stringArg(args, d.arg)
	if st := answerStatus(answer); d.arg == "" || id == "" || (st != "deleted" && st != "already_deleted") {
		return
	}
	dir := MadeDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		// Only a name of the form madeKey makes: the directory is read, and
		// what it lists is not a path to remove (FB180-1).
		if !madeName.MatchString(e.Name()) {
			continue
		}
		file := filepath.Join(dir, e.Name())
		b, err := config.ReadFileNoFollow(file)
		var mc madeCall
		if err == nil && json.Unmarshal(b, &mc) == nil && mc.ID == id {
			_ = os.Remove(file)
		}
	}
}
