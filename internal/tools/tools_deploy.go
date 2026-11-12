// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"cmp"
	"context"
	stderrors "errors"
	"fmt"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
	"golang.org/x/crypto/ssh"
)

// authedEngine builds an engine and sets a fresh bearer token on its cloud
// client, so any handler can make Cloud.ru API calls.
func (s *Registry) authedEngine(ctx context.Context, overrides map[string]any) (*engine.Engine, error) {
	eng, err := s.localEngine(ctx, overrides)
	if err != nil {
		return nil, err
	}
	if err := authorize(ctx, eng); err != nil {
		return nil, err
	}
	return eng, nil
}

// localEngine builds an engine for a handler that reads the local state
// before it talks to the cloud (deploy_apply's plan): no token yet.
func (s *Registry) localEngine(ctx context.Context, overrides map[string]any) (*engine.Engine, error) {
	eng, err := s.engineFor(ctx, overrides)
	if err != nil {
		return nil, err
	}
	if eng == nil {
		return nil, fmt.Errorf("engine unavailable")
	}
	return eng, nil
}

// authorize sets a fresh bearer token on the engine's cloud client.
func authorize(ctx context.Context, eng *engine.Engine) error {
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return err
	}
	eng.Cloud.SetToken(tok.Value)
	return nil
}

// handleDeployPlan resolves a plan (read-only) and returns it with a cost
// estimate and a plan_id. Creates nothing. If base_plan_id is given, it starts
// from that plan's parameters and overlays only the provided fields (a patch),
// so the user can tweak one value without restating everything.
func (s *Registry) handleDeployPlan(ctx context.Context, args map[string]any) (string, bool) {
	// The ports first, before the cloud: the rule of open_ports (QA-3).
	ports, err := portsArg("ports", args["ports"])
	if err != nil {
		return errResult(err), true
	}
	// A negative disk_gb is the caller's, not the cloud's: USAGE before the
	// token, not an IAM or network error first (release audit, REL-9).
	if _, ok := args["disk_gb"]; ok {
		if err := provision.CheckBootDiskGB(intArg(args, "disk_gb", 0)); err != nil {
			return errResult(err), true
		}
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}

	// Base parameters: a fresh request, or a patch on an existing plan.
	var params deployplan.Params
	if base := stringArg(args, "base_plan_id"); base != "" {
		bp, lErr := deployplan.Load(eng.StateDir, base)
		if lErr != nil {
			return errResult(lErr), true
		}
		params = bp.SourceParams
	} else {
		params.ExternalIP = true // default for a fresh plan
	}

	// Overlay only the fields actually present in this call.
	overlayString(args, "name", &params.Name)
	overlayString(args, "product", &params.Product)
	overlayString(args, "mode", &params.Mode)
	overlayInt(args, "cpu", &params.CPU)
	overlayInt(args, "ram_gb", &params.RAMGB)
	overlayInt(args, "disk_gb", &params.DiskGB)
	overlayString(args, "compose_file", &params.ComposeFile)
	overlayString(args, "flavor", &params.Flavor)
	overlayString(args, "zone", &params.Zone)
	overlayString(args, "image", &params.Image)
	overlayString(args, "disk_type", &params.DiskType)
	overlayString(args, "login", &params.Login)
	overlayString(args, "ssh_key", &params.SSHKey)
	if v, ok := args["external_ip"].(bool); ok {
		params.ExternalIP = v
	}
	if _, ok := args["ports"]; ok {
		params.Ports = ports
	}
	overlayString(args, "cidr", &params.Cidr)
	if _, ok := args["image_metadata"]; ok {
		meta, err := metadataArg(args, "image_metadata")
		if err != nil {
			return usageResult(err), true
		}
		params.Meta = meta
	}

	// Default to the AGENT's deploy key so the agent can SSH in to install
	// software (the image is pubkey-only; password SSH is disabled). The
	// the user's own key is added to authorized_keys post-install (see SKILL).
	// An explicit ssh_key arg overrides this.
	//
	// A read-only server writes nothing for a plan nobody may apply there
	// (B12-9): it makes no key (the plan is for the deploy key all the
	// same, KeyLater) and saves no plan, so it hands out no plan_id.
	deployKey := params.SSHKey == ""
	switch {
	case deployKey && s.readOnly:
		params.KeyLater = true
	case deployKey:
		if err := eng.EnsureSSHKey(); err != nil {
			return errResult(err), true
		}
		params.SSHKey = eng.SSHPubKey
	}

	resolved, err := planDefaults(ctx, eng, params)
	if err != nil {
		return errResult(err), true
	}
	plan, err := deployplan.Resolve(ctx, eng.Cloud, eng.Config.ProjectID, resolved)
	if err != nil {
		return errResult(err), true
	}
	// A name another VM has is refused now, not at deploy_apply: only the
	// folder's own VM is planned again (D73).
	if ans, refused := vmNameFree(ctx, eng, plan.VM.Name, ownIn(folderVMs(eng))); refused {
		return ans, true
	}
	// A patch (base_plan_id) starts from what the caller asked, not from
	// the defaults filled in: "cpu: 4" on top of a configured flavor must
	// size anew.
	plan.SourceParams = params

	apply := "On explicit approval, call deploy_apply with this plan_id and confirmed=true."
	if plan.ComposeRequired {
		// deploy_apply makes the VM and nothing on it (G13-7).
		apply += " deploy_apply creates the VM without the application: to deploy the compose project, call deploy instead."
	}
	// Compact, user-facing view (omit internal fields like meta_fields/public_key).
	view := map[string]any{
		"product":          plan.Product,
		"mode":             plan.Mode,
		"vm":               plan.VM,
		"network":          planNetworkView(plan),
		"ssh":              plan.SSH,
		"cost":             plan.Cost,
		"warnings":         plan.Warnings,
		"compose_required": plan.ComposeRequired,
		"next":             "Show this plan and its monthly cost to the user. " + apply,
	}
	// deploy_apply appends these to authorized_keys of the VM: the user
	// approves them with the plan.
	if deployKey || params.SSHKey == eng.SSHPubKey {
		if keys, rejected, keysFile := loadUserKeyLines(eng.StateDir); len(keys)+len(rejected) > 0 {
			plan.UserKeysHash = userKeysHash(keys)
			view["user_ssh_keys"] = describeUserKeys(keys, rejected)
			view["user_ssh_keys_file"] = filepath.Join(filepath.Base(config.DataDir(eng.StateDir)), keysFile)
			view["next"] = "Show this plan, its monthly cost and the user_ssh_keys to the user: those keys get an SSH login on the VM, and the file may have come with a repository someone else wrote. " + apply
		}
	}
	if s.readOnly {
		view["note"] = "Not saved: this server runs with --read-only, so the plan has no plan_id and deploy_apply is refused here; " +
			"a server without --read-only makes the plan again with deploy_plan and applies it."
		view["next"] = "Show this plan and its monthly cost to the user; creating the VM needs a server without --read-only."
		return toJSON(view), false
	}
	if err := deployplan.Save(eng.StateDir, plan); err != nil {
		return errResult(err), true
	}
	view["plan_id"] = plan.PlanID
	view["expires_at"] = plan.ExpiresAt
	return toJSON(view), false
}

