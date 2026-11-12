// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// handleVMGet returns LIVE details for a single VM (by vm_id or name): its
// private IP, any public/floating IP, state, and every network interface.
//
// This closes a real gap: the `list` and `status` tools read local state and do
// NOT expose a VM's private IP, so an agent that needed a freshly-created VM's
// private address (e.g. to wire a service endpoint) had no tool for it and would
// fall back to raw shell/API probing. GetVMDetail already carries the data
// (interfaces[].ip_address); this tool surfaces and normalizes it.
//
// IP normalization is grounded in the real API shape:
//   - a "regular" interface's ip_address is the PRIVATE (subnet) address;
//   - a "direct_ip" interface's ip_address is a directly-attached PUBLIC address;
//   - a floating_ip (when present on a regular interface) is its PUBLIC address.
//
// Read-only; touches eng.Cloud, so it uses authedEngine.
// vmRefNotBoth is the refusal of a call with both vm_id and name (vm_get,
// destroy).
func vmRefNotBoth() error {
	return clierrors.New(clierrors.CodeUsage,
		i18n.T("pass vm_id or name, not both", "укажите vm_id или name, не оба"),
		i18n.T("vm_id is the VM's UUID; name finds the VM by its name.",
			"vm_id — UUID ВМ; по name ВМ ищется по имени."))
}

func (s *Registry) handleVMGet(ctx context.Context, args map[string]any) (string, bool) {
	// Both: the vm_id was taken and the name dropped without a word (QA
	// 20.10, QA-11), as release_floating_ip did with id and ip_address; one
	// of only spaces was dropped as well (break r4, R4-11).
	ref, _, err := oneOfArgs(args, vmRefNotBoth(), "vm_id", "name")
	if err != nil {
		return errResult(err), true
	}
	if ref == "" {
		return usageT("vm_id or name is required", "нужен vm_id или name"), true
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}

	// Accept either a UUID or a human name.
	vmID := ref
	if !looksUUID(ref) {
		vm, e := s.resolveVM(ctx, ref, eng.Config.ProjectID)
		if e != nil {
			return errResult(e), true
		}
		vmID = vm.ID
	}

	det, err := eng.Cloud.GetVMDetail(ctx, vmID)
	if err != nil {
		return errResult(err), true
	}

	privateIP, publicIP := vmIPs(det)
	ifaces := make([]map[string]any, 0, len(det.Interfaces))
	for _, in := range det.Interfaces {
		row := map[string]any{
			"id":         in.ID,
			"name":       in.Name,
			"ip_address": in.IPAddress,
			"type":       in.Type,
			"primary":    in.Primary,
			"state":      in.State,
		}
		if in.FloatingIP != nil && in.FloatingIP.IPAddress != "" {
			row["floating_ip"] = in.FloatingIP.IPAddress
		}
		ifaces = append(ifaces, row)
	}

	return toJSON(map[string]any{
		"id":         det.ID,
		"name":       det.Name,
		"state":      det.State,
		"private_ip": privateIP,
		"public_ip":  publicIP,
		"interfaces": ifaces,
	}), false
}

// vmIPs are the private and the public address of a VM, by the rule of
// vm_get: the primary interface's when several have one.
func vmIPs(det *provision.VMDetail) (privateIP, publicIP string) {
	for _, in := range det.Interfaces {
		if in.FloatingIP != nil && in.FloatingIP.IPAddress != "" {
			if publicIP == "" || in.Primary {
				publicIP = in.FloatingIP.IPAddress
			}
		}
		if in.Type == "direct_ip" {
			// The interface IP itself is the public address.
			if in.IPAddress != "" && (publicIP == "" || in.Primary) {
				publicIP = in.IPAddress
			}
		} else if in.IPAddress != "" && (privateIP == "" || in.Primary) {
			// "regular" (or unspecified) => subnet-internal private IP.
			privateIP = in.IPAddress
		}
	}
	return privateIP, publicIP
}

// handleVMList lists the project's VMs from the live API (read-only).
func (s *Registry) handleVMList(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	vms, err := eng.Cloud.ListVMsInProject(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(vms))
	for _, v := range vms {
		row := map[string]any{
			"id":    v.ID,
			"name":  v.Name,
			"state": v.Status,
			"zone":  v.ZoneID(),
		}
		if v.AvailabilityZone != nil && v.AvailabilityZone.Name != "" {
			row["zone"] = v.AvailabilityZone.Name
		}
		if v.FlavorName != "" {
			row["flavor"] = v.FlavorName
		}
		if ip := v.PublicIP; ip != "" {
			row["public_ip"] = ip
		} else if v.FloatingIP != "" {
			row["public_ip"] = v.FloatingIP
		}
		if !v.CreatedAt.IsZero() {
			row["created_time"] = v.CreatedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"vms": out}), false
}

