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
	// A record another key made is read whatever its age: its 404 is no
	// proof the VM is gone (L3, «Риск» 43).
	other := rec.CreatedBy.Other(e.Maker())
	if rec.VMID == "" || (rec.CreatedAt.IsZero() && !other) || e.Auth == nil || e.Cloud == nil {
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
		if r := clerrors.Reason(err); other && (r == clerrors.CodeVMNotFound || r == clerrors.CodeNotFound) {
			return nil, nil, e.NotVisibleToThisKey(rec)
		}
		return nil, passedRead(err), uncheckedRecord(rec, err)
	}
	if rec.CreatedAt.IsZero() {
		return vm, nil, nil
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

type boundDestroyKey struct{}

// WithBoundDestroy restricts directory cleanup to the project selected by
// the MCP factory. No local record can override that server boundary.
func WithBoundDestroy(ctx context.Context) context.Context {
	return context.WithValue(ctx, boundDestroyKey{}, true)
}

func unknownBoundDeployment() error {
	return clerrors.New(clerrors.CodeNotFound,
		i18n.T("No deployment could be verified in the server's project.", "Не удалось подтвердить развёртывание в проекте сервера."),
		i18n.T("Ask the person to select a VM from vm_list.", "Попросите пользователя выбрать ВМ из vm_list."))
}

// BoundDestroyTarget names a VM for a noninteractive confirmation refusal
// only if the very same snapshot is within the server's project.
func (e *Engine) BoundDestroyTarget(ctx context.Context) (string, error) {
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		return "", unknownBoundDeployment()
	}
	if err := e.boundDirectoryRecords(ctx, sf); err != nil {
		return "", err
	}
	if active := sf.ActiveDeployments(); len(active) == 1 {
		return active[0].VMID, nil
	}
	return "", nil
}

func (e *Engine) boundDirectoryRecords(ctx context.Context, sf *state.File) error {
	if e.Config.ProjectID == "" {
		return clerrors.ProjectRequired()
	}
	var legacyVMs, legacyGroups []string
	count := 0
	for _, d := range sf.Deployments {
		if d.Status == "destroyed" && len(d.OpenedRules) == 0 && d.PendingRuleScan == nil {
			continue
		}
		count++
		if err := e.CheckDestroyReceipt(sf, &d, e.Config.ProjectID); err != nil {
			return unknownBoundDeployment()
		}
		project := RecordedCleanupProject(sf, &d)
		if project != "" && project != e.Config.ProjectID {
			return unknownBoundDeployment()
		}
		if project == "" {
			if d.Status == "destroyed" {
				for _, r := range d.OpenedRules {
					legacyGroups = append(legacyGroups, r.SecurityGroupID)
				}
			} else {
				legacyVMs = append(legacyVMs, d.VMID)
			}
		}
	}
	if count == 0 {
		return unknownBoundDeployment()
	}
	if len(legacyGroups) > 0 {
		ok, err := e.cleanupGroupsInProject(ctx, e.Config.ProjectID, legacyGroups)
		if err != nil {
			return boundInventoryError(err)
		}
		if !ok {
			return unknownBoundDeployment()
		}
	}
	if len(legacyVMs) == 0 {
		return nil
	}
	listed, err := e.cleanupVMsInProject(ctx, e.Config.ProjectID)
	if err != nil {
		return boundInventoryError(err)
	}
	for _, id := range legacyVMs {
		if !listed[id] {
			return unknownBoundDeployment()
		}
	}
	return nil
}

func boundInventoryError(err error) error {
	if reason := clerrors.Reason(err); reason == clerrors.CodeNotFound || reason == clerrors.CodeVMNotFound {
		return unknownBoundDeployment()
	}
	return err
}

func (e *Engine) cleanupGroupsInProject(ctx context.Context, project string, ids []string) (bool, error) {
	if e.Auth == nil || e.Cloud == nil {
		return false, unverifiedCleanupProject()
	}
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return false, err
	}
	e.Cloud.SetToken(tok.Value)
	groups, err := provision.ReadSecurityGroupsForCleanup(ctx, e.Cloud, project)
	if err != nil {
		return false, err
	}
	found := make(map[string]bool, len(groups))
	for _, g := range groups {
		if g != nil && g.ID != "" {
			found[g.ID] = true
		}
	}
	for _, id := range ids {
		if !found[id] {
			return false, nil
		}
	}
	return true, nil
}

