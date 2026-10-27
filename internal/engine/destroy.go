// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// DestroyOptions holds parameters for the destroy command.
type DestroyOptions struct {
	Force  bool // skip confirmation prompt (--force / --yes)
	DryRun bool // show what would be deleted and stop
	// Gate, when set, asks for consent to the preview instead of
	// confirm.Ask: the destroy tool sets it for 'evo destroy', which shows
	// the preview with the tool's answer as its details (spec REQ-4).
	Gate func(ctx context.Context, p confirm.Preview) (bool, error)
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
// False when destroy would ask nothing: no deployment, or several (it
// refuses then). Reads only; the destroy tool answers it without
// confirmed=true.
func (e *Engine) DestroyPreview(ctx context.Context) (confirm.Preview, bool) {
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		return confirm.Preview{}, false
	}
	pending := sgrules.Pending(sf)
	active := sf.ActiveDeployment()
	switch {
	case active == nil && len(pending) > 0:
		return pendingRulesPreview(sf, pending), true
	case active == nil || len(sf.ActiveDeployments()) > 1:
		return confirm.Preview{}, false
	}
	shown := e.withNames(ctx, *active, nil, true)
	return destroyPreview(&shown, sf, pending, e.projectOf(active), e.projectFrom(active)), true
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

	e.Printer.Step(1, 3, i18n.T("Reading deployment info...", "Чтение сведений о развёртывании..."))
	// The names, not only the UUIDs: with several VMs the person approving
	// must see which one goes (first real run, v1.0.1).
	shown := e.withNames(ctx, *active, nil, true)
	e.Printer.StepDone(destroyTarget(&shown))

	// Without a terminal nobody can answer: confirm.Ask fails with exit code 3
	// instead of reading EOF as a refusal and exiting 0.
	proceed, err := e.destroyGate(ctx, opts, destroyPreview(&shown, stateFile, pending, e.projectOf(active), e.projectFrom(active)))
	if err != nil {
		return err
	}
	if !proceed {
		return nil // --dry-run
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
	prevStatus := active.Status
	active.Status = "destroying"
	active.UpdatedAt = time.Now()
	stateFile.UpsertDeployment(*active)
	_ = state.Write(statePath, stateFile)

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
				return clerrors.Wrap(clerrors.CodeAPIError,
					i18n.Tf("VM %s cannot be deleted in its current state", "ВМ %s нельзя удалить в её текущем состоянии", active.VMID),
					i18n.Tf("It is probably still being created or changing state (waited %s). Wait until it is running "+
						"(Cloud.ru console, or 'evo vm get %s') and re-run 'evo destroy': the record is kept, nothing is lost.",
						"Вероятно, она ещё создаётся или меняет состояние (ждали %s). Дождитесь, пока она заработает "+
							"(консоль Cloud.ru или 'evo vm get %s'), и повторите 'evo destroy': запись сохранена, ничего не потеряно.",
						e.timings().DeletableWaitTimeout, active.VMID),
					err)
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
	fipErr := e.releaseFloatingIP(ctx, active)

	// Port rules only now that the VM is gone: had its delete failed, the
	// app would still be serving and must keep its ports. A failed rule
	// delete is a warning, not a failed destroy: the record stays (as
	// "destroyed") with the rule marked, and the next destroy retries.
	rules := e.closeRules(ctx, stateFile, append([]string{active.ID}, pending...))
	left := portLabels(active.PortsAlreadyOpen)
	if len(left) > 0 {
		e.Printer.StepDone(notOursText(left))
	}
	if fipErr != nil {
		_ = state.Write(statePath, stateFile)
		return fipErr
	}

	// Clean up state. Retire moves records: active points into the slice.
	vmID, vmIP := active.VMID, active.VMIP
	for _, id := range append([]string{active.ID}, pending...) {
		sgrules.Retire(stateFile, id)
	}
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	if e.Printer.IsJSON() {
		out := map[string]any{"status": "destroyed", "vm_id": vmID, "vm_ip": vmIP}
		if shown.VMName != "" {
			out["vm_name"] = shown.VMName
		}
		if !rules.Empty() {
			out["security_group_rules"] = rules
		}
		if len(left) > 0 {
			out["ports_left_open"] = left
		}
		return e.Printer.JSON(out)
	}
	e.Printer.Success(i18n.T("Deployment destroyed successfully.", "Развёртывание удалено."))
	return nil
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

// closeRules deletes the port rules the given deployments created and
// reports it; see sgrules.Close.
func (e *Engine) closeRules(ctx context.Context, sf *state.File, ids []string) sgrules.CloseResult {
	var res sgrules.CloseResult
	for _, id := range ids {
		res.Add(sgrules.Close(ctx, e.Cloud, sf, id))
	}
	if len(res.Closed) > 0 {
		e.Printer.StepDone(i18n.Tf("Closed ports %s in the security group", "Закрыты порты %s в группе безопасности", strings.Join(res.Closed, ",")))
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
	rules := e.closeRules(ctx, sf, pending)
	for _, id := range pending {
		sgrules.Retire(sf, id)
	}
	if err := state.Write(statePath, sf); err != nil {
		return err
	}
	if e.Printer.IsJSON() {
		status := "destroyed"
		if len(rules.Failed) > 0 {
			status = "rules_pending"
		}
		return e.Printer.JSON(map[string]any{"status": status, "security_group_rules": rules})
	}
	if len(rules.Failed) == 0 {
		e.Printer.Success(i18n.T("Security group rules deleted.", "Правила группы безопасности удалены."))
	}
	return nil
}

// fipReleaseAttempts bounds DeleteFloatingIP retries: right after the VM is
// deleted the platform may still report the FIP as attached.
const fipReleaseAttempts = 5

// releaseFloatingIP deletes the deployment's floating IP. The ID comes from
// state; records written before floating_ip_id existed fall back to looking
// the address up in list_floating_ips. Nothing to release is not an error.
func (e *Engine) releaseFloatingIP(ctx context.Context, rec *state.DeploymentRecord) error {
	projectID := e.projectOf(rec)
	fipID := rec.FloatingIPID
	// Without an ID: by address, else by the name deploy gives it (its
	// create response may have been lost, leaving neither in state).
	if fipID == "" && (rec.VMIP != "" || rec.VMID != "") {
		fips, err := e.Cloud.ListFloatingIPs(ctx, projectID)
		if err != nil {
			return clerrors.Wrap(clerrors.CodeAPIError,
				i18n.Tf("cannot look up floating IP %s", "не удалось найти floating IP %s", fipLabel(rec.VMIP, fipName(rec.VMID))),
				i18n.T("The VM is deleted. Re-run 'evo destroy --yes' to release the floating IP, or release it in the Cloud.ru console.",
					"ВМ удалена. Чтобы освободить floating IP, повторите 'evo destroy --yes' или освободите его в консоли Cloud.ru."),
				err)
		}
		for _, f := range fips {
			if (rec.VMIP != "" && f.IPAddress == rec.VMIP) || (rec.VMID != "" && f.Name == fipName(rec.VMID)) {
				fipID = f.ID
				break
			}
		}
	}
	if fipID == "" {
		return nil
	}

	t := e.timings()
	var lastErr error
	for attempt := 0; attempt < fipReleaseAttempts; attempt++ {
		err := e.Cloud.DeleteFloatingIP(ctx, fipID, projectID)
		if err == nil {
			e.Printer.StepDone(i18n.Tf("Floating IP %s released", "Floating IP %s освобождён", fipLabel(rec.VMIP, fipID)))
			return nil
		}
		// 404 — already released: NOT_FOUND (VM_NOT_FOUND before QA-10).
		var cliErr *clerrors.CLIError
		if stderrors.As(err, &cliErr) && (cliErr.Code == clerrors.CodeNotFound || cliErr.Code == clerrors.CodeVMNotFound) {
			e.Printer.StepDone(i18n.T("Floating IP already released", "Floating IP уже освобождён"))
			return nil
		}
		lastErr = err
		if attempt == fipReleaseAttempts-1 {
			break
		}
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

func (e *Engine) waitAndDeleteVM(ctx context.Context, vmID string) error {
	err := e.Cloud.DeleteVM(ctx, vmID)
	if err == nil || !isNotDeletableYet(err) {
		return err
	}
	t := e.timings()
	start := time.Now()
	deadline := start.Add(t.DeletableWaitTimeout)
	shown := clock.Now() // the duration the reports print
	e.Printer.StepDone(i18n.Tf("VM %s cannot be deleted yet (still being created or changing state); waiting up to %s...",
		"ВМ %s пока нельзя удалить (ещё создаётся или меняет состояние); ждём до %s...",
		vmID, t.DeletableWaitTimeout))
	lastReport := start
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
				status = vm.Status
			}
			e.Printer.StepDone(i18n.Tf("still waiting: VM is %s (%s elapsed)", "всё ещё ждём: состояние ВМ — %s (прошло %s)",
				status, clock.Since(shown).Round(time.Second)))
		}
	}
	return err
}
