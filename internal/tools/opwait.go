// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// opwait: a single, uniform "wait for an async operation" contract shared by every
// *_operation tool. The MCP model stays async by default (one call → current state),
// but a caller that wants the RESULT can pass wait_seconds and get it in ONE call —
// the engine polls with capped exponential backoff instead of the agent hammering
// raw get/list every 2-3s. Single resource → optional capped wait here; multi-step
// orchestration → stack_status owns the waiting. No blocking beyond the cap.

// opWaitMaxSeconds is the longest wait_seconds of an *_operation tool: D69's
// budget of an MCP call (MCPCallBudget), in the schema (opWaitProp).
const opWaitMaxSeconds = int(MCPCallBudget / time.Second)

// opWaitDescRU is wait_seconds of the *_operation tools in the CLI's
// Russian (opWaitProp).
var opWaitDescRU = fmt.Sprintf("Ждать итогового состояния до N секунд (не больше %d), опрашивая операцию с нарастающим интервалом. "+
	"Без флага — одна проверка", opWaitMaxSeconds)

// stackStatusMaxWait is the longest wait of one stack_status call.
const stackStatusMaxWait = 20

// opWaitFirstDelay is the first poll interval of waitForOpJSON. A variable only
// so that tests can shorten it; production code never changes it.
var opWaitFirstDelay = 2 * time.Second

// opTerminalStates are the status/state values we treat as "operation finished".
// RUNNING is not one of them: for an operation it means "still in progress", and
// wait_seconds would return on the first poll. An operation that ends without
// one of these states still ends on done=true or a non-empty error; at worst the
// wait runs to its cap and returns the last state.
var opTerminalStates = map[string]bool{
	"DONE": true, "READY": true, "ACTIVE": true, "AVAILABLE": true,
	"COMPLETED": true, "COMPLETE": true, "SUCCEEDED": true, "SUCCESS": true,
	"ERROR": true, "FAILED": true, "FAILURE": true, "CANCELLED": true,
	"CANCELED": true, "DELETED": true,
}

// opTerminal reports whether an operation JSON map looks finished: an explicit
// done=true, a terminal status/state string, or a non-null error field.
func opTerminal(m map[string]any) bool {
	if m == nil {
		return false
	}
	if b, ok := m["done"].(bool); ok && b {
		return true
	}
	for _, k := range []string{"status", "state", "phase"} {
		if v, ok := m[k].(string); ok && opTerminalStates[strings.ToUpper(strings.TrimSpace(v))] {
			return true
		}
	}
	if e, ok := m["error"]; ok && e != nil {
		if s, isStr := e.(string); !isStr || strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// opToMap converts a typed operation value to a generic JSON map so waitForOpJSON
// can inspect status fields uniformly across every service's Operation type.
func opToMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

// waitForOpJSON polls fetch() with capped exponential backoff (2s → 15s, capped at
// wait_seconds, hard max opWaitMaxSeconds) until the operation is terminal or the
// budget elapses. Returns the last observed state. Honors ctx cancellation.
// The person at the CLI sees the wait as one line (Caller.Progress).
func waitForOpJSON(ctx context.Context, waitSeconds int, fetch func(context.Context) (map[string]any, error)) (map[string]any, error) {
	if waitSeconds > opWaitMaxSeconds {
		waitSeconds = opWaitMaxSeconds
	}
	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	// Within the budget of the call (D69): an MCP call answers with the
	// operation's state so far, and the agent asks again. Its reads end
	// with the call too (callCtx): the last one had no bound.
	callCtx, cancel := callCtx(ctx)
	defer cancel()
	if until, bounded := callCtx.Deadline(); bounded && until.Before(deadline) {
		deadline = until
	}
	// cut: the budget, not the caller, ended a read or the sleep.
	cut := func() bool { return ctx.Err() == nil && callCtx.Err() != nil }
	delay := opWaitFirstDelay
	pp, _ := progressOf(ctx)
	wait := engine.NewWaitLine(pp, 0, func(elapsed, state string) string {
		return i18n.Tf("waiting for the operation… %s (%s)", "ожидание операции… %s (%s)", elapsed, state)
	})
	defer wait.Done()
	var last map[string]any
	for {
		m, err := fetch(callCtx)
		if err != nil {
			if cut() && last != nil {
				return stillRunning(last, waitSeconds), nil
			}
			return last, err
		}
		last = m
		if opTerminal(m) {
			return m, nil
		}
		wait.State(opState(m))
		if !time.Now().Before(deadline) {
			return stillRunning(m, waitSeconds), nil
		}
		if rem := time.Until(deadline); rem < delay {
			delay = rem
		}
		if delay <= 0 {
			return stillRunning(m, waitSeconds), nil
		}
		if err := sleepShowing(callCtx, delay, wait); err != nil {
			if cut() {
				return stillRunning(last, waitSeconds), nil
			}
			return last, err
		}
		if delay < 15*time.Second {
			delay += delay / 2 // 2 → 3 → 4.5 → 6.75 → 10 → 15 → 15…
		}
	}
}

// sleepShowing waits d, or until ctx ends, keeping the wait line going.
func sleepShowing(ctx context.Context, d time.Duration, wait *engine.WaitLine) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait.C():
			wait.Show()
		case <-timer.C:
			return nil
		}
	}
}

