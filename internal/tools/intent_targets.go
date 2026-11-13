// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
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
