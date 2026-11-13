// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shatim-ops/evo-cli/internal/bootstrap"
	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/deploy"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/floatingip"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sizer"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/verify"
)

const (
	totalSteps        = 5
	sshMaxRetries     = 60
	remoteComposeDir  = "/opt/app"
	remoteComposeFile = "/opt/app/docker-compose.yml"

	// defaultDiskTypeName is looked up in list_disk_types (case-insensitive);
	// its UUID is per tenant.
	defaultDiskTypeName = "ssd"
	// dockerReadyCmd succeeds once the compose plugin exists and the SSH user
	// may talk to the daemon (docker group).
	dockerReadyCmd = "docker compose version && docker info >/dev/null"
)

// Deploy orchestrates the full deploy lifecycle.
func (e *Engine) Deploy(ctx context.Context, opts DeployOptions) (retErr error) {
	e.until, e.boundFrom, e.phase, e.decided = opts.Until, time.Now(), "", false
	caller := ctx
	defer func() { retErr = e.atBound(caller, retErr) }()
	// Stage 1: Parse compose file.
	project, err := compose.Parse(opts.ComposeFile)
	if err != nil {
		return err
	}

	statePath := state.DefaultPath(e.StateDir)

	// One deploy or destroy per directory at a time: the state read here is
	// written back minutes later.
	if !opts.DryRun {
		unlock, err := state.Lock(statePath)
		if err != nil {
			return err
		}
		defer unlock()
	}

	// Stage 2: Size — check existing state.
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	existing := stateFile.ActiveComposeDeployment()
	isNewDeploy := existing == nil
	// Another project's compose file is never sent to this VM (D62).
	if err := composeRefusal(existing, opts.ComposeFile); err != nil {
		return err
	}
	if w := composeUnknown(existing, opts.ComposeFile); w != "" && !isNewDeploy {
		e.Printer.Warning(w)
	}

	// A destroy that stopped half-way (VM deleted, floating IP not yet
	// released) keeps the record for its retry; there is no VM to reuse.
	if !isNewDeploy && existing.Status == "destroying" {
		return clerrors.New(clerrors.CodeVMProvisionFailed,
			i18n.Tf("deployment %s is being destroyed", "развёртывание %s в процессе удаления", existing.ID),
			i18n.T("Ask the person whether to finish its delete ('evo destroy'), then deploy again.", "Спросите человека, завершить ли удаление ('evo destroy'), затем разверните заново."))
	}
	// A VM that went to error_creating is not reused: it never boots, and a
	// re-run would give it a billed floating IP and retry SSH in a loop
	// (release QA, B-lifecycle 4; register CMP-9: such a VM is deleted
	// through the API).
	if !isNewDeploy && existing.Status == "error" {
		return clerrors.New(clerrors.CodeVMProvisionFailed,
			i18n.Tf("VM %s of this deployment is in an error state", "ВМ %s этого развёртывания в состоянии ошибки", existing.VMID),
			i18n.T("Ask the person whether to delete it ('evo destroy'), then deploy again (in another zone if it repeats).",
				"Спросите человека, удалить ли её ('evo destroy'), и разверните заново (если повторится — в другой зоне)."))
	}
	// One directory holds one deployment: never send this compose file to
	// a VM of another project, nor open its ports there.
	if !isNewDeploy && existing.ProjectID != "" && e.Config.ProjectID != "" && existing.ProjectID != e.Config.ProjectID {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("this directory already has deployment %s in project %s, not in %s",
				"в этом каталоге уже есть развёртывание %s в проекте %s, а не в %s", existing.ID, existing.ProjectID, e.Config.ProjectID),
			i18n.Tf("Update it with --project-id %s; to deploy to the new project instead, ask the person whether to delete it first ('evo destroy').",
				"Обновите его с --project-id %s; чтобы развернуть в новом проекте, спросите человека, удалить ли его сначала ('evo destroy').", existing.ProjectID))
	}

	// A stopped VM takes no upload and runs nothing: refused with the cause
	// before the question (axis 1), not asked and then timed out on SSH —
	// and before «nothing to deploy», which would say the service runs.
	if !isNewDeploy {
		if err := e.DeployVMStopped(ctx, existing); err != nil {
			return err
		}
	}
	// Skip only a deployment that finished with this file (UpToDate): after
	// an SSH or Docker failure, or a deploy cut before compose up, the hash
	// is already recorded, and a plain re-run must retry.
	if !isNewDeploy && existing.UpToDate(project.Hash) && !opts.Force {
		if e.Printer.IsJSON() {
			return e.Printer.JSON(map[string]any{"status": "unchanged", "vm_id": existing.VMID, "vm_ip": existing.VMIP})
		}
		e.Printer.Success(i18n.T("No changes detected in compose file (hash unchanged). Nothing to deploy.",
			"Compose-файл не изменился (хеш тот же), развёртывать нечего."))
		e.Printer.Hint(i18n.T("Use --force to redeploy anyway.", "Развернуть заново всё равно: --force."))
		return nil
	}
	// Updating the VM of an existing deployment needs consent as creating
	// one does: it replaces what runs there, maybe in production, and the
	// MCP deploy always asked for it (owner's decision Q1, spec REQ-12: one
	// gate for both surfaces). Before v1.2 only a VM without a public IP
	// was gated here, for the billed floating IP deploy makes for it
	// (release QA, B-lifecycle 1); the preview still says so.
	if !isNewDeploy {
		if opts.DryRun {
			// --dry-run must not touch the VM. It stops before step 2,
			// which prints the compose warnings: print them here.
			for _, w := range project.Warnings {
				e.Printer.Warning(w)
			}
		}
		// Port 22 from here, before the question (M2): the update uploads
		// over SSH and would wait for it after consent.
		probed, reach := e.ReachSSH(ctx, existing.VMIP)
		proceed, err := e.redeployGate(ctx, opts, redeployPreview(existing, e.projectOf(existing), e.projectFrom(existing),
			existing.ComposeHash != project.Hash, opts.Force, probed, reach))
		if err != nil {
			return err
		}
		if !proceed {
			return nil // --dry-run
		}
	}

	// An existing VM needs the key for SSH from here on; a new one gets it
	// in provisionVM after consent, so --dry-run leaves no key behind.
	if !isNewDeploy {
		if err := e.EnsureSSHKey(); err != nil {
			return err
		}
	}

	// A dry run of a new VM without an access key shows the preview of what
	// is known, the missing settings named in it, as the tool's own dry run
	// does: the key is needed to create, not to look (D31, REL12-7; it was
	// AUTH_REQUIRED).
	if isNewDeploy && opts.DryRun && opts.Gate != nil && (e.Config.KeyID == "" || e.Config.Secret == "") {
		_, err := opts.Gate(ctx, nil)
		return err
	}

	// A new VM needs the project, and that is a setting to fix, not a cloud
	// answer: refuse before the key is exchanged for a token, as every
	// other project command does (tools engineFor) — behind a dead proxy
	// the exchange gave API_UNREACHABLE (exit 1) instead (release audit,
	// REL-8). With no key either, the key is named first (AUTH_REQUIRED):
	// the provider refuses it with no request.
	if isNewDeploy && e.Config.ProjectID == "" {
		if (e.Config.KeyID == "" || e.Config.Secret == "") && e.Auth != nil {
			if _, err := e.Auth.Token(ctx); err != nil {
				return err
			}
		}
		return clerrors.ProjectRequired()
	}
	// Nor without an image or a zone: settings too, refused before the
	// token as the project is (D48).
	if isNewDeploy {
		if err := e.DeployInputError(); err != nil {
			return err
		}
	}

	// Step 1: Auth (must happen before any API calls).
	e.Printer.Step(1, totalSteps, i18n.T("Authenticating...", "Аутентификация..."))
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	if ts, ok := e.Cloud.(interface{ SetToken(string) }); ok {
		ts.SetToken(tok.Value)
	}
	e.Printer.StepDone(i18n.T("IAM token valid", "Токен IAM действителен"))

	// Step 2: Analyze.
	e.Printer.Step(2, totalSteps, i18n.T("Analyzing compose file...", "Разбор compose-файла..."))
	e.Printer.StepDone(i18n.Tf("%d service(s), %d exposed port(s), hash=%s", "сервисов: %d, открытых портов: %d, хеш=%s",
		len(project.Services), len(project.Ports)+len(project.UDPPorts), project.Hash[:12]))
	for _, w := range project.Warnings {
		e.Printer.Warning(w)
	}

	// Determine region.
	region := opts.Region
	if region == "" {
		region = e.Config.Region
	}

	var rec state.DeploymentRecord
	if isNewDeploy {
		// Step 3: Provision.
		rec, err = e.provisionVM(ctx, opts, stateFile, statePath, project, region)
		if err == errDryRun {
			return nil
		}
		if vmID := rec.VMID; vmID != "" {
			defer func() { retErr = e.vmLeftRunning(caller, retErr, vmID) }()
		}
		if err != nil {
			return err
		}
	}
	// From here every step ends with the bound of the deploy (D69); the
	// create of the VM above does not: a create cut midway is a VM
	// without a record.
	ctx, cancel := e.bounded(ctx)
	defer cancel()
	if !isNewDeploy {
		e.Printer.Step(3, totalSteps, i18n.T("Reusing existing VM...", "Используется существующая ВМ..."))
		e.phase = "vm"
		// Records from before v1.0.1 have no names: they are looked up
		// once and saved with the record.
		rec = e.withNames(ctx, *existing, nil, true)
		rec.ComposeHash = project.Hash
		rec.ComposePath = composePath(opts.ComposeFile)
		rec.ExposedPorts = project.Ports
		rec.UpdatedAt = time.Now()
		// A deploy whose bound came while the VM was created left it
		// creating: wait for it before its floating IP and SSH.
		if existing.Status == "creating" {
			if err := e.awaitCreated(ctx, &rec, stateFile, statePath); err != nil {
				return err
			}
		}
		if rec.VMIP == "" && rec.VMID != "" {
			// The floating IP failed on an earlier run: try again instead
			// of failing on "no public IP" on every re-run.
			ferr := e.recoverFloatingIP(ctx, &rec, stateFile)
			stateFile.UpsertDeployment(rec)
			_ = state.Write(statePath, stateFile)
			if ferr != nil {
				return ferr
			}
		}
		// An earlier run may have stopped before the ports were opened, or
		// the compose file publishes new ones: open what is missing. Records
		// from before security_group_id existed have no group to open in.
		if rec.SecurityGroupID != "" {
			e.openPorts(ctx, e.projectOf(&rec), rec.SecurityGroupID, project.Ports, project.UDPPorts, opts.Cidr, &rec)
			stateFile.UpsertDeployment(rec)
			_ = state.Write(statePath, stateFile)
		}
		e.Printer.StepDone(i18n.Tf("VM %s at %s", "ВМ %s, адрес %s", vmText(&rec), rec.VMIP))
	}

	// Step 4: Deploy — SSH connect, upload, compose up.
	e.Printer.Step(4, totalSteps, i18n.T("Deploying application...", "Развёртывание приложения..."))

	if rec.VMIP == "" {
		return clerrors.New(clerrors.CodeVMProvisionFailed,
			i18n.T("VM has no public IP address", "у ВМ нет внешнего IP-адреса"),
			i18n.T("Floating IP assignment failed. Check Cloud.ru console and assign a floating IP manually.",
				"Не удалось привязать floating IP. Проверьте ВМ в консоли Cloud.ru и привяжите floating IP вручную."))
	}

	maxRetries := sshMaxRetries
	switch {
	case isNewDeploy:
	case rec.SSHHostKeyFingerprint == "" && unfinishedStatuses[existing.Status]:
		// The VM this deploy created, which no run has reached over SSH
		// yet: a bound of an MCP call ended the earlier run while it was
		// created. It is a new VM, with the limit the CLI gives it in one
		// call (FB171-1, D80).
	case rec.SSHHostKeyFingerprint == "":
		// Never reached over SSH (an earlier run timed out, or a VM made by
		// vm create): it may be booting again after a reboot from the console.
		maxRetries = sshMaxRetries / 2
	default:
		maxRetries = 12 // existing VM should be ready quickly
	}

	sshUserForConnect, err := sshUserOf(&rec)
	if err != nil {
		return err
	}

	// A wait for SSH a bound ended goes on (D80): its attempts and its
	// start are the record's, not the call's, so the wait of deploy over
	// MCP ends and says why as one call of the CLI does.
	if rec.SSHWait == nil {
		rec.SSHWait = &state.SSHWait{Since: clock.Now(), Limit: maxRetries}
	}
	if err := e.waitForSSH(ctx, rec.VMIP, sshUserForConnect, rec.SSHHostKeyFingerprint, rec.SSHWait); err != nil {
		// An error at the bound — a connect cut by its deadline too — is
		// the deploy in progress, as its answer says (atBound).
		err = e.atBound(caller, err)
		var going *InProgress
		switch {
		case stderrors.As(err, &going):
			// Not a timeout: deploy goes on, and its record says so, as
			// the answer does — logs and verify answer DEPLOY_IN_PROGRESS
			// (D76). A running deployment stays running. Past the first
			// minutes of the wait the answer says why SSH may never answer,
			// as the CLI does (D80).
			keepUnfinished(&rec)
			if clock.Since(rec.SSHWait.Since) >= e.timings().SSHSlowNote {
				going.Note = sshSlowText(rec.VMIP, rec.SSHWait.Limit)
			}
		case caller.Err() != nil:
			// Cancelled (the MCP client, Ctrl-C): not the end of the wait
			// either. The record stays unfinished with its wait, and the
			// next deploy goes on counting (FB171-2).
			keepUnfinished(&rec)
		default:
			// The status says which: a refused key or a changed host key
			// is not a timeout, and a re-run will not fix it (QA r4 Q7).
			rec.SSHWait = nil
			rec.Status = "ssh_timeout"
			switch clerrors.Reason(err) {
			case clerrors.CodeSSHAuthFailed:
				rec.Status = "ssh_auth_failed"
			case clerrors.CodeSSHHostKeyChanged:
				rec.Status = "ssh_host_key_changed"
			}
		}
		rec.UpdatedAt = time.Now()
		stateFile.UpsertDeployment(rec)
		return e.writeDecided(statePath, stateFile, err)
	}
	rec.SSHWait = nil
	defer e.Deployer.Close()

	// Store host key fingerprint on first connect, and save it now: a
	// failure further on must not reopen trust-on-first-use. Until compose
	// up succeeds the record is "deploying", so a re-run with the same
	// compose file retries instead of answering "No changes".
	if rec.SSHHostKeyFingerprint == "" {
		rec.SSHHostKeyFingerprint = e.Deployer.HostKeyFingerprint()
	}
	rec.Status = "deploying"
	rec.UpdatedAt = time.Now()
	stateFile.UpsertDeployment(rec)
	_ = state.Write(statePath, stateFile)

	// Install Docker if the image has none (Ubuntu/Debian chosen by init).
	if err := e.prepareDocker(ctx, rec.VMIP, sshUserForConnect, rec.SSHHostKeyFingerprint); err != nil {
		// One decision for the record and the answer, as for SSH: at the
		// bound the record stays deploying (unfinished, D76) and the answer
		// says so; a failure before it is docker_failed both ways (FB171-4).
		err = e.atBound(caller, err)
		var going *InProgress
		if !stderrors.As(err, &going) && ctx.Err() == nil {
			rec.Status = "docker_failed"
			rec.UpdatedAt = time.Now()
			stateFile.UpsertDeployment(rec)
			return e.writeDecided(statePath, stateFile, err)
		}
		e.decided = true
		return err
	}
	e.Printer.StepDone(i18n.T("SSH connected, docker compose ready", "SSH подключён, docker compose готов"))

	// Upload compose file.
	composeData, err := os.ReadFile(opts.ComposeFile)
	if err != nil {
		return clerrors.Wrap(clerrors.CodeComposeNotFound,
			i18n.Tf("cannot read compose file: %s", "не удалось прочитать compose-файл: %s", opts.ComposeFile),
			i18n.T("Check file permissions.", "Проверьте права на файл."),
			err)
	}

	files := []deploy.FileEntry{
		{RemotePath: remoteComposeFile, Content: composeData},
	}
	e.phase = "upload"
	if err := e.Deployer.Upload(ctx, files); err != nil {
		// One decision for the record and the answer, as for Docker: at the
		// bound or cancelled the record stays deploying (D76); a failure
		// before it is upload_failed both ways (D87, R-51).
		err = e.atBound(caller, err)
		var going *InProgress
		if stderrors.As(err, &going) || ctx.Err() != nil {
			e.decided = true
			return err
		}
		rec.Status = "upload_failed"
		rec.UpdatedAt = time.Now()
		stateFile.UpsertDeployment(rec)
		return e.writeDecided(statePath, stateFile, clerrors.Wrap(clerrors.CodeUploadFailed,
			i18n.T("uploading the compose file to the VM failed", "не удалось загрузить compose-файл на ВМ"),
			i18n.T("Run 'evo deploy' again: it uploads the file again.", "Повторите 'evo deploy': он загрузит файл заново."),
			err))
	}

	// Run docker compose up: a stage on the VM, apart from this session
	// (vmstage, D69 variant C) — pulling images takes minutes, and the end
	// of the session (the budget of an MCP call, a lost connection) does
	// not cut it; the next deploy reads it. Its input is the compose file:
	// a stage done for this one is not run again. The last lines go with a
	// failure in every mode: with --json the output goes nowhere else
	// (#558). They are the workload's words: the tool marks the field
	// untrusted.
	composeUp := fmt.Sprintf("cd %s && docker compose -f docker-compose.yml up -d", remoteComposeDir)
	// --force (force) runs compose up again with the same compose file, as
	// it did before the stage: the stage done for this input is not taken
	// for it. Its repeat after the bound goes without force (the tool's
	// next_args) and takes the stage this one started.
	composeInput := stageInput(composeUp, string(composeData))
	st, err := e.runStage(ctx, "compose-up", composeInput, composeUp, time.Time{}, opts.Force)
	if err == nil && st.State == "failed" {
		err = fmt.Errorf("exit status %d", st.Exit)
	}
	if err != nil {
		// At the bound or cancelled the record stays deploying (D76), as
		// for the upload and Docker: the next deploy takes the stage on.
		err = e.atBound(caller, err)
		var going *InProgress
		if stderrors.As(err, &going) || ctx.Err() != nil {
			e.decided = true
			return err
		}
		// Over, not in progress: compose_failed with its own reason, one
		// decision for the record and the answer (D87, R-51). logs reads
		// the services then; a re-run runs compose up again.
		ce := clerrors.Wrap(clerrors.CodeComposeFailed,
			i18n.T("docker compose up failed", "docker compose up завершился ошибкой"),
			i18n.T("Check your compose file; logs shows the services' logs (none if compose up started no container).",
				"Проверьте compose-файл; logs покажет журналы сервисов (их нет, если compose up не запустил ни одного контейнера)."),
			err)
		if out := lastLines(st.LogTail, composeTailLines); out != "" {
			ce.Details = map[string]any{"output": out}
			ce.Hint += i18n.T(" The last lines of docker compose up are in output.", " Последние строки docker compose up — в output.")
		}
		rec.Status = "compose_failed"
		rec.UpdatedAt = time.Now()
		stateFile.UpsertDeployment(rec)
		return e.writeDecided(statePath, stateFile, ce)
	}
	e.Printer.StepDone(i18n.T("Application deployed", "Приложение развёрнуто"))

	// Update state to running: compose up finished with this file.
	rec.Status, rec.DeployedHash = "running", project.Hash
	rec.UpdatedAt = time.Now()
	stateFile.UpsertDeployment(rec)
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	// Step 5: Verify.
	var results []verify.ProbeResult
	e.Printer.Step(5, totalSteps, i18n.T("Verifying health...", "Проверка работоспособности..."))
	e.phase = "verify"
	if len(project.Ports) > 0 {
		// Give containers a moment to start.
		t := e.timings()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.SettleDelay):
		}
		probe := e.Probe
		if probe == nil {
			probe = verify.Check
		}
		results = probe(ctx, rec.VMIP, project.Ports, t.VerifyTimeout)
		e.printVerifyResults(results, rec.VMIP)
	} else {
		e.Printer.StepDone(i18n.T("No exposed ports to verify", "Открытых портов нет, проверять нечего"))
	}

	// --json: the result on stdout, as destroy and status give it (QA r4
	// Q1: "evo deploy --yes --json > deploy.json" in docs/exit-codes.md).
	if e.Printer.IsJSON() {
		out := map[string]any{
			"status": "deployed", "vm_id": rec.VMID, "vm_ip": rec.VMIP,
			"project_id": e.projectOf(&rec), "region": rec.Region, "flavor": rec.VMFlavor,
			"ports": rec.ExposedPorts, "verify": results,
		}
		if len(e.portsOnlyFromGroups) > 0 {
			// Not open to the source of the deploy: said apart from ports.
			out["ports_open_only_to_groups"] = e.portsOnlyFromGroups
		}
		for k, v := range map[string]string{"vm_name": rec.VMName, "zone_name": rec.ZoneName, "flavor_name": rec.FlavorName} {
			if v != "" {
				out[k] = v
			}
		}
		return e.Printer.JSON(out)
	}
	e.Printer.Success(i18n.Tf("\nDeployment complete! VM IP: %s", "\nГотово! IP ВМ: %s", rec.VMIP))
	return nil
}

