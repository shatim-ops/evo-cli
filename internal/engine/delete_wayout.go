// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// DeleteWayOut is what a preview warns of, and the hint of the refusal,
// when Cloud.ru refuses to delete a resource in status; "" for a status
// it deletes in. One place for every delete, the tools' and destroy's
// (CLAUDE.md §6, axis 3): a refusal says so and names the way out. Still
// being created (DEPLOYING, CREATING: register DB-17, DB-19, NET-11,
// CRD-B15) ends by itself — wait and delete again; FAILED, ERROR,
// error_deleting (DB-21, NET-7, G13-19) do not — the console or support
// (N2); DELETING is the platform's own delete under way, which a VM was
// seen to stay in (OPS-18, one run: a hypothesis) — wait, and if it stays,
// the console or support.
func DeleteWayOut(status, id string) (warning, hint string) {
	switch up := strings.ToUpper(status); {
	case PassingDeleteState(status):
		return i18n.Tf("it is still being created (%s): Cloud.ru refuses the delete until it is ready",
				"он ещё создаётся (%s): Cloud.ru не удалит его, пока он не будет готов", status),
			i18n.Tf("Cloud.ru does not delete a resource while it is being created (%s): wait until it is ready, then delete it again.",
				"Cloud.ru не удаляет ресурс, пока он создаётся (%s): дождитесь, пока он будет готов, и повторите удаление.", status)
	case up == "DELETING":
		return i18n.Tf("Cloud.ru is already deleting it (%s); ", "Cloud.ru уже удаляет его (%s); ", status) + FailedDeleteNote(id),
			i18n.Tf("Cloud.ru is already deleting it (%s): wait and check that it is gone; if it stays %s, delete it in the Cloud.ru console, or contact Cloud.ru support with its id %s.",
				"Cloud.ru уже удаляет его (%s): подождите и проверьте, что его нет; если он остаётся в %s, удалите его в консоли Cloud.ru или обратитесь в поддержку Cloud.ru с его id %s.", status, status, id)
	case finalDeleteStates[up]:
		return i18n.Tf("it is in %s: ", "он в состоянии %s: ", status) + FailedDeleteNote(id),
			i18n.Tf("Cloud.ru does not delete a resource in %s through its API: delete it in the Cloud.ru console, or contact Cloud.ru support with its id %s.",
				"Cloud.ru не удаляет ресурс в состоянии %s через API: удалите его в консоли Cloud.ru или обратитесь в поддержку Cloud.ru с его id %s.", status, id)
	}
	return "", ""
}

// FailedDeleteNote is what to do when Cloud.ru refuses to delete a
// resource in FAILED or ERROR: the preview says it before the question,
// the error after the refusal (G13-19).
func FailedDeleteNote(id string) string {
	return i18n.Tf("Cloud.ru may refuse to delete it in this state; if it does, contact Cloud.ru support with its id %s.",
		"Cloud.ru может отказать в удалении в этом состоянии; при отказе обратитесь в поддержку Cloud.ru с его id %s.", id)
}

// finalDeleteStates are the states a resource does not leave by itself and
// Cloud.ru does not delete it in: the way out is the console or support,
// not a wait (DeleteWayOut).
var finalDeleteStates = map[string]bool{"FAILED": true, "ERROR": true, "ERROR_DELETING": true}

// FinalDeleteState reports whether status is one of finalDeleteStates.
func FinalDeleteState(status string) bool { return finalDeleteStates[strings.ToUpper(status)] }

// PassingDeleteState reports whether status is one a resource leaves by
// itself — still being created — and Cloud.ru does not delete it in: the
// way out is to wait (DeleteWayOut), and a delete is refused before its
// question (D61).
func PassingDeleteState(status string) bool {
	switch strings.ToUpper(status) {
	case "DEPLOYING", "CREATING", "PROVISIONING":
		return true
	}
	return false
}

// DeletingState reports whether status is Cloud.ru's own delete under way
// (DELETING, ERASING): a delete of the resource has nothing to send and is
// answered without the question, as a repeat of a delete (D61, D26).
func DeletingState(status string) bool {
	switch strings.ToUpper(status) {
	case "DELETING", "ERASING":
		return true
	}
	return false
}
