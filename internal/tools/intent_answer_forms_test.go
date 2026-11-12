// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The layer of intents reads every form of an answer the registry gives
// (release plan 1.9, «Слой намерений D92 читает причину ответа с
// ошибкой»; escaped to the audit of backup_create, #1544): answerReason
// took the field error for an object, a tool's answer has it as a string,
// the parse failed, the reason read as none — and the record of a call
// whose answer was lost closed as «the outcome is known». The tests of
// D92 handed it answers with no string error. Here the answers are the
// registry's own: every tool that asks for consent is called in the world
// of the contract with a cloud that fails, and the reason its answer
// carries — read the way a client reads JSON, with no shape assumed — is
// the reason the layer reads. A new form of an answer, of any tool, is
// read the day it is written.

// reasonAsAClientReadsIt is the reason of an answer for one who parses it
// with no struct: the field reason of the answer, else the reason inside
// an error that is an object.
func reasonAsAClientReadsIt(answer string) string {
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return ""
	}
	if r, _ := m["reason"].(string); r != "" {
		return r
	}
	if e, ok := m["error"].(map[string]any); ok {
		r, _ := e["reason"].(string)
		return r
	}
	return ""
}

func TestIntent_ReadsTheReasonOfEveryAnswerTheRegistryGives(t *testing.T) {
	rt := &contractRT{}
	reg := contractReg(t, rt)
	forms := map[string]bool{}
	read, unknown := 0, 0
	for _, tl := range reg.Tools() {
		if !tl.Mutating || contractHTTPSkip[tl.Name] {
			continue
		}
		args := contractArgs(tl)
		args["confirmed"] = true
		// A cloud that answers 500: an outcome the call is not known by
		// (the answer of a write may be lost behind it). A cloud that does
		// not answer gives the same form of the answer and costs the
		// world's backoff at every tool — left out.
		for _, kind := range []string{"500"} {
			rt.kind = kind
			rt.take()
			text, isErr, _ := contractCall(tl, args, contractCallLimit)
			want := reasonAsAClientReadsIt(text)
			if !isErr || want == "" {
				continue
			}
			read++
			var shape map[string]json.RawMessage
			_ = json.Unmarshal([]byte(text), &shape)
			if e := shape["error"]; len(e) > 0 {
				forms[string(e[:1])] = true // " — a string, { — an object
			}
			if got := answerReason(text); got != want {
				t.Errorf("%s, the cloud answers %s: the answer carries the reason %s, the layer of intents reads %q — the record of the call would close as if its outcome were known: %.300s",
					tl.Name, kind, want, got, text)
				continue
			}
			if intent.OutcomeUnknown(want, 0) {
				unknown++
			}
		}
	}
	if read == 0 {
		t.Fatal("no answer with a reason was read: the world of the contract does not reach the tools")
	}
	if !forms[`"`] {
		t.Errorf("no answer of the %d read has error as a string: the form the defect was in is not in the world — the test sees nothing of it", read)
	}
	if unknown == 0 {
		t.Errorf("none of the %d answers read is one whose outcome is not known (intent.OutcomeUnknown): the world gives no lost answer", read)
	}
}
