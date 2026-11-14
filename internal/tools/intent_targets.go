// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// vmTarget is the target of a call that names a VM by vm_id or by name
// (changeRule.target): the VM's id, read as the plan of a power tool
// reads it — an id as it is, a name through the one point names are
// resolved at (resolveVMFor). The record names no name: the call makes
// nothing.
func vmTarget(tool string) func(ctx context.Context, r *Registry, args map[string]any) (name, target string, err error) {
	return func(ctx context.Context, r *Registry, args map[string]any) (string, string, error) {
		ref, _, err := oneOfArgs(args, vmRefNotBoth(), "vm_id", "name")
		if err != nil || ref == "" {
			// The plan of the tool refuses these arguments in its own words.
			return "", "", nil
		}
		if looksUUID(ref) {
			return "", "vm_id=" + ref, nil
		}
		eng, err := r.authedEngine(ctx, nil)
		if err != nil {
			return "", "", err
		}
		vm, err := r.resolveVMFor(ctx, tool, ref, eng.Config.ProjectID)
		if err != nil {
			return "", "", err
		}
		return "", "vm_id=" + vm.ID, nil
	}
}

// vmSettled is what the state of the VM a call of the power tool names
// says of a cut call of it (changeRule.settledBy): where the action leads —
// made; in a transition — goes on; where it started, in an error, being
// deleted — asked about. A VM that cannot be read is asked about too.
func vmSettled(tool string) func(ctx context.Context, r *Registry, args map[string]any) (outcome, error) {
	act, resolve := vmPowerActions[tool], vmTarget(tool)
	return func(ctx context.Context, r *Registry, args map[string]any) (outcome, error) {
		_, target, err := resolve(ctx, r, args)
		_, id, found := strings.Cut(target, "=")
		if err != nil || !found {
			return outcomeAsk, err
		}
		eng, err := r.authedEngine(ctx, nil)
		if err != nil {
			return outcomeAsk, err
		}
		vm, err := eng.Cloud.GetVMDetail(ctx, id)
		switch {
		case err != nil:
			return outcomeAsk, err
		case vm.State == act.to:
			return outcomeMade, nil
		case vmSettling[vm.State]:
			return outcomeGoesOn, nil
		}
		return outcomeAsk, nil
	}
}

// groupTarget is the target of a call that names a security group in arg
// by its id or its name (changeRule.target): the group's id, read as the
// plan of the tool reads it (securityGroupFor), so a cut call is found
// however the repeat spells the group (release plan 1.9, #1608). Nothing
// is read for a call its arguments refuse (valid): it is left as it spells
// the group, and the plan refuses it in its own words and order, before
// anything is sent. A name that is no group, or two, refuses the call
// here with the error the plan would give, as vmTarget does: no record is
// opened for it, so a refusal that sent nothing does not settle the record
// of a cut call of the same arguments (R-101; «Риск · 1.9 (2)», check of
// #1726).
func groupTarget(arg string, valid func(args map[string]any) error) func(ctx context.Context, r *Registry, args map[string]any) (name, target string, err error) {
	return func(ctx context.Context, r *Registry, args map[string]any) (string, string, error) {
		ref := stringArg(args, arg)
		if ref == "" {
			return "", "", nil
		}
		if valid(args) != nil {
			return "", arg + "=" + ref, nil
		}
		eng, err := r.authedEngine(ctx, nil)
		if err != nil {
			return "", "", err
		}
		group, err := securityGroupFor(ctx, eng, ref)
		if err != nil {
			return "", "", err
		}
		return "", arg + "=" + group.ID, nil
	}
}

// securityGroupFor is securityGroupByRef once a call: the layer of intents
// resolves the group a call names before the plan of the tool does
// (changeRule.target).
func securityGroupFor(ctx context.Context, eng *engine.Engine, ref string) (*provision.SecurityGroup, error) {
	return onceACall(ctx, "sg\x00"+ref+"\x00"+eng.Config.ProjectID, func() (*provision.SecurityGroup, error) {
		return securityGroupByRef(ctx, eng, ref)
	})
}
