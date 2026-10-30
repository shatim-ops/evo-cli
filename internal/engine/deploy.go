// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"io"
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
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
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
			i18n.T("Run 'evo destroy' to finish it, then deploy again.", "Завершите удаление командой 'evo destroy', затем разверните заново."))
	}
	// A VM that went to error_creating is not reused: it never boots, and a
	// re-run would give it a billed floating IP and retry SSH in a loop
	// (release QA, B-lifecycle 4; register CMP-9: such a VM is deleted
	// through the API).
	if !isNewDeploy && existing.Status == "error" {
		return clerrors.New(clerrors.CodeVMProvisionFailed,
			i18n.Tf("VM %s of this deployment is in an error state", "ВМ %s этого развёртывания в состоянии ошибки", existing.VMID),
			i18n.T("Delete it with 'evo destroy', then deploy again (in another zone if it repeats).",
				"Удалите её командой 'evo destroy' и разверните заново (если повторится — в другой зоне)."))
	}
	// One directory holds one deployment: never send this compose file to
	// a VM of another project, nor open its ports there.
	if !isNewDeploy && existing.ProjectID != "" && e.Config.ProjectID != "" && existing.ProjectID != e.Config.ProjectID {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("this directory already has deployment %s in project %s, not in %s",
				"в этом каталоге уже есть развёртывание %s в проекте %s, а не в %s", existing.ID, existing.ProjectID, e.Config.ProjectID),
			i18n.Tf("Update it with --project-id %s, or run 'evo destroy' first and deploy to the new project.",
				"Обновите его с --project-id %s или сначала выполните 'evo destroy' и разверните в новом проекте.", existing.ProjectID))
	}

	// Skip only a deployment that finished: after an SSH or Docker failure the
	// hash is already recorded, and a plain re-run must retry.
	if !isNewDeploy && existing.ComposeHash == project.Hash && existing.Status == "running" && !opts.Force {
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
		proceed, err := e.redeployGate(ctx, opts, redeployPreview(existing, e.projectOf(existing), e.projectFrom(existing),
			existing.ComposeHash != project.Hash, opts.Force))
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
			defer func() { retErr = e.vmLeftRunning(ctx, retErr, vmID) }()
		}
		if err != nil {
			return err
		}
	} else {
		e.Printer.Step(3, totalSteps, i18n.T("Reusing existing VM...", "Используется существующая ВМ..."))
		// Records from before v1.0.1 have no names: they are looked up
		// once and saved with the record.
		rec = e.withNames(ctx, *existing, nil, true)
		rec.ComposeHash = project.Hash
		rec.ComposePath = composePath(opts.ComposeFile)
		rec.ExposedPorts = project.Ports
		rec.UpdatedAt = time.Now()
		if rec.VMIP == "" && rec.VMID != "" {
			// The floating IP failed on an earlier run: try again instead
			// of failing on "no public IP" on every re-run.
			e.recoverFloatingIP(ctx, &rec)
			stateFile.UpsertDeployment(rec)
			_ = state.Write(statePath, stateFile)
		}
		// An earlier run may have stopped before the ports were opened, or
		// the compose file publishes new ones: open what is missing. Records
		// from before security_group_id existed have no group to open in.
		if rec.SecurityGroupID != "" {
			e.openPorts(ctx, e.projectOf(&rec), rec.SecurityGroupID, project.Ports, project.UDPPorts, &rec)
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

	if err := e.waitForSSH(ctx, rec.VMIP, sshUserForConnect, rec.SSHHostKeyFingerprint, maxRetries); err != nil {
		// The status says which: a refused key or a changed host key is
		// not a timeout, and a re-run will not fix it (QA r4 Q7).
		rec.Status = "ssh_timeout"
		switch clerrors.Reason(err) {
		case clerrors.CodeSSHAuthFailed:
			rec.Status = "ssh_auth_failed"
		case clerrors.CodeSSHHostKeyChanged:
			rec.Status = "ssh_host_key_changed"
		}
		rec.UpdatedAt = time.Now()
		stateFile.UpsertDeployment(rec)
		_ = state.Write(statePath, stateFile)
		return err
	}
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
		if ctx.Err() == nil {
			rec.Status = "docker_failed"
			rec.UpdatedAt = time.Now()
			stateFile.UpsertDeployment(rec)
			_ = state.Write(statePath, stateFile)
		}
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
	if err := e.Deployer.Upload(ctx, files); err != nil {
		return err
	}

	// Run docker compose up. Streamed, not Exec: pulling images can take
	// longer than Exec's 120 s limit. Progress goes to stderr in text mode.
	// The last lines go with a failure in every mode: with --json the
	// output goes nowhere else (#558). They are the workload's words: the
	// tool marks the field untrusted.
	tail := newTailWriter(composeTailLines)
	var progress io.Writer = io.MultiWriter(e.Printer.ErrWriter(), tail)
	if e.Printer.IsJSON() {
		progress = tail
	}
	if err := e.Deployer.ExecStream(ctx, fmt.Sprintf("cd %s && docker compose -f docker-compose.yml up -d", remoteComposeDir), progress, progress); err != nil {
		ce := clerrors.Wrap(clerrors.CodeSSHTimeout,
			i18n.T("docker compose up failed", "docker compose up завершился ошибкой"),
			i18n.T("Check your compose file and service logs on the VM.", "Проверьте compose-файл и журналы сервисов на ВМ."),
			err)
		if out := tail.String(); out != "" {
			ce.Details = map[string]any{"output": out}
			ce.Hint += i18n.T(" The last lines of docker compose up are in output.", " Последние строки docker compose up — в output.")
		}
		return ce
	}
	e.Printer.StepDone(i18n.T("Application deployed", "Приложение развёрнуто"))

	// Update state to running.
	rec.Status = "running"
	rec.UpdatedAt = time.Now()
	stateFile.UpsertDeployment(rec)
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	// Step 5: Verify.
	var results []verify.ProbeResult
	e.Printer.Step(5, totalSteps, i18n.T("Verifying health...", "Проверка работоспособности..."))
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
		u.Unchanged = !changed && existing.Status == "running" && !force && !otherProject
	}
	return u
}

