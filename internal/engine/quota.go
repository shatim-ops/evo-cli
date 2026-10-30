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
}

// quotaCheck asks QuotaProbe whether the project's quotas can be read; nil
// without a probe or a project.
func (e *Engine) quotaCheck(ctx context.Context, projectID string) *QuotaCheck {
	if e.QuotaProbe == nil || e.Auth == nil || projectID == "" {
		return nil
	}
	switch err := e.QuotaProbe(ctx, e.Auth, projectID); {
	case err == nil:
		return &QuotaCheck{Reason: QuotaNotMapped}
	case errors.Is(err, ErrQuotasForbidden):
		return &QuotaCheck{Reason: QuotaProjectKey}
	default:
		return &QuotaCheck{Reason: QuotaNotRead}
	}
}

// line is the quota line of the human preview, "" without a check.
func (q *QuotaCheck) line() string {
	if q == nil || q.Checked {
		return ""
	}
	why := map[string]string{
		QuotaProjectKey: i18n.T("project key", "ключ проекта"),
		QuotaNotRead:    i18n.T("not read", "не прочитана"),
		QuotaNotMapped:  i18n.T("evo does not match quota types yet", "evo пока не сверяет типы квот"),
	}[q.Reason]
	return previewLabel("quota", "квота") + i18n.Tf("not checked (%s)", "не проверена (%s)", why)
}