// vmPowerAction is a power operation on a VM (D99, the first wave of
// second-day operations; docs/internal/briefs/ops-wave-1.md §2): the
// command of POST /vms/{id}/set-power, the state the VM is taken from,
// the transient state the command puts it in and the state the transition
// ends in; already and done are the statuses of the answer — of a repeat
// that finds the change made and of the change this call made. An action
// every send of which is a change of its own (a reboot: from and to are
// one state) has no already: the target state before the command is not
// a change made, and after it counts only once the transition was seen.
type vmPowerAction struct {
	command, from, via, to string
	already, done          string
	// message is the preview's line, {en, ru}.
	message [2]string
	// verb and noun are the action in the words of a person, {en, ru}:
	// «to start», «the start». The texts of a refusal are made of them,
	// not of the platform's command.
	verb, noun [2]string
	// billed is what the change starts to bill, {en, ru}; empty when it
	// starts no billing.
	billed [2]string
}

// vmPowerActions is the one table of the power tools: a tool is a line of
// it on the one planner (planVMPower), not a handler of its own.
var vmPowerActions = map[string]vmPowerAction{
	"vm_start": {command: provision.PowerOn, from: "stopped", via: "starting", to: "running",
		already: "already_running", done: "started",
		message: [2]string{"vm_start starts the stopped VM (power_on).", "vm_start запускает остановленную ВМ (power_on)."},
		verb:    [2]string{"start", "запускать"}, noun: [2]string{"the start", "запуск"},
		billed: [2]string{"price unknown, the resource is billable: a running VM is billed for its flavor by the rates at tariff_url",
			"цена неизвестна, ресурс платный: запущенная ВМ тарифицируется по своей конфигурации, тарифы — по tariff_url"}},
}

// vmSettling are the states of a VM another change put it in and the
// platform takes it out of on its own: a power tool waits for them to
// settle before it decides (П4, as a disk's, D53).
var vmSettling = map[string]bool{"starting": true, "stopping": true, "rebooting": true, "creating": true, "updating": true}

// vmNothingToChange are the states of a VM there is nothing to power: it
// is being deleted, or was never made.
var vmNothingToChange = map[string]bool{"deleting": true, "error_deleting": true, "error_creating": true}

// vmSettleTimeout bounds the wait before the question for a VM in a
// transient state of another change, as a disk's (D53: 30 s, under the
// call timeout of MCP clients).
const vmSettleTimeout = 30 * time.Second

// vmPowerTimeout bounds the wait for the transition a power command
// started. How long the platform takes is not measured yet (probe Г-10 of
// the 1.9 plan): the bound is the one evo holds a new disk to.
const vmPowerTimeout = 5 * time.Minute