// resolveFlavor determines the VM flavor via auto-sizing or explicit selection.
func (e *Engine) resolveFlavor(ctx context.Context, opts DeployOptions, project *compose.Project) (string, error) {
	flavor := opts.Flavor
	if flavor == "" {
		flavor = e.Config.Flavor
	}

	if flavor != "" && flavor != "auto" {
		// A flavor the catalogue has not is refused first, as the preview
		// refuses it (#470): before the image and the network are read.
		if _, err := e.flavorAPIName(ctx, flavor); err != nil {
			return "", err
		}
		return flavor, nil
	}

	// Auto-size: estimate resources, then match against available flavors.
	estimated := sizer.EstimateFromServices(project.Services)

	flavors, err := e.Cloud.ListFlavors(ctx)
	if err != nil {
		return "", err
	}

	// Convert []*Flavor to []Flavor for sizer.Match.
	flavorList := make([]provision.Flavor, len(flavors))
	for i, f := range flavors {
		flavorList[i] = *f
	}

	match := sizer.Match(estimated, flavorList)
	if match == nil {
		return "", clerrors.New(clerrors.CodeConfigInvalid,
			i18n.T("no suitable flavor found for the workload", "не найден flavor, подходящий под нагрузку"),
			i18n.T("Specify a flavor explicitly with --flavor.", "Задайте flavor явно: --flavor."))
	}

	e.Printer.StepDone(i18n.Tf("Recommended flavor: %s (%d vCPU, %d GB RAM)", "Подобран flavor: %s (%d vCPU, %d ГБ RAM)",
		match.Flavor.Name, match.Flavor.VCPUs, match.Flavor.RAMGB))

	return match.Flavor.ID, nil
}