// redeployGate asks for consent to update the existing VM (p): through
// opts.Gate, else confirm.Ask (--dry-run: the preview only).
func (e *Engine) redeployGate(ctx context.Context, opts DeployOptions, p confirm.Preview) (bool, error) {
	if opts.Gate != nil {
		return opts.Gate(ctx, &p)
	}
	return confirm.Ask(ctx, p, e.confirmOptions(opts.Yes, opts.DryRun, opts.NonInteractive))
}

// DeployUpdate is what deploy does in a directory that already has a
// deployment: it updates that deployment's VM, after consent (spec REQ-12),
// or does nothing when the same compose file runs there.
type DeployUpdate struct {
	// Deployment is the record deploy updates.
	Deployment state.DeploymentRecord
	// ProjectID is the project of its VM.
	ProjectID string
	// ComposeChanged: the compose file is not the one deployed; nil when
	// it cannot be read (deploy stops on it then).
	ComposeChanged *bool
	// Unchanged: the same compose file is running there and force is off —
	// deploy has nothing to do, and asks nothing.
	Unchanged bool
	// Creates are the billable resources the update makes: a floating IP
	// for a VM that has no public address.
	Creates []string
}

// DeployUpdateOf is what deploy would do with the deployment of this
// directory (force: redeploy an unchanged compose file), nil when there is
// none — or the state cannot be read: deploy creates a VM or stops then.
// Reads the state and the compose file only; the deploy tool answers it
// without confirmed=true.
func (e *Engine) DeployUpdateOf(force bool) *DeployUpdate {
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil || sf.ActiveComposeDeployment() == nil {
		return nil
	}
	existing := sf.ActiveComposeDeployment()
	u := &DeployUpdate{Deployment: *existing, ProjectID: e.projectOf(existing), Creates: redeployCreates(existing)}
	if project, err := compose.Parse(e.Config.ComposeFile); err == nil {
		changed := existing.ComposeHash != project.Hash
		u.ComposeChanged = &changed
		// As Deploy skips it: a record of another project is refused
		// before, so it is not "unchanged".
		otherProject := existing.ProjectID != "" && e.Config.ProjectID != "" && existing.ProjectID != e.Config.ProjectID
		u.Unchanged = existing.UpToDate(project.Hash) && !force && !otherProject
	}
	return u
}

// DeployAddressAsked is the question a deploy of this directory asks
// before it reserves a floating IP: the address an earlier run asked for
// with no answer is not listed now (floatingip.Settle) — nil when there is
// none, or the deploy reserves no address. It reads only.
func (e *Engine) DeployAddressAsked(ctx context.Context) *floatingip.AskError {
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil || len(sf.LostFloatingIPs) == 0 {
		return nil
	}
	projectID, vmName, vmID := e.Config.ProjectID, e.deployVMName(), ""
	own := floatingip.Free
	if rec := sf.ActiveComposeDeployment(); rec != nil {
		if !needsFloatingIP(rec) {
			return nil
		}
		projectID, vmName, vmID, own = e.projectOf(rec), rec.VMName, rec.VMID, floatingip.FreeOrOf(rec.VMID)
	}
	lctx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	_, err = floatingip.Settle(lctx, e.Cloud, sf, projectID, vmName, vmID, own)
	ask, _ := floatingip.AsAsk(err)
	return ask
}

// deployVMName is the name deploy gives the VM of this directory.
func (e *Engine) deployVMName() string {
	projectName := e.Config.ProjectName
	if projectName == "" {
		projectName = filepath.Base(e.StateDir)
	}
	return vmNameFor(projectName)
}

// resolveFlavor determines the VM flavor via auto-sizing or explicit selection.
func (e *Engine) resolveFlavor(ctx context.Context, opts DeployOptions, project *compose.Project) (string, error) {
	flavor := opts.Flavor
	if flavor == "" {
		flavor = e.Config.Flavor
	}
	flavors, err := e.Cloud.ListFlavors(ctx)
	if err != nil {
		return "", fmt.Errorf(i18n.T("list flavors: %w", "список flavor: %w"), err)
	}
	// The zone of the VM resolved as the create resolves it (ResolveZone:
	// a UUID, or a name in any case), so the flavor is checked against the
	// zone the VM goes to, as the preview checks it (D67; Fable, FB160-15,
	// FB160-16): a flavor named or picked out of the zone stops here, before
	// anything is created.
	zone := e.deployZone(ctx, firstNonEmpty(opts.Region, e.Config.Region))

	if flavor != "" && flavor != "auto" {
		// A flavor the catalogue has not is refused first, as the preview
		// refuses it (#470): before the image and the network are read.
		for _, f := range flavors {
			if f.ID == flavor || f.Name == flavor {
				return flavor, FlavorOutOfZone(f, zone)
			}
		}
		return "", flavorNotFound(flavor, flavors)
	}

	// Auto-size: estimate resources, then match against the flavors of the
	// zone.
	match := sizeFlavor(project, flavors, zone)
	if match == nil {
		return "", clerrors.New(clerrors.CodeConfigInvalid,
			i18n.T("no suitable flavor found for the workload", "не найден flavor, подходящий под нагрузку"),
			i18n.T("Specify a flavor explicitly with --flavor.", "Задайте flavor явно: --flavor."))
	}
	if err := FlavorOutOfZone(match, zone); err != nil {
		return "", err
	}

	e.Printer.StepDone(i18n.Tf("Recommended flavor: %s (%d vCPU, %d GB RAM)", "Подобран flavor: %s (%d vCPU, %d ГБ RAM)",
		match.Name, match.VCPUs, match.RAMGB))

	return match.ID, nil
}

// sizeFlavor is the flavor deploy picks for the compose file's services
// among the flavors of zone (FlavorsInZone): the preview's pick and the
// create's are one. nil when none fits.
func sizeFlavor(project *compose.Project, flavors []*provision.Flavor, zone *deployplan.RefSpec) *provision.Flavor {
	flavors = FlavorsInZone(flavors, zone)
	list := make([]provision.Flavor, len(flavors))
	for i, f := range flavors {
		list[i] = *f
	}
	match := sizer.Match(sizer.EstimateFromServices(project.Services), list)
	if match == nil {
		return nil
	}
	for _, f := range flavors {
		if f.ID == match.Flavor.ID {
			return f
		}
	}
	return &match.Flavor
}

