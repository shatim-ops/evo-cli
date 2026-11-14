// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/s3"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// --- stack orchestrator: crank-driven, disk-persisted multi-resource deploys ---
//
// The heavy execution loop lives HERE (server-side), not in the agent's context.
// The agent calls stack_apply once, then polls stack_status; each poll is one
// "crank" that advances the persisted state machine as far as it can and returns
// a COMPACT status. Agent context stays O(1) regardless of stack size — no
// trajectory/context overflow, LLM-agnostic, resumable after context loss.

// callTool invokes an existing MCP tool handler in-process and parses its JSON.
// This reuses every typed operation (pg_create, provision, tg_create, …) with
// zero duplication — the orchestrator drives the same tools the agent would.
func (s *Registry) callTool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	h, ok := s.handlers[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	text, isErr := h(stepOf(ctx), args)
	var out map[string]any
	if e := json.Unmarshal([]byte(text), &out); e != nil {
		return nil, fmt.Errorf("%s: unparseable result: %s", name, text)
	}
	if isErr {
		msg, _ := out["error"].(string)
		if msg == "" {
			msg = text
		}
		return out, fmt.Errorf("%s: %s", name, msg)
	}
	return out, nil
}

// substituteRefs replaces "$stepkey" string arg values with the produced output.
func substituteRefs(args map[string]any, outputs map[string]string) map[string]any {
	res := make(map[string]any, len(args))
	for k, v := range args {
		if sv, ok := v.(string); ok && len(sv) > 1 && sv[0] == '$' {
			if id, ok := outputs[sv[1:]]; ok {
				res[k] = id
				continue
			}
		}
		res[k] = v
	}
	return res
}

func depsDone(step *stack.Step, job *stack.Job) bool {
	for _, d := range step.DependsOn {
		found := false
		for _, s := range job.Steps {
			if s.Key == d {
				found = s.Status == "done"
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// findIDByName resolves a resource id from a list tool by matching name. extra
// carries any params the list tool needs (e.g. dns_records needs zone_id).
func (s *Registry) findIDByName(ctx context.Context, tool, field, name string, extra map[string]any) string {
	args := map[string]any{}
	for k, v := range extra {
		args[k] = v
	}
	res, err := s.callTool(ctx, tool, args)
	if err != nil {
		return ""
	}
	arr, _ := res[field].([]any)
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			// A deleted namesake is not the step's resource (as takenName).
			if statusGone(fmt.Sprint(m["status"])) {
				continue
			}
			if n, _ := m["name"].(string); n == name {
				id, _ := m["id"].(string)
				return id
			}
		}
	}
	return ""
}

// stepFind is how to find what a step makes by its name: the list tool of
// its create and the array of the answer (Decl.Created), the name the step
// gives it (Decl.CreateName) and the create's arguments the list takes too.
type stepFind struct {
	list, field, name string
	args              map[string]any
}

// stepFinder is the stepFind of st; ok is false for a create not found by
// name (its Decl.Created declare no list).
func stepFinder(st *stack.Step) (f stepFind, ok bool) {
	c := decls[st.Tool].Created
	n := decls[st.Tool].CreateName
	if c.list == "" || len(n.args) == 0 {
		return stepFind{}, false
	}
	f = stepFind{list: c.list, field: c.field, name: stringArg(st.Args, n.args[0]), args: map[string]any{}}
	for _, a := range c.listArgs {
		f.args[a] = st.Args[a]
	}
	return f, f.name != ""
}

// findStepID is the id of a live resource named as the step's; "" when
// there is none, or the step's create is not found by name.
func (s *Registry) findStepID(ctx context.Context, st *stack.Step) string {
	f, ok := stepFinder(st)
	if !ok {
		return ""
	}
	return s.findIDByName(ctx, f.list, f.field, f.name, f.args)
}

// takenByName is the id of a live resource named as the step's, from its
// list tool; an error when the list could not be read (then nothing is
// known and nothing may be created yet).
func (s *Registry) takenByName(ctx context.Context, st *stack.Step) (string, error) {
	f, _ := stepFinder(st)
	res, err := s.callTool(ctx, f.list, f.args)
	if err != nil {
		return "", err
	}
	arr, ok := res[f.field].([]any)
	if !ok {
		return "", fmt.Errorf("%s answered without %q", f.list, f.field)
	}
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n != f.name {
			continue
		}
		if statusGone(fmt.Sprint(m["status"])) {
			continue
		}
		id, _ := m["id"].(string)
		return firstNonEmptyStr(id, f.name), nil
	}
	return "", nil
}

// vmClockSkew is how much earlier than the step's start a VM's creation time
// may read and still be the step's own (cloud and local clocks differ).
const vmClockSkew = 2 * time.Minute

// findStepResource looks up what a step may have created when its create
// answer is unknown. A VM (provision) is the step's own only when its name
// matches AND it was created after the step started: the name alone could
// be a user's VM, which stack_destroy would then delete (QA R3/R8 class).
// state is the VM's state ("" for other kinds). Steps that never started,
// or VMs without a creation time, are not adopted. A step that holds the
// id of its resource finds that one or nothing: another of the same name
// is not the step's (Астра, #1766).
func (s *Registry) findStepResource(ctx context.Context, st *stack.Step) (id, state string) {
	f, ok := stepFinder(st)
	if !ok {
		return "", ""
	}
	if st.Tool != "provision" {
		id := s.findIDByName(ctx, f.list, f.field, f.name, f.args)
		if st.ResourceID != "" && id != st.ResourceID {
			return "", ""
		}
		return id, ""
	}
	if st.StartedAt.IsZero() {
		return "", ""
	}
	res, err := s.callTool(ctx, f.list, f.args)
	if err != nil {
		return "", ""
	}
	arr, _ := res[f.field].([]any)
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n != f.name {
			continue
		}
		if vid, _ := m["id"].(string); st.ResourceID != "" && vid != st.ResourceID {
			continue
		}
		created, _ := m["created_time"].(string)
		t, err := time.Parse(time.RFC3339, created)
		if err != nil || t.Before(st.StartedAt.Add(-vmClockSkew)) {
			continue
		}
		vid, _ := m["id"].(string)
		vstate, _ := m["state"].(string)
		return vid, vstate
	}
	return "", ""
}

// adoptVM finishes a provision step with a VM found by findStepResource: an
// active VM completes the step; any other state (creating, error_creating)
// is recorded for stack_destroy and stops the job — it is billed, and
// marking it done would hand a broken VM to the steps after it.
func (s *Registry) adoptVM(ctx context.Context, job *stack.Job, st *stack.Step, id, state string) {
	switch strings.ToLower(state) {
	case "running", "active":
		s.finalizeStep(ctx, job, st, id)
	default:
		st.ResourceID = id
		failStep(job, st, clierrors.CodeVMProvisionFailed, fmt.Sprintf("%s: VM %s found in state %q (kept for stack_destroy)", st.Key, id, state))
	}
}

// failStep ends a step and its job in error: msg is the job's error, reason
// the class of what ended it (a code of internal/errors; "" — the tool
// answered none — is API_ERROR), which stack_status names in error_reasons
// (G140-5). Every step that ends in error ends here.
func failStep(job *stack.Job, st *stack.Step, reason, msg string) {
	if reason == "" {
		reason = clierrors.CodeAPIError
	}
	st.Status, st.Reason = "error", reason
	job.Error, job.Status = msg, "error"
}

// maxStepAttempts bounds create retries per step (transient-error tolerance).
const maxStepAttempts = 4

// stackCrankInterval is the pause between cranks inside one stack_status
// call. A variable only so that tests can shorten it.
var stackCrankInterval = 8 * time.Second

// isTerminalFail reports whether a resource status means it will never become
// ready (a failed create) — so the orchestrator stops waiting and fails the job
// instead of polling forever. Covers pg/kafka/redis (FAILED/ERASED) and k8s.
func isTerminalFail(status string) bool {
	u := strings.ToUpper(status)
	return strings.Contains(u, "FAIL") || strings.Contains(u, "ERROR") || strings.Contains(u, "ERASED")
}

// finalizeStep transitions a step to its resolved state given a resource id that
// was either just created or ADOPTED during reconcile (create response failed but
// the resource exists). ReadyKind decides whether it's done or still needs a wait.
func (s *Registry) finalizeStep(ctx context.Context, job *stack.Job, st *stack.Step, id string) {
	st.ResourceID = id
	switch st.ReadyKind {
	case "status":
		st.Status = "waiting" // resource exists; poll its status on later cranks
	case "op":
		job.Outputs[st.Key] = id
		if st.Key == "lb" && id != "" {
			if lb, e := s.callTool(ctx, "lb_get", map[string]any{"id": id}); e == nil {
				if ext, ok := lb["external_address"].(map[string]any); ok {
					if ip, _ := ext["ipv4"].(string); ip != "" {
						job.Outputs["lb_external_ip"] = ip
					}
				}
			}
		}
		st.Status = "done"
	default:
		job.Outputs[st.Key] = id
		st.Status = "done"
	}
}