// provisionVM creates a new VM and waits for it to become active.
func (e *Engine) provisionVM(ctx context.Context, opts DeployOptions, stateFile *state.File, statePath string, project *compose.Project, region string) (state.DeploymentRecord, error) {
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

	projectName := e.Config.ProjectName
	if projectName == "" {
		projectName = filepath.Base(e.StateDir)
	}
	vmName := vmNameFor(projectName)

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
		proceed, err = confirm.Ask(ctx, e.deployPreview(ctx, req, imgDetail, diskType.ID, network, project.Ports, project.UDPPorts), e.confirmOptions(opts.Yes, opts.DryRun, opts.NonInteractive))
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
	bootFIP, err := e.createBootFIP(ctx, projectID, region, vmName)
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
				i18n.Tf("Check it with 'evo vm get %s'. If it is not this directory's deployment, delete it ('evo vm delete %s') or deploy from a directory with another name.",
					"Проверьте её: 'evo vm get %s'. Если это не развёртывание этого каталога, удалите её ('evo vm delete %s') или разверните из каталога с другим именем.", vmName, vmName),
				createErr)
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
			i18n.Tf("VM %s is billed and recorded in the state: run 'evo deploy' again to finish it, or 'evo destroy' to delete it.",
				"ВМ %s тарифицируется и записана в state: повторите 'evo deploy', чтобы завершить развёртывание, или удалите её: 'evo destroy'.", vm.ID),
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
				ferr = stderrors.New(i18n.Tf("deletion of VM %s is not confirmed yet; run 'evo destroy' to finish",
					"удаление ВМ %s ещё не подтверждено; завершите его командой 'evo destroy'", vm.ID))
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
	for {
		select {
		case sig := <-sigCh:
			return interrupted(sig.String())
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
						i18n.Tf("VM %s is billed and recorded in the state. Check the key ('evo whoami'), then run 'evo deploy' again to finish, or 'evo destroy' to delete it.",
							"ВМ %s тарифицируется и записана в state. Проверьте ключ ('evo whoami') и повторите 'evo deploy', чтобы завершить, или удалите её: 'evo destroy'.", vm.ID),
						err)
				}
				e.Printer.Warning(i18n.Tf("poll error: %v", "ошибка опроса: %v", err))
				continue
			}
			if updated.Status != "active" && updated.Status != "running" {
				wait.State(updated.Status)
			}
			switch updated.Status {
			case "active", "running":
				e.Printer.StepDone(i18n.Tf("VM %s active (%s)", "ВМ %s активна (%s)", vmText(&rec), ElapsedText(wait.Elapsed())))

				// The floating (public) IP: attached at creation, or now.
				var publicIP, fipID string
				if bootFIP != nil {
					publicIP, fipID, err = e.waitBootFIP(ctx, bootFIP, vm.ID, projectID)
				} else {
					publicIP, fipID, err = e.assignFloatingIP(ctx, vm.ID, projectID, region)
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
				e.openPorts(ctx, projectID, network.SecurityGroupID, project.Ports, project.UDPPorts, &rec)
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
				_ = state.Write(statePath, stateFile)
				return rec, clerrors.New(clerrors.CodeVMProvisionFailed,
					i18n.Tf("VM %s entered state %s", "ВМ %s перешла в состояние %s", vm.ID, updated.Status),
					i18n.T("Check Cloud.ru console for details.", "Подробности — в консоли Cloud.ru."))
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
func (e *Engine) AssignFloatingIP(ctx context.Context, vmID, projectID, azID string) (ip, fipID string, err error) {
	return assignFloatingIP(ctx, e.Cloud, e.timings(), nil, vmID, projectID, azID)
}

// assignFloatingIP prints its progress: the waits add up to minutes.
func (e *Engine) assignFloatingIP(ctx context.Context, vmID, projectID, azID string) (ip, fipID string, err error) {
	return assignFloatingIP(ctx, e.Cloud, e.timings(), e.Printer.StepDone, vmID, projectID, azID)
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

	// Create floating IP with retry.
	report("%s", i18n.T("Creating floating IP...", "Создание floating IP..."))
	var fip *provision.FloatingIP
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		fip, err = cloud.CreateFloatingIP(ctx, provision.CreateFloatingIPRequest{
			Name:               fipName(vmID),
			ProjectID:          projectID,
			InterfaceID:        ifaceID,
			AvailabilityZoneID: azID,
		})
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		report(i18n.T("Creating floating IP failed (%v), retrying", "Не удалось создать floating IP (%v), повтор"), err)
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(t.FIPRetryInterval):
		}
	}
	if err != nil {
		// The platform may have made the FIP before the response was lost
		// (then the next attempts fail: the interface already has one).
		// It is billed, so find it by its name.
		found := findFloatingIPByName(ctx, cloud, projectID, fipName(vmID))
		if found == nil {
			return "", "", fmt.Errorf(i18n.T("create floating IP: %w", "создание floating IP: %w"), err)
		}
		fip = found
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
func (e *Engine) createBootFIP(ctx context.Context, projectID, azID, vmName string) (*provision.FloatingIP, error) {
	// A free boot FIP of this VM name left by an earlier run (its release
	// failed after a 409, break r3a R3A-2) is taken instead of a second
	// billed address. A boot FIP name is unique to its VM name.
	if f := e.leftBootFIP(ctx, projectID, vmName); f != nil {
		e.Printer.StepDone(i18n.Tf("Reusing floating IP %s left by an earlier run",
			"Использую floating IP %s, оставшийся от прошлого запуска", f.IPAddress))
		return f, nil
	}
	name := provision.BootFIPName(vmName, clock.Now())
	fip, err := e.Cloud.CreateFloatingIP(ctx, provision.CreateFloatingIPRequest{
		Name:               name,
		ProjectID:          projectID,
		AvailabilityZoneID: azID,
	})
	if (err != nil || fip == nil || fip.ID == "") && !provision.IsQuotaError(err) {
		// The create may have reached the cloud with its answer lost: then
		// the FIP exists under this unique name. Taken as the boot FIP, it is
		// recorded and released like one, not left billed (QA r3 R7).
		if f := e.findBootFIP(ctx, projectID, name); f != nil {
			return e.settledBootFIP(ctx, f, projectID), nil
		}
		if ctx.Err() != nil {
			e.Printer.Warning(i18n.Tf("reserving floating IP %s was interrupted: the platform may still create it; check 'evo vpc fips' and release it ('evo vpc fip-release') if it is there",
				"резервирование floating IP %s прервано: платформа ещё может его создать; проверьте 'evo vpc fips' и освободите его ('evo vpc fip-release'), если он там есть", name))
		}
	}
	if err != nil || fip == nil || fip.ID == "" {
		if provision.IsQuotaError(err) {
			return nil, clerrors.Wrap(clerrors.CodeVMProvisionFailed,
				i18n.T("no public IP can be reserved: the floating IP quota is exhausted",
					"не удалось зарезервировать внешний IP: квота floating IP исчерпана"),
				i18n.T("Nothing was created. Delete a floating IP you no longer need (list_floating_ips / release_floating_ip) or raise the quota in the Cloud.ru console, then retry.",
					"Ничего не создано. Освободите ненужный floating IP ('evo vpc fips', 'evo vpc fip-release') или увеличьте квоту в консоли Cloud.ru и повторите."),
				err)
		}
		if ctx.Err() == nil {
			e.Printer.Warning(i18n.Tf("could not reserve a public IP before creating the VM (%v); it will be attached after boot, which can make the first boot slow",
				"не удалось зарезервировать внешний IP до создания ВМ (%v); он будет привязан после загрузки, из-за этого первая загрузка может затянуться", err))
		}
		return nil, nil
	}
	return e.settledBootFIP(ctx, fip, projectID), nil
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
	for _, f := range fips {
		if f != nil && f.ID != "" && provision.IsBootFIPOf(f.Name, vmName) && f.Available() {
			return f
		}
	}
	return nil
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

// findFloatingIPByName returns the project's floating IP with that name, or nil.
func findFloatingIPByName(ctx context.Context, cloud provision.CloudAPI, projectID, name string) *provision.FloatingIP {
	fips, err := cloud.ListFloatingIPs(ctx, projectID)
	if err != nil {
		return nil
	}
	for _, f := range fips {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// recoverFloatingIP gives a recorded VM without a public address one: the
// recorded floating IP's address, else a new floating IP. Failures are
// warnings; the caller reports the missing address.
func (e *Engine) recoverFloatingIP(ctx context.Context, rec *state.DeploymentRecord) {
	if rec.FloatingIPID != "" {
		// Never a second one next to the recorded FIP.
		f, err := e.Cloud.GetFloatingIP(ctx, rec.FloatingIPID)
		if err != nil {
			e.Printer.Warning(i18n.Tf("floating IP %s: %v", "не удалось прочитать floating IP %s: %v", rec.FloatingIPID, err))
			return
		}
		rec.VMIP = f.IPAddress
		return
	}
	ip, fipID, err := e.assignFloatingIP(ctx, rec.VMID, e.projectOf(rec), rec.Region)
	if fipID != "" {
		rec.FloatingIPID = fipID
	}
	rec.VMIP = ip
	if err != nil {
		e.Printer.Warning(i18n.Tf("floating IP: %v", "floating IP не назначен: %v", err))
	}
}

// needsFloatingIP: the record has neither an address nor a recorded floating
// IP, so a re-run creates one (recoverFloatingIP).
func needsFloatingIP(rec *state.DeploymentRecord) bool {
	return rec.VMIP == "" && rec.FloatingIPID == "" && rec.VMID != ""
}

// waitForSSH retries SSH connection until the VM is reachable.
func (e *Engine) waitForSSH(ctx context.Context, ip, user, knownHostKey string, maxRetries int) error {
	var lastErr error
	t := e.timings()
	// start times the waits; shown, the duration the reports print.
	start, lastReport, shown := time.Now(), time.Now(), clock.Now()
	noted := false
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := e.Deployer.Connect(ctx, ip, 22, user, e.SSHKeyPEM, knownHostKey)
		if err == nil {
			return nil
		}
		lastErr = err
		// A fresh VM's sshd can take minutes: say so once a minute, as the
		// wait for an active VM does, so it does not look hung (QA r4 Q13).
		if time.Since(lastReport) >= time.Minute {
			e.Printer.StepDone(i18n.Tf("waiting for SSH on %s (%s elapsed: %s)", "ожидание SSH на %s (прошло %s: %s)",
				ip, clock.Since(shown).Round(time.Second), errLine(err)))
			lastReport = time.Now()
		}
		// Past the usual few minutes, say once why deploy still waits and
		// what that may mean (cloud-e2e 16-18.10.2026, register OPS-1).
		if !noted && time.Since(start) >= t.SSHSlowNote {
			e.Printer.Warning(i18n.Tf("SSH on %s does not answer yet. A new VM usually answers within 2-5 minutes; on Cloud.ru its first boot "+
				"sometimes does not bring the network up, and then SSH never answers. evo keeps trying (%d attempts in all).",
				"SSH на %s пока не отвечает. Новая ВМ обычно отвечает за 2–5 минут; на Cloud.ru первая загрузка иногда "+
					"не поднимает сеть, и тогда SSH не ответит вовсе. evo продолжает попытки (всего %d).", ip, maxRetries))
			noted = true
		}

		// Don't retry on auth failures or host key mismatches.
		if cliErr, ok := err.(*clerrors.CLIError); ok {
			if cliErr.Code == clerrors.CodeSSHAuthFailed || cliErr.Code == clerrors.CodeSSHHostKeyChanged {
				return err
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.SSHRetryInterval):
		}
	}
	// evo does not reboot or recreate the VM by itself: both are the
	// user's call (a reboot interrupts whatever the VM is doing, a new VM
	// is billed). The hint says how.
	hint := i18n.T("What to do: run 'evo deploy' again in a few minutes (it reuses this VM). If SSH still does not answer, "+
		"reboot the VM in the Cloud.ru console (Virtual machines, the VM's menu, Reboot; evo has no reboot command) and run 'evo deploy' again, "+
		"or look at its boot log in the serial console. Otherwise run 'evo destroy' and deploy again, in another zone if it repeats: "+
		"on Cloud.ru a VM sometimes boots without network (no DNS inside the VM, the image's apt mirror unreachable) while the next one is fine. "+
		"If it repeats in one zone, check the subnet's DNS in the Cloud.ru console.",
		"Что делать: через несколько минут повторите 'evo deploy' (он возьмёт эту же ВМ). Если SSH так и не ответит, "+
			"перезагрузите ВМ в консоли Cloud.ru (Виртуальные машины, меню ВМ, «Перезагрузить»; своей команды перезагрузки в evo нет) "+
			"и снова выполните 'evo deploy' или посмотрите журнал загрузки в серийной консоли. Иначе — 'evo destroy' и deploy заново, "+
			"при повторе в другой зоне: на Cloud.ru ВМ иногда загружается без сети (нет DNS внутри ВМ, недоступно apt-зеркало образа), "+
			"а следующая — нормально. Если повторяется в одной зоне, проверьте DNS подсети в консоли Cloud.ru.")
	if d := sshDiagnosis(lastErr, ip); d != "" {
		hint = d + "\n" + hint
	}
	return clerrors.Wrap(clerrors.CodeSSHTimeout,
		i18n.Tf("SSH connection failed after %d attempts", "не удалось подключиться по SSH за %d попыток", maxRetries),
		hint, lastErr)
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
		var out bytes.Buffer
		attemptCtx, cancel := context.WithDeadline(ctx, deadline)
		err := e.Deployer.ExecStream(attemptCtx, installCmd, &out, &out)
		cancel()
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
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if isFatalSSHError(err) {
			return err
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
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isFatalSSHError(err) {
				return err
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
		"then re-run 'evo deploy'. \"Temporary failure in name resolution\" in the install output means no DNS inside the VM: a network problem of the zone or subnet, not of the compose project; if it repeats, destroy and deploy in another zone.",
		"Проверьте, что ВМ разрешает имена (DNS подсети) и достаёт до своего apt-зеркала (или официального зеркала дистрибутива) и download.docker.com, "+
			"затем повторите 'evo deploy'. \"Temporary failure in name resolution\" в выводе установки означает, что внутри ВМ нет DNS: "+
			"это проблема сети зоны или подсети, а не compose-проекта; если повторится, удалите ВМ (destroy) и разверните в другой зоне.")
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
			"evo installs Docker only there, and another attempt would not help. Delete the VM ('evo destroy') and deploy from an Ubuntu or Debian image (--image).",
			"Диагноз: на ВМ не Ubuntu, не Debian и не RPM-дистрибутив (CentOS, RHEL, Fedora, Rocky Linux, AlmaLinux): "+
				"evo ставит Docker только на них, и повтор не поможет. Удалите ВМ ('evo destroy') и разверните из образа Ubuntu или Debian (--image).")
	case strings.Contains(out, bootstrap.NoCodenameLine):
		return i18n.T("Diagnosis: /etc/os-release of the image names no release (VERSION_CODENAME), so evo cannot pick Docker's repository for it, "+
			"and another attempt would not help. Delete the VM ('evo destroy') and deploy from a standard Ubuntu or Debian image (--image).",
			"Диагноз: в /etc/os-release образа не указан выпуск (VERSION_CODENAME), и evo не может выбрать для него репозиторий Docker; "+
				"повтор не поможет. Удалите ВМ ('evo destroy') и разверните из стандартного образа Ubuntu или Debian (--image).")
	case strings.Contains(out, bootstrap.CurlMissingLine):
		return i18n.T("Diagnosis: the image has no curl, and evo installs Docker on RPM-based distributions with get.docker.com; another attempt would not help. "+
			"Deploy from an image with curl, or from Ubuntu or Debian (--image).",
			"Диагноз: в образе нет curl, а на RPM-дистрибутивах evo ставит Docker через get.docker.com; повтор не поможет. "+
				"Разверните из образа с curl или из Ubuntu либо Debian (--image).")
	case strings.Contains(out, bootstrap.DockerNoReleaseLine):
		return i18n.T("Diagnosis: Docker publishes no packages for this release of the distribution (download.docker.com has no Release file for its codename), "+
			"and another attempt would not help. Delete the VM ('evo destroy') and deploy from a release Docker supports, such as a current Ubuntu LTS or Debian stable (--image).",
			"Диагноз: Docker не выпускает пакетов для этого выпуска дистрибутива (у download.docker.com нет файла Release для его кодового имени), "+
				"и повтор не поможет. Удалите ВМ ('evo destroy') и разверните из выпуска, который Docker поддерживает, например текущей Ubuntu LTS или Debian stable (--image).")
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
			"or destroy and deploy in another zone. Not a problem of the compose project.",
			"Диагноз: apt не справился ни с зеркалом образа, ни с официальным зеркалом дистрибутива (archive.ubuntu.com, deb.debian.org) — "+
				"скорее всего, у ВМ нет выхода в интернет: проверьте исходящие правила её группы безопасности и подсеть в консоли Cloud.ru "+
				"или удалите ВМ и разверните в другой зоне. Дело не в compose-проекте.")
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
	var cliErr *clerrors.CLIError
	if stderrors.As(err, &cliErr) && strings.Contains(cliErr.Hint, vmID) {
		return err // handleInterrupt already said what happened to the VM
	}
	e.Printer.Warning(i18n.Tf("Interrupted. VM %s left running.", "Прервано. ВМ %s продолжает работать.", vmID))
	cause := err
	if !stderrors.Is(err, context.Canceled) {
		cause = fmt.Errorf("%w: %w", context.Canceled, err)
	}
	return clerrors.Wrap(clerrors.CodeVMProvisionFailed,
		deployInterrupted(),
		vmStillRunningHint(vmID),
		cause)
}

func deployInterrupted() string {
	return i18n.T("deployment interrupted", "развёртывание прервано")
}

func vmStillRunningHint(vmID string) string {
	return i18n.Tf("VM %s was created and is still running. Use 'evo destroy --yes' to delete it.",
		"ВМ %s создана и работает. Удалить её: 'evo destroy --yes'.", vmID)
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
		i18n.Tf("VM %s was created. Use 'evo destroy' to clean up if needed.",
			"ВМ %s была создана. Если она осталась, удалите её: 'evo destroy'.", vmID),
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
func (e *Engine) CreateBootFIP(ctx context.Context, projectID, azID, vmName string) (*provision.FloatingIP, error) {
	return e.createBootFIP(ctx, projectID, azID, vmName)
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