// deployZone is the zone of region (a UUID or a name) as the create
// resolves it; a zone the list does not name, or a list that cannot be
// read, is the region as given — compared by ID and, in any case, by name.
// The list is the one resolveNetwork reads next (zoneList).
func (e *Engine) deployZone(ctx context.Context, region string) *deployplan.RefSpec {
	zones, err := e.zoneList(ctx)
	if z, zerr := ResolveZoneRef(zones, err, region); zerr == nil && z != nil {
		return &deployplan.RefSpec{ID: z.ID, Name: z.Name}
	}
	return &deployplan.RefSpec{ID: region, Name: region}
}

// zoneList is the project's zone list, read once per call: the flavor's
// zone (deployZone) and the network's (resolveNetwork) resolve against
// the same list (FB160-21). A list that could not be read is not kept: the
// next reader asks again.
func (e *Engine) zoneList(ctx context.Context) ([]*provision.AvailabilityZone, error) {
	if e.zones != nil {
		return e.zones, nil
	}
	zones, err := e.Cloud.ListAvailabilityZones(ctx)
	if err == nil {
		e.zones = zones
	}
	return zones, err
}

// provisionVM creates a new VM and waits for it to become active.
func (e *Engine) provisionVM(ctx context.Context, opts DeployOptions, stateFile *state.File, statePath string, project *compose.Project, region string) (state.DeploymentRecord, error) {
	e.phase = "vm"
	e.Printer.Step(3, totalSteps, i18n.T("Provisioning VM...", "Создание ВМ..."))

	// The project before anything is asked of the cloud: the hint of every
	// other command (evo project use, --project-id), not only the variable.
	projectID := e.Config.ProjectID
	if projectID == "" {
		return state.DeploymentRecord{}, clerrors.ProjectRequired()
	}

	// The flavor: given, or picked for the workload (ListFlavors needs the
	// token of step 1). "Recommended flavor" belongs to this step.
	flavorName, err := e.resolveFlavor(ctx, opts, project)
	if err != nil {
		return state.DeploymentRecord{}, err
	}

	vmName := e.deployVMName()
	// A VM of the project with this name, one this folder did not make, is
	// refused here, before the question (FB170-2, D73): the create would
	// get a 409 after the consent.
	if err := e.deployNameFree(ctx, projectID, vmName, stateFile); err != nil {
		return state.DeploymentRecord{}, err
	}

	// The image with its metadata fields: image_id by UUID, image by name
	// (images.Resolve). Not found or ambiguous stops here, before anything
	// is created.
	imgDetail, err := e.BootImage(ctx, projectID)
	if err != nil {
		return state.DeploymentRecord{}, err
	}
	imageID := imgDetail.ID
	// Docker publishes no packages for the domestic distributions (evo
	// installs it from Docker's repository): refuse before the VM is
	// billed, not after an SSH session fails on it.
	if family := images.NoDockerFamily(imgDetail.Name, imgDetail.DisplayName, imgDetail.OS); family != "" {
		return state.DeploymentRecord{}, images.NoDockerError(clerrors.CodeConfigInvalid, imgDetail.Name, family)
	}
	diskSize, diskNote := provision.BootDiskGB(e.Config.DiskSize, imgDetail.MinDisk)
	if diskNote != "" {
		e.Printer.Warning(diskNote)
	}

	// image_metadata by the image's declared fields (internal/images, the
	// same rules as MCP provision and deploy_apply). The key is made only
	// after consent, so its fields are filled again then (KeyLater). Values
	// the image needs and evo cannot make come from image_metadata: in
	// .evo.yaml. linux_password is set as well: without it the VM never
	// leaves "creating" (seen live 2026-10-14). Login is by SSH key: the
	// password is random and not kept.
	imgMeta, filled, err := images.FillMetadata(imgDetail.MetadataFields, images.Inputs{
		PublicKey:   e.SSHPubKey,
		KeyLater:    true,
		Hostname:    vmName,
		Login:       "ubuntu",
		GenPassword: func() string { return randomPassword(16) },
		Values:      e.Config.ImageMetadata,
	})
	if err != nil {
		return state.DeploymentRecord{}, inProjectFile(err)
	}
	if len(filled.Missing) > 0 {
		return state.DeploymentRecord{}, images.MissingError(clerrors.CodeConfigInvalid, filled.Missing,
			i18n.T("image_metadata: {%s: <value>} in .evo.yaml", "image_metadata: {%s: <значение>} в .evo.yaml"))
	}
	keyFields := filled.KeyFields // filled with the SSH public key after consent

	// The SSH user is the login field the metadata filled (Cloud.ru images
	// default it to user1).
	sshUser := filled.Login
	if sshUser == "" {
		return state.DeploymentRecord{}, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("image %s declares no login user", "в образе %s не задан пользователь для входа", imageID),
			i18n.T("Choose a Linux image whose metadata has a 'name' field (evo image list), e.g. Ubuntu-24.04.",
				"Выберите образ Linux, в метаданных которого есть поле 'name' (evo image list), например Ubuntu-24.04."))
	}

	diskType, err := e.resolveDiskType(ctx, projectID, defaultDiskTypeName)
	if err != nil {
		return state.DeploymentRecord{}, err
	}

	network, err := e.resolveNetwork(ctx, projectID, region)
	if err != nil {
		return state.DeploymentRecord{}, err
	}
	region = network.ZoneID // a zone named by its name, as its UUID
	flavorAPIName, err := e.flavorAPIName(ctx, flavorName)
	if err != nil {
		return state.DeploymentRecord{}, err
	}

	// The create API takes names, not UUIDs, and an explicit interface:
	// UUIDs or a missing interface make it answer 500.
	req := provision.CreateVMRequest{
		ProjectID:          projectID,
		Name:               vmName,
		FlavorName:         flavorAPIName,
		AvailabilityZoneID: region,
		ImageName:          imgDetail.Name,
		ImageMetadata:      imgMeta,
		Disks: []provision.DiskReq{
			{
				Name:         provision.FitName(vmName, "-boot"),
				Size:         diskSize,
				DiskTypeName: diskType.Name,
			},
		},
		Interfaces: []provision.InterfaceReq{
			{
				Type:                     "regular",
				SubnetID:                 network.SubnetID,
				InterfaceSecurityEnabled: true,
				SecurityGroups:           []string{network.SecurityGroupID},
			},
		},
	}

	// The VM is billed from this call on: show what will be created and what
	// it costs, and get consent (or stop here on --dry-run).
	var proceed bool
	if opts.Gate != nil {
		proceed, err = opts.Gate(ctx, nil)
	} else {
		proceed, err = confirm.Ask(ctx, e.deployPreview(ctx, req, imgDetail, diskType.ID, network, project.Ports, project.UDPPorts, opts.Cidr), e.confirmOptions(opts.Yes, opts.DryRun, opts.NonInteractive))
	}
	if err != nil {
		return state.DeploymentRecord{}, err
	}
	if !proceed {
		return state.DeploymentRecord{}, errDryRun
	}
	if err := e.EnsureSSHKey(); err != nil {
		return state.DeploymentRecord{}, err
	}
	for _, name := range keyFields {
		imgMeta[name] = e.SSHPubKey
	}

	// The public IP goes on at creation: Cloud.ru images set up their apt
	// mirror at first boot and need the internet for it. Attached after
	// boot, the VM boots offline and cloud-init can spend 10+ minutes on
	// mirror timeouts before sshd starts. A FIP that cannot be made now is
	// attached after boot, as before.
	bootFIP, err := e.createBootFIP(ctx, projectID, region, vmName, stateFile)
	if _, lost := floatingip.AsLost(err); lost {
		// The mark of the address goes to the state before the answer:
		// destroy, doctor and the next deploy name it.
		if werr := state.Write(statePath, stateFile); werr != nil {
			e.Printer.Warning(i18n.Tf("the state file could not be saved (%v)", "файл state не удалось сохранить (%v)", werr))
		}
	}
	if err != nil {
		return state.DeploymentRecord{}, err
	}
	req.Interfaces[0].AttachExternalIPID = BootFIPAttachID(bootFIP)

	createStart := time.Now()
	vm, createErr := provision.CreateVMChecked(ctx, e.Cloud, req)
	if createErr != nil && provision.CreateRejected(createErr) {
		// The API refused the create: no VM came of it, and one found by
		// the name is another (a 409 for a name already taken). It is not
		// adopted or recorded over; the boot FIP is released (release
		// audit, L-01).
		relErr := e.releaseBootFIP(bootFIP, projectID) // the warning is printed
		if provision.IsNameTaken(createErr) {
			created := i18n.T("nothing was created", "ничего не создано")
			if relErr != nil {
				// Not "nothing": the address stays billed (break r3a R3A-2).
				created = i18n.Tf("the VM was not created, but floating IP %s (%s) is left and billed: release it with 'evo vpc fip-release %s'; a later deploy of this directory reuses it",
					"ВМ не создана, но floating IP %s (%s) остался и тарифицируется: освободите его ('evo vpc fip-release %s'); следующий deploy этого каталога использует его", bootFIP.IPAddress, bootFIP.ID, bootFIP.ID)
			}
			return state.DeploymentRecord{}, clerrors.Wrap(clerrors.CodeConfigInvalid,
				i18n.Tf("a VM named %s already exists in the project (Cloud.ru answered 409); %s",
					"ВМ с именем %s в проекте уже есть (Cloud.ru ответил 409); %s", vmName, created),
				deployNameHint(), createErr)
		}
		return state.DeploymentRecord{}, createErr
	}
	if createErr != nil {
		// The VM can exist although the call failed (timeout, Ctrl-C, a
		// lost answer). Then it is billed and its FIP is attached: record
		// both below instead of releasing the FIP and forgetting the VM.
		vm = provision.FindCreatedVM(e.Cloud, projectID, vmName, createStart)
		if vm == nil {
			_ = e.releaseBootFIP(bootFIP, projectID) // the warning is printed
			return state.DeploymentRecord{}, createErr
		}
	}

	// Register signal handler after VM creation.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Write initial state immediately.
	rec := state.DeploymentRecord{
		ID:           vm.ID,
		VMID:         vm.ID,
		VMIP:         vm.PublicIP,
		ProjectID:    projectID,
		CreatedBy:    e.Maker(),
		VMFlavor:     flavorName,
		Region:       region,
		VMName:       vmName,
		ZoneName:     network.ZoneName,
		FlavorName:   flavorAPIName,
		SSHUser:      sshUser,
		Status:       "creating",
		ComposeHash:  project.Hash,
		ComposePath:  composePath(opts.ComposeFile),
		ExposedPorts: project.Ports,
		// Saved now: a re-run after an interruption opens the ports here.
		SecurityGroupID:   network.SecurityGroupID,
		SecurityGroupName: network.SecurityGroup,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	if vm.Name != "" {
		rec.VMName = vm.Name
	}
	if bootFIP != nil {
		// Recorded at once: destroy releases it even if deploy stops here.
		rec.FloatingIPID = bootFIP.ID
		rec.VMIP = bootFIP.IPAddress
	}
	stateFile.UpsertDeployment(rec)
	if err := state.Write(statePath, stateFile); err != nil {
		// The VM exists and is billed; without its ID here nothing would
		// point to it.
		return rec, clerrors.Wrap(clerrors.CodeVMProvisionFailed,
			i18n.Tf("VM %s was created, but the state file could not be saved", "ВМ %s создана, но файл state не удалось сохранить", vm.ID),
			i18n.Tf("VM %s is running and billed. Delete it in the Cloud.ru console, or fix the state file problem and record it there.",
				"ВМ %s работает и тарифицируется. Удалите её в консоли Cloud.ru или устраните проблему с файлом state и запишите ВМ туда.", vm.ID),
			err)
	}
	if createErr != nil {
		return rec, clerrors.Wrap(clerrors.CodeVMProvisionFailed,
			i18n.Tf("VM %s was created, but the create call failed", "ВМ %s создана, но запрос на создание завершился ошибкой", vm.ID),
			i18n.Tf("VM %s is billed and recorded in the state: run 'evo deploy' again to finish it; whether to delete it instead ('evo destroy') — ask the person.",
				"ВМ %s тарифицируется и записана в state: повторите 'evo deploy', чтобы завершить развёртывание; удалять ли её ('evo destroy') — спросите человека.", vm.ID),
			createErr)
	}

	// Poll until active.
	t := e.timings()
	pollDeadline := time.After(t.VMPollTimeout)
	pollTick, stopPoll := newPollTicker(t.VMPollInterval)
	defer stopPoll()
	// A new VM takes minutes, sometimes more than ten (ru.AZ-1, 15.10.2026):
	// the wait shows its time and the VM's state, so a slow zone is told
	// apart from a stuck deploy. On a terminal one line is redrawn every
	// second; without one a line goes out on every change of the state and
	// every VMProgressInterval; --json prints nothing (WaitLine).
	wait := e.VMWaitLine(e.Printer)
	defer wait.Done()

	interrupted := func(sig string) (state.DeploymentRecord, error) {
		wait.Done() // the question goes to the same terminal
		deleted, err := e.handleInterrupt(sig, vm.ID, opts.NonInteractive)
		if deleted {
			// The VM goes asynchronously and its FIP stays attached until
			// it is gone: wait, then release with retries, like destroy.
			// Only then the record goes; if the FIP stays, the record keeps
			// its floating_ip_id so 'evo destroy' finishes the job (QA H2).
			rctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			gone := e.waitVMGone(rctx, vm.ID)
			ferr := e.releaseFloatingIP(rctx, &rec)
			if !gone && ferr == nil {
				// Deletion accepted but not confirmed: keep the record, or
				// a VM stuck in deleting would be billed without a trace.
				ferr = stderrors.New(i18n.Tf("deletion of VM %s is not confirmed yet; ask the person whether to finish it ('evo destroy')",
					"удаление ВМ %s ещё не подтверждено; спросите человека, завершить ли его ('evo destroy')", vm.ID))
			}
			if ferr != nil {
				e.Printer.Warning(ferr.Error())
				rec.Status = "destroying"
				rec.UpdatedAt = time.Now()
				stateFile.UpsertDeployment(rec)
				_ = state.Write(statePath, stateFile)
				return rec, err
			}
			// Gone in the cloud, gone from state: a kept record would be
			// "reused" by the next deploy.
			stateFile.RemoveDeployment(rec.ID)
			_ = state.Write(statePath, stateFile)
			return state.DeploymentRecord{}, err
		}
		return rec, err
	}

	var vmPollRefusals refusals
	// The bound of the deploy (D69): the VM is in the state as creating;
	// the next deploy takes it and goes on.
	var bound <-chan time.Time
	if !e.until.IsZero() {
		bound = time.After(time.Until(e.until))
	}
	vmState := "creating"
	for {
		select {
		case sig := <-sigCh:
			return interrupted(sig.String())
		case <-bound:
			return rec, &InProgress{Phase: "vm", Detail: vmState}
		case <-pollDeadline:
			return rec, clerrors.New(clerrors.CodeVMProvisionFailed,
				i18n.Tf("VM %s did not become active within %s; it is still being created and is billed", "ВМ %s не стала активной за %s; она продолжает создаваться и оплачивается", vm.ID, t.VMPollTimeout),
				i18n.Tf("Check the VM with 'evo vm get %s' or in the Cloud.ru console; to wait longer, set CLOUDRU_TIMEOUT (seconds).",
					"Проверьте ВМ: 'evo vm get %s' или консоль Cloud.ru; чтобы ждать дольше, задайте CLOUDRU_TIMEOUT (в секундах).", vm.ID))
		case <-wait.C():
			wait.Show()
		case <-pollTick:
			updated, err := e.Cloud.GetVM(ctx, vm.ID)
			keyRefused := vmPollRefusals.keyRefused(err)
			if err != nil {
				// An expired or refused key does not get better by waiting
				// out the timeout: stop now with its code (exit 2). The VM
				// is in the state already (break r3a R3A-4). One refused
				// token is the sleep's, not the key's: the next poll takes
				// a new one (refusals).
				if keyRefused {
					return rec, clerrors.Wrap(clerrors.Reason(err),
						i18n.Tf("VM %s was created, but checking it failed: the key was refused (%v)",
							"ВМ %s создана, но проверить её не удалось: ключ отклонён (%v)", vm.ID, err),
						i18n.Tf("VM %s is billed and recorded in the state. Check the key ('evo whoami'), then run 'evo deploy' again to finish; whether to delete it instead ('evo destroy') — ask the person.",
							"ВМ %s тарифицируется и записана в state. Проверьте ключ ('evo whoami') и повторите 'evo deploy', чтобы завершить; удалять ли её ('evo destroy') — спросите человека.", vm.ID),
						err)
				}
				e.Printer.Warning(i18n.Tf("poll error: %v", "ошибка опроса: %v", err))
				continue
			}
			vmState = updated.Status
			if updated.Status != "active" && updated.Status != "running" {
				wait.State(updated.Status)
			}
			switch updated.Status {
			case "active", "running":
				e.Printer.StepDone(i18n.Tf("VM %s active (%s)", "ВМ %s активна (%s)", vmText(&rec), ElapsedText(wait.Elapsed())))

				// The floating (public) IP: attached at creation, or now —
				// within the bound of the deploy (D69).
				ctx, cancel := e.bounded(ctx)
				defer cancel()
				var publicIP, fipID string
				if bootFIP != nil {
					publicIP, fipID, err = e.waitBootFIP(ctx, bootFIP, vm.ID, projectID)
				} else {
					publicIP, fipID, err = e.assignFloatingIP(ctx, vm.ID, rec.VMName, projectID, region, stateFile)
				}
				if _, lost := floatingip.AsLost(err); lost {
					// The VM is recorded with the mark of its address, and
					// the call stops: no second address is reserved for it.
					rec.Status = "provisioned"
					rec.UpdatedAt = time.Now()
					stateFile.UpsertDeployment(rec)
					_ = state.Write(statePath, stateFile)
					return rec, err
				}

				// Record the FIP even if it is not confirmed: destroy releases it.
				rec.VMIP = publicIP
				rec.FloatingIPID = fipID
				rec.Status = "provisioned" // VM up; "running" once compose up succeeds
				rec.UpdatedAt = time.Now()
				stateFile.UpsertDeployment(rec)
				_ = state.Write(statePath, stateFile)
				if ctx.Err() != nil {
					return rec, ctx.Err()
				}
				if err != nil {
					e.Printer.Warning(i18n.Tf("floating IP: %v", "floating IP не назначен: %v", err))
					// Continue without public IP — user can assign manually.
				}
				if publicIP != "" {
					e.Printer.StepDone(i18n.Tf("Public IP: %s", "Внешний IP: %s", publicIP))
				}
				// The rule IDs go to state at once: destroy deletes them.
				e.openPorts(ctx, projectID, network.SecurityGroupID, project.Ports, project.UDPPorts, opts.Cidr, &rec)
				stateFile.UpsertDeployment(rec)
				_ = state.Write(statePath, stateFile)
				return rec, nil
			}
			if provision.VMFailed(updated.Status) {
				// The record says so too: status showed "creating" for a
				// VM in error_creating (QA r4 Q6). destroy deletes it.
				rec.Status = "error"
				rec.UpdatedAt = time.Now()
				stateFile.UpsertDeployment(rec)
				// The record says the VM failed: so does the answer, whenever
				// the bound comes (FB171-4).
				return rec, e.writeDecided(statePath, stateFile, clerrors.New(clerrors.CodeVMProvisionFailed,
					i18n.Tf("VM %s entered state %s", "ВМ %s перешла в состояние %s", vm.ID, updated.Status),
					i18n.T("Check Cloud.ru console for details.", "Подробности — в консоли Cloud.ru.")))
			}
		case <-ctx.Done():
			// The CLI cancels ctx on the same SIGINT/SIGTERM that sigCh
			// receives, and select picks a ready case at random. Both cases
			// must lead to the same handling, or the VM may be left running
			// without a word.
			return interrupted("interrupt")
		}
	}
}