// opState is the state an operation reports (status, state or phase), as
// the wait line shows it.
func opState(m map[string]any) string {
	for _, k := range []string{"status", "state", "phase"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	if b, ok := m["done"].(bool); ok {
		return fmt.Sprintf("done=%t", b)
	}
	return ""
}

// stillRunning marks an operation the wait gave up on: done=false alone
// read as an answer, not as "still going" (release audit re-run, N-08).
func stillRunning(m map[string]any, waitSeconds int) map[string]any {
	out := make(map[string]any, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	out["still_running"] = true
	out["note"] = fmt.Sprintf("not done after waiting %d s; call again (wait_seconds) to keep waiting", waitSeconds)
	return out
}

// nextStep puts into an answer that a budget or wait=false cut short the
// tool to call next and its arguments (D69): the machine's next step,
// beside the text of "next" for the person. A scope argument with no value
// is left out: the next call would be refused for it (D100), and an agent
// follows next_args to the letter.
func nextStep(answer map[string]any, tool string, args map[string]any) {
	next := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" && slices.Contains(scopeArgs, k) {
			continue
		}
		next[k] = v
	}
	answer["next_tool"] = tool
	answer["next_args"] = next
}

// waitState polls the state of a resource until done says the state ends
// the wait, at the VM poll interval (Engine.Timings.VMPollInterval, 5 s by
// default), for at most limit (or Timings.VMPollTimeout when shorter). read
// gives the state and whether the read saw the resource; a read that did
// not is tried again. waiting words each state that does not end the wait,
// and the wait says it when the state changes: a wait is not silent, in the CLI and over MCP alike
// (CLAUDE.md §6, axis 2). It returns the last state read and how the wait
// ended: by done (err is done's), by its own limit (timedOut is how long it
// waited) or by the budget of an MCP call (atLimit, D69) — the resource
// goes on in the cloud, and the caller answers its state and the next
// step, not an error. One loop for the waits on a state: a disk's, a VM's
// (D99, П4 and П5).
func waitState(ctx context.Context, eng *engine.Engine, limit time.Duration,
	read func(context.Context) (state string, seen bool), waiting func(state string) string,
	done func(state string) (bool, error)) (last string, timedOut time.Duration, atLimit bool, err error) {
	pp, _ := progressOf(ctx)
	every := 5 * time.Second
	if eng.Timings.VMPollInterval > 0 {
		every = eng.Timings.VMPollInterval
	}
	timeout := limit
	if t := eng.Timings.VMPollTimeout; t > 0 && t < timeout {
		timeout = t
	}
	// Within an MCP call the call's budget ends the wait too (D69).
	timeout, capped := callBound(ctx, timeout)
	deadline := time.Now().Add(timeout)
	last = "unknown"
	for {
		if st, seen := read(ctx); seen {
			ok, err := done(st)
			if st != last && !ok {
				pp.Step(0, 0, waiting(st))
			}
			last = st
			if ok {
				return st, 0, false, err
			}
		}
		if !time.Now().Before(deadline) {
			if capped {
				return last, 0, true, nil
			}
			return last, timeout, false, nil
		}
		select {
		case <-ctx.Done():
			return last, 0, false, ctx.Err()
		case <-time.After(min(every, time.Until(deadline))):
		}
	}
}