// advance is ONE crank: poll in-flight async steps and start ready pending steps,
// looping until no further progress is possible this turn. Persists nothing (the
// caller saves). Returns quickly — long async waits (PG READY) span cranks.
func (s *Registry) advance(ctx context.Context, job *stack.Job) {
	for progress := true; progress; {
		progress = false
		for _, st := range job.Steps {
			if ctx.Err() != nil {
				// The call was cancelled: every further tool call would fail
				// on the context, burn an attempt and be reported as a cloud
				// error. The job is saved as it is; the next crank goes on.
				return
			}
			switch st.Status {
			case "done", "error":
				continue

			case "waiting":
				if st.ReadyKind == "op" {
					if st.OpID == "" {
						failStep(job, st, clierrors.CodeAPIError, st.Key+": missing operation id")
						return
					}
					op, err := s.callTool(ctx, st.PollTool, map[string]any{"operation_id": st.OpID})
					if err != nil {
						// Transient poll failure — don't kill the job; retry next crank.
						st.Detail = "poll: " + err.Error()
						continue
					}
					if done, _ := op["done"].(bool); done {
						// A completed operation can still carry an error (create
						// failed server-side). Treat that as terminal, don't proceed.
						if oe, ok := op["error"].(map[string]any); ok && oe != nil {
							msg, _ := oe["message"].(string)
							failStep(job, st, clierrors.CodeAPIError, fmt.Sprintf("%s: operation failed: %v", st.Key, msg))
							return
						}
						// The name finds what the operation made when the
						// create's answer named nothing; a step that holds its
						// id keeps it — another of the name is not the step's
						// («Риск», 222, У2).
						if st.ResourceID == "" {
							st.ResourceID = s.findStepID(ctx, st)
						}
						id := st.ResourceID
						job.Outputs[st.Key] = id
						if st.Key == "lb" && id != "" {
							if lb, e := s.callTool(ctx, "lb_get", map[string]any{"id": id}); e == nil {
								if ext, ok := lb["external_address"].(map[string]any); ok {
									if ip, _ := ext["ipv4"].(string); ip != "" {
										job.Outputs["lb_external_ip"] = ip
									}
								}
							}
						}
						st.Status = "done"
						progress = true
					}
				} else if st.ReadyKind == "status" {
					if st.ResourceID == "" {
						st.ResourceID = s.findStepID(ctx, st)
					}
					if st.ResourceID != "" {
						cl, err := s.callTool(ctx, st.StatusTool, map[string]any{st.StatusArg: st.ResourceID})
						if err == nil {
							status, _ := cl[st.StatusField].(string)
							if status == "" {
								status, _ = cl["status"].(string)
							}
							switch {
							case status != "" && strings.Contains(status, st.ReadyValue):
								job.Outputs[st.Key] = st.ResourceID
								st.Status = "done"
								progress = true
							case isTerminalFail(status):
								// FAILED/ERROR/ERASED — never reaches READY; fail the
								// job instead of polling forever.
								failStep(job, st, clierrors.CodeAPIError, fmt.Sprintf("%s: cluster reached terminal status %s", st.Key, status))
								return
							default:
								st.Detail = "status=" + status
							}
						}
					}
				}

			case "pending":
				if !depsDone(st, job) {
					continue
				}
				resumed := st.Interrupted
				if st.Interrupted {
					// The previous create was cut off and may have reached the
					// cloud: adopt what it made instead of creating it twice.
					id, state := st.ResourceID, ""
					if id == "" {
						id, state = s.findStepResource(ctx, st)
					}
					if ctx.Err() != nil {
						return
					}
					st.Interrupted = false
					if id != "" {
						if st.Tool == "provision" && state != "" {
							s.adoptVM(ctx, job, st, id, state)
							if job.Failed() {
								return
							}
						} else {
							s.finalizeStep(ctx, job, st, id)
						}
						st.Detail = "adopted after an interrupted create"
						progress = true
						continue
					}
				}
				if _, findable := stepFinder(st); st.StartedAt.IsZero() && !resumed && st.Tool != "provision" && findable {
					// Before the step's first create: a resource with its
					// name now is not the job's. Without this, a retry after
					// an attempt that failed before its create found it by
					// name below and adopted it — stack_destroy would delete
					// a user's cluster (final audit, stage 1). Whatever the
					// name finds after this point the step made itself. A
					// VM is matched by its creation time instead.
					id, err := s.takenByName(ctx, st)
					if err != nil {
						st.Detail = "check the name is free: " + err.Error()
						continue // pending: the next pass checks again
					}
					if id != "" {
						f, _ := stepFinder(st)
						// A taken name, as a create refuses one (D28).
						failStep(job, st, clierrors.CodeUsage, fmt.Sprintf("%s: %q is already taken by an existing resource (%s) — rename it in the manifest, or remove the existing one", st.Key, f.name, id))
						return
					}
				}
				if st.StartedAt.IsZero() {
					st.StartedAt = time.Now()
				}
				args := substituteRefs(st.Args, job.Outputs)
				// stack_apply was confirmed for the whole stack. Jobs planned
				// before provision and s3_put got their gates lack the flag.
				args["confirmed"] = true
				if st.Tool == "provision" && st.ResourceID != "" {
					// The repeat goes on with the step's VM and no other: a VM
					// gone since is refused, not made again (D102; «Риск»,
					// conclusion 216, У2).
					args["vm_id"] = st.ResourceID
				}
				res, err := s.callTool(ctx, st.Tool, args)
				if err != nil && ctx.Err() != nil {
					// Cancelled mid-create: not a failed attempt. Keep the id
					// the tool reported (provision names the VM it made).
					st.Interrupted = true
					st.ResourceID = createdID(st.Tool, res)
					st.Detail = "interrupted: " + err.Error()
					return
				}
				if err != nil {
					st.Attempts++
					st.Detail = err.Error()
					// The class of the refusal, as its tool answered it, for
					// the branches below that end the step on it (G140-5).
					reason, _ := res["reason"].(string)
					// The tool named what it made although the call failed
					// (provision: a VM in error_creating or not active in
					// time). It exists and is billed: record it for
					// stack_destroy and stop — a retry would create another.
					if id := createdID(st.Tool, res); id != "" {
						st.ResourceID = id
						failStep(job, st, reason, fmt.Sprintf("%s: %s (resource %s kept for stack_destroy)", st.Key, err.Error(), id))
						return
					}
					// The tool refused a name a resource already has
					// (existing_id; release audit, L-32). Unless the step
					// resumes after a cut-off create, that resource is not
					// known to be the job's: adopting it by name below would
					// hand it to stack_destroy. Only on the first try it was
					// refused; a retry after an attempt that failed before
					// its create adopted a user's cluster the same way
					// (final audit, stage 1). An attempt whose create did go
					// through with its answer lost leaves its resource
					// named here, for the user to keep or delete.
					// The step's own resource (the VM a repeat of provision
					// goes on with, D102) is gone: final at once. Neither a
					// resource found by name nor another create is the
					// step's (Астра, #1766; «Риск», 216).
					// VM_NOT_FOUND says the step's VM is gone; NOT_FOUND may
					// be about another thing (an image), the resource still
					// there: kept for stack_destroy («Риск», 222, У1).
					switch {
					case st.ResourceID == "":
					case reason == clierrors.CodeVMNotFound:
						failStep(job, st, reason, fmt.Sprintf("%s: %s (resource %s of the step is gone; no new one was made)", st.Key, err.Error(), st.ResourceID))
						return
					case reason == clierrors.CodeNotFound:
						failStep(job, st, reason, fmt.Sprintf("%s: %s (resource %s of the step kept for stack_destroy; no new one was made)", st.Key, err.Error(), st.ResourceID))
						return
					}
					if ex := firstStr(res, "existing_id"); ex != "" && !resumed {
						failStep(job, st, reason, fmt.Sprintf("%s: %s — rename it in the manifest, or remove the existing one (%s); "+
							"if an earlier attempt of this step created it, delete it or keep it: the stack does not adopt it by name", st.Key, err.Error(), ex))
						return
					}
					// The cloud refused the create on purpose — the call is
					// wrong (USAGE), names what is not there (NOT_FOUND), or
					// got a 4xx — and nothing was made: a retry meets the same
					// refusal, and a resource of the step's name is not the
					// step's (G130-04, G130-24; D41). Only on the first try:
					// a refusal after an attempt whose answer was lost may be
					// that attempt's own resource, found by name below.
					if !resumed && st.Attempts == 1 && createRefused(res, err) {
						failStep(job, st, reason, fmt.Sprintf("%s: %s", st.Key, err.Error()))
						return
					}
					// Reconcile before retrying: a create can fail the RESPONSE yet
					// succeed server-side (e.g. 504 gateway timeout after the backend
					// applied it). If the resource now exists by name, ADOPT it — never
					// blindly retry a create (that would double-provision).
					if id, state := s.findStepResource(ctx, st); id != "" {
						if st.Tool == "provision" {
							s.adoptVM(ctx, job, st, id, state)
							if job.Failed() {
								return
							}
						} else {
							s.finalizeStep(ctx, job, st, id)
						}
						progress = true
						continue
					}
					// Genuinely not created → retry on a later crank, bounded.
					if st.Attempts >= maxStepAttempts {
						failStep(job, st, reason, fmt.Sprintf("%s: %s (after %d attempts)", st.Key, err.Error(), st.Attempts))
						return
					}
					continue // stay pending, no progress → retried next stack_status crank
				}
				// The password evo made for the step's VM: in the answer of
				// this crank only, kept nowhere (FB121-7).
				if pw := firstStr(res, "password"); pw != "" {
					job.ShowPassword(st.Key, pw)
				}
				if st.Tool == "provision" && firstStr(res, "status") == "creating" {
					// The VM is made and still coming up when the call's
					// budget ended (D69): neither made nor failed. The step
					// stays pending with its VM, for stack_destroy; the next
					// crank repeats provision, which adopts the VM and
					// finishes it. No attempt is used.
					st.ResourceID = firstStr(res, "vm_id")
					st.Detail = fmt.Sprintf("VM %s is %s; the next stack_status finishes it", st.ResourceID, firstStr(res, "vm_status"))
					continue
				}
				if st.ReadyKind == "" {
					// synchronous — capture what it made (Decl.Created) and
					// finish. An answer that names nothing is not success:
					// the step made something billed or nothing, and only
					// its name can tell (G13-14: it was "ok", and
					// stack_destroy kept the SNAT gateway).
					id := createdRef(st.Tool, args, res)
					if id == "" {
						id, _ = s.findStepResource(ctx, st)
					}
					if id == "" {
						failStep(job, st, clierrors.CodeAPIError, fmt.Sprintf("%s: %s answered no id of what it made, and its name finds nothing", st.Key, st.Tool))
						return
					}
					job.Outputs[st.Key] = id
					st.ResourceID = id
					st.Status = "done"
				} else {
					// async — remember the op and wait on subsequent cranks;
					// the id, when the answer gives it (k8s: cluster_id),
					// lets stack_destroy delete what is still being made.
					st.OpID = firstStr(res, "operation_id")
					st.ResourceID = createdID(st.Tool, res)
					st.Status = "waiting"
				}
				progress = true
			}
		}
	}

	// terminal check
	if job.Failed() {
		return
	}
	allDone := true
	for _, st := range job.Steps {
		if st.Status != "done" {
			allDone = false
			break
		}
	}
	if allDone {
		job.Status = "done"
	} else {
		job.Status = "running"
	}
}

