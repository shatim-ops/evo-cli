// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/stack"
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

func lbDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// approvalDigest keeps previously displayed offers compatible. It binds the exact
// POST body and resolved zone to the caller and the interrupted attempts shown.
func (p *lbPlacement) approvalDigest(interruptedIDs []string) (string, error) {
	if p.config.KeyID == "" && p.config.ServiceAccountID == "" {
		return "", usageErr("Cannot identify the caller for load balancer approval.", "Не удалось определить учётную запись для согласования балансировщика.")
	}
	terms, err := json.Marshal([]any{
		p.body, p.zoneID, p.config.Profile, p.config.ServiceAccountID,
		lbDigest([]byte(p.config.KeyID)), interruptedIDs,
	})
	if err != nil {
		return "", err
	}
	return lbDigest(terms), nil
}

// planLbCreate binds each way to execute the paid request to its approved placement.
// Both a direct call and a resumed stack open the intent only on that transition.
func (s *Registry) planLbCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	plan, placement, err := s.prepareLBCreate(ctx, args)
	if err != nil || plan.Execute == nil {
		return plan, err
	}
	preview := plan.Preview
	cuts := s.cutCalls(ctx, "lb_create", args)
	var ids []string
	if len(cuts) > 0 {
		message, warn := interruptedWarning(ctx, "lb_create", args, cuts)
		preview["interrupted"] = warn
		preview["message"] = message + " " + stringArg(preview, "message")
		for _, cut := range cuts {
			ids = append(ids, cut.ID)
		}
		slices.Sort(ids)
	}
	digest, err := placement.approvalDigest(ids)
	if err != nil {
		return nil, err
	}
	send := func(ctx context.Context) (string, bool) {
		if err := startPlacementIntent(ctx); err != nil {
			return errResult(err), true
		}
		return plan.Execute(ctx)
	}
	caller := callerOf(ctx)
	session, httpCall := ctx.Value(networkSessionKey{}).(string)
	scope := "stdio-" + s.upgradeClient
	if caller != nil {
		scope = "cli"
	} else if httpCall {
		if session == "" && ctx.Value(lbJobKey{}) == nil {
			return nil, clierrors.New(clierrors.CodeConfirmationRequired, i18n.T("An initialized MCP session is required.", "Нужна открытая сессия MCP."), i18n.T("Call initialize, send Mcp-Session-Id, then request a preview.", "Вызовите initialize, передайте Mcp-Session-Id и запросите превью."))
		}
		scope = "http-" + session
	}
	if job, _ := ctx.Value(lbJobKey{}).(*stack.Job); job != nil {
		root, ok := s.stateDir()
		if !ok {
			return nil, errNoStateDir()
		}
		jobPath := filepath.Join(root, "lb-previews", "stack-"+lbDigest([]byte(job.ID))+".json")
		if capture, _ := ctx.Value(lbPinKey{}).(bool); capture {
			unlock, err := state.FileLock(jobPath+".lock", 5*time.Second)
			if err != nil {
				return nil, err
			}
			defer unlock()
			pin, err := config.ReadFileNoFollow(jobPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if len(pin) == 0 {
				if err := config.WriteDurableReceipt(jobPath, []byte(digest)); err != nil {
					return nil, err
				}
			}
			return &Plan{Preview: preview}, nil
		}
		if job.LBPending == nil {
			pin, err := config.ReadFileNoFollow(jobPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if string(pin) == digest {
				return &Plan{Preview: preview, Execute: send}, nil
			}
		}
		scope = scope + ":stack:" + lbDigest([]byte(job.ID))
		if job.LBUncertain {
			warning := i18n.T("The preceding LB submission may already be billed. Inspect lb_list and the operation before explicitly approving a second creation.", "Предыдущее создание могло привести к оплате. Проверьте lb_list и операцию перед явным согласием на повторное создание.")
			preview["warning"] = warning
			human := preview["preview"].(engine.CreatePreview)
			human.Warnings = append(human.Warnings, warning)
			preview["preview"] = human
			digest = lbDigest([]byte("uncertain:" + digest))
		}
		if parent := ctx.Value(lbStackCLIKey{}); parent != nil {
			scope = "cli:stack:" + lbDigest([]byte(job.ID))
		}
		caller = nil // A paused stack always needs the actual offer returned to the operator.
		will, _ := preview["will_create"].(map[string]any)
		planned := job.LBPlacement
		if planned != nil && job.LBPending == nil && planned.Project == will["project_id"] && planned.VPC == will["vpc_id"] && planned.Zone == will["zone_id"] && planned.Subnet == will["subnet_id"] && planned.Name == will["name"] && planned.External == will["external"] && planned.Port == pgInt(args, "port", 0) && planned.TargetPort == pgInt(args, "target_port", pgInt(args, "port", 0)) {
			if _, cut := preview["interrupted"]; !cut {
				return &Plan{Preview: preview, Execute: send}, nil
			}
		}
	}
	if httpCall && session == "" {
		preview["status"] = "confirmation_required"
		preview["hint"] = i18n.T("Initialize an MCP session and request stack_apply for this job.", "Откройте сессию MCP и запросите stack_apply для этого задания.")
		return &Plan{Preview: preview, PreviewOf: func(context.Context) (map[string]any, error) { return preview, nil }, Execute: func(context.Context) (string, bool) { return toJSON(preview), true }}, nil
	}
	root, ok := s.stateDir()
	if !ok {
		return nil, errNoStateDir()
	}
	path := filepath.Join(root, "lb-previews", lbDigest([]byte(scope))+".json")
	type offer struct{ Digest, ID string }
	lock := func() (func(), error) { return state.FileLock(path+".lock", 5*time.Second) }
	read := func() (o offer, err error) {
		b, e := config.ReadFileNoFollow(path)
		if errors.Is(e, os.ErrNotExist) {
			return o, nil
		}
		if e != nil {
			return o, e
		}
		err = json.Unmarshal(b, &o)
		return
	}
	show := func() error {
		id, e := uuid.NewRandom()
		if e != nil {
			return e
		}
		b, e := json.Marshal(offer{digest, id.String()})
		if e != nil {
			return e
		}
		if e = config.WriteDurableReceipt(path, b); e != nil {
			return e
		}
		if caller == nil {
			preview["preview_id"] = id.String()
			preview["approval"] = i18n.T("Show this placement to the person. After explicit approval send its preview_id with confirmed=true in this session.", "Покажите размещение человеку. После явного согласия передайте preview_id с confirmed=true в этой сессии.")
		}
		return nil
	}
	return &Plan{Preview: preview, PreviewOf: func(context.Context) (map[string]any, error) {
		unlock, e := lock()
		if e != nil {
			return nil, e
		}
		defer unlock()
		if caller != nil {
			caller.lbDigest = digest
			if caller.AutomaticPreview {
				o, e := read()
				if e != nil {
					return nil, e
				}
				if o.Digest == "" || o.Digest == digest {
					return preview, nil
				}
				caller.RequireNewApproval = true
				return preview, nil
			}
		}
		return preview, show()
	}, Execute: func(ctx context.Context) (string, bool) {
		unlock, e := lock()
		if e != nil {
			return errResult(e), true
		}
		defer unlock()
		o, e := read()
		if e != nil {
			return errResult(e), true
		}
		valid := o.Digest == digest && o.ID == stringArg(args, "preview_id") && o.ID != ""
		if caller != nil {
			valid = caller.lbDigest == digest && (o.Digest == "" || o.Digest == digest)
		}
		if !valid {
			if e := show(); e != nil {
				return errResult(e), true
			}
			preview["status"] = "confirmation_required"
			return toJSON(preview), true
		}
		if o.ID != "" {
			if e := os.Remove(path); e != nil {
				return errResult(e), true
			}
		}
		return send(ctx)
	}}, nil
}

// planSnatCreate creates a SNAT gateway so a private subnet's nodes can reach
// the internet (pull public images, apt, outbound calls). Gated on confirmed.
func (s *Registry) planSnatCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	if name == "" {
		return answered(usageT("'name' is required", "нужен 'name'"), true), nil
	}
	if err := provision.ValidName(i18n.T("SNAT gateway", "SNAT-шлюза"), name); err != nil {
		return nil, err
	}
	placement, err := s.snatPlacement(ctx, args)
	if err != nil {
		return nil, err
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	plan, err := prepareSNATGateway(ctx, eng, name, placement.id, placement.name)
	if err != nil || plan.Execute == nil {
		return plan, err
	}
	return s.bindSNATPlacement(ctx, eng, plan, name, placement)
}

// bindSNATPlacement remembers the UUID displayed for a named gateway. A
// first confirmed call needs no offer. The CLI's implicit --yes planning
// pass must not overwrite an earlier offer before comparing the UUID.
func (s *Registry) bindSNATPlacement(ctx context.Context, eng *engine.Engine, plan *Plan, name string, placement snatPlacement) (*Plan, error) {
	root, ok := s.stateDir()
	if !ok {
		return nil, errNoStateDir()
	}
	execute := plan.Execute
	send := func(ctx context.Context) (string, bool) {
		if err := startPlacementIntent(ctx); err != nil {
			return errResult(err), true
		}
		return execute(ctx)
	}
	if placement.ref == "" {
		plan.Execute = send
		return plan, nil
	}
	caller := callerOf(ctx)
	// A retry can arrive on a new connection or after a process restart.
	// Share the offer only within the same credential, project and gateway.
	terms, _ := json.Marshal([]string{eng.Config.ProjectID, eng.Config.Profile, eng.Config.ServiceAccountID, eng.Config.KeyID, name})
	path := filepath.Join(root, "snat-previews", lbDigest(terms)+".json")
	type offer struct {
		ID      string `json:"id"`
		Changed bool   `json:"changed,omitempty"`
	}
	check := func(show bool) (bool, error) {
		unlock, err := state.FileLock(path+".lock", 5*time.Second)
		if err != nil {
			return false, err
		}
		defer unlock()
		var saved offer
		b, err := config.ReadFileNoFollow(path)
		if err == nil {
			if json.Unmarshal(b, &saved) != nil || !isUUID(saved.ID) {
				return false, clierrors.New(clierrors.CodeStateCorrupted, i18n.T("Cannot read the displayed SNAT placement.", "Не удалось прочитать сохранённое размещение SNAT."), "")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		changed := saved.Changed || (saved.ID != "" && saved.ID != placement.id)
		if caller != nil && !show && caller.snatZoneID != "" && caller.snatZoneID != placement.id {
			changed = true
		}
		if (show && saved.ID == "" && (caller == nil || !caller.AutomaticPreview)) || (changed && !saved.Changed) {
			if saved.ID == "" {
				saved.ID = placement.id
			}
			saved.Changed = changed
			b, _ := json.Marshal(saved)
			if err := config.WriteDurableReceipt(path, b); err != nil {
				return false, err
			}
		}
		if caller != nil {
			if show {
				caller.snatZoneID = placement.id
				caller.RequireNewApproval = changed && caller.AutomaticPreview
			} else if !caller.AutomaticPreview && caller.snatZoneID == placement.id {
				// The CLI has just asked the person about this exact UUID.
				return false, nil
			}
		}
		return changed, nil
	}
	markChanged := func() {
		message := i18n.T("The zone placement changed. Show the new UUID to the person. After fresh approval, pass will_create.availability_zone_id as availability_zone_id with confirmed=true.", "Размещение зоны изменилось. Покажите человеку новый UUID. После нового согласия передайте will_create.availability_zone_id как availability_zone_id вместе с confirmed=true.")
		if caller != nil {
			message = i18n.T("The zone placement changed. Review the new UUID and confirm it at the terminal, or pass the approved --availability-zone-id.", "Размещение зоны изменилось. Проверьте новый UUID и подтвердите его в терминале или передайте одобренный --availability-zone-id.")
		}
		plan.Preview["message"] = message
		human := plan.Preview["preview"].(engine.CreatePreview)
		human.Warnings = append(human.Warnings, message)
		plan.Preview["preview"] = human
	}
	return &Plan{Preview: plan.Preview, PreviewOf: func(context.Context) (map[string]any, error) {
		changed, err := check(true)
		if changed {
			markChanged()
		}
		return plan.Preview, err
	}, Execute: func(ctx context.Context) (string, bool) {
		changed, err := check(false)
		if err != nil {
			return errResult(err), true
		}
		if changed {
			markChanged()
			plan.Preview["status"] = "confirmation_required"
			return toJSON(plan.Preview), true
		}
		return send(ctx)
	}}, nil
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
	if t.Name == "dns_record_create" {
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
		vpcCreate := t.Name == "vpc_create"
		var release func()
		if confirmed, _ := args["confirmed"].(bool); confirmed && (vpcCreate || t.Name == "lb_create" || everySendIsAChange[t.Name].listedName != nil) && r.newEngine != nil {
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
		deferIntent := vpcCreate || t.Name == "lb_create" || t.Name == "snat_create"
		if t.Name == "stack_apply" {
			job, err := stack.LoadJob(stringArg(args, "job_id"))
			deferIntent = err == nil && jobHasPendingLB(job)
		}
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
