// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
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
// that finds the change made and of the change this call made.
type vmPowerAction struct {
	command, from, via, to string
	already, done          string
	// message is the preview's line, {en, ru}.
	message [2]string
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

// strictVMName are the tools that refuse a name two VMs of the project
// have, naming both, where resolveVM takes the first («Риск», conclusion
// 111, У2). The platform refuses a second VM of a name (409 on create), so
// this is a guard, not a path. Whether the released tools over resolveVM
// change their answer the same way is the owner's decision, open: until
// then theirs stays.
var strictVMName = map[string]bool{"vm_start": true}

// vmOf finds the VM a power tool names — by vm_id, or by name through the
// project's list — and reads it.
func (s *Registry) vmOf(ctx context.Context, tool string, eng *engine.Engine, ref string) (*provision.VMDetail, error) {
	id := ref
	if !looksUUID(ref) {
		vms, err := eng.Cloud.ListVMsInProject(ctx, eng.Config.ProjectID)
		if err != nil {
			return nil, err
		}
		var ids []string
		for _, v := range vms {
			if v.Name == ref {
				ids = append(ids, v.ID)
			}
		}
		switch {
		case len(ids) == 0:
			return nil, clierrors.New(clierrors.CodeVMNotFound,
				i18n.Tf("VM %q not found", "ВМ %q не найдена", ref),
				i18n.T("vm_list shows the VMs of the project.", "ВМ проекта показывает vm_list."))
		case len(ids) > 1 && strictVMName[tool]:
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("%d VMs of the project are named %q: %s", "в проекте %d ВМ с именем %q: %s", len(ids), ref, strings.Join(ids, ", ")),
				i18n.T("Pass vm_id of the one you mean.", "Передайте vm_id нужной ВМ."))
		}
		id = ids[0]
	}
	return eng.Cloud.GetVMDetail(ctx, id)
}

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
		vm, err := s.vmOf(ctx, tool, eng, ref)
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
				ans := vmAnswer(vm, last, last)
				ans["next"] = fmt.Sprintf("VM %s is %s at the time limit of an MCP call; it cannot be changed until its state settles. Call %s to go on.", vm.ID, last, tool)
				nextStep(ans, tool, maps.Clone(args))
				return answered(toJSON(ans), false), nil
			case timedOut > 0:
				return nil, clierrors.New(clierrors.CodeUsage,
					i18n.Tf("VM %s is in state %s: it cannot be changed until the state settles", "ВМ %s в состоянии %s: менять её можно, когда состояние устоится", vm.ID, last),
					i18n.T("Repeat the call when vm_get shows the VM running or stopped.", "Повторите вызов, когда vm_get покажет ВМ в running или stopped."))
			}
			state = last
		}
		switch {
		case state == act.to:
			return answered(toJSON(vmAnswer(vm, act.already, state)), false), nil
		case state == act.via:
			// A start in flight: this call's own, cut before its end, or
			// another caller's. No command is sent, so no question.
			text, isErr := s.finishVMPower(ctx, eng, tool, act, vm, args, false)
			return answered(text, isErr), nil
		case vmNothingToChange[state]:
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("VM %s is in state %s: there is nothing to %s", "ВМ %s в состоянии %s: %s нечего", vm.ID, state, act.command),
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
				"ВМ в состоянии %s, а не %s: примет ли платформа %s — неизвестно", state, act.from, act.command)
		}
		return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
			if err := eng.Cloud.SetVMPower(ctx, vm.ID, act.command); err != nil {
				return s.vmPowerRefused(ctx, eng, tool, act, vm, args, state, err)
			}
			return s.finishVMPower(ctx, eng, tool, act, vm, args, true)
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
		return s.finishVMPower(ctx, eng, tool, act, now, args, true)
	}
	if provision.HTTPStatus(cause) != http.StatusUnprocessableEntity {
		return errAnswer(cause, map[string]any{"vm_id": vm.ID, "state": now.State}), true
	}
	hint := i18n.T("vm_get shows the state of the VM; repeat the call when it is "+act.from+".", "Состояние ВМ показывает vm_get; повторите вызов, когда оно станет "+act.from+".")
	if planned != act.from {
		hint = i18n.T("A repeat will not help: the platform does not take this command in this state. The Cloud.ru console or support is the way.",
			"Повтор не поможет: платформа не принимает эту команду в этом состоянии. Путь — консоль Cloud.ru или поддержка.")
	}
	// One machine field for the cause, of the form of a quota refusal's
	// (details.cause); the platform's own code goes beside it as the
	// platform worded it (conclusion 111, У4).
	details := map[string]any{"cause": "state"}
	if code := platformCode(cause); code != "" {
		details["platform_code"] = code
	}
	return errAnswer(clierrors.Wrap(clierrors.CodeAPIError,
		i18n.Tf("the platform refused %s of VM %s in state %s", "платформа отказала в %s для ВМ %s в состоянии %s", act.command, vm.ID, now.State), hint, cause),
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
func (s *Registry) finishVMPower(ctx context.Context, eng *engine.Engine, tool string, act vmPowerAction,
	vm *provision.VMDetail, args map[string]any, sent bool) (string, bool) {
	started := clock.Now()
	last, timedOut, atLimit, err := s.waitVM(ctx, eng, vm, vmPowerTimeout, func(st string) (bool, error) {
		switch st {
		case act.to:
			return true, nil
		case act.via, act.from:
			// The command is taken and the state has not moved yet.
			return false, nil
		}
		return true, clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("VM %s is in state %s after %s, not %s", "ВМ %s после %s в состоянии %s, а не %s", vm.ID, st, act.command, act.to),
			i18n.T("vm_get shows the VM.", "ВМ показывает vm_get."))
	})
	elapsed := int(clock.Since(started).Seconds())
	switch {
	case err != nil:
		return errAnswer(err, map[string]any{"vm_id": vm.ID, "state": last}), true
	case atLimit || timedOut > 0:
		ans := vmAnswer(vm, last, last)
		ans["elapsed_sec"] = elapsed
		if atLimit && sent {
			ans["next"] = fmt.Sprintf("VM %s is %s at the time limit of an MCP call. Call %s to go on: it waits for the VM again and sends no second command.", vm.ID, last, tool)
			nextStep(ans, tool, maps.Clone(args))
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
		func(ctx context.Context) (string, bool) {
			d, err := eng.Cloud.GetVMDetail(ctx, vm.ID)
			if err != nil {
				return "", false
			}
			return d.State, true
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
