// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// deletePreview is the confirmation answer of a *_delete tool: one GET of
// the resource names what goes — its name and status next to the ID the
// caller passed — and an ID that is not there is already deleted, the
// answer of a repeated delete (D26), instead of a question (release audit,
// R-04); any other error of the GET is the answer. The message, in English (en) and
// Russian (ru) for the CLI's language, has one %s: the resource as
// "name" (id), the name quoted on one line (quoteCloud).
func (s *Registry) deletePreview(tool, en, ru, id string, get func() (map[string]any, error)) (string, bool) {
	m, err := get()
	if err != nil {
		return s.deleteFailed(tool, id, err)
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
	out := map[string]any{
		"status":  "confirmation_required",
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
	}
	return toJSON(out), true
}

// failedDeleteNote is what to do when Cloud.ru refuses to delete a
// resource in FAILED or ERROR: the preview says it before the question,
// the error after the refusal (G13-19).
func failedDeleteNote(id string) string {
	return i18n.Tf("Cloud.ru may refuse to delete it in this state; if it does, contact Cloud.ru support with its id %s.",
		"Cloud.ru может отказать в удалении в этом состоянии; при отказе обратитесь в поддержку Cloud.ru с его id %s.", id)
}
