// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"slices"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// folderVMs are the VMs the state of eng's folder records: the folder's
// own, which deploy_plan plans again and deploy_apply adopts (D73).
func folderVMs(eng *engine.Engine) []string {
	sf, err := state.Read(state.DefaultPath(eng.StateDir))
	if err != nil {
		return nil
	}
	var ids []string
	for _, d := range sf.ActiveDeployments() {
		if d.VMID != "" {
			ids = append(ids, d.VMID)
		}
	}
	return ids
}

// vmNameFree refuses, before the question, a VM name another VM of the
// project has (D73, the mechanism of D49): USAGE with existing_id. own
// says which VMs are the call's to take — those pass. A list that cannot
// be read refuses the call too: the name cannot be told free.
func vmNameFree(ctx context.Context, eng *engine.Engine, name string, own func(id string) bool) (string, bool) {
	// An engine without a cloud (the preview of a server without a key)
	// reads nothing, as completePreview.
	if name == "" || eng.Cloud == nil {
		return "", false
	}
	vms, err := eng.Cloud.ListVMsInProject(ctx, eng.Config.ProjectID)
	switch reason := clierrors.Reason(err); {
	case err == nil:
	case reason == clierrors.CodeAuthFailed || reason == clierrors.CodeAuthExpired || reason == clierrors.CodeAuthRequired:
		// No key that reads the project: the preview shows what is known
		// without the cloud, and the call itself is refused for the key.
		return "", false
	default:
		return errResult(fmt.Errorf(i18n.T("check the VM name is free: %w", "проверка, что имя ВМ свободно: %w"), err)), true
	}
	for _, vm := range vms {
		if vm != nil && vm.Name == name && !own(vm.ID) {
			return foreignVMResult(name, vm.ID, vm.Status)
		}
	}
	return "", false
}

// foreignVMResult is the answer of a call whose VM name a VM this folder's
// evo did not make has (D73): never adopted — it would get a billed
// floating IP, the plan's ports and a record destroy deletes it by. The
// way out is another name; a delete is the person's own decision.
func foreignVMResult(name, id, status string) (string, bool) {
	if status == "" {
		status = "unknown"
	}
	return errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("a VM named %q already exists (id %s, status %s), and this folder's evo did not create it (its state does not record it); nothing was created",
			"ВМ с именем %q уже есть (id %s, статус %s), и её создал не evo этой папки (в её state её нет); ничего не создано", name, id, status),
		i18n.T("Pick another name (name). If the VM is yours and you no longer need it, delete it yourself.",
			"Выберите другое имя (name) или удалите её сами, если она ваша.")),
		map[string]any{"existing_id": id}), true
}

// ownIn is the own of vmNameFree for the VMs ids lists.
func ownIn(ids []string) func(string) bool {
	return func(id string) bool { return slices.Contains(ids, id) }
}
