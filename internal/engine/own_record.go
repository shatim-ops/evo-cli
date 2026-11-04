// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"

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
// person's own decision. A record or VM without a time is left to destroy
// as before; so is a VM the cloud no longer has (already_deleted, D26) and
// a key the cloud refuses (the call that needs it is refused for the key).
// Any other failed read refuses: a delete after a check that did not run
// would take the VM D73 guards.
func (e *Engine) AdoptedRecord(ctx context.Context, rec state.DeploymentRecord) error {
	_, _, err := e.checkedRecord(ctx, rec)
	return err
}

// checkedRecord is AdoptedRecord with what its read gave, for the
// provenance of the preview (ProvenanceOf): the VM it read, nil when it
// read none, and the error of a read it let pass (a key the cloud refuses;
// a VM the cloud no longer has is no error: the record still names it).
func (e *Engine) checkedRecord(ctx context.Context, rec state.DeploymentRecord) (vm *provision.VM, passed, err error) {
	if rec.VMID == "" || rec.CreatedAt.IsZero() || e.Auth == nil || e.Cloud == nil {
		return nil, nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	tok, err := e.Auth.Token(cctx)
	if err != nil {
		return nil, passedRead(err), uncheckedRecord(rec, err)
	}
	e.Cloud.SetToken(tok.Value)
	vm, err = e.Cloud.GetVM(cctx, rec.VMID)
	if err != nil {
		return nil, passedRead(err), uncheckedRecord(rec, err)
	}
	return vm, nil, AdoptedVM(rec, vm)
}

// passedRead is err when uncheckedRecord lets it pass for a key the cloud
// refuses; nil for any other.
func passedRead(err error) error {
	switch clerrors.Reason(err) {
	case clerrors.CodeAuthFailed, clerrors.CodeAuthExpired, clerrors.CodeAuthRequired:
		return err
	}
	return nil
}

// uncheckedRecord is the answer of AdoptedRecord when its read failed: nil
// for a VM the cloud no longer has and for a key it refuses, else a refusal
// that keeps the class and the hint of the error.
func uncheckedRecord(rec state.DeploymentRecord, err error) error {
	switch clerrors.Reason(err) {
	case clerrors.CodeVMNotFound, clerrors.CodeNotFound,
		clerrors.CodeAuthFailed, clerrors.CodeAuthExpired, clerrors.CodeAuthRequired:
		return nil
	case clerrors.ReasonInterrupted:
		return err
	}
	prefix := i18n.Tf("could not check that VM %s was made by this folder's evo, so nothing was deleted and the record is kept: ",
		"не удалось проверить, что ВМ %s создал evo этой папки, поэтому ничего не удалено, запись сохранена: ", rec.VMID)
	retry := i18n.T("The check only reads the VM: run the same call again in a minute.",
		"Проверка только читает ВМ: повторите тот же вызов через минуту.")
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) {
		hint := ce.Hint
		if hint == "" {
			hint = retry
		}
		return clerrors.Wrap(ce.Code, prefix+ce.Message, hint, err)
	}
	return clerrors.Wrap("", prefix+err.Error(), clerrors.NetworkHint(err, retry), err)
}

// AdoptedVM is the rule of AdoptedRecord for a VM already read: the
// record's VM made before it, beyond the clocks' skew (madeBeforeRecord,
// which ProvenanceOf answers by too).
func AdoptedVM(rec state.DeploymentRecord, vm *provision.VM) error {
	if vm == nil || !madeBeforeRecord(rec, vm.CreatedAt) {
		return nil
	}
	return clerrors.New(clerrors.CodeUsage,
		i18n.Tf("VM %s was made before this folder recorded it: an evo before 1.7 adopted it by its name, it did not create it; nothing was deleted and the record is kept",
			"ВМ %s создана раньше, чем эта папка её записала: evo до 1.7 взял её по имени, а не создал; ничего не удалено, запись сохранена", rec.VMID),
		i18n.Tf("Ask the person whose VM it is: if it is theirs and no longer needed, they delete it by its id (vm_delete %s), and destroy then drops the record; if it is not, leave it.",
			"Спросите человека, чья это ВМ: если его и больше не нужна, он удаляет её по id ('evo vm delete %s'), после этого destroy уберёт запись; если нет — не трогайте её.", rec.VMID))
}
