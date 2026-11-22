// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/google/uuid"
	"maps"
	"sync"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/state"
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
	// with confirmed=true (docs/internal/risk/1.9.md, conclusion 119).
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

type placementIntentStartKey struct{}

// startPlacementIntent records the accepted change before execution. Re-showing a
// placement (including a paused stack) never opens a second cloud intent.
func startPlacementIntent(ctx context.Context) error {
	if start, ok := ctx.Value(placementIntentStartKey{}).(func() error); ok {
		return start()
	}
	return nil
}

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
	run := r.recordedCall(t, next)
	step := next
	if t.Decl.intentGate.checkProjectFirst {
		// Check the zone before resolving or replaying the record's intent.
		run = r.boundToProjects(t, run)
		step = r.boundToProjects(t, step)
	}
	return func(ctx context.Context, args map[string]any) (string, bool) {
		// A step of a stack is a call one level deeper (journaled): the
		// intent of the call the person consented to covers it, and the
		// job of the stack records its steps.
		depth, _ := ctx.Value(journalDepthKey{}).(int)
		if depth > 1 {
			return step(ctx, args)
		}
		// The holder of what the call reads and says of its records: the
		// call of a tool has it (budgeted); a layer called on its own gets
		// its own, so the mark of its first write is heard (markSent).
		if _, ok := ctx.Value(callRecordsKey{}).(*callRecords); !ok {
			ctx = context.WithValue(ctx, callRecordsKey{}, &callRecords{})
		}
		// No directory for the record of the call: no question is asked
		// about a call that would be refused, and none is sent.
		if r.stateDirUndeclared() {
			return errResult(errNoStateDir()), true
		}
		return run(ctx, args)
	}
}

func (r *Registry) recordedCall(t Tool, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		// Serialize the check and opening of network create intents across
		// processes sharing this state directory. Release before the POST:
		// a second client can read the in-flight record while it is running.
		// VPC name checks can finish the call before it needs an intent.
		// Hold the same create lock through planning and opening the record
		// so another caller can observe this send before making its own.
		policy := t.Decl.intentGate
		var release func()
		if confirmed, _ := args["confirmed"].(bool); confirmed && (policy.serialize || everySendIsAChange[t.Name].listedName != nil) && r.newEngine != nil {
			unlock, err := r.lockNetworkIntents(t.Name)
			if err != nil {
				return errResult(err), true
			}
			release = sync.OnceFunc(unlock)
			defer release()
			forgetPending(ctx)
		}
		// The same create before was taken and goes on: its progress is
		// read, with or without consent, and no second create is sent.
		if text, isErr, ok := r.repeatOfMade(ctx, t.Name, args); ok {
			return text, isErr
		}
		if confirmed, _ := args["confirmed"].(bool); !confirmed || !NeedsConsent(t, args) {
			text, isErr := next(ctx, args)
			r.settleHeld(ctx, t.Name, args, text)
			text = r.questionNamesTheCut(ctx, t.Name, args, text)
			return r.namedKept(ctx, t.Name, args, text), isErr
		}
		// The writes of the call are counted, the first marked in its
		// record before it goes out: a call cut before it sent one changed
		// nothing («Риск», conclusion 187, У2).
		holder := ctx
		var startBeforeWrite func() error
		ctx = httpretry.CountWrites(ctx, func() (bool, error) {
			if startBeforeWrite != nil {
				if err := startBeforeWrite(); err != nil {
					return false, err
				}
			}
			return markSent(holder, func() []string { return r.partiesOf(holder, t.Name, args) })
		})
		// The resource the call names cannot be read: nothing is sent,
		// and no record is written by the spelling — a cut call of the
		// same resource spelled another way would not be found («Риск»,
		// conclusion 177, В1).
		if _, _, err := r.resourceOf(ctx, t.Name, args); err != nil {
			return errResult(err), true
		}
		if text, asked := r.askAgainAfterACut(ctx, t.Name, args, func(ctx context.Context) (string, bool) {
			return next(ctx, withoutConsent(args))
		}); asked {
			return text, true
		}
		// The operation of this call goes on under its held record: a
		// repeat with consent is a step of it, not a new call — a new
		// record would take the place of the first and move the start
		// the limit of the repeats counts from («Риск», conclusion 177,
		// У7).
		var end func(string)
		deferIntent := policy.beforeSend != nil && policy.beforeSend(args)
		if held, goesOn := r.goesOn(ctx, t.Name, args); goesOn {
			end = func(answer string) { cutSendOf(ctx, held, answer) }
		} else if deferIntent {
			startBeforeWrite = sync.OnceValue(func() error {
				var err error
				end, err = r.beginIntent(ctx, t.Name, args)
				if release != nil {
					release()
				}
				return err
			})
			ctx = context.WithValue(ctx, placementIntentStartKey{}, startBeforeWrite)
		} else {
			var err error
			if end, err = r.beginIntent(ctx, t.Name, args); err != nil {
				return errResult(err), true
			}
		}
		if release != nil && !deferIntent {
			release()
		}
		// A handler that panics does not return: the server recovers it,
		// and the record would read as a call still going on (InFlight)
		// for the life of the server. It ends with its outcome unknown
		// («Риск», conclusion 401, У1).
		returned := false
		defer func() {
			if !returned && end != nil {
				end(toJSON(map[string]any{"status": "error", "reason": clierrors.ReasonInterrupted,
					"error": i18n.T("the call stopped before it returned", "вызов остановился, не вернувшись")}))
			}
		}()
		text, isErr := next(ctx, args)
		returned = true
		// The mark of the first write could not be written: nothing went
		// out, and the call says that, not the handler's error of a request
		// that never left.
		if err := httpretry.WriteRefused(ctx); err != nil {
			text, isErr = errResult(err), true
		}
		if end != nil {
			end(text)
		}
		// A held record the call names by another argument than its step
		// (a name, where the record has the id).
		r.settleHeld(ctx, t.Name, args, text)
		return r.namedKept(ctx, t.Name, args, text), isErr
	}
}

