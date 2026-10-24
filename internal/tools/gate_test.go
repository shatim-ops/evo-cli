// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// The gate (D35): the plan first — its refusal and an answer with nothing
// to consent to come with no question, whatever confirmed says; then the
// preview without consent; the execution runs only with it.
func TestGate_PlanQuestionExecution(t *testing.T) {
	r := New(nil)
	ran := 0
	exec := func(context.Context) (string, bool) { ran++; return toJSON(map[string]any{"status": "deleted"}), false }
	cases := []struct {
		name      string
		plan      Planner
		confirmed bool
		status    string
		isErr     bool
		runs      int
	}{
		{"refused", func(context.Context, map[string]any) (*Plan, error) {
			return nil, clierrors.New(clierrors.CodeUsage, "no", "")
		}, true, "error", true, 0},
		{"answered", func(context.Context, map[string]any) (*Plan, error) {
			return answered(toJSON(map[string]any{"status": "already_deleted"}), false), nil
		}, true, "already_deleted", false, 0},
		{"preview", func(context.Context, map[string]any) (*Plan, error) {
			return &Plan{Preview: map[string]any{"message": "x"}, Execute: exec}, nil
		}, false, "confirmation_required", true, 0},
		{"consented", func(context.Context, map[string]any) (*Plan, error) {
			return &Plan{Preview: map[string]any{"message": "x"}, Execute: exec}, nil
		}, true, "deleted", false, 1},
	}
	for _, c := range cases {
		ran = 0
		text, isErr := r.gated("tg_delete", c.plan)(context.Background(), map[string]any{"id": "x", "confirmed": c.confirmed})
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatalf("%s: %s", c.name, text)
		}
		if m["status"] != c.status || isErr != c.isErr || ran != c.runs {
			t.Errorf("%s: status %v isErr %v runs %d, want %s %v %d", c.name, m["status"], isErr, ran, c.status, c.isErr, c.runs)
		}
	}
}

// A call that needs no consent (k8s_kubectl with a read verb) runs with no
// question (NeedsConsent).
func TestGate_NoConsentNeeded(t *testing.T) {
	r := New(nil)
	ran := false
	h := r.gated("k8s_kubectl", func(context.Context, map[string]any) (*Plan, error) {
		return &Plan{Preview: map[string]any{}, Execute: func(context.Context) (string, bool) { ran = true; return toJSON(map[string]any{"status": "ok"}), false }}, nil
	})
	if _, isErr := h(context.Background(), map[string]any{"cluster_id": "c", "args": "get pods"}); isErr || !ran {
		t.Errorf("a read verb asked for consent: isErr %v ran %v", isErr, ran)
	}
}
