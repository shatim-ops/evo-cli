// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The question that names a cut call closes its record when the question
// is the answer of the call — over MCP. A command of the CLI answers the
// question itself (--yes, a script, nobody under --dry-run), so its
// question, without consent or with it, closes nothing: every call of the
// command finds the record and asks again, and nothing is sent, until the
// command says a person answered (CutCallAnswered) — then the call with
// consent is sent as any other (the interim delta audit of 1.9, high;
// «Риск», conclusion 176, У1 and У2).
func TestIntent_ACommandsQuestionKeepsTheCutCallUntilAPersonAnswers(t *testing.T) {
	const tool = "cert_renew" // stands for a tool of the list
	everySendIsAChange[tool] = changeRule{read: "cert_get"}
	t.Cleanup(func() { delete(everySendIsAChange, tool) })
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, _ := r.newEngine(nil)
	dir := intent.Dir(eng.StateDir)
	if _, err := intent.Open(dir, intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: &state.Maker{KeyID: config.MaskKeyID("k")}}); err != nil {
		t.Fatal(err)
	}
	sent := 0
	tl, _ := r.Lookup(tool)
	call := r.intended(tl, func(ctx context.Context, args map[string]any) (string, bool) {
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return `{"status":"confirmation_required","message":"cert_renew renews the certificate."}`, true
		}
		_ = httpretry.NoteWrite(ctx)
		sent++
		return `{"status":"ok"}`, false
	})
	command := WithCaller(context.Background(), &Caller{})
	args := func(confirmed bool) map[string]any {
		a := map[string]any{"certificate_id": resMine}
		if confirmed {
			a["confirmed"] = true
		}
		return a
	}
	for i, confirmed := range []bool{false, true, false, true, true} {
		text, isErr := call(command, args(confirmed))
		if !isErr || !strings.Contains(text, `"interrupted"`) || !strings.Contains(text, "its outcome is unknown") {
			t.Fatalf("call %d of the command (confirmed %v) does not ask about the cut call:\n%s", i, confirmed, text)
		}
		if sent != 0 || len(intent.Pending(dir)) != 1 {
			t.Fatalf("call %d of the command (confirmed %v): sent %d, records %d; want nothing sent and the record kept", i, confirmed, sent, len(intent.Pending(dir)))
		}
	}
	// A person answered the question of the command.
	r.CutCallAnswered(command, tool, args(false))
	if left := intent.Pending(dir); len(left) != 0 {
		t.Fatalf("the person answered and the record stays: %+v", left)
	}
	if text, isErr := call(command, args(true)); isErr || sent != 1 {
		t.Errorf("the call after the person's yes: sent %d\n%s", sent, text)
	}
	// The answer closes the caller's own cut calls of this tool on this
	// resource, and no other record.
	for i, other := range []intent.Intent{
		{Tool: tool, Target: "certificate_id=" + resForeign, Key: &state.Maker{KeyID: config.MaskKeyID("k")}},
		{Tool: tool, Target: "certificate_id=" + resMine, Key: &state.Maker{ServiceAccountID: "sa-other", KeyID: "ot…er"}},
		{Tool: tool, Target: "certificate_id=" + resMine, Key: &state.Maker{KeyID: config.MaskKeyID("k")}, Held: true},
		{Tool: "cert_delete", Target: "certificate_id=" + resMine, Key: &state.Maker{KeyID: config.MaskKeyID("k")}},
	} {
		if _, err := intent.Open(dir, other); err != nil {
			t.Fatal(err)
		}
		r.CutCallAnswered(command, tool, args(false))
		if got := len(intent.Pending(dir)); got != i+1 {
			t.Errorf("the answer closed a record that is not the caller's cut call of the tool on the resource: %+v (%d left, want %d)", other, got, i+1)
		}
	}
}

// A write that the API answered with a 408 may have been taken: the
// record of the call stays, as after any answer that does not say the
// outcome («Риск», conclusion 176, У5 and У6). Any other 4xx is a refusal,
// and the record is closed.
func TestIntent_A408KeepsTheRecordOfTheCall(t *testing.T) {
	for status, kept := range map[int]int{408: 1, 409: 0, 400: 0} {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		tl, _ := r.Lookup("cert_renew")
		answer := fmt.Sprintf(`{"status":"error","reason":"API_ERROR","%s":%d}`, apiStatusField, status)
		r.intended(tl, func(ctx context.Context, _ map[string]any) (string, bool) {
			_ = httpretry.NoteWrite(ctx) // the write went out
			return answer, true
		})(context.Background(), map[string]any{"certificate_id": resMine, "confirmed": true})
		if got := len(intent.Pending(intent.Dir(eng.StateDir))); got != kept {
			t.Errorf("a write answered %d: %d records left, want %d", status, got, kept)
		}
	}
}