// withoutConsent is the same call without the consent it was given: the
// question it answers is the preview of what the call would do.
func withoutConsent(args map[string]any) map[string]any {
	out := maps.Clone(args)
	delete(out, "confirmed")
	return out
}

// nextSameCall puts into answer the same call as its next step (nextStep)
// — without the consent this call was given. The step is a wait: it reads
// the resource and sends nothing, so it needs no consent; and consent
// carried into next_args would let an agent that follows them send the
// change again with no question, once the resource is back where the
// change started (a start that fell back between two calls: R-85; «Риск»,
// conclusion 123, К1; the audit of the reference vm_start). The one place
// a next step is made of the arguments of the call: a new operation names
// its next step through it, not through a copy of args.
func nextSameCall(answer map[string]any, tool string, args map[string]any) {
	next := maps.Clone(args)
	delete(next, "confirmed")
	nextStep(answer, tool, next)
}

// submissionError runs under the project upgrade lock. Only the response to
// this POST can retire its reservation. A fresh refusal ID invalidates offers
// issued to all clients before the rejection, including unused offers.
func (r upgradeReceipt) submissionError(path string, current upgradeCard, err error) (string, bool) {
	var response *mk8s.UpgradeHTTPError
	if !errors.As(err, &response) || !intent.DefiniteRefusal(response.Status) {
		return errAnswer(err, upgradeResult(r, current, "outcome_unknown")), true
	}
	id, genErr := uuid.NewRandom()
	if genErr != nil {
		return errResult(errors.Join(err, genErr)), true
	}
	r.RejectionID = id.String()
	if saveErr := r.save(path); saveErr != nil {
		return errResult(errors.Join(err, saveErr)), true
	}
	return errAnswer(err, upgradeResult(r, current, "rejected")), true
}

// Reserving the cloud write and consuming a matching offer happen under the
// same local lock. A different client cannot use another client's preview.
func reserveUpgrade(ctx context.Context, offerPath, receiptPath string, mcpCall, contextChanged bool, r upgradeReceipt, suppliedID string, preview map[string]any) (bool, error) {
	unlock, err := state.FileLock(offerPath+".lock", 5*time.Second)
	if err != nil {
		return false, err
	}
	defer unlock()
	shown, err := readUpgradeReceipt(offerPath)
	if err != nil {
		return false, err
	}
	validID := false
	if shown != nil {
		validID = suppliedID != "" && shown.PreviewID == suppliedID
		shown.PreviewID = "" // Compare the transition separately from its one-time approval.
	}
	if contextChanged || (mcpCall && !validID) || (shown != nil && *shown != r) {
		return false, r.offer(offerPath, mcpCall, preview)
	}

	if err := startPlacementIntent(ctx); err != nil {
		return false, err
	}
	r.RejectionID = "" // An interrupted submission must remain unknown.
	if err := r.save(receiptPath); err != nil {
		return false, err
	}
	if shown != nil {
		if err := os.Remove(offerPath); err != nil {
			return false, err
		}
	}
	return true, nil
}
