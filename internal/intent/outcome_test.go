// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// One rule of «the cloud did not say what it did» for the tools and for
// evo login (audit Fable 1.8, FB180-3): a cut call, a break, an API error
// or none with a code keep the intent; a refusal with a code, or a 4xx
// the caller knows, does not.
func TestOutcomeUnknown(t *testing.T) {
	for _, c := range []struct {
		reason string
		status int
		want   bool
	}{
		{clierrors.ReasonInterrupted, 0, true},
		{clierrors.CodeAPIUnreachable, 0, true},
		{clierrors.CodeAPIError, 0, true},
		{clierrors.ReasonGeneral, 0, true},
		{clierrors.CodeAPIError, 500, true},
		{clierrors.CodeAPIError, 400, false},
		{clierrors.CodeAPIError, 404, false},
		{clierrors.CodeUsage, 0, false},
		{clierrors.CodeNotFound, 0, false},
		{clierrors.CodeAuthFailed, 0, false},
		{clierrors.CodeAPIRateLimited, 0, false},
		{"", 0, false},
	} {
		if got := OutcomeUnknown(c.reason, c.status); got != c.want {
			t.Errorf("OutcomeUnknown(%q, %d) = %v, want %v", c.reason, c.status, got, c.want)
		}
	}
}
