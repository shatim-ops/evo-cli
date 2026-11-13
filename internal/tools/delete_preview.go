// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// deletePlan is the plan of a *_delete tool (gate.go): one GET of the
// resource names what goes — its name and status next to the ID the
// caller passed — and an ID that is not there is already deleted, the
// answer of a repeated delete (D26), with no question (release audit,
// R-04); any other error of the GET is the answer. The message, in English
// (en) and Russian (ru) for the CLI's language, has one %s: the resource as
// "name" (id), the name quoted on one line (quoteCloud). execute deletes
// it.
func (s *Registry) deletePlan(tool, en, ru, id string, get func() (map[string]any, error), execute func(context.Context) (string, bool)) *Plan {
	m, err := get()
	if err != nil {
		return answered(s.deleteFailed(tool, id, err))
	}
	name, _ := m["name"].(string)
	status, _ := m["status"].(string)
	if status == "" {
		status, _ = m["state"].(string)
	}
	label := id
	if name != "" {
		label = fmt.Sprintf("%s (%s)", quoteCloud(name), id)
	}
	// Deleted earlier: Cloud.ru keeps a DBaaS cluster readable in ERASED
	// for a while, and a second DELETE of it is a 500 FailedPrecondition
	// (Grok live run on 1.3.0, N1). It is gone: already_deleted, without the
	// question and without a DELETE (D26).
	// The state as every delete names it; status stays the service's own
	// word in the answer's fields.
	decl := deleteTargets[tool]
	state := strings.TrimPrefix(status, decl.statePrefix)
	unseen := decl.creatingUnseen != "" && engine.PassingDeleteState(state) ||
		decl.failedUnseen != "" && inError(state)
	if unseen {
		// No rule of this service for the state: no refusal and no way out
		// of one — the preview says the outcome is unknown (FB19I-7, -23).
		state = ""
	}
	if statusGone(state) {
		return answered(s.deleteFailed(tool, id, clierrors.New(clierrors.CodeNotFound,
			i18n.Tf("%s is %s", "%s в состоянии %s", label, status), "")))
	}
	target := map[string]any{"id": id, "name": name, "status": status}
	if refused := passingRefusal(label, id, state, target); refused != nil {
		return refused
	}
	if deleting := alreadyDeleting(label, state, target); deleting != nil {
		return deleting
	}
	out := map[string]any{
		"message": i18n.Tf(en, ru, label),
		"target":  target,
		// A kind the folder's state does not record: unknown, said by the
		// one rule of every delete preview (P3).
		"provenance": engine.ProvenanceOf(deleteTargets[tool].what, id, time.Time{}, nil, nil, nil),
	}
	if warning, _ := engine.DeleteWayOut(state, id); warning != "" {
		out["warning"] = warning
	} else if unseen {
		out["warning"] = i18n.Tf("it is %s: whether Cloud.ru deletes a %s in this state no run has shown — the outcome of the delete is unknown; a refusal names the platform's answer",
			"он в состоянии %s: удаляет ли Cloud.ru %s в этом состоянии, ни один прогон не показал — исход удаления неизвестен; отказ назовёт ответ платформы",
			status, i18n.T(decl.what, decl.whRU))
	}
	if unseen {
		return &Plan{Preview: out, Execute: withStatus(execute, status)}
	}
	return &Plan{Preview: out, Execute: withWayOut(execute, state, status, id)}
}

// inError reports whether state is an error of the resource: one Cloud.ru
// does not delete in (engine.FinalDeleteState), or another error state of
// the service, such as error_creating of a backup.
func inError(state string) bool {
	return engine.FinalDeleteState(state) || strings.HasPrefix(strings.ToUpper(state), "ERROR")
}

// withStatus is execute whose refusal names the status read before the
// question (resource_status) and keeps the platform's own error and hint:
// in a state no run has shown, evo has no way out to name (FB19I-7).
func withStatus(execute func(context.Context) (string, bool), status string) func(context.Context) (string, bool) {
	return func(ctx context.Context) (string, bool) {
		text, isErr := execute(ctx)
		if !isErr {
			return text, isErr
		}
		var m map[string]any
		if json.Unmarshal([]byte(text), &m) != nil {
			return text, isErr
		}
		m["resource_status"] = status
		return toJSON(m), true
	}
}

// passingRefusal is the answer of a delete of a resource still being
// created (engine.PassingDeleteState): Cloud.ru refuses it until the
// resource is ready, so it is refused before the question — USAGE, exit
// code 1, the way out is to wait (D61); nil in any other status. target
// names the resource in the answer.
func passingRefusal(label, id, status string, target map[string]any) *Plan {
	if !engine.PassingDeleteState(status) {
		return nil
	}
	_, hint := engine.DeleteWayOut(status, id)
	return answered(errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("%s is still being created (%s): Cloud.ru does not delete it until it is ready; nothing was done",
			"%s ещё создаётся (%s): Cloud.ru не удалит его, пока он не будет готов; ничего не сделано", label, status), hint),
		map[string]any{"resource_status": target["status"], "target": target}), true)
}

// alreadyDeleting is the answer of a delete of a resource Cloud.ru is
// already deleting (engine.DeletingState): nothing to send and nothing to
// ask — status deleting, exit code 0, as a repeated delete is answered
// (D61, D26); nil in any other status.
func alreadyDeleting(label, status string, target map[string]any) *Plan {
	if !engine.DeletingState(status) {
		return nil
	}
	return answered(toJSON(map[string]any{
		"status":           "deleting",
		"already_deleting": true,
		"resource_status":  target["status"],
		"target":           target,
		"message": i18n.Tf("%s is already being deleted by Cloud.ru (%s); nothing was sent — check later that it is gone",
			"%s уже удаляется Cloud.ru (%s); ничего не отправлено — позже проверьте, что его нет", label, status),
	}), false)
}

// notTheState are the reasons of an error that the status of the resource
// does not explain: the key, the role, the rate limit, the connection. Their
// own hint stays (FB150-2): «delete it in the console» does not fix an
// expired key. resource_status is added all the same: the status read
// before the question is still true.
var notTheState = map[string]bool{
	clierrors.CodeAuthRequired:   true,
	clierrors.CodeAuthExpired:    true,
	clierrors.CodeAuthFailed:     true,
	clierrors.CodeAPIRateLimited: true,
	clierrors.CodeAPIUnreachable: true,
}

// withWayOut is execute whose refusal, of a resource read in state (status
// as its service writes it) before the question, says what the preview said: the status and the way
// out (engine.DeleteWayOut), not the platform's bare error. The reason stays the
// platform's (owner's decision 28.10, #455, option (б)); an error the
// status does not explain (notTheState) keeps its own hint, with the status.
func withWayOut(execute func(context.Context) (string, bool), state, status, id string) func(context.Context) (string, bool) {
	_, hint := engine.DeleteWayOut(state, id)
	if hint == "" {
		return execute
	}
	return func(ctx context.Context) (string, bool) {
		text, isErr := execute(ctx)
		if !isErr {
			return text, isErr
		}
		var m map[string]any
		if json.Unmarshal([]byte(text), &m) != nil {
			return text, isErr
		}
		m["resource_status"] = status
		if reason, _ := m["reason"].(string); !notTheState[reason] {
			m["hint"] = hint
		}
		return toJSON(m), true
	}
}
