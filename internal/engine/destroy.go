// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/floatingip"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// DestroyOptions holds parameters for the destroy command.
type DestroyOptions struct {
	// Result identifies the target and incomplete cleanup under the state lock.
	Result *DestroyResult
	Force  bool // skip confirmation prompt (--force / --yes)
	DryRun bool // show what would be deleted and stop
	// Gate, when set, asks for consent to the preview instead of
	// confirm.Ask: the destroy tool sets it for 'evo destroy', which shows
	// the preview with the tool's answer as its details (spec REQ-4).
	Gate func(ctx context.Context, p confirm.Preview) (bool, error)
}

type DestroyResult struct {
	VMID               string
	CleanupPending     bool
	RuleCleanupPending bool
}

// gate asks for consent to p: through opts.Gate, else confirm.Ask.
func (e *Engine) destroyGate(ctx context.Context, opts DestroyOptions, p confirm.Preview) (bool, error) {
	if opts.Gate != nil {
		return opts.Gate(ctx, p)
	}
	return confirm.Ask(ctx, p, e.confirmOptions(opts.Force, opts.DryRun, false))
}

// DestroyPreview is the preview destroy asks consent to, in the directory
// as it is: the VM of the deployment (named after the cloud's lists), its
// floating IP and port rules, or the port rules an earlier destroy left.
// False when destroy would ask nothing: no deployment; an error when it
// refuses before the question — several deployments (as Destroy does,
// with the same answer: a call bound to fail asks nothing, agent run of
// 1.7.0, №3), a VM evo did not make (D73).
// Reads only; the destroy tool answers it without confirmed=true.
func (e *Engine) DestroyPreview(ctx context.Context) (confirm.Preview, bool, error) {
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		return confirm.Preview{}, false, nil
	}
	pending := sgrules.Pending(sf)
	active := sf.ActiveDeployment()
	switch {
	case active == nil && len(pending) > 0:
		return pendingRulesPreview(sf, pending), true, nil
	case active == nil:
		return confirm.Preview{}, false, nil
	case len(sf.ActiveDeployments()) > 1:
		return confirm.Preview{}, false, severalDeployments(sf.ActiveDeployments())
	}
	if err := e.CheckFloatingIPReceiptProject(sf, active); err != nil {
		return confirm.Preview{}, false, err
	}
	// A VM evo did not make is refused before the question (D73).
	vm, passed, err := e.checkedRecord(ctx, *active)
	if err != nil {
		return confirm.Preview{}, false, err
	}
	shown := e.withNames(ctx, *active, nil, true)
	labeled, uncheckedErr := e.labeledRules(ctx, sf, active)
	return destroyPreview(&shown, sf, pending, labeled, uncheckedErr, e.openLeftOf(ctx, sf, active, pending, labeled), e.projectOf(active), e.projectFrom(active), madeAt(vm), passed), true, nil
}

