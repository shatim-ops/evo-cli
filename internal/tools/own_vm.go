// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
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
	vm, err := engine.ForeignVMNamed(ctx, eng.Cloud, eng.Config.ProjectID, name, own)
	if err != nil {
		return errResult(err), true
	}
	if vm != nil {
		return foreignVMResult(name, vm.ID, vm.Status)
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
		i18n.T("Pick another name (name). Whether to delete that VM, ask the person: it may be someone else's.",
			"Выберите другое имя (name). Удалять ли ту ВМ, спросите человека: она может быть чужой.")),
		map[string]any{"existing_id": id}), true
}

// ownIn is the own of vmNameFree for the VMs ids lists.
func ownIn(ids []string) func(string) bool {
	return func(id string) bool { return slices.Contains(ids, id) }
}