// createRefused reports a step create the cloud refused on purpose: its
// answer's reason is USAGE, NOT_FOUND or CONFIG_INVALID, or the error
// carries a 4xx status other than 429 (lostCreate draws the same line,
// D42). A lost answer — no status, a 5xx, 429 — is not one.
func createRefused(res map[string]any, err error) bool {
	switch reason, _ := res["reason"].(string); reason {
	case clierrors.CodeUsage, clierrors.CodeNotFound, clierrors.CodeConfigInvalid:
		return true
	}
	st := apiStatus(err)
	return st >= 400 && st < 500 && st != http.StatusTooManyRequests
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// --- handlers ---

// unknownPathsNamed is how many unknown paths a refusal of a manifest
// names: all at once, so a caller fixes them in one round, but not an
// answer without bound (D108; «Риск», 186, У9).
const unknownPathsNamed = 20

// unknownManifestFields is the refusal of a manifest with fields its type
// does not have: each path with the nearest name, then the fields of each
// level once, in the error and in unknown_fields and known_fields (D108;
// «Риск», 186, У1, У9). The names are the caller's text: quoted
// (fieldSegment) and marked untrusted (untrustedFields).
func unknownManifestFields(unknown []unknownField) string {
	more := 0
	if len(unknown) > unknownPathsNamed {
		more, unknown = len(unknown)-unknownPathsNamed, unknown[:unknownPathsNamed]
	}
	var paths, levels []string
	known := map[string][]string{}
	for _, u := range unknown {
		p := u.Path
		if u.Nearest != "" {
			p += i18n.Tf(" (nearest: %s)", " (ближайшее: %s)", u.Nearest)
		}
		paths = append(paths, p)
		if _, seen := known[u.level]; !seen {
			known[u.level] = u.known
			levels = append(levels, i18n.Tf("fields of %s: %s", "поля %s: %s", u.level, strings.Join(u.known, ", ")))
		}
	}
	if more > 0 {
		paths = append(paths, i18n.Tf("and %d more", "и ещё %d", more))
	}
	fields := map[string]any{"unknown_fields": unknown, "known_fields": known}
	if more > 0 {
		fields["unknown_fields_more"] = more
	}
	return errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("the manifest has fields evo does not know: %s; %s; nothing was planned", "в манифесте поля, которых evo не знает: %s; %s; план не создан",
			strings.Join(paths, ", "), strings.Join(levels, "; ")),
		i18n.T("Fix each name or drop the field: evo does not skip a field it does not know, since a typo would plan another stack. Note fields (_comment, x-…) are not taken either.",
			"Исправьте каждое имя или уберите поле: незнакомое поле evo не пропускает — опечатка спланировала бы другой стек. Поля-заметки (_comment, x-…) тоже не принимаются.")),
		fields)
}