// Destroy tears down the active deployment.
func (e *Engine) Destroy(ctx context.Context, opts DestroyOptions) error {
	statePath := state.DefaultPath(e.StateDir)
	if !opts.DryRun {
		unlock, err := state.Lock(statePath)
		if err != nil {
			return err
		}
		defer unlock()
	}
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	active := stateFile.ActiveDeployment()
	// Deployments destroyed earlier whose port rules could not be deleted
	// then: this run tries again.
	pending := sgrules.Pending(stateFile)
	if active == nil {
		if len(pending) > 0 {
			return e.finishPendingRules(ctx, opts, statePath, stateFile, pending)
		}
		if e.Printer.IsJSON() {
			return e.Printer.JSON(noDeploymentAnswer())
		}
		e.Printer.Success(noDeploymentToDestroy())
		return nil
	}

	// Several active records (provision next to a deploy, R2-2): deleting the
	// first and answering "destroyed" would leave the others billed while
	// the person sees success. Each one goes by its own ID.
	if all := stateFile.ActiveDeployments(); len(all) > 1 {
		return severalDeployments(all)
	}

	if err := e.CheckFloatingIPReceiptProject(stateFile, active); err != nil {
		return err
	}
	// A VM evo did not make is refused before the question (D73).
	vm, passed, err := e.checkedRecord(ctx, *active)
	if err != nil {
		return err
	}

	e.Printer.Step(1, 3, i18n.T("Reading deployment info...", "Чтение сведений о развёртывании..."))
	// The names, not only the UUIDs: with several VMs the person approving
	// must see which one goes (first real run, v1.0.1).
	shown := e.withNames(ctx, *active, nil, true)
	e.Printer.StepDone(destroyTarget(&shown))

	// Without a terminal nobody can answer: confirm.Ask fails with exit code 3
	// instead of reading EOF as a refusal and exiting 0.
	labeled, uncheckedErr := e.labeledRules(ctx, stateFile, active)
	proceed, err := e.destroyGate(ctx, opts, destroyPreview(&shown, stateFile, pending, labeled, uncheckedErr, e.openLeftOf(ctx, stateFile, active, pending, labeled), e.projectOf(active), e.projectFrom(active), madeAt(vm), passed))
	if err != nil {
		return err
	}
	if !proceed {
		return nil // --dry-run
	}

	if opts.Result != nil {
		opts.Result.VMID = active.VMID
		opts.Result.CleanupPending = true
	}

	// Auth.
	e.Printer.Step(2, 3, i18n.T("Authenticating...", "Аутентификация..."))
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	if ts, ok := e.Cloud.(interface{ SetToken(string) }); ok {
		ts.SetToken(tok.Value)
	}
	e.Printer.StepDone(i18n.T("Authenticated", "Аутентификация пройдена"))

	// Delete VM, then its floating IP and the port rules deploy created.
	e.Printer.Step(3, 3, i18n.T("Deleting VM, floating IP and port rules...", "Удаление ВМ, floating IP и правил портов..."))
	// Older receipts may lack the address ID. Capture a proven binding while
	// the VM still exists, and persist it before deletion detaches the IP.
	fipErr := e.RecordLegacyFloatingIP(ctx, active, vm, floatingip.Of(stateFile, e.projectOf(active), active.VMName, active.VMID)...)
	// Persist the labelled IDs with the target while the approval is current.
	// A retry can reconcile them even if discovery omits a deleting rule.
	sgrules.TrackLabeled(stateFile, active, labeled)
	prevStatus := active.Status
	active.Status = "destroying"
	active.UpdatedAt = time.Now()
	stateFile.UpsertDeployment(*active)
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	if active.VMID == "" {
		// No ID to delete by: DELETE /vms/ with an empty ID is not a VM.
		e.Printer.Warning(i18n.T("No VM ID in state; nothing to delete in the cloud", "В state нет ID ВМ; удалять в облаке нечего"))
	} else if err = e.deleteVMWhenDeletable(ctx, active.VMID); err != nil {
		// If VM is already gone (VM_NOT_FOUND), that's fine — unless the
		// record is of another project than the active one: DELETE
		// /vms/{id} carries no project, so a key that does not see that
		// project gets a 404 for a VM that lives on, and "already deleted"
		// would drop the record of a billed VM (ревью Fable, A2; status and
		// the MCP destroy refuse the same way).
		var cliErr *clerrors.CLIError
		otherProject := active.ProjectID != "" && e.Config.ProjectID != "" && active.ProjectID != e.Config.ProjectID
		if stderrors.As(err, &cliErr) && cliErr.Code == clerrors.CodeVMNotFound && otherProject {
			active.Status = prevStatus
			stateFile.UpsertDeployment(*active)
			_ = state.Write(statePath, stateFile)
			return clerrors.Wrap(clerrors.CodeConfigInvalid,
				i18n.Tf("VM %s is recorded for project %s, not the active one (%s), and the active key does not see it; nothing was deleted and its record is kept",
					"ВМ %s записана для проекта %s, а активный — %s, и ключ активного проекта её не видит; ничего не удалено, запись сохранена",
					active.VMID, active.ProjectID, e.Config.ProjectID),
				i18n.Tf("Switch to the record's project ('evo project use %s' or CLOUDRU_PROJECT_ID) and run 'evo destroy' again.",
					"Переключитесь на проект записи ('evo project use %s' или CLOUDRU_PROJECT_ID) и повторите 'evo destroy'.",
					active.ProjectID), err)
		} else if stderrors.As(err, &cliErr) && cliErr.Code == clerrors.CodeVMNotFound {
			e.Printer.StepDone(i18n.T("VM already deleted", "ВМ уже удалена"))
		} else {
			// The VM is still there: the record keeps saying what it was.
			active.Status = prevStatus
			stateFile.UpsertDeployment(*active)
			_ = state.Write(statePath, stateFile)
			if st := provision.HTTPStatus(err); st == http.StatusConflict || st == http.StatusUnprocessableEntity {
				return e.RefusedVMDelete(ctx, active.VMID, fipRef(active), err, i18n.T(" The deployment record is kept.", " Запись развёртывания сохранена."))
			}
			return err
		}
	} else {
		// Poll until gone.
		t := e.timings()
		deadline := time.After(t.DestroyPollTimeout)
		pollTick, stopPoll := newPollTicker(t.DestroyPollInterval)
		defer stopPoll()
		confirmed := false
	pollLoop:
		for {
			select {
			case <-deadline:
				e.Printer.Warning(i18n.T("Timeout waiting for VM deletion confirmation", "Не дождались подтверждения удаления ВМ"))
				break pollLoop
			case <-pollTick:
				_, getErr := e.Cloud.GetVM(ctx, active.VMID)
				if getErr != nil {
					if cliErr, ok := getErr.(*clerrors.CLIError); ok && cliErr.Code == clerrors.CodeVMNotFound {
						confirmed = true
						break pollLoop
					}
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if !confirmed {
			// Accepted but not confirmed: the VM may still exist, its FIP
			// stays attached and its app may still serve. The record stays
			// "destroying" with the FIP and the rules; nothing is released
			// and destroy does not answer "destroyed" — a re-run finishes
			// once the VM is gone (release audit, A-005, L-11).
			e.Printer.StepDone(i18n.Tf("VM deletion requested (not confirmed within %s)",
				"Удаление ВМ запрошено (подтверждения за %s не было)", t.DestroyPollTimeout))
			stateFile.UpsertDeployment(*active)
			if err := state.Write(statePath, stateFile); err != nil {
				return err
			}
			return deletionUnconfirmed(active.VMID, t.DestroyPollTimeout)
		}
		e.Printer.StepDone(i18n.T("VM deleted", "ВМ удалена"))
	}

	// The VM is gone but its floating IP is billed until released. On failure
	// the record stays ("destroying"), so re-running destroy retries.
	stateFile.UpsertDeployment(*active)
	if fipErr == nil {
		fipErr = e.releaseFloatingIP(ctx, active)
	}

	// Port rules only now that the VM is gone: had its delete failed, the
	// app would still be serving and must keep its ports. A failed rule
	// delete is a warning, not a failed destroy: the record stays (as
	// "destroyed") with the rule marked, and the next destroy retries.
	rules := e.closeRules(ctx, stateFile, append([]string{active.ID}, pending...))
	left := portLabels(active.PortsAlreadyOpen)
	if len(left) > 0 {
		e.Printer.StepDone(notOursText(left))
	}
	ipPending := stderrors.Is(fipErr, errFloatingIPReleaseUnconfirmed)
	if fipErr != nil && !ipPending {
		_ = state.Write(statePath, stateFile)
		return fipErr
	}

	// A lost create stays on the deployment until its own address is
	// identified and the final GET confirms its removal.
	lostReleased, lostMissing, extraErr := e.releaseLostAddresses(ctx, stateFile, active)

	// Clean up state. Retire moves records: active points into the slice.
	recID, vmID, vmIP, fipID := active.ID, active.VMID, active.VMIP, fipRef(active)
	for _, id := range append([]string{active.ID}, pending...) {
		if (ipPending || extraErr != nil) && id == recID {
			// The receipt and VM ID are needed to retry the address.
			continue
		}
		sgrules.Retire(stateFile, id)
	}
	if uncheckedErr != nil {
		e.Printer.Warning(UncheckedRules(uncheckedErr))
	}
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	cleanupPending := ipPending || extraErr != nil || len(rules.Failed) > 0 || uncheckedErr != nil || len(lostMissing) > 0
	if opts.Result != nil {
		opts.Result.CleanupPending = cleanupPending
		opts.Result.RuleCleanupPending = len(rules.Failed) > 0
	}

	if e.Printer.IsJSON() {
		status := "destroyed"
		if cleanupPending {
			status = "partial"
		}
		out := map[string]any{"status": status, "cleanup_pending": cleanupPending, "vm_id": vmID, "vm_ip": vmIP}
		if shown.VMName != "" {
			out["vm_name"] = shown.VMName
		}
		if ipPending {
			out["floating_ip_unconfirmed"] = fipID
			out["floating_ip_warning"] = fipErr.Error()
		}
		if len(rules.Failed) > 0 {
			out["hint"] = sgrules.RuleRetryHint()
		}
		if !rules.Empty() {
			out["security_group_rules"] = rules
		}
		if uncheckedErr != nil {
			out["labelled_rules_unchecked"] = clerrors.Reason(uncheckedErr)
		}
		if len(left) > 0 {
			out["ports_left_open"] = left
		}
		if len(lostReleased) > 0 {
			out["lost_floating_ips_released"] = lostReleased
		}
		if ipPending || extraErr != nil {
			out["cleanup_hint"] = FloatingIPReceiptHint()
		}
		if extraErr != nil {
			out["warning"] = extraErr.Error()
		}
		if len(lostMissing) > 0 {
			out["lost_floating_ips_not_found"] = lostMissing
		}
		return e.Printer.JSON(out)
	}
	if ipPending {
		e.Printer.Warning(fipErr.Error())
	}
	if ipPending || extraErr != nil {
		e.Printer.Warning(FloatingIPReceiptHint())
	}
	if cleanupPending {
		e.Printer.Warning(i18n.T("Resource cleanup is incomplete.", "Уборка ресурсов не завершена."))
		return nil
	}
	e.Printer.Success(i18n.T("Deployment destroyed successfully.", "Развёртывание удалено."))
	return nil
}

// releaseLostAddresses reports released addresses and unresolved create receipts.
func (e *Engine) releaseLostAddresses(ctx context.Context, sf *state.File, rec *state.DeploymentRecord) (released, missing []string, releaseErr error) {
	rel, miss, err := floatingip.Release(ctx, e.Cloud, sf, e.projectOf(rec), rec.VMName, rec.VMID, rec.FloatingIPID)
	for _, m := range rel {
		released = append(released, m.Name)
		e.Printer.StepDone(i18n.Tf("Floating IP %s, requested with no answer, released", "Floating IP %s, запрошенный без ответа облака, освобождён", m.Name))
	}
	for _, m := range miss {
		missing = append(missing, m.Name)
		e.Printer.Warning(i18n.Tf("floating IP %s, requested %s with no answer, is not listed. The receipt was kept. Check the request and address ID in the cloud before deciding whether to release it",
			"floating IP %s, запрошенный %s без ответа облака, в списке не найден. Квитанция сохранена. Перед решением об освобождении сверьте запрос и ID адреса в облаке", m.Name, m.RequestedAt.Local().Format("02.01.2006 15:04")))
	}
	if err != nil {
		e.Printer.Warning(i18n.Tf("cleanup of the floating IPs requested with no answer is unconfirmed (%v); the next destroy checks the receipts again",
			"уборка floating IP, запрошенных без ответа облака, не подтверждена (%v). Повтор destroy проверит сохранённые квитанции", err))
	}
	return released, missing, err
}

// ErrDeletionUnconfirmed is the cause of the error destroy returns when
// Cloud.ru accepted the delete but the VM was still there after the wait:
// the MCP tool answers it as partial.
var ErrDeletionUnconfirmed = stderrors.New("VM deletion not confirmed")

// deletionUnconfirmed is that error: API_ERROR (exit 5) — the outcome is
// Cloud.ru's, as for a VM that "cannot be deleted in its current state"
// above; the record is kept and a re-run finishes.
func deletionUnconfirmed(vmID string, waited time.Duration) error {
	return clerrors.Wrap(clerrors.CodeAPIError,
		i18n.Tf("Cloud.ru accepted the deletion of VM %s, but the VM is still there after %s; its floating IP and port rules are kept, and the deployment record stays (destroying)",
			"Cloud.ru принял удаление ВМ %s, но через %s она всё ещё есть; её floating IP и правила портов оставлены, запись о развёртывании сохранена (destroying)", vmID, waited),
		i18n.Tf("While the VM exists, it and its floating IP are billed. Check it with 'evo vm get %s' and re-run 'evo destroy': once the VM is gone it releases the floating IP and closes the ports.",
			"Пока ВМ существует, она и её floating IP оплачиваются. Проверьте её: 'evo vm get %s' — и повторите 'evo destroy': когда ВМ не станет, он освободит floating IP и закроет порты.", vmID),
		ErrDeletionUnconfirmed)
}

// destroyTarget is the first line of destroy: which VM, at what address,
// in which zone.
// severalDeployments refuses destroy without a VM when the directory holds
// more than one active deployment, naming each VM.
func severalDeployments(all []*state.DeploymentRecord) error {
	vms := make([]string, len(all))
	for i, rec := range all {
		vms[i] = vmText(rec)
	}
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("this directory has several active deployments (%d: %s); destroy without a VM would delete only the first",
			"в этом каталоге несколько активных развёртываний (%d: %s); destroy без указания ВМ удалил бы только первое",
			len(all), strings.Join(vms, "; ")),
		i18n.T("Delete each VM by its ID: 'evo vm delete <VM ID>' (MCP: destroy with vm_id). Its record in the state is updated.",
			"Удалите каждую ВМ по её ID: 'evo vm delete <ID ВМ>' (в MCP: destroy с vm_id). Её запись в состоянии обновится."))
}

func destroyTarget(rec *state.DeploymentRecord) string {
	s := i18n.T("VM ", "ВМ ") + vmText(rec)
	if rec.VMIP != "" {
		s += ", " + rec.VMIP
	}
	if z := zoneText(rec.ZoneName, rec.Region); z != "" {
		s += i18n.T(", zone ", ", зона ") + z
	}
	return s
}

// portLabels turns state port keys into labels ("80/tcp" → "80").
func portLabels(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = sgrules.Label(k)
	}
	return out
}

// notOursText: destroy leaves the ports deploy found already open.
func notOursText(labels []string) string {
	if len(labels) == 1 {
		return i18n.Tf("port %s was not opened by this deploy — left open", "порт %s открывался не этим deploy — оставлен", labels[0])
	}
	return i18n.Tf("ports %s were not opened by this deploy — left open", "порты %s открывались не этим deploy — оставлены", strings.Join(labels, ","))
}

func noDeploymentToDestroy() string {
	return i18n.T("No active deployment to destroy.", "Активного развёртывания нет, удалять нечего.")
}

// noDeploymentAnswer is the JSON answer of a destroy with nothing to
// destroy, as 'evo destroy --json' has given it since v1.0.
func noDeploymentAnswer() map[string]any {
	return map[string]any{"status": "none", "message": noDeploymentToDestroy()}
}

// NothingToDestroy is the answer of a destroy of this directory with
// nothing to destroy — no active deployment and no port rules an earlier
// destroy left — the one 'evo destroy --json' gives; false when there is
// something to destroy or the state cannot be read (the destroy meets it).
// The MCP destroy answers it with no question, as the CLI does (owner's
// decision 24.10, W5).
func (e *Engine) NothingToDestroy() (map[string]any, bool) {
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil || sf.ActiveDeployment() != nil || len(sgrules.Pending(sf)) > 0 {
		return nil, false
	}
	return noDeploymentAnswer(), true
}

// LabeledRules are the rules labelled for the VM vmID that no record of
// sf holds (sgrules.FindLabeled, P6): what a delete of the VM closes beside
// the rules its records hold. zoneID or zoneName, the VM's zone when known,
// limits the reads to that zone's groups. Read within the time of a
// preview's lookups. The error is a list not read: the answer says the
// rules were not checked (UncheckedRules), and the VM's delete goes on.
func (e *Engine) LabeledRules(ctx context.Context, sf *state.File, projectID, vmID, zoneID, zoneName string) ([]sgrules.Planned, error) {
	if e.Cloud == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	return sgrules.FindLabeled(ctx, e.Cloud, projectID, vmID, zoneID, zoneName, sf)
}

// UncheckedRules says that the rules labelled for the VM were not all
// checked, and why (the class of the error): the same words in the
// preview and the answer of every delete of a VM, CLI and MCP.
func UncheckedRules(err error) string {
	return i18n.Tf("the security group rules labelled for the VM were not all checked (%s); to close them, repeat the same call with the VM's id",
		"правила группы безопасности с меткой ВМ проверены не все (%s); чтобы закрыть их, повторите тот же вызов с id ВМ", clerrors.Reason(err))
}

// openLeftOf is OpenLeft for the VM of rec: its group, less the rules this
// destroy deletes (those of rec and of pending, and the labelled ones).
func (e *Engine) openLeftOf(ctx context.Context, sf *state.File, rec *state.DeploymentRecord, pending []string, labeled []sgrules.Planned) *OpenLeft {
	if rec == nil {
		return nil
	}
	var groups []string
	if rec.SecurityGroupID != "" {
		groups = []string{rec.SecurityGroupID}
	}
	closing := ClosingRules(append(sgrules.Plan(sf, append([]string{rec.ID}, pending...)...), labeled...))
	return e.OpenLeft(ctx, e.projectOf(rec), rec.VMID, groups, closing)
}

// labeledRules is LabeledRules for the VM of rec, in its zone.
func (e *Engine) labeledRules(ctx context.Context, sf *state.File, rec *state.DeploymentRecord) ([]sgrules.Planned, error) {
	if rec == nil {
		return nil, nil
	}
	return e.LabeledRules(ctx, sf, e.projectOf(rec), rec.VMID, rec.Region, rec.ZoneName)
}

// closeRules deletes the port rules the given deployments created and
// reports it; see sgrules.Close.
func (e *Engine) closeRules(ctx context.Context, sf *state.File, ids []string) sgrules.CloseResult {
	var res sgrules.CloseResult
	for _, id := range ids {
		res.Add(sgrules.Close(ctx, e.Cloud, sf, id, e.Config.ProjectID, e.Maker()))
	}
	plain := slices.DeleteFunc(slices.Clone(res.Closed), func(label string) bool { return slices.Contains(res.LabeledClosed, label) })
	if len(plain) > 0 {
		e.Printer.StepDone(i18n.Tf("Closed ports %s in the security group", "Закрыты порты %s в группе безопасности", strings.Join(plain, ",")))
	}
	if len(res.LabeledClosed) > 0 {
		e.Printer.StepDone(i18n.Tf("Closed the security group rules labelled for the VM: ports %s", "Закрыты правила группы безопасности, помеченные для ВМ: порты %s", strings.Join(res.LabeledClosed, ", ")))
	}

	for _, k := range res.Kept {
		e.Printer.StepDone(i18n.T("Kept port ", "Оставлен порт ") + k)
	}
	if len(res.Failed) > 0 {
		e.Printer.Warning(i18n.Tf("Could not delete the security group rules for ports %s"+
			". They stay in state: re-run 'evo destroy' to retry, or delete them in the Cloud.ru console.",
			"Не удалось удалить правила группы безопасности для портов %s"+
				". Они остаются в state: повторите 'evo destroy' или удалите их в консоли Cloud.ru.", strings.Join(res.Failed, "; ")))
	}
	return res
}

// finishPendingRules is destroy with no active deployment, only rules left
// by an earlier destroy.
func (e *Engine) finishPendingRules(ctx context.Context, opts DestroyOptions, statePath string, sf *state.File, pending []string) error {
	proceed, err := e.destroyGate(ctx, opts, pendingRulesPreview(sf, pending))
	if err != nil {
		return err
	}
	if !proceed {
		return nil // --dry-run
	}
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	if ts, ok := e.Cloud.(interface{ SetToken(string) }); ok {
		ts.SetToken(tok.Value)
	}
	e.Printer.Step(1, 1, i18n.T("Deleting port rules left by an earlier destroy...", "Удаление правил портов, оставшихся от прошлого destroy..."))
	if opts.Result != nil && len(pending) == 1 {
		opts.Result.VMID = sf.FindDeployment(pending[0]).VMID
		opts.Result.CleanupPending = true
	}
	rules := e.closeRules(ctx, sf, pending)
	for _, id := range pending {
		sgrules.Retire(sf, id)
	}
	if err := state.Write(statePath, sf); err != nil {
		return err
	}
	cleanupPending := len(rules.Failed) > 0
	if opts.Result != nil {
		opts.Result.CleanupPending = cleanupPending
		opts.Result.RuleCleanupPending = len(rules.Failed) > 0
	}
	if e.Printer.IsJSON() {
		status := "destroyed"
		if cleanupPending {
			status = "rules_pending"
		}
		out := map[string]any{"status": status, "cleanup_pending": cleanupPending, "security_group_rules": rules}
		if cleanupPending {
			out["hint"] = sgrules.RuleRetryHint()
		}
		return e.Printer.JSON(out)
	}
	if len(rules.Failed) == 0 {
		e.Printer.Success(i18n.T("Security group rules deleted.", "Правила группы безопасности удалены."))
	}
	return nil
}

// fipReleaseAttempts bounds DeleteFloatingIP retries: right after the VM is
// deleted the platform may still report the FIP as attached.
const fipReleaseAttempts = 5

// RecordLegacyFloatingIP resolves an old receipt before the VM is deleted.
// A matching name or recorded address selects candidates. Only the current
// VM binding proves which ID can be released. An empty list cannot retire
// a receipt for a previously recorded external address or ambiguous candidate.
func (e *Engine) RecordLegacyFloatingIP(ctx context.Context, rec *state.DeploymentRecord, vm *provision.VM, lost ...state.LostFloatingIP) error {
	if rec.FloatingIPID != "" || rec.VMID == "" && rec.VMIP == "" && !rec.LegacyFloatingIPPending {
		return nil
	}
	fips, err := e.Cloud.ListFloatingIPs(ctx, e.projectOf(rec))
	if err != nil {
		return clerrors.Wrap(clerrors.CodeAPIError,
			i18n.T("Could not check the recorded floating IP. It may still be billed.",
				"Не удалось проверить записанный floating IP. Он может оставаться платным."), "", errFloatingIPReleaseUnconfirmed)
	}
	resolution := legacyIPResolution(e.projectOf(rec), rec)
	proved, matched, ambiguous := legacyBinding(rec, vm, fips, lost, resolution)
	// A previously released ID that still exists is itself a contradiction,
	// even after a rename or detachment. It supplies no authority to DELETE.
	releasedStillListed := listsReleasedAddress(fips, resolution)
	if resolution != nil && (resolution.Contradicted || rec.LegacyFloatingIPPending || releasedStillListed || proved != nil || ambiguous) {
		resolution.Contradicted = true
		if resolution.ConflictingAddressID == "" {
			if releasedStillListed {
				resolution.ConflictingAddressID = resolution.AddressID
			} else if proved != nil {
				resolution.ConflictingAddressID = proved.ID
			}
		}
		rec.LegacyFloatingIPPending = true
		return clerrors.Wrap(clerrors.CodeAPIError,
			i18n.T("Current cloud evidence contradicted the recorded outcome. The floating IP may still be billed. Check the receipt and provide fresh independent evidence.",
				"Данные облака опровергли записанный исход. Плавающий IP может оставаться платным. Проверьте квитанцию и укажите новое независимое свидетельство."), "", errFloatingIPReleaseUnconfirmed)
	}
	if ambiguous {
		rec.LegacyFloatingIPPending = true
		return clerrors.Wrap(clerrors.CodeAPIError, i18n.T("Conflicting evidence for the deployment's floating IP. The receipt was kept.", "Данные об адресе развёртывания расходятся. Квитанция сохранена."), "", errFloatingIPReleaseUnconfirmed)
	}
	if proved != nil {
		rec.LegacyFloatingIPPending = false
		rec.FloatingIPID, rec.VMIP = proved.ID, proved.IPAddress
		return nil
	}
	if resolution != nil {
		return nil
	}
	if matched || rec.LegacyFloatingIPPending || rec.ID != "" && recordedExternalIP(rec.VMIP) {
		rec.LegacyFloatingIPPending = true
		return clerrors.Wrap(clerrors.CodeAPIError,
			i18n.T("The floating IP's binding to this VM could not be verified. Check the address in the Cloud.ru console before releasing it.",
				"Не удалось подтвердить привязку floating IP к этой ВМ. До освобождения проверьте адрес в консоли Cloud.ru."), "", errFloatingIPReleaseUnconfirmed)
	}
	return nil
}

func legacyBinding(rec *state.DeploymentRecord, vm *provision.VM, fips []*provision.FloatingIP, lost []state.LostFloatingIP, resolution *state.FloatingIPResolution) (proved *provision.FloatingIP, matched, ambiguous bool) {
	// The oldest records have no VM name. An actual VM card read by
	// the recorded ID may supply the name for candidate selection only.
	vmName := rec.VMName
	if vmName == "" && vm != nil && rec.VMID != "" && vm.ID == rec.VMID {
		vmName = vm.Name
	}
	for _, f := range fips {
		if f == nil {
			continue
		}
		// A legacy decision may predate the first observation of the address
		// on the VM. Never rewrite that receipt's identity with a live value.
		fromDecision := resolution != nil && ((resolution.AddressID != "" && resolution.AddressID == f.ID) ||
			(vm != nil && vm.ID == rec.VMID && vm.FloatingIP != "" && vm.FloatingIP == f.IPAddress))
		if !fromDecision && !(rec.VMIP != "" && f.IPAddress == rec.VMIP) && !provision.IsEvoFIPOf(f.Name, rec.VMID, vmName) {
			continue
		}
		// A name-only candidate of an explicitly tracked create belongs to
		// that receipt. Counting its unbound namesake once again here
		// would manufacture a second receipt after reconciliation.
		if rec.VMIP != "" || !nameOfLostRequest(rec, lost, f.Name) {
			matched = true
		}
		var bound struct{ ID string }
		_ = json.Unmarshal(f.VM, &bound)
		onVM := rec.VMID != "" && bound.ID == rec.VMID
		// Old responses may name the bound IP only on the VM itself.
		if !f.Bound() && vm != nil && vm.ID == rec.VMID && vm.FloatingIP != "" && vm.FloatingIP == f.IPAddress {
			onVM = true
		}
		if !onVM || f.ID == "" || f.OnNATGateway() {
			continue
		}
		if proved != nil && proved.ID != f.ID {
			return nil, matched, true
		}
		proved = f
	}
	return proved, matched, false
}

// A private-only VM may have its private address in VMIP. Emptiness or a
// non-routable address alone is not a receipt for a billed floating IP.
func recordedExternalIP(address string) bool {
	if address == "" {
		return false
	}
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return true
	} // keep ambiguous old data for reconciliation
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate()
}