// ElapsedText is a wait in whole seconds: "1m20s", in Russian «1м20с».
func ElapsedText(d time.Duration) string {
	s := d.Round(time.Second).String()
	if i18n.Lang() == i18n.RU {
		s = strings.NewReplacer("h", "ч", "m", "м", "s", "с").Replace(s)
	}
	return s
}

// statusText shows an empty VM state as such, not as a blank.
func statusText(s string) string {
	if s == "" {
		return i18n.T("(not reported)", "(не сообщается)")
	}
	return s
}

// AssignFloatingIP creates a floating IP and attaches it to the VM's primary interface.
// It polls until the floating IP is in_use or times out. The FIP ID is set
// as soon as the FIP exists, even with an error: the caller records it, so
// destroy releases it (release audit, R-02). The waits are the engine's
// Timings: a test world waits milliseconds, not the production 5 s polls.
//
// The marks of sf come first (floatingip.Settle): the address an earlier
// call asked for with no answer is taken when the list shows it free or on
// this VM; not shown, the call asks (an AskError) before it reserves
// another. A create of this call with no answer leaves its mark in sf and
// answers LostAddressError. sf is changed in memory; the caller writes it.
func (e *Engine) AssignFloatingIP(ctx context.Context, vmID, vmName, projectID, azID string, sf *state.File) (ip, fipID string, err error) {
	return e.giveFloatingIP(ctx, nil, vmID, vmName, projectID, azID, sf)
}

// assignFloatingIP prints its progress: the waits add up to minutes.
func (e *Engine) assignFloatingIP(ctx context.Context, vmID, vmName, projectID, azID string, sf *state.File) (ip, fipID string, err error) {
	return e.giveFloatingIP(ctx, e.Printer.StepDone, vmID, vmName, projectID, azID, sf)
}

// giveFloatingIP is AssignFloatingIP with its progress.
func (e *Engine) giveFloatingIP(ctx context.Context, progress func(string), vmID, vmName, projectID, azID string, sf *state.File) (string, string, error) {
	f, err := floatingip.Settle(ctx, e.Cloud, sf, projectID, vmName, vmID, floatingip.FreeOrOf(vmID))
	if err != nil {
		return "", "", askAddress(err)
	}
	if f != nil {
		if f.BoundTo(vmID) {
			return f.IPAddress, f.ID, nil
		}
		return e.attachToPrimary(ctx, f, vmID, projectID)
	}
	ip, id, err := assignFloatingIP(ctx, e.Cloud, e.timings(), progress, vmID, projectID, azID)
	if lost, ok := floatingip.AsLost(err); ok {
		floatingip.Note(sf, state.LostFloatingIP{Name: lost.Name, ProjectID: projectID, VMName: vmName, VMID: vmID,
			RequestedAt: clock.Now(), CreatedBy: e.Maker()})
		return "", "", LostAddressError(lost, vmID, "")
	}
	return ip, id, err
}

// fipProgressEvery is how many polls pass between progress lines.
const fipProgressEvery = 6

// assignFloatingIP creates a floating IP and attaches it to the VM's primary
// interface. It returns the address and the FIP ID; the ID is set as soon as
// the FIP exists, even when the wait for it fails, so the caller can record
// it for destroy. progress (may be nil) receives human-readable status.
func assignFloatingIP(ctx context.Context, cloud provision.CloudAPI, t Timings, progress func(string), vmID, projectID, azID string) (string, string, error) {
	start := clock.Now()
	report := func(format string, args ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, args...))
		}
	}
	elapsed := func() time.Duration { return clock.Since(start).Round(time.Second) }

	// Poll until the primary interface reaches "created" state.
	// Cloud.ru returns 422 "interface_can_not_be_changed_from_current_state"
	// if the interface is still being provisioned.
	report("%s", i18n.T("Waiting for the VM network interface...", "Ожидание сетевого интерфейса ВМ..."))
	var ifaceID string
	for attempt := 0; attempt < ifaceMaxAttempts; attempt++ {
		if attempt > 0 && attempt%fipProgressEvery == 0 {
			report(i18n.T("Still waiting for the network interface (%s)", "Всё ещё ждём сетевой интерфейс (%s)"), elapsed())
		}
		vmDetail, err := cloud.GetVMDetail(ctx, vmID)
		if err != nil {
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-time.After(t.IfacePollInterval):
				continue
			}
		}

		for _, iface := range vmDetail.Interfaces {
			if iface.Primary && iface.State == "created" {
				ifaceID = iface.ID
				break
			}
		}
		if ifaceID != "" {
			break
		}

		// Also accept primary interface without state check as fallback.
		if attempt >= ifaceFallbackAttempt {
			for _, iface := range vmDetail.Interfaces {
				if iface.Primary {
					ifaceID = iface.ID
					break
				}
			}
			if ifaceID != "" {
				break
			}
		}

		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(t.IfacePollInterval):
		}
	}
	if ifaceID == "" {
		return "", "", fmt.Errorf(i18n.T("no primary interface found on VM %s after %s (or interface not ready)",
			"у ВМ %s за %s не найден основной интерфейс (или он не готов)"), vmID, elapsed())
	}

	// Create floating IP with retry — only after a refusal, which made no
	// address. A create with no answer is sent once: the address is looked
	// for by its name, and one not found is a floatingip.LostError, not a
	// second create (D107; «Риск», conclusion 180, У1).
	report("%s", i18n.T("Creating floating IP...", "Создание floating IP..."))
	var fip *provision.FloatingIP
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		fip, err = floatingip.Reserve(ctx, cloud, provision.CreateFloatingIPRequest{
			Name:               fipName(vmID),
			ProjectID:          projectID,
			InterfaceID:        ifaceID,
			AvailabilityZoneID: azID,
		}, floatingip.FreeOrOf(vmID, ifaceID), t.FIPRetryInterval)
		if err == nil || !floatingip.Refused(err) {
			break
		}
		report(i18n.T("Creating floating IP failed (%v), retrying", "Не удалось создать floating IP (%v), повтор"), err)
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(t.FIPRetryInterval):
		}
	}
	if err != nil {
		if _, lost := floatingip.AsLost(err); lost {
			return "", "", err
		}
		return "", "", fmt.Errorf(i18n.T("create floating IP: %w", "создание floating IP: %w"), err)
	}

	// Poll until floating IP is active.
	fipID := fip.ID
	for attempt := 0; attempt < 12; attempt++ {
		if fip.State == "in_use" || fip.State == "active" {
			return fip.IPAddress, fipID, nil
		}
		if attempt == 0 {
			report(i18n.T("Waiting for floating IP %s to attach...", "Ожидание привязки floating IP %s..."), fip.IPAddress)
		} else if attempt%fipProgressEvery == 0 {
			report(i18n.T("Still waiting for floating IP %s (state %q, %s)", "Всё ещё ждём floating IP %s (состояние %q, %s)"),
				fip.IPAddress, fip.State, elapsed())
		}
		select {
		case <-ctx.Done():
			return fip.IPAddress, fipID, ctx.Err()
		case <-time.After(t.FIPPollInterval):
		}
		updated, err := cloud.GetFloatingIP(ctx, fipID)
		if err != nil {
			continue
		}
		fip = updated
	}

	// Return whatever IP we got even if state isn't confirmed.
	report(i18n.T("Floating IP %s not confirmed after %s; continuing", "Привязка floating IP %s не подтвердилась за %s; продолжаем"),
		fip.IPAddress, elapsed())
	return fip.IPAddress, fipID, nil
}