func (s *Registry) handleStackPlan(ctx context.Context, args map[string]any) (string, bool) {
	raw, ok := args["manifest"]
	if !ok {
		return usageT("'manifest' is required (object describing the stack)", "нужен 'manifest' — объект с описанием стека"), true
	}
	// A field the manifest's type does not have would be dropped by the
	// decoding: refused here, every one at once, before any request (D108).
	if unknown := unknownFields(raw, reflect.TypeOf(stack.Manifest{}), "manifest"); len(unknown) > 0 {
		return unknownManifestFields(unknown), true
	}
	// A scope field of the manifest given empty is refused here, at the
	// plan: decoded, it reads as «not set» and the step would work in the
	// zone or the project of the profile (D100) — or stop a stack half
	// made.
	if path := emptyScopeField(raw, "manifest"); path != "" {
		return errResult(clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s is empty; nothing was planned", "%s пустой; план не создан", path),
			i18n.Tf("Give %s a value or drop the field: an empty value is not read as «not set».",
				"Задайте %s значение или уберите поле: пустое значение не читается как «не задано».", path))), true
	}
	b, _ := json.Marshal(raw)
	var m stack.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return errResult(fmt.Errorf(i18n.T("invalid manifest: %w", "неверный манифест: %w"), err)), true
	}
	if m.Name == "" {
		return usageT("manifest.name is required", "нужен manifest.name"), true
	}
	// A public record on a balancer without an external address points at
	// an address the internet does not reach: the stack would look done
	// and serve nothing (D64, FB15-47). The default of nlb.external stays.
	// Read nothing: refused before the cloud is asked.
	if d := m.DNS; d != nil && (d.Target == "" || d.Target == "lb") && m.NLB != nil && !m.NLB.External {
		return errResult(clierrors.New(clierrors.CodeUsage,
			i18n.T("manifest.dns.target=lb points the record at the balancer, but manifest.nlb.external is not set: the balancer gets no external address and the record would point at an address the internet does not reach; nothing was planned",
				"manifest.dns.target=lb направляет запись на балансировщик, но manifest.nlb.external не задан: у балансировщика не будет внешнего адреса, и запись укажет на адрес, недоступный из интернета; план не создан"),
			i18n.T("Set manifest.nlb.external: true, or give manifest.dns.target an address.",
				"Задайте manifest.nlb.external: true или укажите адрес в manifest.dns.target."))), true
	}
	// Validate + normalize the manifest BEFORE planning, so a malformed field
	// fails fast at plan time (nothing created) instead of wedging the stack in
	// "error" mid-deploy after other resources are already live.
	s3Notes, flavor, err := s.normalizeManifest(ctx, &m)
	if err != nil {
		return errResult(err), true
	}
	if err := s.checkStackSecrets(ctx, &m); err != nil {
		return errResult(err), true
	}
	var vmNote map[string]any
	if m.VM != nil {
		if vmNote, err = s.normalizeStackVM(ctx, m.VM, flavor); err != nil {
			return errResult(err), true
		}
	}
	// Redis shape and Kafka brokers fail at plan time, like the VM: a stack
	// with redis.replicas=3 (the old documented default) used to plan and
	// then stop at the redis step with vpc, pg, kafka and k8s already live
	// (Grok review of PR #89).
	if r := m.Redis; r != nil {
		ra := map[string]any{}
		if r.Type != "" {
			ra["type"] = r.Type
		}
		if r.Shards != 0 {
			ra["shards"] = r.Shards
		}
		if r.Replicas != 0 {
			ra["replicas"] = r.Replicas
		}
		if _, _, _, err := redisShape(ra); err != nil {
			return usageResult(fmt.Errorf("manifest.redis: %w", err)), true
		}
	}
	if m.Kafka != nil && m.Kafka.Brokers < 0 {
		return usageT("manifest.kafka.brokers %d: must be 1 or more (or omitted)", "manifest.kafka.brokers %d: нужно 1 или больше (или не указывать)", m.Kafka.Brokers), true
	}
	if m.K8s != nil && m.K8s.MasterCount != 0 {
		if err := k8sArgsError(map[string]any{"master_count": m.K8s.MasterCount}); err != nil {
			return errResult(err), true
		}
	}
	job := stack.Plan(m)
	if eng, e := s.newEngine(nil); e == nil && eng != nil {
		job.ProjectID = eng.Config.ProjectID
	}
	if err := job.CheckDeps(); err != nil {
		return errResult(fmt.Errorf(i18n.T("invalid manifest: %w", "неверный манифест: %w"), err)), true
	}
	if err := job.Save(); err != nil {
		return errResult(fmt.Errorf(i18n.T("persist plan: %w", "сохранение плана: %w"), err)), true
	}
	steps := make([]string, 0, len(job.Steps))
	for _, st := range job.Steps {
		steps = append(steps, st.Key)
	}
	out := map[string]any{
		"status":     "planned",
		"job_id":     job.ID,
		"project_id": job.ProjectID,
		"steps":      steps,
		"cost":       s.stackCost(ctx, job),
		// The instruction about consent, in next as deploy_plan has it
		// (A9, decision 21.10).
		"next": "The stack is BILLABLE: show the user the steps and the cost, and on explicit approval call stack_apply with this job_id and confirmed=true.",
	}
	if s.readOnly {
		out["next"] = "Show the user the steps and the cost; creating the stack (stack_apply) needs a server without --read-only."
	}
	// message, the field of 1.2, says the same until 2.0 (--json is kept
	// within a major, RELEASING).
	out["message"] = out["next"]
	if len(s3Notes) > 0 {
		out["s3"] = s3Notes
	}
	if vmNote != nil {
		out["vm"] = vmNote
	}
	// Warn if a prior job for this stack name still holds resources — applying now
	// would create duplicates (double billing). Point the user at stack_destroy.
	if orphans := stack.FindResourcefulJobs(m.Name, job.ID); len(orphans) > 0 {
		out["warning"] = fmt.Sprintf("%d earlier job(s) for stack %q still hold resources — applying THIS plan will create DUPLICATES. Whether to destroy them first (stack_destroy) — ask the person.", len(orphans), m.Name)
		out["existing_jobs"] = orphans
	}
	return toJSON(out), false
}

// stackKinds are the kinds of the resources the steps of a stack create,
// by the step's tool: the class of each one's price is its kind's
// (engine price classes). A step tool without a line here fails
// TestStackPlan_EveryStepHasAKind.
var stackKinds = map[string]string{
	"vpc_create":        engine.ResourceVPC,
	"pg_create":         engine.ResourcePGCluster,
	"kafka_create":      engine.ResourceKafka,
	"redis_create":      engine.ResourceRedis,
	"k8s_create":        engine.ResourceK8sCluster,
	"ar_create":         engine.ResourceARRegistry,
	"snat_create":       engine.ResourceSNAT,
	"secret_create":     engine.ResourceSecret,
	"s3_put":            engine.ResourceS3Object,
	"provision":         engine.ResourceVM,
	"tg_create":         engine.ResourceTargetGroup,
	"lb_create":         engine.ResourceLB,
	"dns_record_create": engine.ResourceDNSRecord,
}

// stackCost is the price of the resources of stack job j, as their own
// create tools name it (owner's decision 04.11): the VM and the
// PostgreSQL cluster by the preview of provision and of pg_create — the
// price-calculation API, else the rate card, and the tariff of
// PostgreSQL — and every other resource by the class of its kind. A
// resource without a price has no sum, only why; the total adds the
// priced ones and the note says what it leaves out. The planners only
// read; a planner that refuses (the VM's name taken, no subnet) leaves
// its resource without a price here, and the step refuses at apply as it
// would.
func (s *Registry) stackCost(ctx context.Context, j *stack.Job) map[string]any {
	planners := map[string]Planner{"provision": s.planProvision, "pg_create": s.planPgCreate}
	lines := []map[string]any{}
	var total float64
	unpriced := 0
	for _, st := range j.Steps {
		kind, ok := stackKinds[st.Tool]
		if !ok {
			continue
		}
		p := engine.CreatePreview{Resource: kind, Name: st.Key}
		if plan := planners[st.Tool]; plan != nil {
			// A planner reads, it never executes: the step's confirmed is
			// the gate's, not the planner's.
			if pl, err := plan(ctx, maps.Clone(st.Args)); err == nil && pl.Execute != nil {
				if pv, ok := pl.Preview["preview"].(engine.CreatePreview); ok {
					p = pv
				}
			}
		}
		if p.PriceClasses == nil {
			switch kind {
			case engine.ResourceVM:
				noPublicIP, _ := st.Args["no_public_ip"].(bool)
				public := !noPublicIP
				p.PublicIP = &public
			case engine.ResourcePGCluster:
				// The size the manifest names, priced by the same tariff as
				// pg_create's preview; without all of it, no price.
				if cpu, ram, gb := pgInt(st.Args, "cpu", 0), pgInt(st.Args, "ram", 0), pgInt(st.Args, "storage_gb", 0); cpu > 0 && ram > 0 && gb > 0 {
					p.PG = &pricing.PGSize{CPU: cpu, RAMGB: ram, StorageGB: gb, Instances: 1}
				}
			}
			p.Price()
		}
		for _, c := range p.PriceClasses {
			line := map[string]any{"component": st.Key, "resource": c.Resource, "class": c.Class}
			if c.Note != "" {
				line["note"] = c.Note
			}
			switch c.Class {
			case engine.PricePriced:
				rub := p.MonthlyRub(c.Resource)
				line["monthly_rub"], line["source"] = rub, p.Cost.Source
				total += rub
			case engine.PricePaidUnpriced:
				unpriced++
				fallthrough
			default:
				if p.TariffURL != "" {
					line["tariff_url"] = p.TariffURL
				}
			}
			lines = append(lines, line)
		}
	}
	note := i18n.T("the price-calculation API, the rate card and the tariffs; not a bill (billing_finops after apply)",
		"API расчёта цены, тарифная карта и тарифы; не счёт (billing_finops после apply)")
	if unpriced > 0 {
		note = i18n.Tf("total of the priced resources only: %d billable resource(s) have no price (class paid_unpriced; the line says why); ",
			"итог — только ресурсы с ценой: у %d платных ресурсов цены нет (класс paid_unpriced; почему — в строке); ", unpriced) + note
	}
	return map[string]any{
		"currency":          "RUB",
		"total_monthly_rub": math.Round(total*100) / 100,
		"by_component":      lines,
		"note":              note,
	}
}

// checkStackSecrets refuses a secret path that is not free: one the Secret
// Manager has, or one another stack's job may have made. The stack would
// take that secret over, and its destroy would delete it (G130-24, D41).
func (s *Registry) checkStackSecrets(ctx context.Context, m *stack.Manifest) error {
	if len(m.Secrets) == 0 {
		return nil
	}
	cli, err := s.secretClient(ctx)
	if err != nil {
		return err
	}
	for i, sc := range m.Secrets {
		if holders := stack.SecretHolders(sc.Path, ""); len(holders) > 0 {
			return clierrors.New(clierrors.CodeUsage,
				i18n.Tf("secrets[%d]: the path %q belongs to stack job %s", "secrets[%d]: путь %q принадлежит заданию стека %s", i, sc.Path, strings.Join(holders, ", ")),
				i18n.T("Pick another secret path in the manifest; whether to destroy that job first (stack_destroy) — ask the person.",
					"Укажите в манифесте другой путь секрета; удалять ли сначала это задание (stack_destroy) — спросите человека."))
		}
		_, err := cli.Get(ctx, sc.Path)
		if err == nil {
			return clierrors.New(clierrors.CodeUsage,
				i18n.Tf("secrets[%d]: the secret %q already exists; the stack would take it over, and stack_destroy would delete it — ask the person whether it may", "secrets[%d]: секрет %q уже есть; стек присвоил бы его, а stack_destroy удалил бы — спросите человека, можно ли", i, sc.Path),
				i18n.T("Pick another secret path in the manifest.", "Укажите в манифесте другой путь секрета."))
		}
		if !isGone(err) {
			return fmt.Errorf(i18n.T("secrets[%d]: check the path: %w", "secrets[%d]: проверка пути: %w"), i, err)
		}
	}
	return nil
}