func nameOfLostRequest(rec *state.DeploymentRecord, lost []state.LostFloatingIP, name string) bool {
	for _, m := range lost {
		if name != "" && m.Name == name {
			return true
		}
	}
	for _, r := range rec.FloatingIPResolutions {
		if name != "" && r.Kind == "lost_request" && r.Name == name {
			return true
		}
	}
	return false
}

// releaseFloatingIP releases only a recorded or verified ID.
func (e *Engine) releaseFloatingIP(ctx context.Context, rec *state.DeploymentRecord) error {
	projectID, fipID := e.projectOf(rec), rec.FloatingIPID
	if fipID == "" {
		return nil
	}
	t := e.timings()
	var lastErr error
	for attempt := 0; attempt < fipReleaseAttempts; attempt++ {
		err := e.Cloud.DeleteFloatingIP(ctx, fipID, projectID)
		if err == nil || floatingip.NotFound(err) {
			return e.confirmFloatingIPReleased(ctx, fipID, rec.VMIP, err != nil)
		}
		lastErr = err
		if attempt == fipReleaseAttempts-1 {
			break
		}
		e.Printer.StepDone(i18n.Tf("Waiting to release floating IP %s after a refused request", "Ожидание освобождения floating IP %s после отказа", fipLabel(rec.VMIP, fipID)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.FIPRetryInterval):
		}
	}
	return clerrors.Wrap(clerrors.CodeAPIError,
		i18n.Tf("VM deleted, but floating IP %s was not released", "ВМ удалена, но floating IP %s не освобождён", fipLabel(rec.VMIP, fipID)),
		i18n.T("The floating IP is still billed. Re-run 'evo destroy --yes' or release it in the Cloud.ru console.",
			"Floating IP по-прежнему тарифицируется. Повторите 'evo destroy --yes' или освободите его в консоли Cloud.ru."),
		lastErr)
}

