// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
)

// The gate of consent is one function of the registry (owner's decision
// 24.10, D35): planner → gate → execution. Where each handler read
// confirmed itself, the checks it made after that read were made after
// the question (G13-3, class K1): a call bound to fail asked for consent
// first. A planner finds out, without a write, everything the call can
// know — the form and bounds of its arguments are CheckArgs' (D36), the
// resources it names and their state are the planner's — and answers one
// of three: a refusal (its error), an answer with nothing to consent to
// (a delete of what is not there, D26), or the preview of what the
// execution will do. The gate asks for consent on the preview and runs the
// execution only with it; the handler never sees confirmed. This file is
// the one place production code reads confirmed (internal/testrules,
// TestConfirmedReadOnlyInTheGate).

// Plan is what a call of a gated tool will do, found out without a write.
type Plan struct {
	// Preview is the answer the consent is asked on, without its status:
	// the gate answers it as confirmation_required.
	Preview map[string]any
	// Execute makes the call; the gate runs it only with consent.
	Execute func(ctx context.Context) (string, bool)
	// answer is the whole answer of a call with nothing to consent to.
	answer    string
	answerErr bool
}

// answered is the plan of a call with nothing to consent to: its answer,
// given with no question whatever confirmed says.
func answered(text string, isErr bool) *Plan {
	return &Plan{answer: text, answerErr: isErr}
}

// Planner plans a call of a gated tool. An error refuses the call before
// the question.
type Planner func(ctx context.Context, args map[string]any) (*Plan, error)

// gated is the handler of tool name on its planner: the plan first, then
// the question when this call needs consent (NeedsConsent), then the
// execution.
func (r *Registry) gated(name string, plan Planner) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		p, err := plan(ctx, args)
		if err != nil {
			return errResult(err), true
		}
		if p.Execute == nil {
			return p.answer, p.answerErr
		}
		t, _ := r.Lookup(name)
		if confirmed, _ := args["confirmed"].(bool); !confirmed && NeedsConsent(t, args) {
			out := map[string]any{"status": "confirmation_required"}
			for k, v := range p.Preview {
				out[k] = v
			}
			return toJSON(out), true
		}
		return p.Execute(ctx)
	}
}