// createBootFIP makes a free floating IP to attach to the VM at creation.
// When that fails the VM still gets an IP after boot (nil, nil) — except on
// a quota rejection: the IP would fail after boot too, and the VM would be
// billed with no way in, so that is an error before anything is created.
//
// The address of an earlier call whose create got no answer is looked for
// first, by its exact name in sf (floatingip.Settle): found, it is taken;
// not found, the call asks before it reserves another (an AskError). A
// create of this call with no answer is not sent again: the call stops
// before the VM, with the mark of the address in sf (a LostError the
// caller writes), and no VM is billed beside an address nothing finds
// (D107; «Риск», conclusion 180, (а)). sf is changed in memory only.
func (e *Engine) createBootFIP(ctx context.Context, projectID, azID, vmName string, sf *state.File) (*provision.FloatingIP, error) {
	if f, err := floatingip.Settle(ctx, e.Cloud, sf, projectID, vmName, "", floatingip.Free); err != nil {
		return nil, askAddress(err)
	} else if f != nil {
		e.Printer.StepDone(i18n.Tf("Taking floating IP %s, which an earlier call reserved with no answer",
			"Беру floating IP %s, который прошлый вызов зарезервировал без ответа облака", f.IPAddress))
		return e.settledBootFIP(ctx, f, projectID), nil
	}
	// A free boot FIP of this VM name left by an earlier run (its release
	// failed after a 409, break r3a R3A-2) is taken instead of a second
	// billed address. A boot FIP name is unique to its VM name.
	if f := e.leftBootFIP(ctx, projectID, vmName); f != nil {
		e.Printer.StepDone(i18n.Tf("Reusing floating IP %s left by an earlier run",
			"Использую floating IP %s, оставшийся от прошлого запуска", f.IPAddress))
		return f, nil
	}
	name := provision.BootFIPName(vmName, clock.Now())
	fip, err := floatingip.Reserve(ctx, e.Cloud, provision.CreateFloatingIPRequest{
		Name:               name,
		ProjectID:          projectID,
		AvailabilityZoneID: azID,
	}, floatingip.Free, e.timings().FIPLookupInterval)
	if lost, ok := floatingip.AsLost(err); ok {
		floatingip.Note(sf, state.LostFloatingIP{Name: lost.Name, ProjectID: projectID, VMName: vmName,
			RequestedAt: clock.Now(), CreatedBy: e.Maker()})
		return nil, LostAddressError(lost, "", "")
	}
	if err != nil {
		if provision.IsQuotaError(err) {
			return nil, clerrors.Wrap(clerrors.CodeVMProvisionFailed,
				i18n.T("no public IP can be reserved: the floating IP quota is exhausted",
					"не удалось зарезервировать внешний IP: квота floating IP исчерпана"),
				i18n.T("Nothing was created. Raise the quota in the Cloud.ru console, or ask the person which floating IP may be released (list_floating_ips / release_floating_ip), then retry.",
					"Ничего не создано. Увеличьте квоту в консоли Cloud.ru или спросите человека, какой floating IP можно освободить ('evo vpc fips', 'evo vpc fip-release'), и повторите."),
				err)
		}
		// Refused: no address came of it (floatingip.Refused).
		e.Printer.Warning(i18n.Tf("could not reserve a public IP before creating the VM (%v); it will be attached after boot, which can make the first boot slow",
			"не удалось зарезервировать внешний IP до создания ВМ (%v); он будет привязан после загрузки, из-за этого первая загрузка может затянуться", err))
		return nil, nil
	}
	return e.settledBootFIP(ctx, fip, projectID), nil
}

// askAddress is the refusal of a call that reached the reserve of an
// address without the question its preview asks (DeployAddressAsked):
// CONFIRMATION_REQUIRED, exit 3. A tool answers the question itself
// (floatingip.AsAsk finds it in the chain); nothing was sent.
func askAddress(err error) error {
	ask, ok := floatingip.AsAsk(err)
	if !ok {
		return err
	}
	return clerrors.Wrap(clerrors.CodeConfirmationRequired,
		i18n.Tf("floating IP %s, which an earlier run asked for, got no answer from the cloud, and the list does not show it: the request may have been accepted, and the address billed. No second address was requested",
			"floating IP %s, который запросил прошлый запуск, не получил ответа облака, и список его не показывает: запрос мог быть принят, а адрес — тарифицироваться. Второй адрес не запрашивался", ask.Mark.Name),
		i18n.Tf("Check list_floating_ips for the name %s; whether to reserve another address is the person's to answer.",
			"Проверьте 'evo vpc fips' по имени %s, затем запустите команду в терминале и ответьте на вопрос: резервировать ли другой адрес, решает человек.", ask.Mark.Name),
		err)
}

// LostAddressError is the answer of a call stopped by the create of a
// floating IP that got no answer (floatingip.LostError): what may be
// billed, that nothing more was sent, and how to find it. vmID and vmState
// name the VM the address was for once it exists ("" before it). Its
// reason is that of the create — API_ERROR, API_UNREACHABLE, INTERRUPTED:
// the outcome is not known (intent.OutcomeUnknown).
func LostAddressError(lost *floatingip.LostError, vmID, vmState string) error {
	code := clerrors.Reason(lost.Err)
	if code == clerrors.ReasonGeneral || code == clerrors.ReasonInterrupted {
		code = clerrors.CodeAPIError
	}
	if vmID == "" {
		return clerrors.Wrap(code,
			i18n.Tf("reserving floating IP %s got no answer from the cloud (%v): the request may have been accepted, and the address billed. No VM was created, and no second address was requested",
				"резервирование floating IP %s не получило ответа облака (%v): запрос мог быть принят, а адрес — тарифицироваться. ВМ не создана, второй адрес не запрашивался", lost.Name, lost.Err),
			i18n.Tf("Check list_floating_ips for the name %s, then repeat the same call: it takes that address if the list shows it bound to nothing; if the list does not show it, the call asks first — whether to reserve another address is the person's to answer.",
				"Проверьте 'evo vpc fips' по имени %s и повторите ту же команду: она возьмёт этот адрес, если список показывает его ни к чему не привязанным; если списка с ним нет, команда сначала спросит — резервировать ли другой адрес, решает человек.", lost.Name),
			lost)
	}
	return clerrors.Wrap(code,
		i18n.Tf("VM %s is created (state %s) and billed, but reserving its floating IP %s got no answer from the cloud (%v): the request may have been accepted, and the address billed. No second address was requested",
			"ВМ %s создана (состояние %s) и тарифицируется, но резервирование её floating IP %s не получило ответа облака (%v): запрос мог быть принят, а адрес — тарифицироваться. Второй адрес не запрашивался", vmID, statusText(vmState), lost.Name, lost.Err),
		i18n.Tf("Check list_floating_ips for the name %s and the VM with vm_get %s, then repeat the same call: it takes that address if the list shows it free or on this VM; if the list does not show it, the call asks first — whether to reserve another address is the person's to answer.",
			"Проверьте 'evo vpc fips' по имени %s и ВМ — 'evo vm get %s', затем повторите ту же команду: она возьмёт этот адрес, если список показывает его свободным или на этой ВМ; если списка с ним нет, команда сначала спросит — резервировать ли другой адрес, решает человек.", lost.Name, vmID),
		lost)
}

// settlePolls is how many polls a floating IP still being created is
// waited for: Timings.FIPSettleTimeout over FIPPollInterval (12 in
// production). A count, not a deadline on the wall clock: a slow API or a
// coarse timer (Windows, CI #283) makes the same polls, each no shorter.
func (t Timings) settlePolls() int {
	return max(1, int(t.FIPSettleTimeout/t.FIPPollInterval))
}

// settledBootFIP waits, up to Timings.settlePolls, for a boot FIP
// still being created to be free: handed to the VM create in "creating",
// it made the create fail with 422 «… from current state» (cloud-e2e
// #121). One not ready in time stays the VM's: created without it, the VM
// gets it after boot (waitBootFIP), so no second address is reserved. One
// that failed to create is released, and the VM gets an IP after boot as
// when none could be reserved (nil).
func (e *Engine) settledBootFIP(ctx context.Context, fip *provision.FloatingIP, projectID string) *provision.FloatingIP {
	t := e.timings()
	last := ""
	for poll := 0; poll < t.settlePolls() && !fip.Available() && !strings.HasPrefix(fip.State, "error"); poll++ {
		if fip.State != last {
			last = fip.State
			e.Printer.Step(0, 0, i18n.Tf("Floating IP %s: %s, waiting until it is ready…", "Floating IP %s: %s, жду, пока он будет готов…", fip.IPAddress, fip.State))
		}
		select {
		case <-ctx.Done():
			return fip
		case <-time.After(t.FIPPollInterval):
		}
		if f, err := e.Cloud.GetFloatingIP(ctx, fip.ID); err == nil && f != nil {
			fip = f
		}
	}
	switch {
	case fip.Available():
	case strings.HasPrefix(fip.State, "error"):
		e.Printer.Warning(i18n.Tf("floating IP %s could not be created (state %s); the VM gets a public IP after boot",
			"floating IP %s не создался (состояние %s); внешний IP ВМ получит после загрузки", fip.IPAddress, fip.State))
		_ = e.releaseBootFIP(fip, projectID) // the warning is printed
		return nil
	default:
		e.Printer.Warning(i18n.Tf("floating IP %s is not ready yet (state %s); the VM is created without it and gets it after boot, which can make the first boot slow",
			"floating IP %s ещё не готов (состояние %s); ВМ создаётся без него и получит его после загрузки, из-за этого первая загрузка может затянуться", fip.IPAddress, fip.State))
	}
	return fip
}

// BootFIPAttachID is the boot FIP the VM create attaches: one that is free
// (settledBootFIP); one still being created goes on after boot.
func BootFIPAttachID(fip *provision.FloatingIP) string {
	if fip == nil || !fip.Available() {
		return ""
	}
	return fip.ID
}

// leftBootFIP finds a free boot FIP of vmName in the project, or nil. A
// failed lookup is nil: a new address is made then, as before.
func (e *Engine) leftBootFIP(ctx context.Context, projectID, vmName string) *provision.FloatingIP {
	fips, err := e.Cloud.ListFloatingIPs(ctx, projectID)
	if err != nil {
		return nil
	}
	// Two of the VM name are not told apart: none is taken (К1 of
	// conclusion 180) — the address of a call cut with no answer is found
	// by its exact name (floatingip.Settle), not by its prefix.
	var left *provision.FloatingIP
	for _, f := range fips {
		if f != nil && f.ID != "" && provision.IsBootFIPOf(f.Name, vmName) && f.Available() {
			if left != nil {
				return nil
			}
			left = f
		}
	}
	return left
}

// findBootFIP looks the boot FIP up by its unique name after a failed
// create. The lookup outlives a Ctrl-C during the POST (ctx cancelled), and
// then tries three times: the server may create the address a moment after
// the client hung up (review Grok on #85).
func (e *Engine) findBootFIP(ctx context.Context, projectID, name string) *provision.FloatingIP {
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	attempts := 1
	if ctx.Err() != nil {
		attempts = 3
		e.Printer.StepDone(i18n.Tf("Looking for floating IP %s made before the interruption...",
			"Ищу floating IP %s, созданный до прерывания...", name))
	}
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-lctx.Done():
				return nil
			case <-time.After(e.timings().FIPLookupInterval):
			}
		}
		fips, err := e.Cloud.ListFloatingIPs(lctx, projectID)
		if err != nil {
			continue
		}
		for _, f := range fips {
			if f.Name == name && f.ID != "" {
				return f
			}
		}
	}
	return nil
}

// releaseBootFIP deletes a boot FIP whose VM was not created or was deleted.
func (e *Engine) releaseBootFIP(fip *provision.FloatingIP, projectID string) error {
	if fip == nil {
		return nil
	}
	// One still being created cannot be deleted yet (422 «… from current
	// state»): only that refusal is retried, up to Timings.settlePolls more
	// tries, rather than left billed (cloud-e2e #121); any other error is
	// warned of at once. The whole release is bounded too — those polls
	// and 30 s for the last delete — so a slow API cannot hold the rollback
	// for minutes. An address still refused then is named in the warning
	// (and in the MCP answer) to delete by hand.
	t := e.timings()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t.settlePolls())*t.FIPPollInterval+30*time.Second)
	defer cancel()
	var refused refusals
	for try := 0; ; try++ {
		err := e.Cloud.DeleteFloatingIP(ctx, fip.ID, projectID)
		if err == nil {
			return nil
		}
		// One refused token after a sleep: the next try takes a new one.
		if refusedToken(err) && !refused.keyRefused(err) {
			continue
		}
		if provision.NotInCurrentState(err) && try < t.settlePolls() {
			if try == 0 {
				e.Printer.StepDone(i18n.Tf("Floating IP %s is still being created; waiting to release it...",
					"floating IP %s ещё создаётся; жду, чтобы освободить его...", fip.IPAddress))
			}
			select {
			case <-ctx.Done():
			case <-time.After(t.FIPPollInterval):
				continue
			}
		}
		e.Printer.Warning(i18n.Tf("floating IP %s (%s) was not released: %v; delete it in the Cloud.ru console",
			"floating IP %s (%s) не освобождён: %v; удалите его в консоли Cloud.ru", fip.IPAddress, fip.ID, err))
		return err
	}
}