// planVMPower is the planner of a power tool (gate.go; D99, П1–П6): it
// reads the VM before the question and answers by its state — the change
// is made already (a repeat: success with no question and no write), it
// is on its way (waited for with no question and no second command),
// there is nothing to change (refused before the question), or the
// preview of the command.
func (s *Registry) planVMPower(tool string) Planner {
	act := vmPowerActions[tool]
	return func(ctx context.Context, args map[string]any) (*Plan, error) {
		ref, _, err := oneOfArgs(args, vmRefNotBoth(), "vm_id", "name")
		if err != nil {
			return nil, err
		}
		if ref == "" {
			return nil, usageErr("vm_id or name is required", "нужен vm_id или name")
		}
		eng, err := s.authedEngine(ctx, nil)
		if err != nil {
			return nil, err
		}
		// An id is read as it is; a name is resolved at the one point
		// names are (resolveVMFor), strictly.
		id := ref
		if !looksUUID(ref) {
			found, err := s.resolveVMFor(ctx, tool, ref, eng.Config.ProjectID)
			if err != nil {
				return nil, err
			}
			id = found.ID
		}
		vm, err := eng.Cloud.GetVMDetail(ctx, id)
		if err != nil {
			return nil, err
		}
		state := vm.State
		// The transient state of another change settles first. The
		// transition of this very change is not another's: it is waited
		// for below, to its end.
		if vmSettling[state] && state != act.via {
			last, timedOut, atLimit, err := s.waitVM(ctx, eng, vm, vmSettleTimeout,
				func(st string) (bool, error) { return !vmSettling[st] || st == act.via, nil })
			switch {
			case err != nil:
				return nil, err
			case atLimit:
				// No question yet: the state, and the same call again.
				ans := vmAnswer(vm, "waiting", last)
				ans["next"] = fmt.Sprintf("VM %s is %s at the time limit of an MCP call; it cannot be changed until its state settles. Call %s again when it settles: it shows the preview and asks before it sends the command.", vm.ID, last, tool)
				nextSameCall(ans, tool, vmRef(args, vm))
				return answered(toJSON(ans), false), nil
			case timedOut > 0:
				return nil, clierrors.New(clierrors.CodeUsage,
					i18n.Tf("VM %s is in state %s: it cannot be changed until the state settles", "ВМ %s в состоянии %s: менять её можно, когда состояние устоится", vm.ID, last),
					i18n.T("Repeat the call when vm_get shows the VM running or stopped.", "Повторите вызов, когда vm_get покажет ВМ в running или stopped."))
			}
			state = last
		}
		switch {
		case state == act.to && act.already != "":
			return answered(toJSON(vmAnswer(vm, act.already, state)), false), nil
		case state == act.via:
			// A start in flight: this call's own, cut before its end, or
			// another caller's. No command is sent, so no question.
			since, _ := s.heldSince(eng, tool, "vm_id="+vm.ID)
			text, isErr := s.finishVMPower(ctx, eng, tool, act, vm, args, false, since)
			return answered(text, isErr), nil
		case vmNothingToChange[state]:
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("VM %s is in state %s: there is nothing to %s", "ВМ %s в состоянии %s: %s нечего", vm.ID, state, i18n.T(act.verb[0], act.verb[1])),
				i18n.T("vm_get shows the VM; what to do with it is the person's decision.", "ВМ показывает vm_get; что с ней делать — решает человек."))
		}
		// What the write sends and what it changes: the command, and the
		// VM as it was read before the question.
		will := map[string]any{"vm_id": vm.ID, "name": vm.Name, "vm_state": state, "command": act.command}
		if _, public := vmIPs(vm); public != "" {
			will["public_ip"] = public
		}
		if act.billed != [2]string{} {
			// What the change starts to bill, as a create's preview says
			// it: no price without a probe of the price — the class and
			// where the rates are (CLAUDE.md §6, axis 4).
			will["price_classes"] = []engine.ResourcePrice{{Resource: engine.ResourceVM, Class: engine.PricePaidUnpriced,
				Note: i18n.T(act.billed[0], act.billed[1])}}
			will["tariff_url"] = engine.TariffURL(engine.ResourceVM)
		}
		preview := map[string]any{"message": i18n.T(act.message[0], act.message[1]), "preview": will}
		if state != act.from {
			// Not refused: whether the platform takes the command in this
			// state is not known (conclusion 111, the question of error).
			preview["warning"] = i18n.Tf("the VM is in state %s, not %s: whether the platform accepts %s is unknown",
				"ВМ в состоянии %s, а не %s: примет ли платформа %s — неизвестно", state, act.from, i18n.T(act.noun[0], act.noun[1]))
		}
		return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
			if err := eng.Cloud.SetVMPower(ctx, vm.ID, act.command); err != nil {
				return s.vmPowerRefused(ctx, eng, tool, act, vm, args, state, err)
			}
			return s.finishVMPower(ctx, eng, tool, act, vm, args, true, time.Time{})
		}}, nil
	}
}