// planDefaults fills what the call left out from the configuration
// (profile, .evo.yaml, CLOUDRU_*), as deploy does: the zone from region,
// the flavor (unless the call sizes by cpu/ram), the image by image_id or
// image. deploy_plan ignored them and planned ru.AZ-2 while 'evo deploy'
// used the configured zone (release audit, A-002). Arguments of the call win.
func planDefaults(ctx context.Context, eng *engine.Engine, p deployplan.Params) (deployplan.Params, error) {
	cfg := eng.Config
	if p.Zone == "" {
		p.Zone = cfg.Region
	}
	if p.Flavor == "" && p.CPU == 0 && p.RAMGB == 0 && cfg.Flavor != "" && cfg.Flavor != "auto" {
		p.Flavor = cfg.Flavor
	}
	if p.Image == "" && eng.ImageRef() != "" {
		// By name, display name or UUID, as deploy resolves it.
		img, err := eng.BootImage(ctx, cfg.ProjectID)
		if err != nil {
			return p, err
		}
		p.Image = img.ID
	}
	return p, nil
}

// overlayString sets *dst from args[key] only if the key is present.
func overlayString(args map[string]any, key string, dst *string) {
	if _, ok := args[key]; ok {
		*dst = stringArg(args, key)
	}
}

// overlayInt sets *dst from args[key] only if the key is present.
func overlayInt(args map[string]any, key string, dst *int) {
	if _, ok := args[key]; ok {
		*dst = intArg(args, key, *dst)
	}
}

