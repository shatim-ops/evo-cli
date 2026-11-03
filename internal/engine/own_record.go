// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// AdoptedRecord refuses the destroy of a record whose VM evo did not make
// (D73, the record against the cloud): one an evo before 1.7 adopted by its
// name — deploy_apply took any VM of the plan's name and recorded it — is
// made long before its record (provision.MadeSince). The refusal comes
// before the question and keeps the record; deleting the VM is the
// person's own decision. A VM it cannot read, or a record or VM without a
// time, is left to destroy as before.
func (e *Engine) AdoptedRecord(ctx context.Context, rec state.DeploymentRecord) error {
	if rec.VMID == "" || rec.CreatedAt.IsZero() || e.Auth == nil || e.Cloud == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	tok, err := e.Auth.Token(cctx)
	if err != nil {
		return nil
	}
	e.Cloud.SetToken(tok.Value)
	vm, err := e.Cloud.GetVM(cctx, rec.VMID)
	if err != nil {
		return nil
	}
	return AdoptedVM(rec, vm)
}

// AdoptedVM is the rule of AdoptedRecord for a VM already read: the
// record's VM made before it, beyond the clocks' skew.
func AdoptedVM(rec state.DeploymentRecord, vm *provision.VM) error {
	if rec.CreatedAt.IsZero() || vm == nil || vm.CreatedAt.IsZero() || provision.MadeSince(vm, rec.CreatedAt) {
		return nil
	}
	return clerrors.New(clerrors.CodeUsage,
		i18n.Tf("VM %s was made before this folder recorded it: an evo before 1.7 adopted it by its name, it did not create it; nothing was deleted and the record is kept",
			"ВМ %s создана раньше, чем эта папка её записала: evo до 1.7 взял её по имени, а не создал; ничего не удалено, запись сохранена", rec.VMID),
		i18n.Tf("Ask the person whose VM it is: if it is theirs and no longer needed, they delete it by its id (vm_delete %s), and destroy then drops the record; if it is not, leave it.",
			"Спросите человека, чья это ВМ: если его и больше не нужна, он удаляет её по id ('evo vm delete %s'), после этого destroy уберёт запись; если нет — не трогайте её.", rec.VMID))
}