// waitBootFIP waits for the FIP attached at creation to be in use. If the
// platform did not attach it, it is attached to the primary interface now.
func (e *Engine) waitBootFIP(ctx context.Context, fip *provision.FloatingIP, vmID, projectID string) (string, string, error) {
	t := e.timings()
	last := ""
	for attempt := 0; attempt < 12; attempt++ {
		if f, err := e.Cloud.GetFloatingIP(ctx, fip.ID); err == nil && f != nil {
			fip = f
			if f.State == "in_use" || f.State == "active" {
				return f.IPAddress, f.ID, nil
			}
		}
		if fip.State != last {
			last = fip.State
			e.Printer.Step(0, 0, i18n.Tf("Floating IP %s: %s, waiting until it is attached to the VM…", "Floating IP %s: %s, жду, пока он привяжется к ВМ…", fip.IPAddress, fip.State))
		}
		select {
		case <-ctx.Done():
			return fip.IPAddress, fip.ID, ctx.Err()
		case <-time.After(t.FIPPollInterval):
		}
	}
	// Not attached at creation: bind it to the primary interface.
	return e.attachToPrimary(ctx, fip, vmID, projectID)
}

// attachToPrimary binds the floating IP fip to the VM's primary interface.
func (e *Engine) attachToPrimary(ctx context.Context, fip *provision.FloatingIP, vmID, projectID string) (string, string, error) {
	e.Printer.StepDone(i18n.Tf("Attaching floating IP %s...", "Привязка floating IP %s...", fip.IPAddress))
	detail, err := e.Cloud.GetVMDetail(ctx, vmID)
	if err != nil {
		return fip.IPAddress, fip.ID, fmt.Errorf(i18n.T("attach floating IP %s: %w", "привязка floating IP %s: %w"), fip.IPAddress, err)
	}
	for _, iface := range detail.Interfaces {
		if iface.Primary {
			// The API rejects an empty name (minLength 1): keep the FIP's own.
			name := fip.Name
			if name == "" {
				name = provision.FitName("fip-"+vmID, "")
			}
			if _, err := e.Cloud.AttachFloatingIP(ctx, fip.ID, projectID, iface.ID, name); err != nil {
				return fip.IPAddress, fip.ID, fmt.Errorf(i18n.T("attach floating IP %s: %w", "привязка floating IP %s: %w"), fip.IPAddress, err)
			}
			return fip.IPAddress, fip.ID, nil
		}
	}
	return fip.IPAddress, fip.ID, fmt.Errorf(i18n.T("attach floating IP %s: no primary interface on VM %s",
		"привязка floating IP %s: у ВМ %s нет основного интерфейса"), fip.IPAddress, vmID)
}

// vmNameFor turns the project (directory) name into the VM name, which is
// also its hostname: RFC 1123 allows lowercase letters, digits and inner
// hyphens, up to 63 characters. "My App (копия)" → "evo-my-app".
func vmNameFor(project string) string {
	const prefix = "evo-"
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(project) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	name := b.String()
	if name == "" {
		name = "app"
	}
	if max := 63 - len(prefix); len(name) > max {
		name = strings.TrimRight(name[:max], "-")
	}
	return prefix + name
}

// fipName is the name deploy gives the VM's floating IP; destroy and the
// recovery of a lost create response look it up by it.
func fipName(vmID string) string { return "fip-" + vmID }

// recoverFloatingIP gives a recorded VM without a public address one: the
// recorded floating IP's address, else a new floating IP. Failures are
// warnings; the caller reports the missing address.
//
// A create of the address with no answer, or the question about one an
// earlier run asked for (floatingip), is the error: the run stops there.
func (e *Engine) recoverFloatingIP(ctx context.Context, rec *state.DeploymentRecord, sf *state.File) error {
	if rec.FloatingIPID != "" {
		// Never a second one next to the recorded FIP.
		f, err := e.Cloud.GetFloatingIP(ctx, rec.FloatingIPID)
		if err != nil {
			e.Printer.Warning(i18n.Tf("floating IP %s: %v", "не удалось прочитать floating IP %s: %v", rec.FloatingIPID, err))
			return nil
		}
		rec.VMIP = f.IPAddress
		return nil
	}
	ip, fipID, err := e.assignFloatingIP(ctx, rec.VMID, rec.VMName, e.projectOf(rec), rec.Region, sf)
	if fipID != "" {
		rec.FloatingIPID = fipID
	}
	rec.VMIP = ip
	if _, lost := floatingip.AsLost(err); lost {
		return err
	}
	if _, ask := floatingip.AsAsk(err); ask {
		return err
	}
	if err != nil {
		e.Printer.Warning(i18n.Tf("floating IP: %v", "floating IP не назначен: %v", err))
	}
	return nil
}

// awaitCreated waits for the VM of a record left creating — a deploy whose
// bound came while the cloud created it (D69) — to be active, as
// provisionVM does, before the reuse path gives it a floating IP and SSH:
// the cloud refuses to attach an address to a VM still being created
// («1.7 — релиз» on #769). The bound ends the wait as InProgress vm; a VM
// that failed is recorded as error, as provisionVM records it.
func (e *Engine) awaitCreated(ctx context.Context, rec *state.DeploymentRecord, stateFile *state.File, statePath string) error {
	t := e.timings()
	timeout := time.After(t.VMPollTimeout)
	tick, stop := newPollTicker(t.VMPollInterval)
	defer stop()
	vmState := "creating"
	var refused refusals
	for {
		select {
		case <-ctx.Done():
			if stderrors.Is(ctx.Err(), context.DeadlineExceeded) && e.past() {
				return &InProgress{Phase: "vm", Detail: vmState}
			}
			return ctx.Err()
		case <-timeout:
			return clerrors.New(clerrors.CodeVMProvisionFailed,
				i18n.Tf("VM %s did not become active within %s; it is still being created and is billed", "ВМ %s не стала активной за %s; она продолжает создаваться и оплачивается", rec.VMID, t.VMPollTimeout),
				i18n.Tf("Check the VM with 'evo vm get %s' or in the Cloud.ru console; to wait longer, set CLOUDRU_TIMEOUT (seconds).",
					"Проверьте ВМ: 'evo vm get %s' или консоль Cloud.ru; чтобы ждать дольше, задайте CLOUDRU_TIMEOUT (в секундах).", rec.VMID))
		case <-tick:
			vm, err := e.Cloud.GetVM(ctx, rec.VMID)
			// One refused token is the sleep's; two, the key's (refusals).
			if refused.keyRefused(err) {
				return clerrors.Wrap(clerrors.Reason(err),
					i18n.Tf("checking VM %s failed: the key was refused (%v)", "проверить ВМ %s не удалось: ключ отклонён (%v)", rec.VMID, err),
					i18n.Tf("VM %s is billed and recorded in the state. Check the key ('evo whoami'), then run 'evo deploy' again to finish.",
						"ВМ %s тарифицируется и записана в state. Проверьте ключ ('evo whoami') и повторите 'evo deploy', чтобы завершить.", rec.VMID),
					err)
			}
			if err != nil {
				if ctx.Err() != nil {
					continue // the case above answers it
				}
				e.Printer.Warning(i18n.Tf("poll error: %v", "ошибка опроса: %v", err))
				continue
			}
			vmState = vm.Status
			switch {
			case vm.Status == "active" || vm.Status == "running":
				rec.Status = "provisioned"
				rec.UpdatedAt = time.Now()
				stateFile.UpsertDeployment(*rec)
				_ = state.Write(statePath, stateFile)
				e.Printer.StepDone(i18n.Tf("VM %s active", "ВМ %s активна", vmText(rec)))
				return nil
			case provision.VMFailed(vm.Status):
				rec.Status = "error"
				rec.UpdatedAt = time.Now()
				stateFile.UpsertDeployment(*rec)
				// As in provisionVM (FB171-4).
				return e.writeDecided(statePath, stateFile, clerrors.New(clerrors.CodeVMProvisionFailed,
					i18n.Tf("VM %s entered state %s", "ВМ %s перешла в состояние %s", rec.VMID, vm.Status),
					i18n.T("Check Cloud.ru console for details.", "Подробности — в консоли Cloud.ru.")))
			}
		}
	}
}

// needsFloatingIP: the record has neither an address nor a recorded floating
// IP, so a re-run creates one (recoverFloatingIP).
func needsFloatingIP(rec *state.DeploymentRecord) bool {
	return rec.VMIP == "" && rec.FloatingIPID == "" && rec.VMID != ""
}

// writeDecided writes sf, whose record a decision of deploy changed, and
// returns err, the answer of that decision (FB171-4: the deferred atBound
// keeps it). A write that failed is said in the answer (FB172-6): the
// state still says the step before, and status and the next deploy go by
// it — as deploy_apply says «the local state was not updated».
func (e *Engine) writeDecided(statePath string, sf *state.File, err error) error {
	e.decided = true
	werr := state.Write(statePath, sf)
	if werr == nil {
		return err
	}
	note := i18n.Tf("The local state was not updated (%s): status and the next deploy still go by the step before this one.",
		"Локальный state не обновлён (%s): status и следующий deploy пока исходят из прежнего шага.", errLine(werr))
	var going *InProgress
	if stderrors.As(err, &going) {
		going.Note = strings.TrimSpace(going.Note + " " + note)
		return err
	}
	if ce, ok := err.(*clerrors.CLIError); ok {
		c := *ce
		c.Hint = strings.TrimSpace(c.Hint + " " + note)
		return &c
	}
	return fmt.Errorf("%w. %s", err, note)
}

// keepUnfinished leaves rec, whose wait for SSH has not ended, among the
// unfinished deployments (D76): provisioned, unless it is unfinished
// already or running — a running deployment stays running.
func keepUnfinished(rec *state.DeploymentRecord) {
	if !unfinishedStatuses[rec.Status] && rec.Status != "running" {
		rec.Status = "provisioned"
	}
}

// waitForSSH retries SSH connection until the VM is reachable, for the
// attempts of w left; w counts them (D80).
func (e *Engine) waitForSSH(ctx context.Context, ip, user, knownHostKey string, w *state.SSHWait) error {
	e.phase = "ssh"
	var lastErr error
	t := e.timings()
	// lastReport times the reports; the duration they print and the note
	// are of the whole wait, from w.Since: a wait a bound ended goes on.
	lastReport := time.Now()
	noted := false
	for w.Attempts < w.Limit {
		err := e.Deployer.Connect(ctx, ip, 22, user, e.SSHKeyPEM, knownHostKey)
		if err == nil {
			return nil
		}
		lastErr = err
		w.Attempts++
		// A fresh VM's sshd can take minutes: say so once a minute, as the
		// wait for an active VM does, so it does not look hung (QA r4 Q13).
		if time.Since(lastReport) >= time.Minute {
			e.Printer.StepDone(i18n.Tf("waiting for SSH on %s (%s elapsed: %s)", "ожидание SSH на %s (прошло %s: %s)",
				ip, clock.Since(w.Since).Round(time.Second), errLine(err)))
			lastReport = time.Now()
		}
		// Past the usual few minutes, say once why deploy still waits and
		// what that may mean (cloud-e2e 16-18.10.2026, register OPS-1).
		if !noted && clock.Since(w.Since) >= t.SSHSlowNote {
			e.Printer.Warning(sshSlowText(ip, w.Limit))
			noted = true
		}

		// Don't retry on auth failures or host key mismatches.
		if cliErr, ok := err.(*clerrors.CLIError); ok {
			if cliErr.Code == clerrors.CodeSSHAuthFailed || cliErr.Code == clerrors.CodeSSHHostKeyChanged {
				return err
			}
		}
		// The bound of the deploy (D69): the next deploy waits on.
		if e.past() {
			return &InProgress{Phase: "ssh", Detail: errLine(err)}
		}

		wait := t.SSHRetryInterval
		if !e.until.IsZero() {
			wait = min(wait, time.Until(e.until))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	// evo does not reboot or recreate the VM by itself, and what next is
	// the person's (D80; «Риск · 1.8», R-1, R-23): the VM may be fine and
	// the network evo runs in let no SSH out, and a repeat of deploy waits
	// anew. The hint says so and how to tell the two apart.
	hint := i18n.Tf("What next — the person decides: an agent should ask them and not repeat deploy on its own. "+
		"The VM stays running and is billed until the person deletes it. "+
		"First check that SSH gets out of the network evo runs in: nc -vz %s 22 (outbound port 22 is often closed by a corporate firewall, a VPN or an agent's sandbox; then the VM is fine). "+
		"If it does, the VM may have booted without network (on Cloud.ru this happens: no DNS inside the VM, the image's apt mirror unreachable): "+
		"reboot the VM in the Cloud.ru console (Virtual machines, the VM's menu, Reboot; evo has no reboot command) or look at its boot log in the serial console, then run 'evo deploy' again — it reuses this VM and waits anew. "+
		"If it repeats in one zone, check the subnet's DNS in the Cloud.ru console.",
		"Что дальше — решает человек: агенту — спросить его и не повторять deploy самому. "+
			"ВМ остаётся запущенной и оплачивается, пока её не удалит человек. "+
			"Сначала проверьте, что SSH выходит из сети, где запущен evo: nc -vz %s 22 (исходящий порт 22 часто закрыт корпоративным межсетевым экраном, VPN или песочницей агента; тогда ВМ исправна). "+
			"Если выходит, ВМ могла загрузиться без сети (на Cloud.ru так бывает: нет DNS внутри ВМ, недоступно apt-зеркало образа): "+
			"перезагрузите ВМ в консоли Cloud.ru (Виртуальные машины, меню ВМ, «Перезагрузить»; своей команды перезагрузки в evo нет) или посмотрите журнал загрузки в серийной консоли, затем снова выполните 'evo deploy' — он возьмёт эту же ВМ и будет ждать заново. "+
			"Если повторяется в одной зоне, проверьте DNS подсети в консоли Cloud.ru.", ip, ip)
	if d := sshDiagnosis(lastErr, ip); d != "" {
		hint = d + "\n" + hint
	}
	return clerrors.Wrap(clerrors.CodeSSHTimeout,
		i18n.Tf("SSH connection failed after %d attempts", "не удалось подключиться по SSH за %d попыток", w.Limit),
		hint, lastErr)
}

// sshSlowText is why deploy still waits for SSH on ip past the usual few
// minutes, and what that may mean. Two ends: the VM's first boot, and the
// network evo runs in — outbound port 22 closed there looks the same
// (agent run C2, Ц-8: the agent's sandbox let no SSH out).
func sshSlowText(ip string, attempts int) string {
	return i18n.Tf("SSH on %s does not answer yet. A new VM usually answers within 2-5 minutes; on Cloud.ru its first boot "+
		"sometimes does not bring the network up, and then SSH never answers. The network evo runs in may also let no SSH out "+
		"(outbound port 22 closed by a corporate firewall, a VPN or an agent's sandbox): check with nc -vz %s 22. evo keeps trying (%d attempts in all).",
		"SSH на %s пока не отвечает. Новая ВМ обычно отвечает за 2–5 минут; на Cloud.ru первая загрузка иногда "+
			"не поднимает сеть, и тогда SSH не ответит вовсе. Не выпускать SSH может и сеть, где запущен evo "+
			"(исходящий порт 22 закрыт корпоративным межсетевым экраном, VPN или песочницей агента): проверка — nc -vz %s 22. evo продолжает попытки (всего %d).", ip, ip, attempts)
}

// sshDiagnosis names what the last failed connection says about port 22
// of the VM, or "" when it says nothing definite.
func sshDiagnosis(err error, ip string) string {
	if err == nil {
		return ""
	}
	var netErr net.Error
	if stderrors.As(err, &netErr) && netErr.Timeout() {
		return i18n.Tf("Diagnosis: port 22 of %s does not answer at all (the connection times out): the VM's network did not come up on boot, "+
			"or the packets are dropped on the way, by a firewall or VPN on your side (check: nc -vz %s 22, from another network too) or by the VM's security group.",
			"Диагноз: порт 22 адреса %s не отвечает совсем (подключение — по таймауту): сеть ВМ не поднялась при загрузке "+
				"или пакеты отбрасываются по пути — межсетевым экраном или VPN на вашей стороне (проверка: nc -vz %s 22, в том числе из другой сети) или группой безопасности ВМ.",
			ip, ip)
	}
	// The cause is in the wrapped errors: CLIError.Error() is only the
	// code and message.
	var msgs []string
	for e := err; e != nil; e = stderrors.Unwrap(e) {
		msgs = append(msgs, strings.ToLower(e.Error()))
	}
	all := strings.Join(msgs, "\n")
	switch {
	case strings.Contains(all, "connection refused") || strings.Contains(all, "actively refused"): // Windows: "…actively refused it"
		return i18n.Tf("Diagnosis: %s answers, but nothing listens on port 22 (connection refused): the VM is up and sshd has not started; "+
			"its first boot (cloud-init) is stuck or still running.",
			"Диагноз: %s отвечает, но порт 22 никто не слушает (connection refused): ВМ работает, а sshd не запущен — "+
				"первая загрузка (cloud-init) зависла или ещё идёт.", ip)
	case strings.Contains(all, "eof") || strings.Contains(all, "connection reset") || strings.Contains(all, "handshake"):
		return i18n.Tf("Diagnosis: sshd on %s accepts connections and drops them before login (EOF, connection reset): it is still starting, or the first boot is stuck.",
			"Диагноз: sshd на %s принимает подключение и обрывает его до входа (EOF, connection reset): он ещё запускается или первая загрузка зависла.", ip)
	}
	return ""
}

// installNotes are the lines the install script says about itself ("evo:
// …"), at most five, in order.
func installNotes(out string) []string {
	var notes []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "evo: ") && len(notes) < 5 {
			notes = append(notes, l)
		}
	}
	return notes
}