// vmPowerRefused answers a power command that failed (П6). The platform
// refuses a VM it does not find and a state it does not accept with one
// 422, and a lost answer may be a command taken: the VM is read again —
// on its way to the target or there, the call goes on as if the command
// were taken; gone, it is not found; else the error with the state the VM
// is in. A refusal from a state the preview warned of is not one a repeat
// gets past: its hint names no next call.
func (s *Registry) vmPowerRefused(ctx context.Context, eng *engine.Engine, tool string, act vmPowerAction,
	vm *provision.VMDetail, args map[string]any, planned string, cause error) (string, bool) {
	if ctx.Err() != nil {
		return errResult(cause), true
	}
	now, err := eng.Cloud.GetVMDetail(ctx, vm.ID)
	if err != nil {
		var ce *clierrors.CLIError
		if errors.As(err, &ce) && ce.Code == clierrors.CodeVMNotFound {
			return errResult(err), true
		}
		return errResult(cause), true
	}
	if now.State == act.to || now.State == act.via {
		return s.finishVMPower(ctx, eng, tool, act, now, args, true, time.Time{})
	}
	if provision.HTTPStatus(cause) != http.StatusUnprocessableEntity {
		return errAnswer(cause, map[string]any{"vm_id": vm.ID, "state": now.State}), true
	}
	// One machine field for the cause, of the form of a quota refusal's
	// (details.cause); the platform's own code goes beside it as the
	// platform worded it (conclusion 111, У4). The state the command is
	// accepted in is not the cause of its refusal: the platform refuses
	// for a reason of its own (changes prohibited in the zone), which a
	// repeat does not get past (conclusion 123, К3); nor does a repeat
	// from the state the preview warned of. Only a state that moved
	// between the plan and the command is one to wait out.
	noRepeat := i18n.T("A repeat will not help: the platform does not take this command for this VM now. The Cloud.ru console or support is the way.",
		"Повтор не поможет: платформа сейчас не принимает эту команду для этой ВМ. Путь — консоль Cloud.ru или поддержка.")
	details := map[string]any{"cause": "state"}
	hint := noRepeat
	switch {
	case now.State != planned:
		// The state moved between the plan and the command — first of
		// all: the refusal was of the state the VM was in then, and a
		// repeat plans anew from the state it is in now.
		hint = i18n.T("The state of the VM changed while the call was made; repeat the call: it reads the VM again.", "Состояние ВМ изменилось, пока шёл вызов; повторите вызов: он прочитает ВМ заново.")
	case now.State == act.from:
		details["cause"] = "refused"
	}
	if code := platformCode(cause); code != "" {
		details["platform_code"] = code
	}
	return errAnswer(clierrors.Wrap(clierrors.CodeAPIError,
		i18n.Tf("the platform refused %s of VM %s in state %s", "платформа отказала: %s ВМ %s в состоянии %s", i18n.T(act.noun[0], act.noun[1]), vm.ID, now.State), hint, cause),
		map[string]any{"vm_id": vm.ID, "state": now.State, "details": details}), true
}

// platformCode is the code of the first error of a refusal of the compute
// API, whose body is a list of {code, message}; "" when the body is not
// that.
func platformCode(err error) string {
	var se *provision.StatusError
	if !errors.As(err, &se) {
		return ""
	}
	var body []struct {
		Code string `json:"code"`
	}
	if json.Unmarshal([]byte(se.Body), &body) != nil || len(body) == 0 {
		return ""
	}
	return body[0].Code
}

