// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The reason of a tool's error answer, whose "error" is a string, and of the
// CLI's --json one, whose "error" is an object: a struct of both read a
// tool's error as one with no reason, and the record of a call whose answer
// was lost was closed as if its outcome were known (the audit of #1544, Б2).
func TestAnswerReason_BothForms(t *testing.T) {
	for answer, want := range map[string]string{
		`{"status":"error","error":"create backup: API_ERROR: 502","reason":"API_ERROR"}`: "API_ERROR",
		`{"error":{"code":5,"reason":"API_ERROR","message":"x"}}`:                         "API_ERROR",
		`{"status":"created"}`: "",
		`not json`:             "",
	} {
		if got := answerReason(answer); got != want {
			t.Errorf("answerReason(%s) = %q, want %q", answer, got, want)
		}
	}
}

// A record whose outcome is unknown is not kept for ever («Риск», the
// question to the PR of answerReason): the call whose answer was lost keeps
// it, and the next call of the same tool on the same resource whose outcome
// is known — made, or refused by the platform — closes it. A tool of
// everySendIsAChange closes it by asking again
// (TestIntent_ConsentedCallAsksAgainAfterItsCutCall).
func TestIntent_UnknownOutcomeKeptThenClosedByAKnownOne(t *testing.T) {
	const tool = "cert_renew" // any tool that takes consent
	args := func() map[string]any { return map[string]any{"certificate_id": resMine, "confirmed": true} }
	for _, second := range []string{
		`{"status":"ok"}`, // made
		`{"status":"error","error":"USAGE: refused","reason":"USAGE"}`, // refused before anything was made
	} {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		t.Chdir(eng.StateDir)
		dir := intent.Dir(eng.StateDir)
		tl, _ := r.Lookup(tool)
		answer := `{"status":"error","error":"renew: API_ERROR: 502","reason":"API_ERROR"}`
		call := r.intended(tl, func(context.Context, map[string]any) (string, bool) { return answer, true })
		call(context.Background(), args())
		if left := intent.Pending(dir); len(left) != 1 {
			t.Fatalf("a lost answer: want its record kept, got %d", len(left))
		}
		answer = second
		call(context.Background(), args())
		if left := intent.Pending(dir); len(left) != 0 {
			t.Errorf("the next call with a known outcome (%s): want the record closed, got %+v", second, left)
		}
	}
}
