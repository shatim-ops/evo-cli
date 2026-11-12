// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// A preview that costs a request (the price) is built for the question
// only (Plan.PreviewOf): a call with consent runs the execution and does
// not build it, a call without gets it as its confirmation_required, and
// its error refuses the call before the question, as the planner's does.
func TestGate_PreviewOfIsBuiltForTheQuestionOnly(t *testing.T) {
	r := New(nil)
	built, ran := 0, 0
	var fail error
	h := r.gated("k8s_create", func(context.Context, map[string]any) (*Plan, error) {
		return &Plan{
			PreviewOf: func(context.Context) (map[string]any, error) {
				built++
				return map[string]any{"message": "what will be made"}, fail
			},
			Execute: func(context.Context) (string, bool) {
				ran++
				return `{"status":"creating"}`, false
			},
		}, nil
	})

	text, isErr := h(context.Background(), map[string]any{"confirmed": true})
	if isErr || built != 0 || ran != 1 {
		t.Fatalf("with consent: %s (error %v), preview built %d times, executed %d", text, isErr, built, ran)
	}

	text, isErr = h(context.Background(), map[string]any{})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	if !isErr || m["status"] != "confirmation_required" || m["message"] != "what will be made" || built != 1 || ran != 1 {
		t.Fatalf("without consent: %s, preview built %d times, executed %d", text, built, ran)
	}

	fail = errors.New("no price")
	text, isErr = h(context.Background(), map[string]any{})
	_ = json.Unmarshal([]byte(text), &m)
	if !isErr || m["status"] != "error" || ran != 1 {
		t.Fatalf("a preview that cannot be built: %s, executed %d", text, ran)
	}
}