var errFloatingIPReleaseUnconfirmed = stderrors.New("floating IP release not confirmed")

// An accepted DELETE can leave the address billed. A definite GET 404 on
// its recorded ID confirms release. An uncertain read keeps it for retry.
func (e *Engine) confirmFloatingIPReleased(ctx context.Context, id, ip string, alreadyGone bool) error {
	gone, _ := floatingip.Gone(ctx, e.Cloud, id)
	if gone {
		if alreadyGone {
			e.Printer.StepDone(i18n.T("Floating IP already released", "Floating IP уже освобождён"))
		} else {
			e.Printer.StepDone(i18n.Tf("Floating IP %s released", "Floating IP %s освобождён", fipLabel(ip, id)))
		}
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return clerrors.Wrap(clerrors.CodeAPIError,
		i18n.Tf("Floating IP %s: release not confirmed; it may still be billed. Re-run 'evo destroy --yes'.",
			"Floating IP %s: освобождение не подтверждено, начисления могут продолжаться. Повторите 'evo destroy --yes'.", fipLabel(ip, id)),
		"", errFloatingIPReleaseUnconfirmed)
}

// projectOf is the project a deployment lives in: the one recorded at
// deploy, else (records from older versions) the configured one.
func (e *Engine) projectOf(rec *state.DeploymentRecord) string {
	if rec.ProjectID != "" {
		return rec.ProjectID
	}
	return e.Config.ProjectID
}

func fipLabel(ip, id string) string {
	if ip == "" || id == "" {
		return ip + id
	}
	return fmt.Sprintf("%s (%s)", ip, id)
}

// isNotDeletableYet reports whether DeleteVM refused because the VM is in a
// transitional state (Cloud.ru answers 409 or 422 while it is being created
// or changes state).
func isNotDeletableYet(err error) bool {
	st := provision.HTTPStatus(err)
	return st == http.StatusConflict || st == http.StatusUnprocessableEntity
}

// isTransientServerError is a 502/503/504 from DELETE: the compute adapter
// repeats those only for GET, and while waiting for a deletable VM one of
// them should not end the wait.
func isTransientServerError(err error) bool {
	switch provision.HTTPStatus(err) {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// deleteVMWhenDeletable deletes the VM, waiting while Cloud.ru refuses
// because it is still being created: a VM stuck in "creating" (seen in
// cloud-e2e on 15.10.2026) is billed with its floating IP, and giving up at
// once left both behind. It retries until Timings.DeletableWaitTimeout, then
// returns the last refusal.
func (e *Engine) deleteVMWhenDeletable(ctx context.Context, vmID string) error {
	return explainDeleteRefusal(vmID, e.waitAndDeleteVM(ctx, vmID))
}

// explainDeleteRefusal names the documented 405 of DELETE /api/v1/vms/{id}
// ("the VM has related entities", openapi-v3; api-contract §4.8): without a
// hint it read as a bare API error.
func explainDeleteRefusal(vmID string, err error) error {
	if provision.HTTPStatus(err) != http.StatusMethodNotAllowed {
		return err
	}
	return clerrors.Wrap(clerrors.CodeAPIError,
		i18n.Tf("Cloud.ru does not delete VM %s: other resources are attached to it (HTTP 405)",
			"Cloud.ru не удаляет ВМ %s: к ней привязаны другие ресурсы (HTTP 405)", vmID),
		i18n.T("Detach its extra disks and floating IPs in the Cloud.ru console (or 'evo vpc fip-release'), then run 'evo destroy' again; the deployment record is kept.",
			"Отвяжите дополнительные диски и floating IP ВМ в консоли Cloud.ru (или 'evo vpc fip-release') и повторите 'evo destroy'; запись развёртывания сохранена."),
		err)
}

func (e *Engine) waitAndDeleteVM(ctx context.Context, vmID string) (err error) {
	err = e.Cloud.DeleteVM(ctx, vmID)
	if err == nil || !isNotDeletableYet(err) {
		return err
	}
	t := e.timings()
	start := time.Now()
	deadline := start.Add(t.DeletableWaitTimeout)
	shown := clock.Now() // the duration the reports print
	// The state it is read in now, not a guess (#558): the line of the wait
	// names it, and the refusal falls back to it (refusedVMDelete).
	lastSeen := ""
	if vm, gerr := e.Cloud.GetVM(ctx, vmID); gerr == nil && vm != nil {
		lastSeen = vm.Status
	}
	if lastSeen != "" {
		e.Printer.StepDone(i18n.Tf("VM %s cannot be deleted yet (state %s); waiting up to %s...",
			"ВМ %s пока нельзя удалить (состояние %s); ждём до %s...", vmID, strconv.Quote(lastSeen), t.DeletableWaitTimeout))
	} else {
		e.Printer.StepDone(i18n.Tf("VM %s cannot be deleted yet; waiting up to %s...",
			"ВМ %s пока нельзя удалить; ждём до %s...", vmID, t.DeletableWaitTimeout))
	}
	lastReport := start
	defer func() {
		if err != nil && lastSeen != "" {
			err = &seenStateError{status: lastSeen, err: err}
		}
	}()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.DeletablePollInterval):
		}
		err = e.Cloud.DeleteVM(ctx, vmID)
		if err == nil || !(isNotDeletableYet(err) || isTransientServerError(err)) {
			return err
		}
		if time.Since(lastReport) >= time.Minute {
			lastReport = time.Now()
			status := i18n.T("unknown (the status request failed)", "неизвестно (запрос состояния не удался)")
			if vm, gerr := e.Cloud.GetVM(ctx, vmID); gerr == nil && vm != nil {
				// The cloud's status in quotes; the words above are evo's.
				status, lastSeen = strconv.Quote(vm.Status), vm.Status
			}
			e.Printer.StepDone(i18n.Tf("still waiting: VM is %s (%s elapsed)", "всё ещё ждём: состояние ВМ — %s (прошло %s)",
				status, clock.Since(shown).Round(time.Second)))
		}
	}
	return err
}

