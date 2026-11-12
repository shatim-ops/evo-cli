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
	// PreviewOf builds the preview when the question is asked, in place
	// of Preview: for a preview that costs a request the execution does
	// not need (the price), so a consented call does not make it. An
	// error refuses the call, as the planner's. It only builds the preview
	// (the price, the quota, the names): every refusal of the call itself
	// is the planner's — one made here would be skipped silently by a call
	// with confirmed=true (conclusion 119 of «Риск»).
	PreviewOf func(ctx context.Context) (map[string]any, error)
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
			preview := p.Preview
			if p.PreviewOf != nil {
				if preview, err = p.PreviewOf(ctx); err != nil {
					return errResult(err), true
				}
			}
			out := map[string]any{"status": "confirmation_required"}
			for k, v := range preview {
				out[k] = v
			}
			return toJSON(out), true
		}
		return p.Execute(ctx)
	}
}

// engineGated is the gate of a tool the engine drives itself (deploy, the
// destroy of the deployment): the Caller of a CLI command asks for consent
// where the engine reaches its gate (Caller.Gate: live), after the checks
// the engine makes first; any other call gates here on confirmed —
// without it, preview answers; with it, execute runs.
func (r *Registry) engineGated(ctx context.Context, name string, args map[string]any,
	live func(*Caller) (string, bool), preview, execute func() (string, bool)) (string, bool) {
	if c := liveCaller(ctx); c != nil {
		// The engine asks at its own gate: the intent is written on the
		// yes, before the engine writes anything (Caller.ask); a call with
		// confirmed has it from intended already.
		var end func(string)
		prev := c.consent
		c.consent = func() error {
			if confirmed, _ := args["confirmed"].(bool); end != nil || confirmed {
				return nil
			}
			e, err := r.beginIntent(ctx, name, args)
			end = e
			return err
		}
		defer func() { c.consent = prev }()
		text, isErr := live(c)
		if end != nil {
			end(text)
		}
		return text, isErr
	}
	t, _ := r.Lookup(name)
	if confirmed, _ := args["confirmed"].(bool); !confirmed && NeedsConsent(t, args) {
		return preview()
	}
	return execute()
}

// intended is the handler of a tool that takes consent, under its intent:
// a call with confirmed=true that needs consent writes it before the
// handler runs — whether the handler gates on a planner (gated) or reads
// confirmed itself — so no consented call reaches the cloud unrecorded.
// One layer for every such tool, set in New (release plan 1.8, (и)).
func (r *Registry) intended(t Tool, next Handler) Handler {
	if !takesConsent(t) {
		return next
	}
	return func(ctx context.Context, args map[string]any) (string, bool) {
		// A step of a stack is a call one level deeper (journaled): the
		// intent of the call the person consented to covers it, and the
		// job of the stack records its steps.
		depth, _ := ctx.Value(journalDepthKey{}).(int)
		if confirmed, _ := args["confirmed"].(bool); !confirmed || !NeedsConsent(t, args) || depth > 1 {
			return next(ctx, args)
		}
		end, err := r.beginIntent(ctx, t.Name, args)
		if err != nil {
			return errResult(err), true
		}
		text, isErr := next(ctx, args)
		end(text)
		return text, isErr
	}
}
