// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// ErrQuotasForbidden is the error of Engine.QuotaProbe for a key the
// quota API refuses (403): a key of a project reads no quotas (P37).
var ErrQuotasForbidden = errors.New("quotas: the key reads no quotas (403)")

// Why the preview did not check the quotas (QuotaCheck.Reason).
const (
	// QuotaProjectKey: the key reads no quotas — a key of a project (P37).
	QuotaProjectKey = "project_key"
	// QuotaNotRead: the read failed (network, the API).
	QuotaNotRead = "not_read"
	// QuotaNotMapped: the key reads them, but evo does not match the
	// quota types to what the call takes yet (the codes of vCPU, RAM,
	// disk and address are to be probed with a key that reads them).
	QuotaNotMapped = "not_mapped"
)

// QuotaCheck is what a preview says of the project's quotas (D60): the
// call may fail on one after the question, so the preview says it was not
// checked and why, rather than say nothing.
type QuotaCheck struct {
	Checked bool   `json:"checked"`
	Reason  string `json:"reason,omitempty"`
	// Note is the reason as text — what the human preview says; the agent
	// reads --json and the answer of MCP, not the code alone (agent run
	// C2, Ц-1).
	Note string `json:"note,omitempty"`
}

// newQuotaCheck is the check not made, for reason, with its note.
func newQuotaCheck(reason string) *QuotaCheck {
	why := map[string]string{
		QuotaProjectKey: i18n.T("project key", "ключ проекта"),
		QuotaNotRead:    i18n.T("not read", "не прочитана"),
		QuotaNotMapped:  i18n.T("evo does not match quota types yet", "evo пока не сверяет типы квот"),
	}[reason]
	return &QuotaCheck{Reason: reason, Note: i18n.Tf("not checked (%s)", "не проверена (%s)", why)}
}

// quotaCheck asks QuotaProbe whether the project's quotas can be read; nil
// without a probe or a project.
func (e *Engine) quotaCheck(ctx context.Context, projectID string) *QuotaCheck {
	if e.QuotaProbe == nil || e.Auth == nil || projectID == "" {
		return nil
	}
	switch err := e.QuotaProbe(ctx, e.Auth, projectID); {
	case err == nil:
		return newQuotaCheck(QuotaNotMapped)
	case errors.Is(err, ErrQuotasForbidden):
		return newQuotaCheck(QuotaProjectKey)
	default:
		return newQuotaCheck(QuotaNotRead)
	}
}

// line is the quota line of the human preview, "" without a check.
func (q *QuotaCheck) line() string {
	if q == nil || q.Checked {
		return ""
	}
	return previewLabel("quota", "квота") + q.Note
}