func (e *Engine) cleanupVMsInProject(ctx context.Context, project string) (map[string]bool, error) {
	if e.Auth == nil || e.Cloud == nil {
		return nil, unverifiedCleanupProject()
	}
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	e.Cloud.SetToken(tok.Value)
	vms, err := e.Cloud.ListVMsInProject(ctx, project)
	if err != nil {
		return nil, err
	}
	listed := make(map[string]bool, len(vms))
	for _, vm := range vms {
		if vm == nil || vm.ID == "" || listed[vm.ID] {
			return nil, clerrors.New(clerrors.CodeAPIError, i18n.T("The project's VM list is incomplete.", "Список ВМ проекта неполон."), "")
		}
		listed[vm.ID] = true
	}
	return listed, nil
}

// RecordedCleanupProject returns a stored project, including independent
// evidence tied to a legacy VM. A configured default alone is not evidence.
func RecordedCleanupProject(sf *state.File, d *state.DeploymentRecord) string {
	project, ok := recordedReceiptProject(sf, d)
	if !ok {
		return ""
	}
	if scan := d.PendingRuleScan; scan != nil {
		if project != "" && scan.ProjectID != project {
			return ""
		}
		return scan.ProjectID
	}
	return project
}

// CheckDestroyReceipt checks the persisted scope before previewing or changing
// resources. A mark from another project with the same name alone is unrelated.
func (e *Engine) CheckDestroyReceipt(sf *state.File, rec *state.DeploymentRecord, project string) error {
	if project == "" {
		return clerrors.ProjectRequired()
	}
	if recorded, consistent := rec.RecordedCleanupProject(); !consistent || recorded != "" && recorded != project {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.T("Cleanup records belong to different projects.", "Записи уборки относятся к разным проектам."),
			i18n.T("Ask the person to select each original project and run destroy for its VM ID.", "Попросите пользователя выбрать исходный проект каждой записи и повторить удаление по ID её ВМ."))
	}
	maker := e.Maker()
	wrongOwner := func(m *state.Maker) bool {
		if m == nil {
			return false // no principal in older records
		}
		if same, known := m.MatchRuleOwner(maker); known {
			return !same
		}
		// Do not downgrade an exact recorded principal to a display-mask
		// comparison just because the new profile cannot establish it.
		_, recorded := m.MatchRuleOwner(m)
		return recorded || maker == nil || m.Other(maker)
	}
	if scan := rec.PendingRuleScan; scan != nil {
		same, _ := scan.StartedBy.MatchRuleOwner(maker)
		if scan.ProjectID == "" || scan.ProjectID != project || (scan.StartedBy != nil && !same) {
			return destroyScopeError(rec, project)
		}
	}
	if wrongOwner(rec.CreatedBy) {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.T("The deployment was recorded by another key. Nothing was deleted.", "Развёртывание записано другим ключом. Ничего не удалено."),
			i18n.T("Ask the person to select the original profile and repeat the call.", "Попросите пользователя выбрать исходный профиль и повторить вызов."))
	}
	for _, m := range sf.LostFloatingIPs {
		exactVM := m.VMID != "" && m.VMID == rec.VMID
		beforeVM := m.VMID == "" && m.VMName != "" && m.VMName == rec.VMName && m.ProjectID == project
		if m.ProjectID != project {
			// A targeted VM found in the project may have no local deployment.
			// Requests from another project are outside that operation.
			if rec.ID != "" && exactVM {
				return destroyScopeError(rec, project)
			}
			continue
		}
		if (exactVM || beforeVM) && wrongOwner(m.CreatedBy) {
			return destroyScopeError(rec, project)
		}
	}
	for _, r := range rec.FloatingIPResolutions {
		if r.ProjectID != "" && r.ProjectID != project {
			return destroyScopeError(rec, project)
		}
	}
	return e.CheckFloatingIPReceiptProject(sf, rec)
}

