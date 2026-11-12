// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
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
		errResult(clierrors.New(clierrors.CodeUsage, "refused", "")), // refused before anything was made
	} {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		t.Chdir(eng.StateDir)
		dir := intent.Dir(eng.StateDir)
		tl, _ := r.Lookup(tool)
		// The answer of a tool whose request got no answer, as errAnswer
		// makes it: "error" a string, the reason beside it.
		answer := errResult(clierrors.New(clierrors.CodeAPIError, "renew: 502", ""))
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

// A refusal of the platform (4xx) is an outcome known: the record of the
// call closes, whatever the tool («Риск», conclusion 170, У1). Its reason
// is API_ERROR, as a 5xx's is; the status of the answer (http_status,
// errAnswer) parts them. Every tool that takes consent, on a cloud that
// answers 400, 403, 409 or 422 to everything: no record is left.
func TestIntent_RefusalOfThePlatformClosesTheRecord(t *testing.T) {
	rt := &contractRT{}
	reg := contractReg(t, rt)
	eng, _ := reg.newEngine(nil)
	t.Chdir(eng.StateDir)
	dir := intent.Dir(eng.StateDir)
	seen := 0
	for _, kind := range []string{"400", "403", "409", "422"} {
		for _, tl := range reg.Tools() {
			if !tl.Mutating || contractHTTPSkip[tl.Name] {
				continue
			}
			args := contractArgs(tl)
			args["confirmed"] = true
			rt.kind = kind
			rt.take()
			_, _, _ = contractCall(tl, args, contractCallLimit)
			if left := intent.Pending(dir); len(left) != 0 {
				t.Errorf("%s, the cloud refuses with %s: the record of the call is kept as if its outcome were unknown: %+v", tl.Name, kind, left[0])
				_ = os.RemoveAll(dir)
			}
			seen++
		}
	}
	if seen == 0 {
		t.Fatal("no tool that takes consent was called")
	}
}

// vm_reboot, after a refusal of the platform: the next call with the
// consent is not answered «the earlier call was cut, the change may have
// happened» — the platform said no, and the record closed (conclusion 170,
// У2).
func TestIntent_RebootAfterARefusalIsNotACut(t *testing.T) {
	r, w, dir := rebootWorldInADir(t)
	w.powerRefused = "zone_changes_prohibited" // a 422 of the platform
	tl, _ := r.Lookup("vm_reboot")
	args := func() map[string]any { return map[string]any{"name": "web", "confirmed": true} }
	if text, isErr := tl.Handler(context.Background(), args()); !isErr {
		t.Fatalf("want the refusal: %s", text)
	}
	if left := intent.Pending(dir); len(left) != 0 {
		t.Fatalf("a refusal of the platform left the record of the call: %+v", left)
	}
	w.powerRefused = ""
	text, _ := tl.Handler(context.Background(), args())
	if strings.Contains(text, "interrupted") || strings.Contains(text, "outcome is unknown") {
		t.Errorf("the call after a refusal is answered as one after a cut call: %.400s", text)
	}
}
