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
	// Cloud.ru refuses to delete a DBaaS cluster that is still being
	// created (400/500 FailedPrecondition, register DB-17, CRD-B15): said
	// before the confirmation, not after (release audit re-run, N-09).
	switch strings.ToUpper(status) {
	case "DEPLOYING", "CREATING", "PROVISIONING":
		out["warning"] = i18n.Tf("it is still being created (%s): Cloud.ru refuses the delete until it is ready",
			"он ещё создаётся (%s): Cloud.ru не удалит его, пока он не будет готов", status)
	case "FAILED", "ERROR":
		// kafka_delete of a FAILED cluster met a bare 500 (G13-19).
		out["warning"] = i18n.Tf("it is in %s: ", "он в состоянии %s: ", status) + failedDeleteNote(id)
		// A refusal after the question says what the preview said: the
		// platform does not delete it, the console or support do — not the
		// platform's bare 500 (N2; CLAUDE.md §6, axis 3).
		return &Plan{Preview: out, Execute: func(ctx context.Context) (string, bool) {
			text, isErr := execute(ctx)
			if !isErr {
				return text, isErr
			}
			var m map[string]any
			if json.Unmarshal([]byte(text), &m) != nil {
				return text, isErr
			}
			m["resource_status"] = status
			m["hint"] = i18n.Tf("Cloud.ru does not delete a resource in %s through its API: delete it in the Cloud.ru console, or contact Cloud.ru support with its id %s.",
				"Cloud.ru не удаляет ресурс в состоянии %s через API: удалите его в консоли Cloud.ru или обратитесь в поддержку Cloud.ru с его id %s.", status, id)
			return toJSON(m), true
		}}
	}
	return &Plan{Preview: out, Execute: execute}
}

// failedDeleteNote is what to do when Cloud.ru refuses to delete a
// resource in FAILED or ERROR: the preview says it before the question,
// the error after the refusal (G13-19).
func failedDeleteNote(id string) string {
	return i18n.Tf("Cloud.ru may refuse to delete it in this state; if it does, contact Cloud.ru support with its id %s.",
		"Cloud.ru может отказать в удалении в этом состоянии; при отказе обратитесь в поддержку Cloud.ru с его id %s.", id)
}