// prepareDocker makes Docker with the compose plugin usable by user on the
// VM. Cloud.ru renders cloud-init only from the image's own template, so the
// install runs over SSH (internal/bootstrap); on a VM that already has
// Docker it only checks the docker group and /opt/app. Retries until
// Timings.DockerWaitTimeout: first-boot cloud-init may still hold the apt lock.
func (e *Engine) prepareDocker(ctx context.Context, ip, user, hostKey string) error {
	e.phase = "docker"
	t := e.timings()
	// deadline bounds the wait; start times the duration the retry line prints.
	deadline := time.Now().Add(t.DockerWaitTimeout)
	start := clock.Now()
	installCmd := bootstrap.InstallCommand(user)

	e.Printer.StepDone(i18n.T("Preparing Docker on the VM (a fresh VM takes a few minutes)...",
		"Подготовка Docker на ВМ (на новой ВМ — несколько минут)..."))
	var lastErr error
	// lastOut is the last attempt's output; failOut that of the last one
	// that failed on its own, before the deadline. The last attempt is
	// usually cut by the deadline with a line or two printed, and an
	// earlier one holds the apt or DNS error (cloud-e2e 16.10.2026).
	var lastOut, failOut string
	// permanent: the install refused in a way another attempt repeats
	// (an unsupported OS, no keyring for the official mirror): no retries
	// until the deadline (break r4, R4-15, R4-17).
	permanent := false
	for attempt := 1; ; attempt++ {
		// The install is a stage on the VM (vmstage, D69 variant C): the
		// end of this session does not cut apt midway, and a later deploy
		// reads it; one done for this install is not run again — but by
		// the next attempt of this call, after Docker was not ready.
		st, err := e.runStage(ctx, "docker", stageInput(installCmd), installCmd, deadline, attempt > 1)
		var going *InProgress
		if stderrors.As(err, &going) {
			return err
		}
		if err == nil && st.State == "failed" {
			err = fmt.Errorf("exit status %d", st.Exit)
		}
		out := bytes.NewBufferString(st.LogTail)
		if err == nil {
			// Group membership is fixed at login: reconnect so the docker
			// group the script may have just added applies.
			err = e.reconnect(ctx, ip, user, hostKey)
			if err == nil {
				_, err = e.Deployer.Exec(ctx, dockerReadyCmd)
			}
			if err == nil {
				// What the install did on the way ("evo: switching apt to
				// the official mirror…"): a successful install printed
				// nothing, and a run could not tell the image's mirror from
				// the fallback (cloud-e2e 20.10, register v2 IMG-6).
				for _, l := range installNotes(out.String()) {
					e.Printer.StepDone(l)
				}
				return nil
			}
		}
		// A refused key or a changed host key first: it is the answer
		// even when the bound cut the call at the same time.
		if isFatalSSHError(err) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr, lastOut = err, out.String()
		if bootstrap.Permanent(lastOut) {
			permanent = true
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		if strings.TrimSpace(lastOut) != "" {
			failOut = lastOut
		}
		msg := i18n.Tf("Docker not ready yet (attempt %d, %s elapsed: %s), retrying...",
			"Docker ещё не готов (попытка %d, прошло %s: %s), повтор...",
			attempt, clock.Since(start).Round(time.Second), errLine(err))
		if l := lastLines(lastOut, 1); l != "" {
			msg += i18n.Tf(" Last line of the install: %s", " Последняя строка установки: %s", clipLine(l))
		}
		e.Printer.Warning(msg)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.DockerPollInterval):
		}
		// A failed attempt may have lost the connection.
		if err := e.reconnect(ctx, ip, user, hostKey); err != nil {
			if isFatalSSHError(err) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}

	if permanent {
		hint := installDiagnosis(lastOut)
		if tail := lastLines(lastOut, 15); tail != "" {
			hint = i18n.T("Last output of the Docker install:\n", "Последние строки установки Docker:\n") + tail + "\n" + hint
		}
		// The image, not the network: another attempt answers the same, and
		// the way out is another image, as for the domestic distributions
		// refused before the VM is made (CONFIG_INVALID). SSH worked.
		return clerrors.Wrap(clerrors.CodeConfigInvalid,
			i18n.Tf("Docker cannot be installed on VM %s", "Docker на ВМ %s не установить", ip),
			hint, lastErr)
	}

	hint := i18n.T("Check that the VM resolves names (the subnet's DNS) and reaches its apt mirror (or the official mirror of the distribution) and download.docker.com, "+
		"then re-run 'evo deploy'. \"Temporary failure in name resolution\" in the install output means no DNS inside the VM: a network problem of the zone or subnet, not of the compose project; if it repeats, deploy in another zone — whether to delete this VM first (evo destroy), ask the person.",
		"Проверьте, что ВМ разрешает имена (DNS подсети) и достаёт до своего apt-зеркала (или официального зеркала дистрибутива) и download.docker.com, "+
			"затем повторите 'evo deploy'. \"Temporary failure in name resolution\" в выводе установки означает, что внутри ВМ нет DNS: "+
			"это проблема сети зоны или подсети, а не compose-проекта; если повторится, разверните в другой зоне — удалять ли сначала эту ВМ (evo destroy), спросите человека.")
	// What the output says outright comes first: an apt mirror that does
	// not answer is the cloud's, not the project's (cloud-e2e 16.10.2026:
	// "W: Failed to fetch https://repo.mng.sbercloud…" in ru.AZ-2).
	if d := installDiagnosis(failOut + "\n" + lastOut); d != "" {
		hint = d + "\n" + hint
	}
	// The install's own output, and always how the last attempt ended: with
	// an empty output (a dropped session, sudo refused, a timeout) the
	// reason is all there is to tell "no DNS" from the rest (QA r4 Q14).
	reason := i18n.Tf("Last attempt ended with: %s", "Последняя попытка завершилась так: %s", errLine(lastErr))
	if tail := lastLines(lastOut, 15); tail != "" {
		hint = i18n.T("Last output of the Docker install:\n", "Последние строки установки Docker:\n") + tail + "\n" + reason + "\n" + hint
	} else {
		hint = reason + i18n.T(" (the install printed nothing)\n", " (установка ничего не вывела)\n") + hint
	}
	if failOut != "" && failOut != lastOut {
		hint = i18n.T("Output of the last attempt that failed on its own:\n", "Вывод последней попытки, завершившейся ошибкой:\n") +
			lastLines(failOut, 15) + "\n" + hint
	}
	return clerrors.Wrap(clerrors.CodeSSHTimeout,
		i18n.Tf("Docker is not ready on VM %s after %s", "Docker на ВМ %s не готов за %s", ip, t.DockerWaitTimeout),
		hint, lastErr)
}

// errLine is an error on one short line for progress and hints.
func errLine(err error) string {
	if err == nil {
		return i18n.T("no error reported", "ошибка не сообщается")
	}
	return clipLine(err.Error())
}

