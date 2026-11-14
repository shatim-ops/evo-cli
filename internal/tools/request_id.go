// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// withRequestID is the error answer of a call that names the request it
// failed on (httpretry.FailedRequest): the X-Request-Id evo sent and the
// one the answer carried, when another — support finds the request by it
// (release plan 1.9). Which request: the last one of the call answered
// with the status of the answer (apiStatusField), or, for API_UNREACHABLE,
// the last one not answered. Any other answer — a success, a local
// refusal, an error of no request — stays as it is. The CLI shows the
// tool's answer, and an engine command its Caller's error: both get it.
func withRequestID(ctx context.Context, answer string) string {
	if !strings.Contains(answer, `"reason"`) {
		return answer
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return answer
	}
	status := 0
	if st, ok := m[apiStatusField].(float64); ok {
		status = int(st)
	} else if m["reason"] != clierrors.CodeAPIUnreachable {
		return answer
	}
	msg, _ := m["error"].(string)
	f, ok := httpretry.FailedRequest(ctx, status)
	if !ok || f.Sent == "" || strings.Contains(msg, f.Sent) {
		return answer
	}
	named := i18n.Tf(" (X-Request-Id %s)", " (X-Request-Id %s)", f.Sent)
	if f.Answered != "" && f.Answered != f.Sent {
		named = i18n.Tf(" (X-Request-Id %s, the answer's %s)", " (X-Request-Id %s, в ответе %s)", f.Sent, f.Answered)
	}
	m["error"] = msg + named
	if c := callerOf(ctx); c != nil && c.Err != nil {
		c.Err = clierrors.Suffix(c.Err, named)
	}
	return toJSON(m)
}
