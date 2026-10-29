// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	if statusGone(status) {
		return answered(s.deleteFailed(tool, id, clierrors.New(clierrors.CodeNotFound,
			i18n.Tf("%s is %s", "%s в состоянии %s", label, status), "")))
	}
	out := map[string]any{
		"message": i18n.Tf(en, ru, label),
		"target":  map[string]any{"id": id, "name": name, "status": status},
	}
	if warning, _ := stateWayOut(status, id); warning != "" {
		out["warning"] = warning
	}
	return &Plan{Preview: out, Execute: withWayOut(execute, status, id)}
}

// stateWayOut is what the preview warns of, and the hint of the refusal,
// when Cloud.ru refuses to delete a resource in status; "" for a status
// it deletes in. One place for every delete (CLAUDE.md §6, axis 3): a
// refusal says so and names the way out. Still being created (DEPLOYING,
// CREATING: register DB-17, DB-19, NET-11, CRD-B15) ends by itself — wait
// and delete again; FAILED, ERROR, error_deleting (DB-21, NET-7, G13-19)
// do not — the console or support (N2).
func stateWayOut(status, id string) (warning, hint string) {
	switch strings.ToUpper(status) {
	case "DEPLOYING", "CREATING", "PROVISIONING":
		return i18n.Tf("it is still being created (%s): Cloud.ru refuses the delete until it is ready",
				"он ещё создаётся (%s): Cloud.ru не удалит его, пока он не будет готов", status),
			i18n.Tf("Cloud.ru does not delete a resource while it is being created (%s): wait until it is ready, then delete it again.",
				"Cloud.ru не удаляет ресурс, пока он создаётся (%s): дождитесь, пока он будет готов, и повторите удаление.", status)
	case "FAILED", "ERROR", "ERROR_DELETING":
		return i18n.Tf("it is in %s: ", "он в состоянии %s: ", status) + failedDeleteNote(id),
			i18n.Tf("Cloud.ru does not delete a resource in %s through its API: delete it in the Cloud.ru console, or contact Cloud.ru support with its id %s.",
				"Cloud.ru не удаляет ресурс в состоянии %s через API: удалите его в консоли Cloud.ru или обратитесь в поддержку Cloud.ru с его id %s.", status, id)
	}
	return "", ""
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

// withWayOut is execute whose refusal, of a resource read in status
// before the question, says what the preview said: the status and the way
// out (stateWayOut), not the platform's bare error. The reason stays the
// platform's (owner's decision 28.10, #455, option (б)); an error the
// status does not explain (notTheState) keeps its own hint, with the status.
func withWayOut(execute func(context.Context) (string, bool), status, id string) func(context.Context) (string, bool) {
	_, hint := stateWayOut(status, id)
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

// failedDeleteNote is what to do when Cloud.ru refuses to delete a
// resource in FAILED or ERROR: the preview says it before the question,
// the error after the refusal (G13-19).
func failedDeleteNote(id string) string {
	return i18n.Tf("Cloud.ru may refuse to delete it in this state; if it does, contact Cloud.ru support with its id %s.",
		"Cloud.ru может отказать в удалении в этом состоянии; при отказе обратитесь в поддержку Cloud.ru с его id %s.", id)
}
