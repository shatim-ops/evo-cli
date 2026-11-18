// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
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
		call := r.intended(tl, func(ctx context.Context, _ map[string]any) (string, bool) {
			_ = httpretry.NoteWrite(ctx) // the request went out
			return answer, true
		})
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
// refusalCallLimit bounds a call of TestIntent_RefusalOfThePlatformClosesTheRecord:
// the world refuses at once, the limit only stops a hang. A call cut by
// its deadline after the send keeps its record — the outcome is unknown —
// and that is right for a cut, wrong for this test: contractCallLimit
// (400 ms) cut ca_stop on a loaded macOS runner (#2396, package 820 s).
// wall-clock: a guard against a hang, no answer depends on it.
const refusalCallLimit = 30 * time.Second

func TestIntent_RefusalOfThePlatformClosesTheRecord(t *testing.T) {
	rt := &contractRT{}
	reg := contractReg(t, rt)
	eng, _ := reg.newEngine(nil)
	t.Chdir(eng.StateDir)
	dir := intent.Dir(eng.StateDir)
	seen := 0
	for _, kind := range []string{"400", "403", "409", "422", "500"} {
		for _, tl := range reg.Tools() {
			if !tl.Mutating || contractHTTPSkip[tl.Name] {
				continue
			}
			args := contractArgs(tl)
			args["confirmed"] = true
			rt.kind = kind
			rt.take()
			text, _, _ := contractCall(tl, args, refusalCallLimit)
			// The status is the record's: no answer carries it (CMP-7).
			if strings.Contains(text, apiStatusField) {
				t.Errorf("%s, the cloud answers %s: the answer carries %s: %.300s", tl.Name, kind, apiStatusField, text)
			}
			if kind == "500" {
				_ = os.RemoveAll(dir)
				continue
			}
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

// refusedWritesReached is how many calls of the test below reach a write
// the cloud refuses (33 on 18.11; the rest stop at a refused read before
// the write): a world that reaches fewer checks less.
const refusedWritesReached = 30

// A refusal of the platform (4xx) ends the work of the call, counted in
// requests, not in time («Риск», 376): the refused write is not sent again,
// and after it at most one read goes out — the check of what the cloud
// holds (409: the resource is there). Every tool that takes consent, on a
// cloud that answers 400, 403, 409 or 422 to everything.
func TestIntent_RefusalOfThePlatformEndsTheRequests(t *testing.T) {
	rt := &contractRT{}
	reg := contractReg(t, rt)
	eng, _ := reg.newEngine(nil)
	t.Chdir(eng.StateDir)
	reached := 0
	for _, kind := range []string{"400", "403", "409", "422"} {
		for _, tl := range reg.Tools() {
			if !tl.Mutating || contractHTTPSkip[tl.Name] {
				continue
			}
			args := contractArgs(tl)
			args["confirmed"] = true
			rt.kind = kind
			rt.take()
			_, _, _ = contractCall(tl, args, refusalCallLimit)
			reqs := rt.take()
			refused := -1
			for i, q := range reqs {
				if contractWrites([]string{q}) == 1 {
					refused = i
					break
				}
			}
			if refused < 0 {
				continue
			}
			reached++
			after := reqs[refused+1:]
			if w := contractWrites(after); w != 0 {
				t.Errorf("%s, the cloud refuses with %s: %d more writes after the refused one: %v", tl.Name, kind, w, reqs)
			}
			if len(after) > 1 {
				t.Errorf("%s, the cloud refuses with %s: %d requests after the refused write, one read at most: %v", tl.Name, kind, len(after), reqs)
			}
		}
	}
	if reached < refusedWritesReached {
		t.Errorf("%d calls reached a refused write, %d expected: the world no longer reaches the writes", reached, refusedWritesReached)
	}
	t.Logf("calls that reached a refused write: %d", reached)
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