func unverifiedCleanupProject() error {
	return clerrors.New(clerrors.CodeConfigInvalid, i18n.T("The cleanup project cannot be verified.", "Не удалось проверить проект уборки."),
		i18n.T("Ask the person to select the original project and profile.", "Попросите пользователя выбрать исходные проект и профиль."))
}

func destroyScopeError(rec *state.DeploymentRecord, project string) error {
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("The cleanup scope of VM %q could not be verified in project %q. Nothing was deleted.",
			"Не удалось подтвердить область уборки ВМ %q в проекте %q. Ничего не удалено.", rec.VMID, project),
		i18n.Tf("Select the original profile and project ('evo project use %s') and repeat the call.",
			"Выберите исходный профиль и проект ('evo project use %s') и повторите вызов.", project))
}

// The directory form follows the recorded project, even for a key that sees
// several projects. When the configured project differs, positively identify
// this VM in its original project before asking. A concealed 404 is ambiguous.
func (e *Engine) checkDestroyScope(ctx context.Context, sf *state.File, active *state.DeploymentRecord, pending []string) error {
	bound, _ := ctx.Value(boundDestroyKey{}).(bool)
	if bound {
		if err := e.boundDirectoryRecords(ctx, sf); err != nil {
			return err
		}
	}
	if all := sf.ActiveDeployments(); len(all) > 1 {
		return severalDeployments(all)
	}
	var project string
	if active != nil {
		project = e.projectOf(active)
		if err := e.CheckDestroyReceipt(sf, active, project); err != nil {
			return err
		}
	}
	var legacyGroups []string
	for _, id := range pending {
		rec := sf.FindDeployment(id)
		if project == "" {
			project = e.projectOf(rec)
		}
		if err := e.CheckDestroyReceipt(sf, rec, project); err != nil {
			return err
		}
		if RecordedCleanupProject(sf, rec) == "" {
			for _, r := range rec.OpenedRules {
				legacyGroups = append(legacyGroups, r.SecurityGroupID)
			}
		}
		if project != e.Config.ProjectID {
			same, _ := rec.CreatedBy.MatchRuleOwner(e.Maker())
			if scan := rec.PendingRuleScan; scan != nil {
				same, _ = scan.StartedBy.MatchRuleOwner(e.Maker())
			}
			if RecordedCleanupProject(sf, rec) == "" || !same {
				return destroyScopeError(rec, project)
			}
		}
	}
	if !bound && len(legacyGroups) > 0 {
		ok, err := e.cleanupGroupsInProject(ctx, project, legacyGroups)
		if err != nil {
			return err
		}
		if !ok {
			return clerrors.New(clerrors.CodeConfigInvalid, i18n.T("The saved security group could not be verified in the selected project.", "Не удалось подтвердить сохранённую группу безопасности в выбранном проекте."),
				i18n.T("Ask the person to select its original project and profile.", "Попросите пользователя выбрать исходные проект и профиль."))
		}
	}
	// All newly identified groups were found in this project's complete
	// inventory. Keep the proof in the preview snapshot. Only the approved
	// operation persists it, before making its first change.
	if len(legacyGroups) > 0 {
		for _, id := range pending {
			rec := sf.FindDeployment(id)
			if RecordedCleanupProject(sf, rec) == "" && len(rec.OpenedRules) > 0 {
				rec.CleanupProjectID = project
			}
		}
	}
	legacy := active != nil && active.VMID != "" && RecordedCleanupProject(sf, active) == ""
	if bound || project == "" || project == e.Config.ProjectID && !legacy {
		return nil
	}
	if e.Auth == nil || e.Cloud == nil {
		return unverifiedCleanupProject()
	}
	if active == nil {
		// Verify that the exact original principal still sees its project.
		tok, err := e.Auth.Token(ctx)
		if err != nil {
			return err
		}
		e.Cloud.SetToken(tok.Value)
		_, err = provision.ReadSecurityGroupsForCleanup(ctx, e.Cloud, project)
		return err
	}
	listed, err := e.cleanupVMsInProject(ctx, project)
	if err != nil {
		if reason := clerrors.Reason(err); reason == clerrors.CodeNotFound || reason == clerrors.CodeVMNotFound {
			return destroyScopeError(active, project)
		}
		return err
	}
	if listed[active.VMID] {
		return nil
	}
	return destroyScopeError(active, project)
}