// recordDeployment records the VM of a deploy_apply in the local state, so
// status, list and destroy work on it. Best effort, as before: the answer
// names the VM either way.
func recordDeployment(stateDir string, maker *state.Maker, plan *deployplan.Plan, res *deployplan.ApplyResult, status string) error {
	return state.Update(state.DefaultPath(stateDir), stateLockWait, func(sf *state.File) error {
		now := time.Now()
		rec := state.DeploymentRecord{
			ID:           res.VMID,
			VMID:         res.VMID,
			VMIP:         res.PublicIP,
			FloatingIPID: res.FloatingIPID,
			CreatedBy:    maker,
			ProjectID:    plan.ProjectID,
			VMFlavor:     plan.VM.Flavor.Name,
			Region:       plan.VM.Zone.ID,
			SSHUser:      plan.SSH.Login,
			Status:       status,
			// Its way on, when unfinished, is deploy_apply with its plan
			// (D82, R-35).
			Source:             state.SourceDeployApply,
			PlanID:             plan.PlanID,
			SecurityGroupID:    res.Ports.SecurityGroupID,
			SecurityGroupPorts: res.Ports.SecurityGroupPorts,
			OpenedRules:        res.Ports.OpenedRules,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		// The record is as old as the plan the VM was made by, not the end
		// of the wait for it: a VM slow to SSH would be older than its own
		// record, and D73 would refuse its destroy (FB170-1). The plan's
		// time is the one ownVM took the VM as the plan's by.
		if !plan.CreatedAt.IsZero() && plan.CreatedAt.Before(now) {
			rec.CreatedAt = plan.CreatedAt
		}
		// An adopted VM opens nothing now: the rules an earlier apply
		// created stay with it, so destroy still deletes them.
		if old := sf.FindDeployment(res.VMID); old != nil {
			rec.CreatedAt = old.CreatedAt
			// A retry that could not read the VM's interface returns no
			// floating IP: the one recorded before stays, else destroy
			// could not find it and it stayed billed (release QA,
			// B-lifecycle 2).
			if rec.FloatingIPID == "" {
				rec.FloatingIPID = old.FloatingIPID
			}
			if rec.VMIP == "" {
				rec.VMIP = old.VMIP
			}
			if rec.SecurityGroupID == "" {
				rec.SecurityGroupID = old.SecurityGroupID
			}
			rec.SecurityGroupPorts = mergeStrings(old.SecurityGroupPorts, rec.SecurityGroupPorts)
			rec.OpenedRules = mergeRules(old.OpenedRules, rec.OpenedRules)
		}
		sf.UpsertDeployment(rec)
		return nil
	})
}

// stateLockWait is how long an MCP tool waits for a CLI deploy or destroy in
// the same directory to release the state before recording a VM it created.
const stateLockWait = 2 * time.Minute

// planDeployApply plans the execution of a previously-resolved plan: the
// plan from the local file only (no token, no request), the keys checked
// against it, the preview the user approves — the VM, the project, the SSH
// keys and the price deploy_plan quoted — not a pointer to an earlier
// answer (break 1.2, B12-8). A plan_id that is malformed, unknown or
// expired, and keys changed since the plan, are refused here, before the
// question: there is nothing to approve (D35; audit Fable 1.8, FB180-7).
func (s *Registry) planDeployApply(ctx context.Context, args map[string]any) (*Plan, error) {
	planID := stringArg(args, "plan_id")
	if planID == "" {
		return nil, usageErr("plan_id is required", "нужен plan_id")
	}
	eng, err := s.localEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	plan, err := deployplan.Load(eng.StateDir, planID)
	if err != nil {
		return nil, err
	}
	// The keys deploy_apply appends are the ones the plan showed (R-68):
	// a key added since gets no SSH login nobody saw — refused before
	// anything is written, as deploy refuses a compose file changed since
	// its consent.
	if err := eng.EnsureSSHKey(); err != nil {
		return nil, err
	}
	// The keys read here are the ones appended after the create: the file
	// is not read again, so a key written while the VM is made gets no
	// login either («Риск», conclusion 76).
	var approvedKeys []string
	approvedFile := ""
	if plan.PublicKey == eng.SSHPubKey {
		approvedKeys, approvedFile = loadUserKeys(eng.StateDir)
		if userKeysHash(approvedKeys) != plan.UserKeysHash {
			return nil, errUserKeysChangedSincePlan(plan.PlanID)
		}
	}
	preview := engine.PlanPreview(plan)
	preview.Add("ssh_public_key", describeVMKey(plan.PublicKey, plan.PublicKey == eng.SSHPubKey))
	if len(approvedKeys) > 0 {
		preview.Add("user_ssh_public_keys", userKeyNames(approvedKeys))
	}
	return &Plan{
		Preview: map[string]any{
			"message": i18n.Tf("deploy_apply executes plan %s: it creates VM %s (billable). preview has the VM, its SSH keys and the monthly cost deploy_plan quoted for it.",
				"deploy_apply выполнит план %s: создаст ВМ %s (платно). В preview — ВМ, её ключи SSH и цена в месяц, которую назвал deploy_plan.", plan.PlanID, plan.VM.Name),
			"plan_id":    plan.PlanID,
			"expires_at": plan.ExpiresAt,
			"preview":    preview,
		},
		Execute: func(ctx context.Context) (string, bool) {
			return s.applyDeployPlan(ctx, args, eng, plan, approvedKeys, approvedFile)
		},
	}, nil
}

// applyDeployPlan creates the VM of plan, with consent; approvedKeys are
// the user's keys the plan showed and planDeployApply checked.
func (s *Registry) applyDeployPlan(ctx context.Context, args map[string]any, eng *engine.Engine, plan *deployplan.Plan,
	approvedKeys []string, approvedFile string) (string, bool) {
	planID := plan.PlanID
	if err := authorize(ctx, eng); err != nil {
		return errResult(err), true
	}
	unlock, err := deployplan.LockApply(eng.StateDir, plan)
	if err != nil {
		return errResult(err), true
	}
	defer unlock()
	// The person at the CLI, and an MCP client that asked, see the stages
	// as they go, in the words of deploy (Caller.Progress, WithProgress).
	pp, _ := progressOf(ctx)
	show := &deployplan.Progress{P: pp, Wait: eng.VMWaitLine(pp), Ports: func(r sgrules.OpenResult, rec *state.DeploymentRecord) {
		engine.ShowOpenedPorts(pp, r, rec, cmp.Or(rec.SecurityGroupName, rec.SecurityGroupID))
	}}
	// The wait of an MCP call ends with its budget, of wait=false at once
	// (D69); the CLI waits as before.
	var until time.Time
	if left, capped := s.waitLeft(ctx, args); capped {
		// wall-clock: the end of the call's wait, D69.
		until = time.Now().Add(left)
	}
	// Only the folder's own VM is adopted (D73).
	plan.Owned = folderVMs(eng)
	res, err := deployplan.ApplyWithTimeout(ctx, eng.Cloud, plan, eng.VMPollTimeout(), until, show)
	var foreign *deployplan.ForeignVMError
	if stderrors.As(err, &foreign) {
		return foreignVMResult(foreign.Name, foreign.ID, foreign.Status)
	}
	if err != nil {
		// Interrupted after the VM was created: record it, so status and
		// destroy see it; the plan stays for a retry, which adopts the VM.
		var fields map[string]any
		if res != nil && res.VMID != "" {
			if rerr := recordDeployment(eng.StateDir, eng.Maker(), plan, res, "creating"); rerr != nil {
				err = fmt.Errorf(i18n.T("%w (VM %s is not in the state either: %v)", "%w (ВМ %s тоже нет в state: %v)"), err, res.VMID, rerr)
			}
			// The VM, the plan that finishes it and its password, which
			// the repeat does not give (D69).
			fields = map[string]any{"vm_id": res.VMID, "plan_id": planID}
			if res.Password != "" {
				fields["password"] = res.Password
				fields["note"] = strings.TrimSpace(passwordOnce)
			}
		}
		return errAnswer(err, fields), true
	}

	// Record the VM in local state so status/list/destroy work on it. The
	// plan goes only after that: if the state cannot be written, a retry
	// of the same plan adopts the VM instead of losing it.
	if res.VMID != "" {
		if err := recordDeployment(eng.StateDir, eng.Maker(), plan, res, res.Status); err != nil {
			return errResult(fmt.Errorf("VM %s was created, but the state file could not be saved (%w); "+
				"it is running and billed — call deploy_apply again with the same plan_id once the state is writable, "+
				"whether to delete it instead (destroy vm_id %s, or the Cloud.ru console) — ask the person", res.VMID, err, res.VMID)), true
		}
	}
	// The plan stays until the VM runs: a timeout or a VM still creating
	// is finished by the same plan_id (the VM is adopted, its IP and ports
	// set then); answering such a VM as a success and dropping the plan made
	// that retry impossible (release QA, B-lifecycle 3). A VM in error does
	// not get better: its plan goes, and the answer says to destroy it.
	switch res.Status {
	case "running":
		deployplan.Delete(eng.StateDir, planID)
	case "creating":
		// Cut by the call's budget or wait=false: not an error, the state
		// so far and the plan to repeat, which finishes the VM (D69).
		out := map[string]any{"status": "creating", "vm_id": res.VMID, "vm_status": res.VMStatus, "plan_id": planID,
			"elapsed_sec": res.ElapsedSec,
			"next":        fmt.Sprintf("The VM is still being created. Call deploy_apply with plan_id %s and confirmed=true again: it adopts this VM (no second one), waits and finishes it (public IP, SSH, ports); vm_get vm_id=%s shows its state.", planID, res.VMID)}
		nextStep(out, "deploy_apply", map[string]any{"plan_id": planID, "confirmed": true})
		if res.Password != "" {
			out["password"] = res.Password
			out["next"] = out["next"].(string) + passwordOnce
		}
		return toJSON(out), false
	case "error":
		deployplan.Delete(eng.StateDir, planID)
		return errAnswer(clierrors.New(clierrors.CodeVMProvisionFailed,
			i18n.Tf("VM %s is in an error state: %s", "ВМ %s в состоянии ошибки: %s", res.VMID, res.Note),
			i18n.Tf("Whether to delete it (destroy vm_id %s) — ask the person; then run deploy_plan again (in another zone if it repeats).", "Удалять ли её (destroy vm_id %s) — спросите человека; затем снова выполните deploy_plan (при повторе — в другой зоне).", res.VMID)),
			map[string]any{"vm_id": res.VMID}), true
	case "timeout":
		// The password is made for this create and kept nowhere: the retry
		// adopts the VM without it, so it goes out now (Grok, PR #87).
		fields := map[string]any{"vm_id": res.VMID, "vm_status": res.Status, "plan_id": planID}
		hint := fmt.Sprintf("Call deploy_apply again with the same plan_id %s in a few minutes: it adopts the VM and finishes it (IP, SSH, ports); it is billed meanwhile; whether to delete it instead (destroy vm_id %s) — ask the person.", planID, res.VMID)
		if res.Password != "" {
			fields["password"] = res.Password
			hint += passwordOnce
		}
		return errAnswer(clierrors.New(clierrors.CodeVMProvisionFailed,
			i18n.Tf("VM %s is not running yet: %s", "ВМ %s ещё не запущена: %s", res.VMID, res.Note), hint), fields), true
	default:
		// An adopted VM that is stopped (or stopping, shut off, …) does not
		// start by itself: retrying the plan would answer the same.
		return errAnswer(clierrors.New(clierrors.CodeVMProvisionFailed,
			fmt.Sprintf("VM %s is %s: %s", res.VMID, res.Status, res.Note),
			fmt.Sprintf("Ask the user to start it in the Cloud.ru console, then call deploy_apply again with the same plan_id %s; whether to delete it instead (destroy vm_id %s) — ask the person. Retrying before it runs answers the same.", planID, res.VMID)),
			map[string]any{"vm_id": res.VMID, "vm_status": res.Status, "plan_id": planID}), true
	}

	// If we injected the agent's own deploy key (EnsureSSHKey above), hand back an ssh command that
	// uses it (deployKeySSH, as provision does). The host key is checked
	// against the project's known_hosts:
	// pinned from the first connection when we make one (user keys), else
	// recorded by the first ssh (accept-new). The entry of an earlier VM on
	// the same reused IP is dropped first, so the new VM is not refused.
	if res.PublicIP != "" && plan.PublicKey == eng.SSHPubKey {
		dataDir := config.DataDir(eng.StateDir)
		note := "Connect with the ssh command above (agent deploy key)."
		hostKey := ""
		if keys, keysFile := approvedKeys, approvedFile; len(keys) > 0 {
			hk, kErr := appendKeys(ctx, res.PublicIP, plan.SSH.Login, eng.SSHKeyPEM, keys)
			if kErr != nil {
				note += fmt.Sprintf(" WARNING: user key auto-append failed (%v) — append the public key to ~/.ssh/authorized_keys on the VM manually.", kErr)
			} else {
				hostKey = hk
				note += fmt.Sprintf(" User SSH access configured automatically (%d key(s) from %s/%s).", len(keys), filepath.Base(dataDir), keysFile)
			}
		} else {
			note += " No " + filepath.Base(dataDir) + "/" + userKeysFile + " configured — to give the user SSH access, append their public key to ~/.ssh/authorized_keys on the VM after install."
		}
		ssh, warning := deployKeySSH(eng.StateDir, plan.SSH.Login, res.PublicIP, hostKey)
		res.SSH = ssh
		note += warning
		if res.Note == "" {
			res.Note = note
		} else {
			res.Note = res.Note + "; " + note
		}
	}
	return toJSON(res), false
}

// describeVMKey names the key a VM of deploy_apply is created with: its
// type, SHA256 fingerprint and comment, and whose it is — the agent's
// deploy key of the folder or the ssh_key of the plan. A text that is no
// authorized_keys entry is named as it is: it is a public key, or nothing.
func describeVMKey(pub string, deployKey bool) string {
	if pub == "" {
		return ""
	}
	name := keyName(pub)
	if deployKey {
		return name + i18n.T(" (the agent's deploy key of evo)", " (ключ развёртывания evo для агента)")
	}
	return name + i18n.T(" (ssh_key of the plan)", " (ssh_key плана)")
}

// userKeyNames names the keys deploy_apply appends to authorized_keys of
// the VM, as describeUserKeys does for deploy_plan, one string each.
func userKeyNames(keys []string) []any {
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, keyName(k))
	}
	return out
}