// normalizeStackVM resolves the image and the zone of the stack's VM at
// plan time and writes their UUIDs into the manifest: vm.image (name or
// UUID, as provision's image), else the server's CLOUDRU_IMAGE_ID; vm.zone
// (name or UUID), else CLOUDRU_REGION. A missing, unknown or closed one is
// refused here, before anything is created: the vm step used to fail
// after the earlier steps were live (release audit, L-36). Returns what
// the plan shows for the VM.
func (s *Registry) normalizeStackVM(ctx context.Context, vm *stack.VMSpec, flavor *provision.Flavor) (map[string]any, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("validate vm: %w", "проверка vm: %w"), err)
	}
	imageRef := firstNonEmptyStr(vm.Image, eng.Config.ImageID, eng.Config.Image)
	if imageRef == "" {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.T("manifest vm.image is required: the image of the VM, by name or UUID", "в манифесте нужен vm.image — образ ВМ, имя или UUID"),
			i18n.T("Set vm.image, e.g. \"Ubuntu-24.04\" (image_list); the server has no CLOUDRU_IMAGE_ID (an image UUID) to default to.", "Задайте vm.image, например \"Ubuntu-24.04\" (image_list): у сервера не задан CLOUDRU_IMAGE_ID (UUID образа по умолчанию)."))
	}
	cat := images.Catalog{Cloud: eng.Cloud, ProjectID: eng.Config.ProjectID}
	img, err := cat.Get(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("vm.image %q: %w", imageRef, err)
	}
	if img.NotInProject {
		return nil, images.NotInProjectError(img.Name)
	}

	zoneRef := firstNonEmptyStr(vm.Zone, eng.Config.Region)
	if zoneRef == "" {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.T("manifest vm.zone is required: the availability zone of the VM, by name or UUID", "в манифесте нужен vm.zone — зона доступности ВМ, имя или UUID"),
			i18n.T("Set vm.zone, e.g. \"ru.AZ-1\" (list_zones); the server has no CLOUDRU_REGION (a zone UUID) to default to.", "Задайте vm.zone, например \"ru.AZ-1\" (list_zones): у сервера не задан CLOUDRU_REGION (UUID зоны по умолчанию)."))
	}
	zones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("validate vm.zone: list availability zones: %w", "проверка vm.zone: список зон доступности: %w"), err)
	}
	zone, err := provision.ResolveZone(zones, zoneRef)
	if err != nil {
		return nil, clierrors.Prefix("vm.zone: ", err)
	}
	if err := provision.ClosedZoneError(zones, zone.ID); err != nil {
		return nil, err
	}
	// A flavor named by the manifest not in the VM's zone: refused here, as
	// provision refuses it (D67). The flavor is the one normalizeManifest
	// found in its read of the catalogue (FB160-19): no second read, and so
	// no read that fails into a skipped check.
	if flavor != nil {
		if err := engine.FlavorOutOfZone(flavor, &deployplan.RefSpec{ID: zone.ID, Name: zone.Name}); err != nil {
			return nil, clierrors.Prefix("vm.flavor: ", err)
		}
	}
	vm.Image, vm.Zone = img.ID, zone.ID
	return map[string]any{"image": firstNonEmptyStr(img.Name, img.ID), "image_id": img.ID,
		"zone": firstNonEmptyStr(zone.Name, zone.ID)}, nil
}

// normalizeManifest validates a stack manifest and fills in safe defaults BEFORE
// planning, so a malformed field fails fast at plan time (nothing is created)
// instead of wedging the whole stack in "error" mid-deploy — the failure mode
// that an empty s3 bucket produced (s3_put needs bucket+key → 4 retries → the
// job goes "error" though snat/secret/vm already deployed).
//
// It returns per-object s3 notes (the resolved bucket/key) for transparency,
// and the catalogue's entry of the flavor the manifest names (nil for auto
// or none), for the zone check of normalizeStackVM.
// The project's buckets are listed once: an object without a bucket takes
// the only one, and a bucket the manifest names must be among them — the
// stack does not create buckets, and its s3 step failed after the steps
// before it were created (G13-13, W7).
func (s *Registry) normalizeManifest(ctx context.Context, m *stack.Manifest) ([]string, *provision.Flavor, error) {
	var flavor *provision.Flavor
	// --- VM flavor: validate at plan time so a bad name fails fast here rather
	// than mid-apply. A non-existent flavor (e.g. "performance-2-4") otherwise
	// passes planning, then the VM step 500s during apply — and by then earlier
	// steps (a Postgres cluster) are already live, leaving a billable orphan that
	// a re-plan duplicates. Catching it here prevents that whole chain.
	if m.VM != nil && m.VM.Flavor != "" && m.VM.Flavor != "auto" {
		eng, err := s.authedEngine(ctx, nil)
		if err != nil {
			return nil, nil, fmt.Errorf(i18n.T("validate vm.flavor: %w", "проверка vm.flavor: %w"), err)
		}
		flavors, err := eng.Cloud.ListFlavors(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf(i18n.T("validate vm.flavor: %w", "проверка vm.flavor: %w"), err)
		}
		names := make([]string, 0, len(flavors))
		for _, f := range flavors {
			if f.Name == m.VM.Flavor || f.ID == m.VM.Flavor {
				flavor = f
				break
			}
			names = append(names, f.Name)
		}
		if flavor == nil {
			return nil, nil, fmt.Errorf(i18n.T("vm.flavor %q does not exist in this project. Pick a real one (see list_flavors) — available: %s", "vm.flavor %q в проекте нет. Выберите существующую конфигурацию (см. list_flavors) — доступны: %s"), m.VM.Flavor, strings.Join(names[:min(len(names), 14)], ", "))
		}
	}

	if len(m.S3) == 0 {
		return nil, flavor, nil
	}

	// The project's buckets, listed once.
	var buckets []string
	var cli *s3.Client
	fetched := false
	fetch := func() error {
		if fetched {
			return nil
		}
		var err error
		if cli, err = s.s3Client(); err != nil {
			return fmt.Errorf(i18n.T("list S3 buckets: %w", "список бакетов S3: %w"), err)
		}
		bs, err := cli.ListBuckets(ctx)
		if err != nil {
			return fmt.Errorf(i18n.T("list S3 buckets: %w", "список бакетов S3: %w"), err)
		}
		for _, b := range bs {
			buckets = append(buckets, b.Name)
		}
		fetched = true
		return nil
	}

	notes := make([]string, 0, len(m.S3))
	for i := range m.S3 {
		if m.S3[i].Key == "" {
			return nil, nil, fmt.Errorf(i18n.T("s3[%d].key is required (the object path inside the bucket, e.g. \"config/app.yaml\")", "нужен s3[%d].key — путь объекта в бакете, например \"config/app.yaml\""), i)
		}
		if m.S3[i].Bucket == "" {
			if err := fetch(); err != nil {
				return nil, nil, err
			}
			switch len(buckets) {
			case 0:
				return nil, nil, fmt.Errorf(i18n.T("s3[%d].bucket is empty and this project has no S3 bucket — create one (s3_buckets to check) or set s3[%d].bucket", "s3[%d].bucket пуст, а в проекте нет бакета S3 — создайте бакет (проверка — s3_buckets) или задайте s3[%d].bucket"), i, i)
			case 1:
				m.S3[i].Bucket = buckets[0]
			default:
				return nil, nil, fmt.Errorf(i18n.T("s3[%d].bucket is empty and this project has %d buckets %v — set s3[%d].bucket explicitly", "s3[%d].bucket пуст, а в проекте %d бакетов %v — задайте s3[%d].bucket"), i, len(buckets), buckets, i)
			}
		}
		// A name S3 cannot take fails the s3 step after the steps before it
		// were created: refused here, before the consent (D19).
		if err := s3.CheckBucketName(m.S3[i].Bucket); err != nil {
			return nil, nil, clierrors.Prefix(fmt.Sprintf("s3[%d].bucket: ", i), err)
		}
		if err := fetch(); err != nil {
			return nil, nil, err
		}
		if !slices.Contains(buckets, m.S3[i].Bucket) {
			return nil, nil, clierrors.New(clierrors.CodeNotFound,
				i18n.Tf("s3[%d].bucket %q is not in this project, and a stack does not create buckets", "бакета s3[%d].bucket %q в проекте нет, а стек бакеты не создаёт", i, m.S3[i].Bucket),
				i18n.T("Create it with s3_bucket_create (evo s3 bucket-create) or name one of s3_buckets, then plan again.",
					"Создайте его через s3_bucket_create (evo s3 bucket-create) или укажите бакет из s3_buckets и спланируйте снова."))
		}
		// An object already at the key is not the stack's: its s3 step
		// would overwrite it and stack_destroy delete it (G130-03, D41).
		objs, err := cli.ListObjects(ctx, m.S3[i].Bucket, m.S3[i].Key, 0)
		if err != nil {
			return nil, nil, fmt.Errorf(i18n.T("s3[%d]: list objects: %w", "s3[%d]: список объектов: %w"), i, err)
		}
		for _, o := range objs {
			if o.Key == m.S3[i].Key {
				return nil, nil, clierrors.New(clierrors.CodeUsage,
					i18n.Tf("s3[%d]: the object s3://%s/%s already exists; the stack would overwrite it, and stack_destroy would delete it", "s3[%d]: объект s3://%s/%s уже есть; стек перезаписал бы его, а stack_destroy удалил бы", i, m.S3[i].Bucket, m.S3[i].Key),
					i18n.T("Pick another s3 key in the manifest; whether to remove the object (s3_rm) so the stack owns it — ask the person.",
						"Укажите в манифесте другой ключ s3; удалять ли объект (s3_rm), чтобы он принадлежал стеку, — спросите человека."))
			}
		}
		notes = append(notes, fmt.Sprintf("%s/%s", m.S3[i].Bucket, m.S3[i].Key))
	}
	return notes, flavor, nil
}