// RefusedVMDelete is the answer to a VM Cloud.ru would not delete (409 or
// 422), by the CLI destroy and the destroy tool by vm_id alike: the state
// the VM is read in now and the way out of it (DeleteWayOut, one place with
// the deletes of the tools), not a guess (release plan 1.6, #558: a VM read
// in deleting was answered «probably still being created»). In a state it
// does not leave by itself, the hint names its floating IP fip (id or
// address, "" for none), which stays billed until released (E11, Grok
// rc.1). kept is what the caller keeps, appended to the hint.
func (e *Engine) RefusedVMDelete(ctx context.Context, vmID, fip string, err error, kept string) error {
	status := ""
	if vm, gerr := e.Cloud.GetVM(ctx, vmID); gerr == nil && vm != nil {
		status = vm.Status
	} else if se := (*seenStateError)(nil); stderrors.As(err, &se) {
		status = se.status // the read now failed: the last one of the wait
	}
	if status == "" {
		return clerrors.Wrap(clerrors.CodeAPIError,
			i18n.Tf("VM %s cannot be deleted in its current state, and its state could not be read", "ВМ %s нельзя удалить в её текущем состоянии, а её состояние прочитать не удалось", vmID),
			i18n.Tf("Read it with 'evo vm get %s' (or in the Cloud.ru console) and re-run 'evo destroy' once it is running.",
				"Прочитайте её состояние ('evo vm get %s' или консоль Cloud.ru) и повторите 'evo destroy', когда она будет работать.", vmID)+kept,
			err)
	}
	_, hint := DeleteWayOut(status, vmID)
	if fip != "" && FinalDeleteState(status) {
		hint += i18n.Tf(" Its floating IP %s stays bound to it and billed: once the VM is gone, release it with 'evo vpc fip-release %s' (release_floating_ip).",
			" Её floating IP %s остаётся привязанным и оплачивается: когда ВМ не станет, освободите его — 'evo vpc fip-release %s' (release_floating_ip).", fip, fip)
	}
	if hint == "" {
		hint = i18n.Tf("Cloud.ru refused the delete in this state (waited %s): re-run 'evo destroy' once the VM is running ('evo vm get %s').",
			"Cloud.ru отказал в удалении в этом состоянии (ждали %s): повторите 'evo destroy', когда ВМ будет работать ('evo vm get %s').",
			e.timings().DeletableWaitTimeout, vmID)
	}
	return clerrors.Wrap(clerrors.CodeAPIError,
		i18n.Tf("VM %s cannot be deleted in state %q", "ВМ %s нельзя удалить в состоянии %q", vmID, status),
		hint+kept, err)
}

// seenStateError is the refusal of a VM delete with the last state the
// wait read, for when the read after it fails (refusedVMDelete).
type seenStateError struct {
	status string
	err    error
}

func (e *seenStateError) Error() string { return e.err.Error() }
func (e *seenStateError) Unwrap() error { return e.err }

// fipRef is how a record names its floating IP for a hint: its id, else
// its address; "" when it has none.
func fipRef(rec *state.DeploymentRecord) string {
	if rec.FloatingIPID != "" {
		return rec.FloatingIPID
	}
	return rec.VMIP
}