// keyName is an authorized_keys entry in words: type, SHA256 fingerprint,
// comment and options (command="…" runs instead of the login shell). The
// comment and the options are text of .evo/user_authorized_keys, which may
// have come with a repository someone else wrote: quoted as data, as text
// from the cloud is (quoteCloud; «Риск», conclusion 92). An entry that does
// not parse is quoted whole.
func keyName(entry string) string {
	pub, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(entry))
	if err != nil {
		return quoteCloud(entry)
	}
	parts := []string{pub.Type(), ssh.FingerprintSHA256(pub)}
	if comment != "" {
		parts = append(parts, quoteCloud(comment))
	}
	if len(options) > 0 {
		parts = append(parts, "options "+quoteCloud(strings.Join(options, ",")))
	}
	return strings.Join(parts, " ")
}

func mergeStrings(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, s := range b {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func mergeRules(a, b []state.SGRule) []state.SGRule {
	out := append([]state.SGRule(nil), a...)
	for _, r := range b {
		if !slices.ContainsFunc(out, func(o state.SGRule) bool { return o.ID == r.ID }) {
			out = append(out, r)
		}
	}
	return out
}

// FlavorsNote is the note of list_flavors; the CLI translates it by this
// text (cli.listNoteRU).
const FlavorsNote = "est_monthly_rub is a rate-card estimate incl. 10GB SSD, not a billed price; cpu_share is the guaranteed share of a vCPU: 1:1 — 100%, 1:3 — 30%, 1:10 — 10% (the Cloud.ru specification); est_note marks an estimate that does not count the share"

// handleListFlavors lists flavors with a monthly cost estimate each.
func (s *Registry) handleListFlavors(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	flavors, err := eng.Cloud.ListFlavors(ctx)
	if err != nil {
		return errResult(err), true
	}
	// Small ones first, GPU last: the CLI table and an agent both pick
	// from the top. A copy: the client may hand out a cached slice.
	flavors = slices.Clone(flavors)
	provision.SortFlavors(flavors)
	out := make([]map[string]any, 0, len(flavors))
	for _, f := range flavors {
		if !flavorMatches(f, args) {
			continue
		}
		est := pricing.EstimateFor(f.Name, f.Share, f.VCPUs, f.RAMGB, 10, "SSD")
		row := map[string]any{
			"name":            f.Name,
			"vcpus":           f.VCPUs,
			"ram_gb":          f.RAMGB,
			"free_tier":       f.FreeTier,
			"est_monthly_rub": est.TotalMonthlyRub,
		}
		// Only what the catalogue sent (С5): no field, no key.
		if f.Share != "" {
			row["cpu_share"] = f.Share
		}
		// The card prices a vCPU at one rate: a share below a core is
		// priced as a whole one (Ц-5, FB15-10).
		if f.Share != "" && f.Share != "1:1" && est.Source == pricing.SourceVCPURAM {
			row["est_note"] = "estimate without the vCPU share: the tariff prices a shared vCPU lower (tariff_url)"
		}
		if f.GPU != nil {
			row["gpu"] = *f.GPU
		}
		if f.Type != nil {
			row["spot"] = f.Spot()
		}
		if z := f.ZoneNames(); z != nil {
			row["zones"] = z
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{
		"flavors": out,
		"note":    FlavorsNote,
		// The rates of a shared vCPU, which the estimate does not know.
		"tariff_url": engine.TariffURL(engine.ResourceVM),
	}), false
}

// flavorMatches reports whether f passes the filters of list_flavors: each
// one set keeps only the flavors whose catalogue entry says so — a flavor
// the catalogue says nothing of on the point does not pass it.
func flavorMatches(f *provision.Flavor, args map[string]any) bool {
	if n := intArg(args, "cpu", 0); n > 0 && f.VCPUs != n {
		return false
	}
	if n := intArg(args, "ram", 0); n > 0 && f.RAMGB != n {
		return false
	}
	if s := stringArg(args, "share"); s != "" && f.Share != s {
		return false
	}
	// The zone by its name in any case or its id, as D67 matches it.
	if z := stringArg(args, "zone"); z != "" && !provision.AvailableIn(f, z, z) {
		return false
	}
	if g, ok := args["gpu"].(bool); ok && (f.GPU == nil || (*f.GPU > 0) != g) {
		return false
	}
	if s, ok := args["spot"].(bool); ok && (f.Type == nil || f.Spot() != s) {
		return false
	}
	return true
}

// handleListDiskTypes lists disk types (id + name).
func (s *Registry) handleListDiskTypes(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	dts, err := eng.Cloud.ListDiskTypes(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(dts))
	for _, d := range dts {
		row := map[string]any{"id": d.ID, "name": d.Name}
		// Only what the catalogue sent (С5): no field, no key (D54,
		// FB15-6: what a disk of the type may be, before it is asked for).
		if d.MinSize != nil {
			row["min_size_gb"] = *d.MinSize
		}
		if d.MaxSize != nil {
			row["max_size_gb"] = *d.MaxSize
		}
		if d.FreeTier != nil {
			row["free_tier"] = *d.FreeTier
		}
		if z := d.ZoneNames(); z != nil {
			row["zones"] = z
		}
		if d.SpeedLimits != nil {
			row["speed_limits"] = d.SpeedLimits
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"disk_types": out}), false
}

// handleListSubnets lists subnets (id, name, zone).
func (s *Registry) handleListSubnets(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	subnets, err := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(subnets))
	for _, s := range subnets {
		item := map[string]any{"id": s.ID, "name": s.Name, "availability_zone_id": s.AvailabilityZoneID}
		// The name of the zone, read with the subnet (Линия CLI).
		if s.AvailabilityZoneName != "" {
			item["availability_zone_name"] = s.AvailabilityZoneName
		}
		// Only what the list sent: no field, no key.
		if s.State != "" {
			item["state"] = s.State
		}
		if s.VPCID != "" {
			item["vpc_id"] = s.VPCID
		}
		out = append(out, item)
	}
	return toJSON(map[string]any{"subnets": out}), false
}

// handleListSecurityGroups lists security groups (id, name, zone), with
// each group's rules when rules=true (one more API call per group).
func (s *Registry) handleListSecurityGroups(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	sgs, err := eng.Cloud.ListSecurityGroups(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	withRules, _ := args["rules"].(bool)
	out := make([]map[string]any, 0, len(sgs))
	for _, g := range sgs {
		item := map[string]any{"id": g.ID, "name": g.Name, "availability_zone_id": g.AvailabilityZoneID}
		if g.AvailabilityZoneName != "" {
			item["availability_zone_name"] = g.AvailabilityZoneName
		}
		if withRules {
			rules, err := eng.Cloud.ListSecurityGroupRules(ctx, g.ID)
			if err != nil {
				return errResult(fmt.Errorf(i18n.T("rules of security group %s: %w", "правила группы безопасности %s: %w"), g.Name, err)), true
			}
			if rules == nil {
				rules = []*provision.SGRule{}
			}
			item["rules"] = rules
		}
		out = append(out, item)
	}
	return toJSON(map[string]any{"security_groups": out}), false
}

// handleOpenPorts adds ingress rules for the given ports to a security group.
// planOpenPorts plans open_ports (gate.go): the group and its rules are read
// before the question, a group that is not there is refused, and the
// preview says the protocol and the source the rules open the ports to
// (0.0.0.0/0 by default) and what is open already (G13-23, D35).
func (s *Registry) planOpenPorts(ctx context.Context, args map[string]any) (*Plan, error) {
	sgRef := stringArg(args, "security_group")
	ports, err := portsArg("ports", args["ports"])
	if err != nil {
		return nil, err
	}
	if sgRef == "" || len(ports) == 0 {
		return nil, usageErr("security_group and ports are required", "нужны security_group и ports")
	}
	proto := firstNonEmptyStr(stringArg(args, "protocol"), "tcp")
	cidr := firstNonEmptyStr(stringArg(args, "cidr"), "0.0.0.0/0")
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	target, err := securityGroupByRef(ctx, eng, sgRef)
	if err != nil {
		return nil, err
	}
	// The group list carries no rules: read them, as deploy does. A port
	// is open already when a rule lets it in from cidr or wider, of the
	// protocol (release audit, L-25) — one rule with deploy (D52).
	rules, _ := eng.Cloud.ListSecurityGroupRules(ctx, target.ID)
	willOpen, alreadyOpen, wider := []string{}, []string{}, []string{}
	for _, p := range ports {
		switch covered, w := sgrules.Coverage(rules, proto, p, cidr); {
		case !covered:
			willOpen = append(willOpen, strconv.Itoa(p))
		case w:
			wider = append(wider, strconv.Itoa(p))
			fallthrough
		default:
			alreadyOpen = append(alreadyOpen, strconv.Itoa(p))
		}
	}
	message := i18n.Tf("open_ports would open %s/%s from %s on security group %q (shared per zone — affects every VM in it).",
		"open_ports откроет %s/%s из %s в группе безопасности %q (она общая для зоны — затронет каждую ВМ в ней).",
		strings.Join(willOpen, ","), proto, cidr, target.Name)
	if len(willOpen) == 0 {
		// Nothing to open: the question stays until major (owner's decision
		// 06.11, variant Г; a repeat without it is the class of D81), and
		// the preview says the consent changes nothing.
		message = i18n.Tf("Every port asked for is already open on security group %q (%s/%s from %s): the consent changes nothing, no rule is added.",
			"Все запрошенные порты уже открыты в группе безопасности %q (%s/%s из %s): подтверждение ничего не изменит, правила не добавятся.",
			target.Name, strings.Join(alreadyOpen, ","), proto, cidr)
	}
	preview := map[string]any{
		"message":        message,
		"security_group": target.Name,
		"protocol":       proto,
		"cidr":           cidr,
		"will_open":      willOpen,
		"already_open":   alreadyOpen,
	}
	if len(wider) > 0 {
		// Open by a rule wider than asked (a range, any protocol, a wider
		// source): said, and no second rule made.
		preview["already_open_wider"] = wider
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {

		var opened, skipped, skippedWider, failed []string
		// Why each port failed: the answer said status ok with failed ports and
		// no reason, exit 0, when every rule was refused (QA 20.10, QA-3).
		failures := map[string]string{}
		var firstErr error
		openedHere := map[int]bool{}
		for _, p := range ports {
			pr := fmt.Sprintf("%d:%d", p, p)
			if covered, wider := sgrules.Coverage(rules, proto, p, cidr); covered || openedHere[p] {
				skipped = append(skipped, strconv.Itoa(p))
				if wider {
					skippedWider = append(skippedWider, strconv.Itoa(p))
				}
				continue
			}
			rule := provision.SGRule{
				Direction:      "ingress",
				EtherType:      "IPv4",
				IPProtocol:     proto,
				PortRange:      pr,
				RemoteIPPrefix: cidr,
				Description:    "opened via open_ports",
			}
			if _, e := eng.Cloud.AddSecurityGroupRule(ctx, target.ID, eng.Config.ProjectID, rule); e != nil {
				failed = append(failed, strconv.Itoa(p))
				failures[strconv.Itoa(p)] = e.Error()
				if firstErr == nil {
					firstErr = fmt.Errorf(i18n.T("open port %d: %w", "открытие порта %d: %w"), p, e)
				}
			} else {
				opened = append(opened, strconv.Itoa(p))
				// Open now: the port is not opened a second time (R4-19).
				openedHere[p] = true
			}
		}
		res := map[string]any{
			"status":         "ok",
			"security_group": target.Name,
			"protocol":       proto,
			"cidr":           cidr,
			"opened":         opened,
			"already_open":   skipped,
			"failed":         failed,
		}
		if len(skippedWider) > 0 {
			// As the preview said it (already_open_wider).
			res["already_open_wider"] = skippedWider
		}
		if firstErr == nil {
			return toJSON(res), false
		}
		res["failures"] = failures
		if len(opened) == 0 {
			// Nothing opened: an error, its reason from the first refusal (an
			// API status: API_ERROR, AUTH_FAILED, ...).
			res["status"] = "error"
			return errAnswer(firstErr, res), true
		}
		// Some opened, some not: partial, as disk_create and destroy say it;
		// the reason is the first refusal's.
		res["status"] = "partial"
		return errAnswer(fmt.Errorf(i18n.T("opened %s, not opened %s: %w", "открыты %s, не открыты %s: %w"), strings.Join(opened, ","), strings.Join(failed, ","), firstErr), res), true
	}}, nil
}

// securityGroupByRef is the project's security group named or numbered
// ref; one that is not there is NOT_FOUND, and a name several groups share
// is USAGE naming their ids.
func securityGroupByRef(ctx context.Context, eng *engine.Engine, ref string) (*provision.SecurityGroup, error) {
	sgs, err := eng.Cloud.ListSecurityGroups(ctx, eng.Config.ProjectID)
	if err != nil {
		return nil, err
	}
	var named []*provision.SecurityGroup
	for _, g := range sgs {
		if g.ID == ref {
			return g, nil
		}
		if g.Name == ref {
			named = append(named, g)
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
	default:
		// The first the list gave was taken, and its rules were the ones
		// changed (review of #315).
		ids := make([]string, 0, len(named))
		for _, g := range named {
			ids = append(ids, g.ID)
		}
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%d security groups are named %q: %s", "%d групп безопасности с именем %q: %s", len(named), ref, strings.Join(ids, ", ")),
			i18n.T("Name the group by its id (list_security_groups).", "Укажите группу по id (list_security_groups)."))
	}
	return nil, clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("security group %q not found", "группа безопасности %q не найдена", ref),
		i18n.T("list_security_groups lists the project's groups.", "Группы проекта — в выводе list_security_groups."))
}

// planClosePorts plans close_ports (gate.go), the way back from open_ports
// (G13-9, D37): the ingress rules of exactly the ports, protocol and source
// given, read before the question. A group that is not there is the call's
// error; no rule to close is already deleted (D26), with no question. The
// group is shared per zone: the preview says so, as open_ports' does.
func (s *Registry) planClosePorts(ctx context.Context, args map[string]any) (*Plan, error) {
	sgRef := stringArg(args, "security_group")
	ports, err := portsArg("ports", args["ports"])
	if err != nil {
		return nil, err
	}
	if sgRef == "" || len(ports) == 0 {
		return nil, usageErr("security_group and ports are required", "нужны security_group и ports")
	}
	proto := firstNonEmptyStr(stringArg(args, "protocol"), "tcp")
	cidr := firstNonEmptyStr(stringArg(args, "cidr"), "0.0.0.0/0")
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	target, err := securityGroupByRef(ctx, eng, sgRef)
	if err != nil {
		return nil, err
	}
	rules, err := eng.Cloud.ListSecurityGroupRules(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	type closing struct {
		port int
		id   string
	}
	var toClose []closing
	var closedAlready []string
	for _, p := range ports {
		found := false
		for _, r := range rules {
			if r.Direction == "ingress" && r.IPProtocol == proto && r.PortRange == fmt.Sprintf("%d:%d", p, p) && r.RemoteIPPrefix == cidr {
				toClose = append(toClose, closing{p, r.ID})
				found = true
			}
		}
		if !found {
			closedAlready = append(closedAlready, strconv.Itoa(p))
		}
	}
	if len(toClose) == 0 {
		ref := fmt.Sprintf("%s/%s from %s", strings.Join(closedAlready, ","), proto, cidr)
		return answered(s.deleteFailedAs("close_ports", "ports", ref, clierrors.New(clierrors.CodeNotFound,
			i18n.Tf("no rule %s in security group %q", "правила %s в группе безопасности %q нет", ref, target.Name), ""))), nil
	}
	listed := make([]map[string]any, 0, len(toClose))
	for _, c := range toClose {
		listed = append(listed, map[string]any{"id": c.id, "port": c.port, "protocol": proto, "cidr": cidr})
	}
	preview := map[string]any{
		"message": i18n.Tf("close_ports would delete %d rule(s) of security group %q (shared per zone — affects every VM in it).",
			"close_ports удалит правила (%d) группы безопасности %q (она общая для зоны — затронет каждую ВМ в ней).", len(toClose), target.Name),
		"security_group": target.Name,
		"rules":          listed,
		"already_closed": closedAlready,
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		var closed, failed []string
		failures := map[string]string{}
		var firstErr error
		for _, c := range toClose {
			port := strconv.Itoa(c.port)
			// A rule gone meanwhile is closed (D26).
			if e := eng.Cloud.DeleteSecurityGroupRule(ctx, target.ID, c.id); e != nil && !isGone(e) {
				failed = append(failed, port)
				failures[port] = e.Error()
				if firstErr == nil {
					firstErr = fmt.Errorf(i18n.T("close port %d: %w", "закрытие порта %d: %w"), c.port, e)
				}
				continue
			}
			closed = append(closed, port)
		}
		res := map[string]any{
			"status":         "ok",
			"security_group": target.Name,
			"protocol":       proto,
			"cidr":           cidr,
			"closed":         closed,
			"already_closed": closedAlready,
			"failed":         failed,
		}
		if firstErr == nil {
			return toJSON(res), false
		}
		res["failures"] = failures
		res["status"] = "partial"
		if len(closed) == 0 {
			res["status"] = "error"
			return errAnswer(firstErr, res), true
		}
		return errAnswer(fmt.Errorf(i18n.T("closed %s, not closed %s: %w", "закрыты %s, не закрыты %s: %w"), strings.Join(closed, ","), strings.Join(failed, ","), firstErr), res), true
	}}, nil
}

// planReleaseFloatingIP plans the release of a floating IP (gate.go), refusing one in use.
func (s *Registry) planReleaseFloatingIP(ctx context.Context, args map[string]any) (*Plan, error) {
	// Both: the id was taken and the address dropped without a word —
	// `evo vpc fip-release <id> --ip-address X` released the id's address
	// (QA 20.10, QA-8). One of only spaces is USAGE too, as in vm_get
	// (break r4, R4-11): id=" " alone went on to the lookup.
	ref, key, err := oneOfArgs(args, clierrors.New(clierrors.CodeUsage,
		i18n.T("pass id or ip_address, not both", "укажите id или ip_address, не оба"),
		i18n.T("The id comes from list_floating_ips; ip_address finds the floating IP by its address.",
			"id — из list_floating_ips; по ip_address floating IP ищется по адресу.")), "id", "ip_address")
	if err != nil {
		return nil, err
	}
	var id, ip string
	switch key {
	case "id":
		id = ref
	case "ip_address":
		ip = ref
	default:
		return nil, usageErr("provide either 'id' or 'ip_address'", "укажите 'id' или 'ip_address'")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	projectID := eng.Config.ProjectID

	// Resolve to a concrete FIP (id, address, state), before the question:
	// one that is not there is already released (D26).
	var fipID, fipIP, state string
	onNAT := false
	if id != "" {
		f, e := eng.Cloud.GetFloatingIP(ctx, id)
		if e != nil {
			return answered(s.deleteFailed("release_floating_ip", id, fmt.Errorf(i18n.T("floating IP %q: %w", "адрес floating IP %q: %w"), id, e))), nil
		}
		fipID, fipIP, state, onNAT = id, f.IPAddress, f.State, f.OnNATGateway()
	} else {
		fips, e := eng.Cloud.ListFloatingIPs(ctx, projectID)
		if e != nil {
			return nil, e
		}
		for _, f := range fips {
			if f.IPAddress == ip {
				fipID, fipIP, state, onNAT = f.ID, f.IPAddress, f.State, f.OnNATGateway()
				break
			}
		}
		if fipID == "" {
			return answered(s.deleteFailedAs("release_floating_ip", "ip_address", ip, clierrors.New(clierrors.CodeNotFound,
				i18n.Tf("no floating IP with address %q in this project", "в проекте нет floating IP с адресом %q", ip), ""))), nil
		}
	}

	// A SNAT gateway's address goes with the gateway: the API refuses to
	// release it alone (422 floating_ip_address_is_used_by_nat_gateway).
	// A refusal is local, nothing was released: USAGE (exit 1) with the
	// reason; it had no reason and the CLI exited 5, API_ERROR (QA-6).
	// Both refusals come before the consent: the call cannot go through
	// whatever the person answers (A6, D31).
	if onNAT {
		msg := fmt.Sprintf("floating IP %s (%s) belongs to a SNAT gateway and is released with it (snat_delete) — whether to delete the gateway, ask the person; not released", fipIP, fipID)
		return answered(errAnswer(clierrors.New(clierrors.CodeUsage, msg,
			i18n.T("It goes with its SNAT gateway: whether to delete the gateway (snat_delete) — ask the person.", "Он освободится вместе с SNAT-шлюзом: удалять ли шлюз (snat_delete) — спросите человека.")),
			map[string]any{"status": "refused", "message": msg, "id": fipID, "ip_address": fipIP}), true), nil
	}

	// Safety: never release an IP that is attached to a VM.
	if state == "in_use" {
		msg := fmt.Sprintf("floating IP %s (%s) is in use by a VM — detach it from the VM first; not released", fipIP, fipID)
		return answered(errAnswer(clierrors.New(clierrors.CodeUsage, msg,
			i18n.T("Detach it from the VM, then release it.", "Отвяжите его от ВМ, затем освободите.")),
			map[string]any{"status": "refused", "message": msg, "id": fipID, "ip_address": fipIP}), true), nil
	}

	if refused := passingRefusal(fipIP, fipID, state, map[string]any{"id": fipID, "ip_address": fipIP, "status": state}); refused != nil {
		return refused, nil
	}
	preview := map[string]any{"message": i18n.Tf("release_floating_ip releases floating IP %s: the address leaves the project.",
		"release_floating_ip освободит floating IP %s: адрес уйдёт из проекта.", ref),
	}
	if warning, _ := engine.DeleteWayOut(state, fipID); warning != "" {
		preview["warning"] = warning
	}
	// The cloud gives no time a floating IP was made: a recorded one is
	// the folder's, as AdoptedVM takes a VM without a time (P3).
	preview["provenance"] = folderProvenance(eng.StateDir, engine.ProvenanceFloatingIP, fipID, time.Time{})
	return &Plan{Preview: preview, Execute: withWayOut(func(ctx context.Context) (string, bool) {
		if err := eng.Cloud.DeleteFloatingIP(ctx, fipID, projectID); err != nil {
			return s.deleteFailed("release_floating_ip", fipID, fmt.Errorf(i18n.T("release floating IP: %w", "освобождение floating IP: %w"), err))
		}
		return toJSON(map[string]any{
			"status":   "ok",
			"released": fipIP,
			"id":       fipID,
			"message":  fmt.Sprintf("floating IP %s released", fipIP),
		}), false
	}, state, state, fipID)}, nil
}

// folderProvenance is engine.ProvenanceOf by the state of the folder dir,
// as it reads: an unread state is said in the answer, not failed on.
func folderProvenance(dir, kind, id string, made time.Time) engine.Provenance {
	sf, err := state.Read(state.DefaultPath(dir))
	return engine.ProvenanceOf(kind, id, made, nil, sf, err)
}

// handleListFloatingIPs lists floating IPs (address, state, availability).
func (s *Registry) handleListFloatingIPs(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	fips, err := eng.Cloud.ListFloatingIPs(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(fips))
	for _, f := range fips {
		row := map[string]any{
			"id":          f.ID,
			"name":        f.Name,
			"ip_address":  f.IPAddress,
			"state":       f.State,
			"available":   f.Available(),
			"nat_gateway": f.OnNATGateway(),
		}
		// The zone only as the answer names it (D54, FB15-18): an address
		// binds to a VM of its own zone.
		if f.Zone != nil && f.Zone.Name != "" {
			row["zone"] = f.Zone.Name
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"floating_ips": out}), false
}

// planNetworkView is the network of deploy_plan's answer: the floating IP
// and, with ports, the ones it opens, the source and the zone's shared
// group they open in (P5).
func planNetworkView(plan *deployplan.Plan) map[string]any {
	view := map[string]any{"floating_ip": plan.Network.FloatingIP, "open_ports": plan.Ports}
	if len(plan.Ports) > 0 {
		view["ports_source"] = sgrules.Source(plan.PortsSource)
		view["security_group"] = firstNonEmptyStr(plan.Network.SecurityGroupName, plan.Network.SecurityGroupID)
		view["security_group_shared"] = true
	}
	return view
}
