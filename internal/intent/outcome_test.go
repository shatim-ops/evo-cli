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
		// A request that timed out on the way may have been taken: a
		// 408 is no refusal, whatever the reason of the answer («Риск»,
		// conclusion 176, У5).
		{clierrors.CodeAPIError, 408, true},
		{clierrors.CodeUsage, 408, true},
		{"", 408, true},
	} {
		if got := OutcomeUnknown(c.reason, c.status); got != c.want {
			t.Errorf("OutcomeUnknown(%q, %d) = %v, want %v", c.reason, c.status, got, c.want)
		}
	}
}

// One rule of «the API said the request was not carried out» for the
// record of a call and for a call that re-reads the resource after an
// error: a 4xx other than 408.
func TestDefiniteRefusal(t *testing.T) {
	for status, want := range map[int]bool{0: false, 200: false, 400: true, 404: true, 408: false, 409: true, 429: true, 499: true, 500: false, 503: false} {
		if got := DefiniteRefusal(status); got != want {
			t.Errorf("DefiniteRefusal(%d) = %v, want %v", status, got, want)
		}
		if want && OutcomeUnknown(clierrors.CodeAPIError, status) {
			t.Errorf("a definite refusal %d leaves the outcome unknown", status)
		}
	}
}