func (s *Registry) planStackApply(_ context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "job_id")
	if id == "" {
		return answered(usageT("'job_id' is required (from stack_plan)", "нужен 'job_id' (из stack_plan)"), true), nil
	}
	// What refuses the call refuses it before the question (axis 1 of
	// CLAUDE.md §6, D35): a job not there, of another project, or
	// destroyed. The execution checks again under the job's lock: another
	// process may have changed the job since.
	job, err := s.appliableJob(id)
	if err != nil {
		return nil, err
	}
	// The preview is built for the question only: its price reads what a
	// consented call does not need.
	previewOf := func(ctx context.Context) (map[string]any, error) {
		create, finish, failed, steps := s.stackSteps(job)
		will := map[string]any{"will_create": create}
		keys := []string{"will_create"}
		var warnings []string
		if len(finish) > 0 {
			will["will_finish"] = finish
			keys = append(keys, "will_finish")
			warnings = append(warnings, i18n.Tf("%d step(s) of will_finish are made already: the consent waits for them or finishes them and creates nothing there; steps_cost prices will_create only",
				"уже сделано шагов will_finish: %d — согласие их дожидается или довершает и ничего там не создаёт; steps_cost — цена только will_create", len(finish)))
		}
		if len(failed) > 0 {
			will["failed_steps"] = failed
			keys = append(keys, "failed_steps")
			warnings = append(warnings, i18n.Tf("the job is stopped by %d step(s) in error (failed_steps): the consent runs the steps that do not wait on them, and the job does not end done; whether to remove what it made (stack_destroy) — ask the person",
				"задание остановлено шагами с ошибкой (failed_steps): %d — согласие выполнит шаги, которые от них не зависят, но задание не завершится; удалять ли созданное (stack_destroy) — спросите человека", len(failed)))
		}
		// The price of what the consent makes: the steps it creates, as
		// stack_plan prices the job (CLAUDE.md §6, axes 1 and 4).
		if !job.Done() {
			pending := *job
			pending.Steps = steps
			will["steps_cost"] = s.stackCost(ctx, &pending)
			keys = append(keys, "steps_cost")
		}
		preview := map[string]any{"message": i18n.T("stack_apply provisions BILLABLE resources.",
			"stack_apply создаёт ПЛАТНЫЕ ресурсы."), "preview": willPreview(engine.ResourceStack, id, "", will, keys, warnings...)}
		// A job done: the question is asked as before — answering without
		// it would change an exit code, which waits for the major — and
		// says that the consent creates nothing, as deploy's does of an
		// unchanged compose file (unchanged: true).
		if job.Done() {
			preview["unchanged"] = true
			preview["message"] = i18n.Tf("Stack job %s is done already: every step is made. stack_apply has nothing to do, and called with confirmed=true it creates nothing; stack_status shows the stack.",
				"Задание стека %s уже выполнено: все шаги сделаны. stack_apply делать нечего, вызов с confirmed=true ничего не создаст; стек показывает stack_status.", id)
		}
		return preview, nil
	}
	return &Plan{PreviewOf: previewOf, Execute: func(ctx context.Context) (string, bool) {
		unlock, err := s.lockJob(id)
		if err != nil {
			return errResult(err), true
		}
		defer unlock()
		job, err := s.appliableJob(id)
		if err != nil {
			return errResult(err), true
		}
		// Done before this call: the call creates nothing, and its answer
		// says so, as the question does — a call sent with confirmed at
		// once never sees the question («Риск», conclusion 139).
		unchanged := job.Done()
		s.advance(ctx, job)
		if e := job.Save(); e != nil {
			return errResult(fmt.Errorf(i18n.T("persist job: %w", "сохранение задания: %w"), e)), true
		}
		out := job.Compact()
		if unchanged {
			out["unchanged"] = true
		}
		out["note"] = "Async — poll stack_status(job_id) until status=done. Each poll advances the deploy."
		return toJSON(out), job.Failed()
	}}, nil
}

// secretNotShown stands for the value of a secret in the question.
const secretNotShown = "(secret: not shown)"

// notShown stands for a value of stack.HiddenArgs that is not a secret
// (an object's body) in the question: its size.
func notShown(v any) string {
	s, _ := v.(string)
	return fmt.Sprintf("(not shown: %d bytes)", len(s))
}

// stepMade reports whether the resource of step st is made already, as
// advance takes it: the step waits for it to be ready; a create was cut
// off and its resource found (advance adopts it); or the VM of a
// provision step is coming up (D69), which the next crank repeats with
// the step's VM (vm_id) — a VM gone since is refused, not made again
// (D102). Any other pending step is created, whatever id it holds
// («Риск», conclusion 216, У1, У2).
func stepMade(st *stack.Step) bool {
	switch {
	case st.Status == "waiting":
		return true
	case st.Status != "pending" || st.ResourceID == "":
		return false
	case st.Interrupted:
		return true
	default:
		return st.Tool == "provision"
	}
}

// stackSteps sorts the steps of job not done yet for the question of
// stack_apply (CLAUDE.md §6, axis 4). create is what the consent makes:
// each step, its tool and the arguments it is called with, but confirmed
// — a reference to the resource of another step is "$" and its key; steps
// are those steps, for their price. finish is what is made already
// (stepMade): the step, its tool, the resource's id and the step's state
// — the consent waits for it or finishes it, and pays for no new one
// (Астра, #1748). failed is the steps in error, which stop the job: the
// step, its tool, the class of its failure, its words and the resource it
// left for stack_destroy («Риск», conclusion 216, У3).
//
// An argument that holds a secret (CLISpec.SecretFiles of the step's
// tool) is named, its value is not (CLAUDE.md §4); nor is the value of
// stack.HiddenArgs, which the job file does not keep either: the question
// gives its size.
func (s *Registry) stackSteps(job *stack.Job) (create, finish, failed []map[string]any, steps []*stack.Step) {
	create, finish = []map[string]any{}, []map[string]any{}
	for _, st := range job.Steps {
		switch {
		case st.Status == "done":
			continue
		case st.Status == "error":
			row := map[string]any{"step": st.Key, "tool": st.Tool, "reason": st.Reason, "detail": st.Detail}
			if st.ResourceID != "" {
				row["resource_id"] = st.ResourceID
			}
			failed = append(failed, row)
			continue
		case stepMade(st):
			finish = append(finish, map[string]any{"step": st.Key, "tool": st.Tool, "resource_id": st.ResourceID, "state": st.Status})
			continue
		}
		t, _ := s.Lookup(st.Tool)
		row := map[string]any{"step": st.Key, "tool": st.Tool}
		for k, v := range st.Args {
			switch {
			case k == "confirmed":
			case t.CLI.SecretFiles[k]:
				row[k] = secretNotShown
			case stack.HiddenArgs[st.Tool] == k:
				row[k] = notShown(v)
			default:
				row[k] = v
			}
		}
		if st.Interrupted {
			// advance looks for what the cut-off create made first.
			row["after_cut_off_create"] = true
		}
		create = append(create, row)
		steps = append(steps, st)
	}
	return create, finish, failed, steps
}

