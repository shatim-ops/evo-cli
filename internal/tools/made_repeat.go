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
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
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

// MadeDir is the directory of the creates going on in root (rememberMade):
// a world of a test that makes the same create twice on purpose clears it.
func MadeDir(root string) string { return filepath.Join(config.DataDir(root), "made") }

// madeKey is the file of the call of tool with the canonical arguments
// canon (canonArgs) on the resource name, target, in project: the same
// call, the same file. Values are hashed, not written in the name.
func madeKey(tool string, canon []byte, name, target, project string) string {
	h := sha256.New()
	for _, part := range [][]byte{[]byte(tool), canon, []byte(name), []byte(target), []byte(project)} {
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

// rememberMade keeps the create of the call of record rec when its answer
// goes on (madeGoesOn). A file that cannot be written leaves the call as it
// was before: the repeat is planned as any call.
func (r *Registry) rememberMade(rec intent.Intent, answer string) {
	id, next, nextArgs, ok := madeGoesOn(rec.Tool, answer)
	root, rooted := r.stateDir()
	if !ok || !rooted || rec.Key == nil {
		return
	}
	mc := madeCall{Tool: rec.Tool, Name: rec.Name, Target: rec.Target, ProjectID: rec.ProjectID, Key: rec.Key,
		ID: id, Next: next, NextArgs: nextArgs, StartedAt: clock.Now()}
	b, err := json.Marshal(mc)
	dir := MadeDir(root)
	if err != nil || os.MkdirAll(dir, 0o700) != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, madeKey(rec.Tool, rec.Canon, rec.Name, rec.Target, rec.ProjectID)), b, 0o600)
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
	resource, target, err := r.resourceOf(ctx, name, args)
	if err != nil || (resource == "" && target == "") {
		return "", false, false
	}
	eng, _ := r.engineOfCall(args)
	if eng == nil {
		return "", false, false
	}
	project := projectOfCall(eng, intent.Intent{Tool: name, Name: resource, Target: target}, args)
	file := filepath.Join(MadeDir(root), madeKey(name, r.canonArgs(name, args), resource, target, project))
	b, err := os.ReadFile(file)
	var mc madeCall
	if err != nil || json.Unmarshal(b, &mc) != nil || mc.Key == nil || mc.Key.Other(eng.Maker()) {
		return "", false, false
	}
	if _, listed := r.checkByList(ctx, intent.Intent{Tool: name, Name: mc.Name, Target: mc.Target, ProjectID: mc.ProjectID}, mc.ProjectID); listed {
		_ = os.Remove(file)
		return "", false, false
	}
	said := i18n.Tf("An earlier %s of %s was taken by the platform as %s and its outcome is not known yet: no second create was sent; this is its progress.",
		"Предыдущий вызов %s для %s принят платформой как %s, его исход ещё неизвестен: второе создание не отправлено; ниже — его ход.",
		name, quoteCloud(resource), mc.ID)
	out := map[string]any{"message": said}
	if t, found := r.Lookup(mc.Next); found && t.Handler != nil {
		read, readErr := t.Handler(ctx, maps.Clone(mc.NextArgs))
		var m map[string]any
		if json.Unmarshal([]byte(read), &m) == nil {
			out = m
			if was, _ := m["message"].(string); was != "" {
				said += " " + was
			}
			out["message"] = said
		}
		isErr = readErr
	}
	if out["status"] == nil {
		out["status"] = "waiting"
	}
	out["repeat_of"] = map[string]any{"tool": name, "id": mc.ID, "started_at": mc.StartedAt.UTC().Format(time.RFC3339)}
	nextStep(out, mc.Next, maps.Clone(mc.NextArgs))
	return toJSON(out), isErr, true
}
