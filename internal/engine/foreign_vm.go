// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// ForeignVMNamed is the VM of project projectID named name that own does
// not take — one this folder's evo did not make (D73): nil when the name
// is free. A list that cannot be read is an error, the name cannot be
// told free; a key that cannot read the project tells nothing (nil, nil):
// the call itself is refused for the key.
func ForeignVMNamed(ctx context.Context, cloud provision.CloudAPI, projectID, name string, own func(id string) bool) (*provision.VM, error) {
	if name == "" || cloud == nil {
		return nil, nil
	}
	vms, err := cloud.ListVMsInProject(ctx, projectID)
	switch reason := clerrors.Reason(err); {
	case err == nil:
	case reason == clerrors.CodeAuthFailed || reason == clerrors.CodeAuthExpired || reason == clerrors.CodeAuthRequired:
		return nil, nil
	default:
		return nil, fmt.Errorf(i18n.T("check the VM name is free: %w", "проверка, что имя ВМ свободно: %w"), err)
	}
	for _, vm := range vms {
		if vm != nil && vm.Name == name && !own(vm.ID) {
			return vm, nil
		}
	}
	return nil, nil
}

// RecordedIn is the own of ForeignVMNamed for the VMs the state sf of a
// folder records.
func RecordedIn(sf *state.File) func(id string) bool {
	var ids []string
	if sf != nil {
		for _, d := range sf.ActiveDeployments() {
			ids = append(ids, d.VMID)
		}
	}
	return func(id string) bool { return slices.Contains(ids, id) }
}

// deployNameHint is the way out of a VM name of deploy that another VM
// has: deploy's VM is evo-<project_name>, so another project_name; a
// delete is the person's own decision (#829, FB170-2).
func deployNameHint() string {
	return i18n.T("deploy names its VM evo-<project_name>: set another project_name in .evo.yaml, or deploy from a directory with another name. If the VM is yours and you no longer need it, delete it yourself.",
		"deploy называет ВМ evo-<project_name>: задайте другой project_name в .evo.yaml или разверните из каталога с другим именем. Если ВМ ваша и больше не нужна, удалите её сами.")
}

// foreignDeployVM is deploy's refusal, before the question, of the VM name
// another VM has (FB170-2; D73, as provision and deploy_plan): USAGE with
// its id (existing_id); nothing is created.
func foreignDeployVM(vm *provision.VM) error {
	return &clerrors.CLIError{Code: clerrors.CodeUsage,
		Message: i18n.Tf("a VM named %q already exists (id %s, status %s), and this folder's evo did not create it (its state does not record it); nothing was created",
			"ВМ с именем %q уже есть (id %s, статус %s), и её создал не evo этой папки (в её state её нет); ничего не создано",
			vm.Name, vm.ID, cmp.Or(vm.Status, "unknown")),
		Hint:    deployNameHint(),
		Details: map[string]any{"existing_id": vm.ID}}
}

// deployNameFree refuses the name of deploy's new VM when another VM of
// the project has it (foreignDeployVM).
func (e *Engine) deployNameFree(ctx context.Context, projectID, name string, sf *state.File) error {
	vm, err := ForeignVMNamed(ctx, e.Cloud, projectID, name, RecordedIn(sf))
	if err != nil {
		return err
	}
	if vm != nil {
		return foreignDeployVM(vm)
	}
	return nil
}