// appliableJob is the job stack_apply may advance: there, of this server's
// project, and not torn down — its steps are all "done" from the first
// apply, so advancing would report status=done for resources that no
// longer exist.
func (s *Registry) appliableJob(id string) (*stack.Job, error) {
	job, err := stack.LoadJob(id)
	if err != nil {
		return nil, err
	}
	if err := s.checkJobProject(job); err != nil {
		return nil, err
	}
	if job.TornDown() {
		return nil, fmt.Errorf(i18n.T("job %s is %s: its resources were torn down; run stack_plan again for a new deploy", "задание %s в состоянии %s: его ресурсы удалены; для нового развёртывания снова выполните stack_plan"), id, job.Status)
	}
	return job, nil
}

// lockJob serializes the stack tools on one job and returns the unlock.
// Each crank loads the job, starts the pending steps and saves it; two
// concurrent cranks (two HTTP clients, a poll retried while the first one
// still runs, a stdio and an HTTP server on one stack-jobs directory) would
// both see a step pending and create it twice. The mutex orders the calls
// of this process; the file lock, other processes.
func (s *Registry) lockJob(id string) (unlock func(), err error) {
	s.jobMu.Lock()
	if s.jobLocks == nil {
		s.jobLocks = map[string]*sync.Mutex{}
	}
	mu := s.jobLocks[id]
	if mu == nil {
		mu = &sync.Mutex{}
		s.jobLocks[id] = mu
	}
	s.jobMu.Unlock()
	mu.Lock()
	fileUnlock, err := stack.LockJob(id, jobLockWait)
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	return func() {
		fileUnlock()
		mu.Unlock()
	}, nil
}

// checkJobProject refuses a job planned in another project than the one
// this server works in: every step tool (pg_create, k8s_delete, destroy…)
// runs in the server's project, where the job's resources are not.
func (s *Registry) checkJobProject(job *stack.Job) error {
	if job.ProjectID == "" {
		return nil // planned by an older version
	}
	eng, err := s.newEngine(nil)
	if err != nil {
		return err
	}
	if eng == nil || eng.Config.ProjectID == job.ProjectID {
		return nil
	}
	return fmt.Errorf(i18n.T("job %s was planned in project %s, but this server works in project %s: its tools would act on the wrong project. Restart the MCP server with --project-id %s (or CLOUDRU_PROJECT_ID)", "задание %s запланировано в проекте %s, а сервер работает в проекте %s: его инструменты действовали бы не в том проекте. Перезапустите MCP-сервер с --project-id %s (или CLOUDRU_PROJECT_ID)"),
		job.ID, job.ProjectID, eng.Config.ProjectID, job.ProjectID)
}

// jobLockWait bounds the wait for another process's crank of the same job:
// one crank is a few API calls, not minutes.
const jobLockWait = time.Minute

// crankJob loads the job and, while it is in progress, advances and saves
// it, all under the job's lock. Only a "running" job moves: stack_apply with
// confirmed=true sets it, and a read-only registry (SetReadOnly) advances
// none. A "planned" job is shown, never advanced —
// stack_status is a read-only poll, and advancing a planned job would create
// every billable step without the user's approval (QA 15.10.2026, C1).
func (s *Registry) crankJob(ctx context.Context, id string) (*stack.Job, error) {
	unlock, err := s.lockJob(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	job, err := stack.LoadJob(id)
	if err != nil {
		return nil, err
	}
	if job.Running() && !s.readOnly {
		if err := s.checkJobProject(job); err != nil {
			return nil, err
		}
		// The operations of a job of the server's project (checkJobProject)
		// were started by evo's own create calls there, maybe before this
		// server did: a bound registry polls them as its own
		// (kindOperation). A job without a project proves nothing.
		if s.projectBound && job.ProjectID != "" {
			for _, st := range job.Steps {
				if st.OpID != "" && st.PollTool != "" {
					s.ops.remember(groupOf(st.PollTool), st.OpID)
				}
			}
		}
		s.advance(ctx, job)
		if e := job.Save(); e != nil {
			return nil, fmt.Errorf(i18n.T("persist job: %w", "сохранение задания: %w"), e)
		}
	}
	return job, nil
}

func (s *Registry) handleStackStatus(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "job_id")
	if id == "" {
		return usageT("'job_id' is required", "нужен 'job_id'"), true
	}
	// Absorb SOME waiting inside the call (crank every few seconds up to a small
	// budget) so the agent doesn't poll in a tight loop — but keep the budget SHORT.
	// A long internal block is uninterruptible from the gateway's side: an MCP call
	// runs in a separate persistent process, so when the gateway aborts a run
	// (timeout / new input) it just stops awaiting the response — it cannot cancel
	// the in-flight tool. A multi-minute wait then makes the gateway's abort-settle
	// time out → "This operation was aborted" → surface_error → the agent goes
	// silent mid-deploy. Capping at 20s keeps every poll quick and abortable; the
	// agent simply re-polls (cheap: compact O(1) status; empty inter-poll turns are
	// handled by the reasoning-only retry). wait_seconds is clamped, not honored.
	waitBudget := pgInt(args, "wait_seconds", stackStatusMaxWait)
	if waitBudget > stackStatusMaxWait {
		waitBudget = stackStatusMaxWait
	}
	if waitBudget < 1 {
		waitBudget = 1
	}
	deadline := time.Now().Add(time.Duration(waitBudget) * time.Second)
	// The VM passwords of every crank of this call: each crank reloads the
	// job, and the answer of the call shows them once (FB121-7).
	shown := map[string]string{}
	compact := func(job *stack.Job) map[string]any {
		for step, pw := range shown {
			job.ShowPassword(step, pw)
		}
		out := job.Compact()
		// A job the cloud is still deleting moves only by stack_destroy:
		// polling stack_status would never see done or error (FB180-15).
		if job.DeletionsPending() {
			stackDestroyNext(out, job.ID)
		}
		return out
	}
	for {
		// Reloaded on every crank: another call may have advanced it.
		job, err := s.crankJob(ctx, id)
		if err != nil {
			return errResult(err), true
		}
		maps.Copy(shown, job.Passwords())
		if job.Planned() {
			out := compact(job)
			out["note"] = "Not started: call stack_apply with this job_id and confirmed=true after the user approved the plan. stack_status only reports progress."
			return toJSON(out), false
		}
		if !job.Running() {
			return toJSON(compact(job)), false // terminal (done/error)
		}
		if s.readOnly {
			out := compact(job)
			out["note"] = "Not advanced: this server runs with --read-only and acts on no consent, the one stack_apply was given included. " +
				"The cloud goes on with the steps already started; a server without --read-only advances the job when it polls it."
			return toJSON(out), false
		}
		rem := time.Until(deadline)
		if rem <= 0 {
			return toJSON(compact(job)), false // budget exhausted — agent polls again
		}
		select {
		case <-ctx.Done():
			return errResult(ctx.Err()), true
		case <-time.After(min(stackCrankInterval, rem)):
		}
	}
}

// stackTeardown are the creates of stack steps in the order stack_destroy
// deletes what they made (Decl.Created names the delete): what uses a
// resource before it, the VPC last — its subnets must be empty.
var stackTeardown = []string{
	"dns_record_create", "lb_create", "tg_create", "k8s_create", "kafka_create", "redis_create",
	"snat_create", "ar_create", "pg_create", "provision", "secret_create", "s3_put", "vpc_create",
}

// stepMayHaveCreated reports whether a step may have reached the cloud: it
// ran (running, waiting, error after a try) or its create was cut off. Only
// such a step is looked up by name at destroy: a step that never ran owns
// nothing, and a resource with its name belongs to someone else — deleting
// it destroyed a foreign cluster (QA r3 R3).
// stackDestroyNext is the next step of a job the cloud is still deleting, the
// same in the answers of stack_destroy and stack_status: the same call
// re-checks them and finishes what waits for them — a VM's floating IP and
// record (D69); stack_status does not move it (D96, FB180-15).
func stackDestroyNext(out map[string]any, id string) {
	out["next"] = fmt.Sprintf("Call stack_destroy with job_id %s and confirmed=true again until 'deleting' is empty.", id)
	nextStep(out, "stack_destroy", map[string]any{"job_id": id, "confirmed": true})
}

// alreadyDestroyed is the answer of stack_destroy for a job Destroyed.
func alreadyDestroyed(id string) string {
	return toJSON(map[string]any{"status": "already_destroyed", "job_id": id,
		"note": i18n.T("The stack was destroyed earlier; its deletions were all finished.",
			"Стек удалён раньше; все его удаления завершены.")})
}

