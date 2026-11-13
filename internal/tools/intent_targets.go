// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "context"

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