// installDiagnosis names the cause the Docker install output shows
// outright, or "".
func installDiagnosis(out string) string {
	switch {
	case strings.Contains(out, bootstrap.UnsupportedOSLine):
		return i18n.T("Diagnosis: the VM's OS is neither Ubuntu nor Debian nor an RPM-based distribution (CentOS, RHEL, Fedora, Rocky Linux, AlmaLinux): "+
			"evo installs Docker only there, and another attempt would not help. Deploy from an Ubuntu or Debian image (--image); whether to delete this VM (evo destroy), ask the person.",
			"Диагноз: на ВМ не Ubuntu, не Debian и не RPM-дистрибутив (CentOS, RHEL, Fedora, Rocky Linux, AlmaLinux): "+
				"evo ставит Docker только на них, и повтор не поможет. Разверните из образа Ubuntu или Debian (--image); удалять ли эту ВМ (evo destroy), спросите человека.")
	case strings.Contains(out, bootstrap.NoCodenameLine):
		return i18n.T("Diagnosis: /etc/os-release of the image names no release (VERSION_CODENAME), so evo cannot pick Docker's repository for it, "+
			"and another attempt would not help. Deploy from a standard Ubuntu or Debian image (--image); whether to delete this VM (evo destroy), ask the person.",
			"Диагноз: в /etc/os-release образа не указан выпуск (VERSION_CODENAME), и evo не может выбрать для него репозиторий Docker; "+
				"повтор не поможет. Разверните из стандартного образа Ubuntu или Debian (--image); удалять ли эту ВМ (evo destroy), спросите человека.")
	case strings.Contains(out, bootstrap.CurlMissingLine):
		return i18n.T("Diagnosis: the image has no curl, and evo installs Docker on RPM-based distributions with get.docker.com; another attempt would not help. "+
			"Deploy from an image with curl, or from Ubuntu or Debian (--image).",
			"Диагноз: в образе нет curl, а на RPM-дистрибутивах evo ставит Docker через get.docker.com; повтор не поможет. "+
				"Разверните из образа с curl или из Ubuntu либо Debian (--image).")
	case strings.Contains(out, bootstrap.DockerNoReleaseLine):
		return i18n.T("Diagnosis: Docker publishes no packages for this release of the distribution (download.docker.com has no Release file for its codename), "+
			"and another attempt would not help. Deploy from a release Docker supports, such as a current Ubuntu LTS or Debian stable (--image); whether to delete this VM (evo destroy), ask the person.",
			"Диагноз: Docker не выпускает пакетов для этого выпуска дистрибутива (у download.docker.com нет файла Release для его кодового имени), "+
				"и повтор не поможет. Разверните из выпуска, который Docker поддерживает, например текущей Ubuntu LTS или Debian stable (--image); удалять ли эту ВМ (evo destroy), спросите человека.")
	case strings.Contains(out, "Temporary failure in name resolution") || strings.Contains(out, "Could not resolve"):
		return i18n.T("Diagnosis: the VM does not resolve names (no DNS inside the VM) — a network problem of the zone or subnet.",
			"Диагноз: ВМ не разрешает имена (внутри ВМ нет DNS) — проблема сети зоны или подсети.")
	case strings.Contains(out, bootstrap.NoKeyringLine):
		return i18n.T("Diagnosis: the image's apt mirror does not work, and the VM has no keyring of the distribution "+
			"(/usr/share/keyrings/ubuntu-archive-keyring.gpg or debian-archive-keyring.gpg) to verify the official mirror with: "+
			"evo does not install from a mirror it cannot verify. Deploy from a standard Ubuntu or Debian image (it has the keyring), "+
			"or restore the package ubuntu-keyring / debian-archive-keyring on the VM and re-run 'evo deploy'. Not a problem of the compose project.",
			"Диагноз: apt-зеркало образа не работает, а на ВМ нет keyring дистрибутива "+
				"(/usr/share/keyrings/ubuntu-archive-keyring.gpg или debian-archive-keyring.gpg), чтобы проверить официальное зеркало: "+
				"с зеркала, которое нечем проверить, evo не ставит. Разверните из стандартного образа Ubuntu или Debian (в нём keyring есть) "+
				"или верните на ВМ пакет ubuntu-keyring / debian-archive-keyring и повторите 'evo deploy'. Дело не в compose-проекте.")
	case strings.Contains(out, bootstrap.FallbackFailedLine):
		return i18n.T("Diagnosis: apt failed both with the image's mirror and with the official mirror of the distribution (archive.ubuntu.com, deb.debian.org) — "+
			"most likely the VM does not reach the internet: check the outbound rules of its security group and the subnet in the Cloud.ru console, "+
			"or deploy in another zone — whether to delete this VM first (evo destroy), ask the person. Not a problem of the compose project.",
			"Диагноз: apt не справился ни с зеркалом образа, ни с официальным зеркалом дистрибутива (archive.ubuntu.com, deb.debian.org) — "+
				"скорее всего, у ВМ нет выхода в интернет: проверьте исходящие правила её группы безопасности и подсеть в консоли Cloud.ru "+
				"или разверните в другой зоне — удалять ли сначала эту ВМ (evo destroy), спросите человека. Дело не в compose-проекте.")
	case strings.Contains(out, bootstrap.DockerRepoFailedLine):
		return i18n.T("Diagnosis: apt failed on Docker's repository download.docker.com while the distribution's packages download: "+
			"it does not answer from the VM, or its signature does not verify (apt's lines above say which) — re-run 'evo deploy' later. Not a problem of the compose project.",
			"Диагноз: apt не справился с репозиторием Docker download.docker.com, хотя пакеты дистрибутива скачиваются: "+
				"он не отвечает с ВМ или его подпись не сходится (что именно — в строках apt выше) — повторите 'evo deploy' позже. Дело не в compose-проекте.")
	case strings.Contains(out, "Failed to fetch"):
		return i18n.T("Diagnosis: apt cannot download packages from the image's mirror (\"Failed to fetch\") — the mirror or the way to it is down, not the compose project. "+
			"evo then installs from the official mirror of the distribution, and a re-run goes there at once: re-run 'evo deploy', or deploy in another zone.",
			"Диагноз: apt не скачивает пакеты с зеркала образа («Failed to fetch») — недоступно зеркало или путь к нему, а не compose-проект. "+
				"Тогда evo ставит пакеты с официального зеркала дистрибутива, и повтор сразу идёт туда: повторите 'evo deploy' или разверните в другой зоне.")
	}
	return ""
}

// clipLine is s on one line, at most 200 characters.
func clipLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

// reconnect opens a fresh SSH connection to the VM.
func (e *Engine) reconnect(ctx context.Context, ip, user, hostKey string) error {
	_ = e.Deployer.Close()
	return e.Deployer.Connect(ctx, ip, 22, user, e.SSHKeyPEM, hostKey)
}

// isFatalSSHError reports errors that retrying cannot fix.
func isFatalSSHError(err error) bool {
	var cliErr *clerrors.CLIError
	return stderrors.As(err, &cliErr) &&
		(cliErr.Code == clerrors.CodeSSHAuthFailed || cliErr.Code == clerrors.CodeSSHHostKeyChanged)
}

// lastLines returns up to n trailing lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// resolveDiskType finds a disk type by name (case-insensitive) in
// list_disk_types: its UUID is per tenant and must not be hardcoded.
func (e *Engine) resolveDiskType(ctx context.Context, projectID, name string) (*provision.DiskType, error) {
	types, err := e.Cloud.ListDiskTypes(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("list disk types: %w", "список типов дисков: %w"), err)
	}
	names := make([]string, 0, len(types))
	for _, dt := range types {
		if strings.EqualFold(dt.Name, name) {
			return dt, nil
		}
		names = append(names, dt.Name)
	}
	return nil, clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("disk type %q not found in project", "тип диска %q в проекте не найден", name),
		i18n.Tf("Available disk types: %s.", "Доступные типы дисков: %s.", strings.Join(names, ", ")))
}

// sshUserOf returns the login user recorded for the deployment.
func sshUserOf(rec *state.DeploymentRecord) (string, error) {
	if rec.SSHUser == "" {
		return "", clerrors.New(clerrors.CodeStateCorrupted,
			i18n.Tf("deployment %s has no ssh_user in state", "у развёртывания %s в state нет ssh_user", rec.ID),
			i18n.T("Set \"ssh_user\" in state.json (.evo/, or the legacy .cloudru-vm/) to the image's login user (Cloud.ru images: user1).",
				"Задайте \"ssh_user\" в state.json (.evo/ или старый .cloudru-vm/) — пользователя для входа из образа (у образов Cloud.ru — user1)."))
	}
	return rec.SSHUser, nil
}

// vmLeftRunning turns an interruption after the VM became active (floating
// IP, SSH, Docker, upload, compose up) into the same exit-130 error with a
// hint that handleInterrupt gives while waiting for the VM: without it
// Ctrl-C would leave a paid VM running without a word.
func (e *Engine) vmLeftRunning(ctx context.Context, err error, vmID string) error {
	if err == nil {
		return nil
	}
	if !stderrors.Is(ctx.Err(), context.Canceled) && !stderrors.Is(err, context.Canceled) {
		return err
	}
	// A cut create of the VM's floating IP is named with the VM: the
	// address may be billed too (D107; «Риск», conclusion 180, У1).
	lost, isLost := floatingip.AsLost(err)
	var cliErr *clerrors.CLIError
	if !isLost && stderrors.As(err, &cliErr) && strings.Contains(cliErr.Hint, vmID) {
		return err // handleInterrupt already said what happened to the VM
	}
	e.Printer.Warning(i18n.Tf("Interrupted. VM %s left running.", "Прервано. ВМ %s продолжает работать.", vmID))
	cause := err
	if !stderrors.Is(err, context.Canceled) {
		cause = fmt.Errorf("%w: %w", context.Canceled, err)
	}
	hint := vmStillRunningHint(vmID)
	if isLost {
		hint += " " + i18n.Tf("Its floating IP %s was requested with no answer and may be billed: check list_floating_ips for the name; the next deploy takes it if it is listed, or asks first.",
			"Её floating IP %s запрошен без ответа облака и может тарифицироваться: проверьте 'evo vpc fips' по имени; следующий deploy возьмёт его, если он в списке, или сначала спросит.", lost.Name)
	}
	return clerrors.Wrap(clerrors.CodeVMProvisionFailed,
		deployInterrupted(),
		hint,
		cause)
}

func deployInterrupted() string {
	return i18n.T("deployment interrupted", "развёртывание прервано")
}

func vmStillRunningHint(vmID string) string {
	return i18n.Tf("VM %s was created and is still running (billed); whether to delete it ('evo destroy --yes') — ask the person.",
		"ВМ %s создана и работает (тарифицируется); удалять ли её ('evo destroy --yes') — спросите человека.", vmID)
}

// handleInterrupt handles SIGINT/SIGTERM (or a cancelled context) after VM
// creation and reports whether the VM was deleted. The returned error wraps
// context.Canceled, so the CLI exits 130.
func (e *Engine) handleInterrupt(sig string, vmID string, nonInteractive bool) (deleted bool, _ error) {
	if nonInteractive || !confirm.StdinIsTTY() {
		e.Printer.Warning(i18n.Tf("Received %s. VM %s left running (non-interactive mode).",
			"Получен %s. ВМ %s продолжает работать (режим без вопросов).", sig, vmID))
		return false, clerrors.Wrap(clerrors.CodeVMProvisionFailed,
			deployInterrupted(),
			vmStillRunningHint(vmID),
			context.Canceled)
	}

	fmt.Fprintf(e.Printer.ErrWriter(), i18n.T("\nReceived %s. Delete the VM %s? [y/N] ",
		"\nПолучен %s. Удалить ВМ %s? (y/д — да, Enter — нет) "), sig, vmID)
	var answer string
	fmt.Fscanln(confirm.Stdin, &answer)

	if confirm.IsYes(answer) {
		e.Printer.StepDone(i18n.T("Deleting VM...", "Удаление ВМ..."))
		// A VM still being created cannot be deleted yet: wait as destroy
		// does instead of leaving it behind (QA H2).
		deleteCtx, cancel := context.WithTimeout(context.Background(), e.timings().DeletableWaitTimeout+time.Minute)
		defer cancel()
		if err := e.deleteVMWhenDeletable(deleteCtx, vmID); err != nil {
			e.Printer.Warning(i18n.Tf("Failed to delete VM: %v", "Не удалось удалить ВМ: %v", err))
		} else {
			e.Printer.StepDone(i18n.T("VM deleted", "ВМ удалена"))
			deleted = true
		}
	} else {
		e.Printer.Warning(i18n.Tf("VM %s left running.", "ВМ %s продолжает работать.", vmID))
	}

	return deleted, clerrors.Wrap(clerrors.CodeVMProvisionFailed,
		i18n.T("deployment interrupted by signal", "развёртывание прервано сигналом"),
		i18n.Tf("VM %s was created and may be billed; whether to delete it ('evo destroy') — ask the person.",
			"ВМ %s была создана и может тарифицироваться; удалять ли её ('evo destroy') — спросите человека.", vmID),
		context.Canceled)
}

// printVerifyResults outputs health check results.
func (e *Engine) printVerifyResults(results []verify.ProbeResult, ip string) {
	allHealthy := true
	for _, r := range results {
		if r.Healthy {
			e.Printer.StepDone(fmt.Sprintf(":%d -> %d (%s)", r.Port, r.StatusCode, r.Latency.Round(time.Millisecond)))
		} else {
			allHealthy = false
			msg := fmt.Sprintf(":%d -> %s", r.Port, i18n.T("UNHEALTHY", "НЕ ОТВЕЧАЕТ"))
			if r.Error != "" {
				msg += fmt.Sprintf(" (%s)", r.Error)
			}
			e.Printer.Warning(msg)
		}
	}
	if !allHealthy {
		e.Printer.Hint(i18n.T("Some health checks failed. Services may still be starting. Re-check with: curl http://",
			"Часть проверок не прошла. Возможно, сервисы ещё запускаются. Проверьте ещё раз: curl http://") + ip + i18n.T(":<port>/", ":<порт>/"))
	}
}

// CreateBootFIP, WaitBootFIP and ReleaseBootFIP give the MCP provision tool
// the same public-IP-at-create path as deploy.
func (e *Engine) CreateBootFIP(ctx context.Context, projectID, azID, vmName string, sf *state.File) (*provision.FloatingIP, error) {
	return e.createBootFIP(ctx, projectID, azID, vmName, sf)
}

// WaitBootFIP: see CreateBootFIP.
func (e *Engine) WaitBootFIP(ctx context.Context, fip *provision.FloatingIP, vmID, projectID string) (ip, fipID string, err error) {
	return e.waitBootFIP(ctx, fip, vmID, projectID)
}

// ReleaseBootFIP: see CreateBootFIP. The error says the FIP was NOT
// released and is billed: the MCP answer names it, where the warning would
// reach only the server's stderr (final audit; Qwen review of PR #89).
func (e *Engine) ReleaseBootFIP(fip *provision.FloatingIP, projectID string) error {
	return e.releaseBootFIP(fip, projectID)
}

// waitVMGone polls until the VM is not found, ctx ends or
// Timings.DestroyPollTimeout passes. Best effort: the FIP release after it
// retries on its own.
func (e *Engine) waitVMGone(ctx context.Context, vmID string) bool {
	t := e.timings()
	deadline := time.Now().Add(t.DestroyPollTimeout)
	last := ""
	for time.Now().Before(deadline) {
		vm, err := e.Cloud.GetVM(ctx, vmID)
		if err != nil {
			var ce *clerrors.CLIError
			if stderrors.As(err, &ce) && ce.Code == clerrors.CodeVMNotFound {
				return true
			}
		} else if vm != nil && vm.Status != last {
			// The wait says the VM's state when it changes (CLAUDE.md §6,
			// axis 2), not only that time passes.
			last = vm.Status
			e.Printer.Step(0, 0, i18n.Tf("VM %s: %s, waiting until it is deleted…", "ВМ %s: %s, жду, пока она удалится…", vmID, vm.Status))
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(t.DestroyPollInterval):
		}
	}
	return false
}

// WaitVMGone is waitVMGone for MCP destroy by vm_id: true once the VM is
// not found.
func (e *Engine) WaitVMGone(ctx context.Context, vmID string) bool { return e.waitVMGone(ctx, vmID) }

// ReleaseFloatingIP releases the FIP a record names (floating_ip_id, else
// its address or the after-boot name), retrying while the platform still
// reports it attached to a just-deleted VM. Nothing to release is nil.
func (e *Engine) ReleaseFloatingIP(ctx context.Context, rec *state.DeploymentRecord) error {
	return e.releaseFloatingIP(ctx, rec)
}

// IsNotDeletableYet reports a 409/422 from deleting a VM that is still
// being created or changing state.
func IsNotDeletableYet(err error) bool { return isNotDeletableYet(err) }