// finishVMPower waits for the end of the transition of act and answers
// it (П5): the target state is the success; the limit of an MCP call
// (D69) is the transient state and — for the call that sent the command
// (sent) — the same call as the next step, which finds the VM on its way
// and waits again without a command; a transition somebody else started,
// and one this call's own limit ran out on, name the read of the VM
// (vm_get) as the next step — a status that says the change is going on
// always names one (Н3). A VM that left the transition for another state
// is an error with that state.
//
// since, when not zero, is the start of this very operation by an earlier
// call of this key (heldSince): the call is a step of evo's own change and
// goes on with it as the call that sent the command does — but only until
// the limit of the operation, counted from its start and not from each
// repeat; past it evo waits no more, and the next step is the read.
func (s *Registry) finishVMPower(ctx context.Context, eng *engine.Engine, tool string, act vmPowerAction,
	vm *provision.VMDetail, args map[string]any, sent bool, since time.Time) (string, bool) {
	started := clock.Now()
	limit, own := vmPowerTimeout, sent
	if !since.IsZero() {
		// wall-clock: the age of the operation decides whether evo goes on
		// with it; the value is also said in the answer (since_start_sec).
		if limit = vmPowerTimeout - clock.Since(since); limit <= 0 {
			ans := vmAnswer(vm, act.via, vm.State)
			ans["since_start_sec"] = int(clock.Since(since).Seconds())
			ans["next"] = fmt.Sprintf("VM %s is still %s %d s after %s began it; evo waits for it no more. vm_get shows its state.", vm.ID, vm.State, int(clock.Since(since).Seconds()), tool)
			nextStep(ans, "vm_get", map[string]any{"vm_id": vm.ID})
			return toJSON(ans), false
		}
		own = true
	}
	// moved: the VM was seen on its way. A call that sent no command found
	// it there. Where the target is the state the VM was taken from, the
	// target reads as reached only after the transition was seen.
	moved := !sent
	last, timedOut, atLimit, err := s.waitVM(ctx, eng, vm, limit, func(st string) (bool, error) {
		switch {
		case st == act.to && (moved || act.to != act.from):
			return true, nil
		case st == act.via:
			moved = true
			return false, nil
		case st == act.from && !moved:
			// The command is taken and the state has not moved yet.
			return false, nil
		case st == act.from:
			// On its way, and back where it was: the change did not take
			// place. Not «still waiting» — a wait here, with a next step,
			// would send the command again on the consent already given
			// («Риск», conclusion 123, К2).
			return true, clierrors.New(clierrors.CodeAPIError,
				i18n.Tf("%s of VM %s did not take place: the VM was %s and is %s again", "%s ВМ %s не состоялся: ВМ была в %s и снова в %s", i18n.T(act.noun[0], act.noun[1]), vm.ID, act.via, st),
				i18n.T("What to do with the VM is the person's decision; vm_get shows it.", "Что делать с ВМ — решает человек; её показывает vm_get."))
		}
		return true, clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("VM %s is in state %s after %s, not %s", "ВМ %s: состояние %s после команды «%s», а не %s", vm.ID, st, i18n.T(act.noun[0], act.noun[1]), act.to),
			i18n.T("vm_get shows the VM.", "ВМ показывает vm_get."))
	})
	elapsed := int(clock.Since(started).Seconds())
	switch {
	case err != nil:
		return errAnswer(err, map[string]any{"vm_id": vm.ID, "state": last}), true
	case (atLimit || timedOut > 0) && !moved:
		// The platform took the command and the VM never left its state:
		// not a change going on, and not one a repeat goes on with (К2).
		return errAnswer(clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("the platform accepted %s of VM %s, and its state has not changed in %d s: still %s", "платформа приняла команду «%s» для ВМ %s, а состояние за %d с не изменилось: по-прежнему %s", i18n.T(act.noun[0], act.noun[1]), vm.ID, elapsed, last),
			i18n.T("vm_get shows the VM; whether to send the command again is the person's decision.", "ВМ показывает vm_get; слать ли команду снова — решает человек.")),
			map[string]any{"vm_id": vm.ID, "state": last, "elapsed_sec": elapsed}), true
	case atLimit || timedOut > 0:
		// The status is the action's word for «on its way», not the raw
		// state: the statuses of a tool are a closed list.
		ans := vmAnswer(vm, act.via, last)
		ans["elapsed_sec"] = elapsed
		if !since.IsZero() {
			ans["since_start_sec"] = int(clock.Since(since).Seconds())
		}
		if atLimit && own {
			ans["next"] = fmt.Sprintf("VM %s is %s at the time limit of an MCP call. Call %s to go on: it waits for the VM again and sends no second command.", vm.ID, last, tool)
			nextSameCall(ans, tool, vmRef(args, vm))
		} else {
			// Not this call's to go on with: the next step is the read.
			ans["next"] = fmt.Sprintf("VM %s is still %s. vm_get shows its state.", vm.ID, last)
			nextStep(ans, "vm_get", map[string]any{"vm_id": vm.ID})
		}
		return toJSON(ans), false
	}
	// The VM as it is after the transition: its address may have changed.
	if now, err := eng.Cloud.GetVMDetail(ctx, vm.ID); err == nil {
		vm = now
	}
	ans := vmAnswer(vm, act.done, act.to)
	ans["elapsed_sec"] = elapsed
	return toJSON(ans), false
}

// waitVM polls the state of vm until done ends the wait (waitState),
// saying each state it passes.
func (s *Registry) waitVM(ctx context.Context, eng *engine.Engine, vm *provision.VMDetail, limit time.Duration,
	done func(state string) (bool, error)) (last string, timedOut time.Duration, atLimit bool, err error) {
	return waitState(ctx, eng, limit,
		func(ctx context.Context) (string, bool, error) {
			d, err := eng.Cloud.GetVMDetail(ctx, vm.ID)
			var ce *clierrors.CLIError
			switch {
			case errors.As(err, &ce) && ce.Code == clierrors.CodeVMNotFound:
				// The VM was deleted while the call waited: not found, not
				// «still starting» (П6; audit of the reference, Н2).
				return "", false, err
			case err != nil:
				return "", false, nil
			}
			return d.State, true, nil
		},
		func(st string) string {
			return fmt.Sprintf(i18n.T("VM %s: %s, waiting…", "ВМ %s: %s, жду…"), vm.ID, st)
		}, done)
}

// vmAnswer is the answer of a power tool: its status and the VM — id,
// name, state and the public address it has now.
func vmAnswer(vm *provision.VMDetail, status, state string) map[string]any {
	ans := map[string]any{"status": status, "vm_id": vm.ID, "name": vm.Name, "state": state}
	if _, public := vmIPs(vm); public != "" {
		ans["public_ip"] = public
	}
	return ans
}

// vmRef are the arguments of the call with the VM named by its id: the
// next step of a call that named the VM by name waits for the VM the call
// read, not for whichever VM has the name by then.
func vmRef(args map[string]any, vm *provision.VMDetail) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if k != "name" {
			out[k] = v
		}
	}
	out["vm_id"] = vm.ID
	return out
}