func (s *Registry) planStackDestroy(_ context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "job_id")
	if id == "" {
		return answered(usageT("'job_id' is required", "нужен 'job_id'"), true), nil
	}
	// The execution checks the job again under its lock: another process
	// may have changed it since the plan.
	execute := func(ctx context.Context) (string, bool) {
		unlock, err := s.lockJob(id)
		if err != nil {
			return errResult(err), true
		}
		defer unlock()
		job, err := stack.LoadJob(id)
		if err != nil {
			return errResult(err), true
		}
		// A 404 from another project would read as "already gone" and the job
		// would be marked destroyed with its resources still billed (QA P2).
		if err := s.checkJobProject(job); err != nil {
			return errResult(err), true
		}
		// confirmed=true answers as the preview: the CLI's --yes stops at the
		// preview's already_destroyed, MCP gets here (TestParity_YesEqualsConfirmed).
		if job.Destroyed() {
			return alreadyDestroyed(id), false
		}
		// Reconcile ids from the STEPS before teardown: a resource created but not yet
		// "done" (e.g. a k8s cluster still PROVISIONING) has its id on the step, not in
		// Outputs — without this, destroying mid-deploy would ORPHAN it. Also resolve
		// by-name for async resources whose op hadn't completed.
		for _, st := range job.Steps {
			if job.Outputs[st.Key] != "" {
				continue
			}
			if st.ResourceID != "" {
				job.Outputs[st.Key] = st.ResourceID
			} else if st.MayHaveCreated() {
				if id, _ := s.findStepResource(ctx, st); id != "" {
					job.Outputs[st.Key] = id
				}
			}
		}
		// Reverse teardown of what was created. A resource already gone is
		// success: the delete tools answer already_deleted (D26), and on a
		// server bound to its projects (H9) they keep NOT_FOUND, which reads
		// the same here. The code (VM_NOT_FOUND, NOT_FOUND) is checked too: in the CLI the
		// message is in the language of the CLI, and destroy of a VM already
		// gone says "no VM … in project …; nothing was deleted" (live run 18.10:
		// a second stack_destroy turned a destroyed job into destroy_failed).
		// deleting: async deletions the cloud accepted but has not finished
		// (status "deleting" with an operation): Postgres takes ~5 min, so the
		// answer must not read as if it were gone already (release audit, L-37).
		var deleted, deleting, failed []string
		del := func(label, tool string, dargs map[string]any) {
			dargs["confirmed"] = true
			if res, e := s.callTool(ctx, tool, dargs); e != nil {
				msg := e.Error()
				reason, _ := res["reason"].(string)
				// The code decides; the text only for an answer without one: a
				// 404 the service did not write is API_ERROR and names the 404
				// in its message (D63).
				gone := reason == clierrors.CodeVMNotFound || reason == clierrors.CodeNotFound
				if reason == "" {
					gone = strings.Contains(msg, "404") || strings.Contains(msg, "NOT_FOUND") ||
						strings.Contains(strings.ToLower(msg), "not found") || strings.Contains(msg, "не найден")
				}
				if gone {
					deleted = append(deleted, label+" (already gone)")
				} else {
					failed = append(failed, label+": "+msg)
				}
			} else {
				switch st, _ := res["status"].(string); st {
				case "already_deleted":
					deleted = append(deleted, label+" (already gone)")
				case "deleting":
					// Not deleted yet: in deleting only (D96).
					deleting = append(deleting, label)
				default:
					deleted = append(deleted, label)
				}
			}
		}
		for _, tool := range stackTeardown {
			for _, st := range job.Steps {
				ref := job.Outputs[st.Key]
				if st.Tool != tool || ref == "" {
					continue
				}
				c := decls[tool].Created
				dt := deleteTargets[c.delete]
				if c.arg != "" {
					// Deleted by the argument that named it (a secret by its
					// path): its output only says it was made.
					ref = stringArg(st.Args, c.arg)
				} else if s.argRefused(c.delete, dt.arg, ref) {
					// Not of the form the delete takes (its schema, D36): a job
					// saved before G13-14 holds "ok" for its SNAT gateway. Its
					// name finds it; nothing found is gone.
					if ref = s.findStepID(ctx, st); ref == "" {
						deleted = append(deleted, st.Key+" (already gone)")
						continue
					}
				}
				dargs := map[string]any{dt.arg: ref}
				if dt.parent != "" {
					dargs[dt.parent] = st.Args[dt.parent]
				}
				del(st.Key, c.delete, dargs)
			}
		}
		// A job with a failed deletion, or one the cloud is still deleting,
		// still holds resources: "destroyed" would hide it from the duplicate
		// warning of stack_plan and read as gone (axis 3 of CLAUDE.md §6, D96).
		// Calling stack_destroy again retries; what is gone already counts as
		// deleted.
		job.Status, job.Deleting = stack.TeardownStatus(deleting), deleting
		if len(failed) > 0 {
			job.Status = "destroy_failed"
		}
		if e := job.Save(); e != nil {
			failed = append(failed, "persist job: "+e.Error())
		}
		out := map[string]any{"status": job.Status, "deleted": deleted, "failed": failed}
		if len(deleting) > 0 {
			out["deleting"] = deleting
			out["note"] = "Deletion accepted; the resources in 'deleting' are still being removed by the cloud (minutes; Postgres ~5 min). Check them with pg_list, k8s_list, lb_list and the like before reusing their names."
			// The same call checks them again and finishes what waits for
			// them — a VM's floating IP and record (D69).
			stackDestroyNext(out, id)
		}
		if len(failed) > 0 {
			// Every error answer has a reason (docs/mcp.md «Ошибки»). This one
			// is the reason the CLI has given the answer since it had none
			// (toolError in internal/cli): NOT_FOUND when it says something
			// was not found, else API_ERROR — the same code and exit code
			// (spec.md REQ-8, TestParity_ErrorReasonEqualsTool).
			out["reason"] = clierrors.CodeAPIError
			if low := strings.ToLower(toJSON(out)); strings.Contains(low, "not found") || strings.Contains(low, "не найден") {
				out["reason"] = clierrors.CodeNotFound
			}
		}
		return toJSON(out), len(failed) > 0
	}
	// The preview names what goes: each step's resource as the job
	// knows it (release audit, R-04, re-run N-11).
	job, err := stack.LoadJob(id)
	if err != nil {
		return nil, err
	}
	// The confirmed call refuses a job of another project: the preview
	// does so before the question (Cursor review of PR #89).
	if err := s.checkJobProject(job); err != nil {
		return nil, err
	}
	// Its resources were deleted by an earlier stack_destroy and none
	// was still being deleted: nothing to ask about (D26, G130-31; D41).
	if job.Destroyed() {
		return answered(alreadyDestroyed(id), false), nil
	}
	// Deletions the cloud was still finishing: the question again, so
	// that the CLI's --yes, which calls this preview first, gets to the
	// confirmed call that re-checks them, as MCP does (cloud-e2e 1.3.1).
	if job.DeletionsPending() {
		res := make([]map[string]any, 0, len(job.Deleting))
		for _, st := range job.Steps {
			if slices.Contains(job.Deleting, st.Key) {
				res = append(res, map[string]any{"step": st.Key, "status": "deleting", "id": job.Outputs[st.Key]})
			}
		}
		return &Plan{Execute: execute, Preview: map[string]any{"resources": res,
			"message": i18n.Tf("stack %s is being deleted: the cloud is still deleting %d resource(s) listed in resources; stack_destroy checks them again and deletes what is left.",
				"стек %s удаляется: облако ещё удаляет ресурсы (%d), перечисленные в resources; stack_destroy проверит их снова и удалит, что осталось.", id, len(res))}}, nil
	}
	var res []map[string]any
	for _, st := range job.Steps {
		rid := firstNonEmptyStr(job.Outputs[st.Key], st.ResourceID)
		if rid == "" && !st.MayHaveCreated() {
			continue
		}
		row := map[string]any{"step": st.Key, "status": st.Status}
		if rid != "" {
			row["id"] = rid
		}
		if f, ok := stepFinder(st); ok {
			row["name"] = f.name
		}
		res = append(res, row)
	}
	out := map[string]any{
		"message": i18n.Tf("stack_destroy PERMANENTLY deletes the %d resource(s) of stack %s listed in resources.",
			"stack_destroy БЕЗВОЗВРАТНО удаляет ресурсы стека (%d) %s, перечисленные в resources.", len(res), id),
		"resources": res}
	if len(res) == 0 {
		out["message"] = i18n.Tf("stack %s has created nothing yet; stack_destroy only marks it destroyed.",
			"стек %s ещё ничего не создал; stack_destroy только отметит его удалённым.", id)
	}
	return &Plan{Execute: execute, Preview: out}, nil
}
