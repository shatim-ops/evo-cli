// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// madeLimit bounds the wait of a create for its resource to be made
// within the call; an MCP call's budget ends it sooner (D69).
const madeLimit = 2 * time.Minute

// made is a resource a create asked for that the platform makes after it
// answers: the answer carries it on its way (a subnet creating, a SNAT
// gateway CREATING).
type made struct {
	// tool is the create; list the list tool whose «ids» filter reads the
	// resource (the next step).
	tool, list string
	// field is the resource's key in the answer, idField the key of its
	// id.
	field, idField string
	// what names the resource at the start of a sentence.
	what, whatRU string
	// read lists the resources the list tool shows.
	read func(ctx context.Context) ([]map[string]any, error)
}

// madeAnswer is the answer of a create whose resource the platform makes
// after it answers (CLAUDE.md §6, axis 2; Астра #1815: the answer said
// created, its resource being made; release plan 1.9, «Честный ход
// операций», (1)). One made already is created; else the call waits for it
// within its limit, saying its state when it changes — made: created, with
// the resource as read; one the platform failed to make (a state of
// error): its refusal, with the id; still being made at the limit:
// creating, with its state and the list of that id as the next step; in a
// state of none of the closed lists (madeStates), or never listed: waiting,
// with its state and the same next step («Риск · 1.9 (3)», conclusion 236,
// У1); the call cancelled: INTERRUPTED (D69), with the id and the same
// next step. The resource stays in res[m.field] as the platform answered
// it, unless a later read names it. A state of the cloud in a text of evo
// is quoted (quoteCloud; conclusion 236, У2).
func madeAnswer(ctx context.Context, eng *engine.Engine, res map[string]any, m made) (string, bool) {
	res["status"] = "created"
	item, _ := res[m.field].(map[string]any)
	id, _ := item["id"].(string)
	if madeKind(madeState(item)) == stateMade {
		return toJSON(res), false
	}
	seen := false
	last, _, _, err := waitState(ctx, eng, madeLimit,
		func(ctx context.Context) (string, bool, error) {
			items, err := m.read(ctx)
			if err != nil {
				// A read that failed is no state: the wait reads again.
				return "", false, nil
			}
			for _, it := range items {
				if iid, _ := it["id"].(string); iid == id {
					item, seen = it, true
					return madeState(it), true, nil
				}
			}
			return "", false, nil
		},
		func(st string) string {
			return i18n.Tf("%s %s: %s, waiting until it is made…", "%s %s: %s, жду завершения создания…",
				i18n.T(m.what, m.whatRU), id, quoteCloud(st))
		},
		func(st string) (bool, error) { return madeKind(st) != stateMaking, nil })
	word := "unknown"
	if seen {
		word = quoteCloud(last)
	} else {
		last = "unknown"
	}
	res[m.idField] = id
	kind := madeKind(last)
	switch {
	case err == nil && seen && kind == stateMade:
		res[m.field] = mergedItem(res[m.field], item)
		return toJSON(res), false
	case err == nil && seen && kind == stateFailed:
		res["status"] = "failed"
		res[m.field] = mergedItem(res[m.field], item)
		return errAnswer(clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("%s %s: Cloud.ru did not finish making it, state %s", "%s %s: Cloud.ru не завершил создание, состояние — %s", i18n.T(m.what, m.whatRU), id, word),
			i18n.Tf("%s ids=[%s] shows it; whether to delete it (%s) — ask the person.",
				"Состояние показывает %s ids=[%s]; удалять ли (%s) — спросите человека.", m.list, id, decls[m.tool].Created.delete)), res), true
	}
	res["status"] = "creating"
	if !seen || kind == stateOther {
		res["status"] = "waiting"
	}
	res[m.field+"_state"] = last
	res["next"] = i18n.Tf("%s %s is %s: the platform has not finished making it. %s ids=[%s] shows its state.",
		"%s %s — %s: платформа ещё не завершила создание. Состояние показывает %s ids=[%s].", i18n.T(m.what, m.whatRU), id, word, m.list, id)
	nextStep(res, m.list, map[string]any{"ids": []any{id}})
	if stderrors.Is(err, context.Canceled) {
		return errAnswer(fmt.Errorf("%s: %w", i18n.Tf("%s %s: the wait was cancelled, the platform goes on making it",
			"%s %s: ожидание прервано, платформа продолжает создание", i18n.T(m.what, m.whatRU), id), err), res), true
	}
	return toJSON(res), false
}

// The kinds of the state of a resource a create makes (madeKind).
const (
	stateMade = iota
	stateMaking
	stateFailed
	stateOther
)

// madeStates and makingStates are the closed lists of the states of a
// resource made and being made, by the published specifications of the
// network resources (created, creating, updating, deleting, error…;
// list_filters.go, networkStates). A state of neither, nor of error, is
// another: not made («Риск · 1.9 (3)», conclusion 236, У1).
var (
	madeStates   = []string{"created", "updating"}
	makingStates = []string{"creating"}
)

// madeKind is the kind of the state st, whatever its case.
func madeKind(st string) int {
	l := strings.ToLower(st)
	switch {
	case slices.Contains(madeStates, l):
		return stateMade
	case slices.Contains(makingStates, l):
		return stateMaking
	case strings.HasPrefix(l, "error"):
		return stateFailed
	}
	return stateOther
}

// madeState is the state of a resource as its item names it: state, else
// status (a list item may carry both, a create's answer one).
func madeState(item map[string]any) string {
	if st, _ := item["state"].(string); st != "" {
		return st
	}
	st, _ := item["status"].(string)
	return st
}

// mergedItem is the resource as the create answered it with what a later
// read of it says over it: a list item may carry fewer fields.
func mergedItem(was any, read map[string]any) map[string]any {
	out := map[string]any{}
	if m, ok := was.(map[string]any); ok {
		maps.Copy(out, m)
	}
	maps.Copy(out, read)
	return out
}
