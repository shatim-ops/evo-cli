// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"cmp"
	"context"
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
		"network":          map[string]any{"floating_ip": plan.Network.FloatingIP, "open_ports": plan.Ports},
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
func recordDeployment(stateDir string, plan *deployplan.Plan, res *deployplan.ApplyResult, status string) error {
	return state.Update(state.DefaultPath(stateDir), stateLockWait, func(sf *state.File) error {
		now := time.Now()
		rec := state.DeploymentRecord{
			ID:                 res.VMID,
			VMID:               res.VMID,
			VMIP:               res.PublicIP,
			FloatingIPID:       res.FloatingIPID,
			ProjectID:          plan.ProjectID,
			VMFlavor:           plan.VM.Flavor.Name,
			Region:             plan.VM.Zone.ID,
			SSHUser:            plan.SSH.Login,
			Status:             status,
			SecurityGroupID:    res.Ports.SecurityGroupID,
			SecurityGroupPorts: res.Ports.SecurityGroupPorts,
			OpenedRules:        res.Ports.OpenedRules,
			CreatedAt:          now,
			UpdatedAt:          now,
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

// handleDeployApply executes a previously-resolved plan, gated on confirmation.
func (s *Registry) handleDeployApply(ctx context.Context, args map[string]any) (string, bool) {
	planID := stringArg(args, "plan_id")
	if planID == "" {
		return usageT("plan_id is required", "нужен plan_id"), true
	}
	// The plan before the gate, from the local file only (no token, no
	// request): the preview is what the user approves — the VM, the project
	// and the price deploy_plan quoted — not a pointer to an earlier answer
	// (break 1.2, B12-8). A plan_id that is malformed, unknown or expired is
	// refused here, confirmed or not: there is nothing to approve.
	eng, err := s.localEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	plan, err := deployplan.Load(eng.StateDir, planID)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("deploy_apply executes plan %s: it creates VM %s (billable). preview has the VM and the monthly cost deploy_plan quoted for it.",
				"deploy_apply выполнит план %s: создаст ВМ %s (платно). В preview — ВМ и цена в месяц, которую назвал deploy_plan.", plan.PlanID, plan.VM.Name),
			"plan_id":    plan.PlanID,
			"expires_at": plan.ExpiresAt,
			"preview":    engine.PlanPreview(plan),
		}), true
	}

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
	res, err := deployplan.ApplyWithTimeout(ctx, eng.Cloud, plan, eng.VMPollTimeout(), show)
	if err != nil {
		// Interrupted after the VM was created: record it, so status and
		// destroy see it; the plan stays for a retry, which adopts the VM.
		if res != nil && res.VMID != "" {
			if rerr := recordDeployment(eng.StateDir, plan, res, "creating"); rerr != nil {
				err = fmt.Errorf("%w (VM %s is not in the state either: %v)", err, res.VMID, rerr)
			}
		}
		return errResult(err), true
	}

	// Record the VM in local state so status/list/destroy work on it. The
	// plan goes only after that: if the state cannot be written, a retry
	// of the same plan adopts the VM instead of losing it.
	if res.VMID != "" {
		if err := recordDeployment(eng.StateDir, plan, res, res.Status); err != nil {
			return errResult(fmt.Errorf("VM %s was created, but the state file could not be saved (%w); "+
				"it is running and billed — call deploy_apply again with the same plan_id once the state is writable, "+
				"or delete the VM with destroy (vm_id %s) or in the Cloud.ru console", res.VMID, err, res.VMID)), true
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
	case "error":
		deployplan.Delete(eng.StateDir, planID)
		return errAnswer(clierrors.New(clierrors.CodeVMProvisionFailed,
			i18n.Tf("VM %s is in an error state: %s", "ВМ %s в состоянии ошибки: %s", res.VMID, res.Note),
			i18n.Tf("Delete it with destroy (vm_id %s), then run deploy_plan again (in another zone if it repeats).", "Удалите её через destroy (vm_id %s) и снова выполните deploy_plan (при повторе — в другой зоне).", res.VMID)),
			map[string]any{"vm_id": res.VMID}), true
	case "timeout":
		// The password is made for this create and kept nowhere: the retry
		// adopts the VM without it, so it goes out now (Grok, PR #87).
		fields := map[string]any{"vm_id": res.VMID, "vm_status": res.Status, "plan_id": planID}
		hint := fmt.Sprintf("Call deploy_apply again with the same plan_id %s in a few minutes: it adopts the VM and finishes it (IP, SSH, ports); it is billed meanwhile, destroy (vm_id %s) deletes it.", planID, res.VMID)
		if res.Password != "" {
			fields["password"] = res.Password
			hint += " The VM password is in this answer only: give it to the user now, the retry does not show it."
		}
		return errAnswer(clierrors.New(clierrors.CodeVMProvisionFailed,
			i18n.Tf("VM %s is not running yet: %s", "ВМ %s ещё не запущена: %s", res.VMID, res.Note), hint), fields), true
	default:
		// An adopted VM that is stopped (or stopping, shut off, …) does not
		// start by itself: retrying the plan would answer the same.
		return errAnswer(clierrors.New(clierrors.CodeVMProvisionFailed,
			fmt.Sprintf("VM %s is %s: %s", res.VMID, res.Status, res.Note),
			fmt.Sprintf("Ask the user to start it in the Cloud.ru console, then call deploy_apply again with the same plan_id %s; or delete it with destroy (vm_id %s). Retrying before it runs answers the same.", planID, res.VMID)),
			map[string]any{"vm_id": res.VMID, "vm_status": res.Status, "plan_id": planID}), true
	}

	if err := eng.EnsureSSHKey(); err != nil {
		return errResult(err), true
	}
	// If we injected the agent's own deploy key, hand back an ssh command that
	// uses it (deployKeySSH, as provision does). The host key is checked
	// against the project's known_hosts:
	// pinned from the first connection when we make one (user keys), else
	// recorded by the first ssh (accept-new). The entry of an earlier VM on
	// the same reused IP is dropped first, so the new VM is not refused.
	if res.PublicIP != "" && plan.PublicKey == eng.SSHPubKey {
		dataDir := config.DataDir(eng.StateDir)
		note := "Connect with the ssh command above (agent deploy key)."
		hostKey := ""
		if keys, keysFile := loadUserKeys(eng.StateDir); len(keys) > 0 {
			hk, kErr := appendUserKeys(ctx, res.PublicIP, plan.SSH.Login, eng.SSHKeyPEM, keys)
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
		est := pricing.EstimateFor(f.Name, f.VCPUs, f.RAMGB, 10, "SSD")
		out = append(out, map[string]any{
			"name":            f.Name,
			"vcpus":           f.VCPUs,
			"ram_gb":          f.RAMGB,
			"free_tier":       f.FreeTier,
			"est_monthly_rub": est.TotalMonthlyRub,
		})
	}
	return toJSON(map[string]any{
		"flavors": out,
		"note":    "est_monthly_rub is a rate-card estimate incl. 10GB SSD, not a billed price",
	}), false
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
		out = append(out, map[string]any{"id": d.ID, "name": d.Name})
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
		out = append(out, map[string]any{"id": s.ID, "name": s.Name, "availability_zone_id": s.AvailabilityZoneID})
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
		if withRules {
			rules, err := eng.Cloud.ListSecurityGroupRules(ctx, g.ID)
			if err != nil {
				return errResult(fmt.Errorf("rules of security group %s: %w", g.Name, err)), true
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
	// The group list carries no rules: read them, as deploy does.
	existing := map[string]bool{}
	if rules, e := eng.Cloud.ListSecurityGroupRules(ctx, target.ID); e == nil {
		for _, r := range rules {
			// Keyed by protocol too: an open tcp port is not an open udp
			// one (release audit, L-25).
			if r.Direction == "ingress" && r.PortRange != "" {
				existing[r.IPProtocol+" "+r.PortRange] = true
			}
		}
	}
	willOpen, alreadyOpen := []string{}, []string{}
	for _, p := range ports {
		if existing[fmt.Sprintf("%s %d:%d", proto, p, p)] {
			alreadyOpen = append(alreadyOpen, strconv.Itoa(p))
		} else {
			willOpen = append(willOpen, strconv.Itoa(p))
		}
	}
	preview := map[string]any{
		"message": i18n.Tf("open_ports would open %s/%s from %s on security group %q (shared per zone — affects every VM in it).",
			"open_ports откроет %s/%s из %s в группе безопасности %q (она общая для зоны — затронет каждую ВМ в ней).",
			strings.Join(willOpen, ","), proto, cidr, target.Name),
		"security_group": target.Name,
		"protocol":       proto,
		"cidr":           cidr,
		"will_open":      willOpen,
		"already_open":   alreadyOpen,
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {

		var opened, skipped, failed []string
		// Why each port failed: the answer said status ok with failed ports and
		// no reason, exit 0, when every rule was refused (QA 20.10, QA-3).
		failures := map[string]string{}
		var firstErr error
		for _, p := range ports {
			pr := fmt.Sprintf("%d:%d", p, p)
			if existing[proto+" "+pr] {
				skipped = append(skipped, strconv.Itoa(p))
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
					firstErr = fmt.Errorf("open port %d: %w", p, e)
				}
			} else {
				opened = append(opened, strconv.Itoa(p))
				// Open now: the port is not opened a second time (R4-19).
				existing[proto+" "+pr] = true
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
		return errAnswer(fmt.Errorf("opened %s, not opened %s: %w", strings.Join(opened, ","), strings.Join(failed, ","), firstErr), res), true
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
			return answered(s.deleteFailed("release_floating_ip", id, fmt.Errorf("floating IP %q: %w", id, e))), nil
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
		msg := fmt.Sprintf("floating IP %s (%s) belongs to a SNAT gateway — it is released with the gateway (snat_delete); not released", fipIP, fipID)
		return answered(errAnswer(clierrors.New(clierrors.CodeUsage, msg,
			i18n.T("Delete the SNAT gateway with snat_delete: its floating IP goes with it.", "Удалите SNAT-шлюз через snat_delete: его floating IP освободится вместе с ним.")),
			map[string]any{"status": "refused", "message": msg, "id": fipID, "ip_address": fipIP}), true), nil
	}

	// Safety: never release an IP that is attached to a VM.
	if state == "in_use" {
		msg := fmt.Sprintf("floating IP %s (%s) is in use by a VM — detach it or destroy the VM first; not released", fipIP, fipID)
		return answered(errAnswer(clierrors.New(clierrors.CodeUsage, msg,
			i18n.T("Detach it from the VM, or delete the VM (destroy), then release it.", "Отвяжите его от ВМ или удалите ВМ (destroy), затем освободите.")),
			map[string]any{"status": "refused", "message": msg, "id": fipID, "ip_address": fipIP}), true), nil
	}

	preview := map[string]any{"message": i18n.Tf("release_floating_ip releases floating IP %s: the address leaves the project.",
		"release_floating_ip освободит floating IP %s: адрес уйдёт из проекта.", ref),
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		if err := eng.Cloud.DeleteFloatingIP(ctx, fipID, projectID); err != nil {
			return s.deleteFailed("release_floating_ip", fipID, fmt.Errorf("release floating IP: %w", err))
		}
		return toJSON(map[string]any{
			"status":   "ok",
			"released": fipIP,
			"id":       fipID,
			"message":  fmt.Sprintf("floating IP %s released", fipIP),
		}), false
	}}, nil
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
		out = append(out, map[string]any{
			"id":          f.ID,
			"name":        f.Name,
			"ip_address":  f.IPAddress,
			"state":       f.State,
			"available":   f.Available(),
			"nat_gateway": f.OnNATGateway(),
		})
	}
	return toJSON(map[string]any{"floating_ips": out}), false
}
