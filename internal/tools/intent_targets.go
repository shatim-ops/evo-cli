// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// bindFloatingIPReleaseTarget pins an address-based release to the ID the
// plan resolved. The address may be reassigned before a lost reply is checked.
// The transport persists the open call's target before it sends the delete.
func bindFloatingIPReleaseTarget(ctx context.Context, address, id string) {
	if address == "" {
		return
	}
	if own := openCallForTool(ctx, "release_floating_ip"); own != nil {
		_, own.open.Target = intentResource("release_floating_ip", map[string]any{"id": id})
	}
}

func dnsRecordIntent(ctx context.Context, r *Registry, args map[string]any) (string, string, error) {
	name := nameArg(args)
	recordType := strings.ToLower(stringArg(args, "record_type"))
	id := stringArg(args, "zone_id")
	if err := validateDNSRecordCreate(args, id, name, recordType); err != nil {
		return "", "", err
	}
	vis := dnsVisibility(args)
	if stringArg(args, "type") == "" {
		cli, err := r.dnsClient(ctx)
		if err != nil {
			return "", "", err
		}
		vis, err = dnsZoneVisibility(ctx, cli, args, id)
		if err != nil {
			return "", "", err
		}
	}
	return name, vis + "_" + recordType + "_zone_id=" + id, nil
}

func snatIntent(ctx context.Context, r *Registry, args map[string]any) (string, string, error) {
	name := nameArg(args)
	if name == "" {
		return "", "", nil
	}
	if err := provision.ValidName(i18n.T("SNAT gateway", "SNAT-шлюза"), name); err != nil {
		return "", "", err
	}
	id, _, err := r.resolveZone(ctx, args)
	return name, "availability_zone_id=" + id, err
}

// Before 1.10 DNS stored just the label and a route just its VPC.
// An ambiguous older record can block a send but cannot prove success.
func networkIntentConflicts(in intent.Intent, project, name, target string, args map[string]any) bool {
	if in.ProjectID != project {
		if in.ProjectID != "" || (in.Tool != "vpc_route_create" && in.Tool != "dns_record_create") {
			return false
		}
	}
	switch in.Tool {
	case "snat_create":
		return in.Name == name || (in.Target != "" && in.Target == target)
	case "dns_record_create":
		if in.Target == target {
			return strings.EqualFold(in.Name, name)
		}
		if strings.HasPrefix(in.Target, "zone_id=") {
			return in.Target == "zone_id="+stringArg(args, "zone_id") && strings.EqualFold(in.Name, nameArg(args))
		}
	case "vpc_route_create":
		if in.Name == "" {
			return in.Target == target
		}
	}
	return in.Target == target && in.Name == name
}

// One lock file per registered operation, independent of user input.
// SNAT conflicts on either name or zone, so a lock on their pair would
// let two conflicting calls check an empty directory at the same time.
func (r *Registry) lockNetworkIntents(tool string) (func(), error) {
	root, _ := r.stateDir()
	unlock, err := state.FileLock(filepath.Join(config.DataDir(root), tool+".lock"), 5*time.Second)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeStateLocked, i18n.T("could not lock network create intents", "не удалось заблокировать записи сетевых созданий"), i18n.T("Check the state directory and retry.", "Проверьте каталог состояния и повторите вызов."), err)
	}
	return unlock, nil
}

type networkSessionKey struct{}

// WithNetworkSession carries the validated HTTP session for recovery guidance.
func WithNetworkSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, networkSessionKey{}, session)
}

// HTTP dispatch always sets the session context, including for stateless requests.
func statelessNetworkHTTP(ctx context.Context) bool {
	session, http := ctx.Value(networkSessionKey{}).(string)
	return http && session == ""
}

// Do not infer a negative outcome from a list. Only a matching, identified
// object can settle an unknown create. Return the occupied-target answer of
// the same preview, even if a later read no longer sees the object.
func (r *Registry) settleNetworkConflict(ctx context.Context, mine []intent.Intent, answer string) {
	if r.readOnly {
		return
	}
	var result struct {
		ID string `json:"existing_id"`
	}
	if json.Unmarshal([]byte(answer), &result) != nil || result.ID == "" {
		return
	}
	for _, in := range mine {
		if in.ProjectID == "" || in.Key == nil || in.Key.KeyID == "" {
			continue
		}
		if _, found, id := r.checkByList(ctx, in, in.ProjectID); found && id == result.ID {
			intent.Close(in, settledOutcome(in, id))
			forgetPending(ctx)
		}
	}
}

// vmTarget is the target of a call that names a VM by vm_id or by name
// (changeRule.target): the VM's id, read as the plan of a power tool
// reads it — an id as it is, a name through the one point names are
// resolved at (resolveVMFor). The record names no name: the call makes
// nothing.
func vmTarget(tool string) func(ctx context.Context, r *Registry, args map[string]any) (name, target string, err error) {
	return func(ctx context.Context, r *Registry, args map[string]any) (string, string, error) {
		ref, _, err := oneOfArgs(args, vmRefNotBoth(), "vm_id", "name")
		if err != nil || ref == "" {
			// The plan of the tool refuses these arguments in its own words.
			return "", "", nil
		}
		if looksUUID(ref) {
			return "", "vm_id=" + ref, nil
		}
		eng, err := r.authedEngine(ctx, nil)
		if err != nil {
			return "", "", err
		}
		vm, err := r.resolveVMFor(ctx, tool, ref, eng.Config.ProjectID)
		if err != nil {
			return "", "", err
		}
		return "", "vm_id=" + vm.ID, nil
	}
}

// vmSettled is what the state of the VM a call of the power tool names
// says of a cut call of it (changeRule.settledBy): where the action leads —
// made; in a transition — goes on; where it started, in an error, being
// deleted — asked about. A VM that cannot be read is asked about too.
func vmSettled(tool string) func(ctx context.Context, r *Registry, args map[string]any) (outcome, error) {
	act, resolve := vmPowerActions[tool], vmTarget(tool)
	return func(ctx context.Context, r *Registry, args map[string]any) (outcome, error) {
		_, target, err := resolve(ctx, r, args)
		_, id, found := strings.Cut(target, "=")
		if err != nil || !found {
			return outcomeAsk, err
		}
		eng, err := r.authedEngine(ctx, nil)
		if err != nil {
			return outcomeAsk, err
		}
		vm, err := eng.Cloud.GetVMDetail(ctx, id)
		switch {
		case err != nil:
			return outcomeAsk, err
		case vm.State == act.to:
			return outcomeMade, nil
		case vmSettling[vm.State]:
			return outcomeGoesOn, nil
		}
		return outcomeAsk, nil
	}
}

// groupTarget is the target of a call that names a security group in arg
// by its id or its name (changeRule.target): the group's id, read as the
// plan of the tool reads it (securityGroupFor), so a cut call is found
// however the repeat spells the group (release plan 1.9, #1608). Nothing
// is read for a call its arguments refuse (valid): it is left as it spells
// the group, and the plan refuses it in its own words and order, before
// anything is sent. A name that is no group, or two, refuses the call
// here with the error the plan would give, as vmTarget does: no record is
// opened for it, so a refusal that sent nothing does not settle the record
// of a cut call of the same arguments (R-101; «Риск · 1.9 (2)», check of
// #1726).
func groupTarget(arg string, valid func(args map[string]any) error) func(ctx context.Context, r *Registry, args map[string]any) (name, target string, err error) {
	return func(ctx context.Context, r *Registry, args map[string]any) (string, string, error) {
		ref := stringArg(args, arg)
		if ref == "" {
			return "", "", nil
		}
		if valid(args) != nil {
			return "", arg + "=" + ref, nil
		}
		eng, err := r.authedEngine(ctx, nil)
		if err != nil {
			return "", "", err
		}
		group, err := securityGroupFor(ctx, eng, ref)
		if err != nil {
			return "", "", err
		}
		return "", arg + "=" + group.ID, nil
	}
}

// securityGroupFor is securityGroupByRef once a call: the layer of intents
// resolves the group a call names before the plan of the tool does
// (changeRule.target).
func securityGroupFor(ctx context.Context, eng *engine.Engine, ref string) (*provision.SecurityGroup, error) {
	return onceACall(ctx, "sg\x00"+ref+"\x00"+eng.Config.ProjectID, func() (*provision.SecurityGroup, error) {
		return securityGroupByRef(ctx, eng, ref)
	})
}

// partiesOf are the parties of a call of tool (idArg.party) its handler
// has resolved: each argument the call gives, as «argument=id», resolved
// by the resolver of the handler without a read (resolvedOnly) — the
// mark of the first write names what that write goes to, and asks the
// cloud nothing the handler did not. An argument the handler has not
// resolved is no party.
func (r *Registry) partiesOf(ctx context.Context, tool string, args map[string]any) []string {
	var out []string
	ctx = resolvedOnly(ctx)
	for _, a := range decls[tool].IDArgs {
		ref := stringArg(args, a.name)
		if a.party == nil || ref == "" {
			continue
		}
		if id, err := a.party(ctx, r, ref); err == nil && id != "" {
			out = append(out, a.name+"="+id)
		}
	}
	sort.Strings(out)
	return out
}

// partyVM is the VM a call names by name or id (idArg.party), as the one
// point a reference to a VM is resolved at (resolveVM) resolved it.
func partyVM(ctx context.Context, r *Registry, ref string) (string, error) {
	eng, err := r.localEngine(ctx, nil)
	if err != nil {
		return "", err
	}
	vm, err := r.resolveVM(ctx, ref, eng.Config.ProjectID)
	if err != nil {
		return "", err
	}
	return vm.ID, nil
}

// partyGroup is the security group a call names by name or id
// (idArg.party), as securityGroupFor resolved it.
func partyGroup(ctx context.Context, r *Registry, ref string) (string, error) {
	eng, err := r.localEngine(ctx, nil)
	if err != nil {
		return "", err
	}
	group, err := securityGroupFor(ctx, eng, ref)
	if err != nil {
		return "", err
	}
	return group.ID, nil
}

// partyFloatingIP is the floating IP a call names by address or id
// (idArg.party), as floatingIPFor resolved it.
func partyFloatingIP(ctx context.Context, r *Registry, ref string) (string, error) {
	eng, err := r.localEngine(ctx, nil)
	if err != nil {
		return "", err
	}
	fip, err := floatingIPFor(ctx, eng, ref)
	if err != nil {
		return "", err
	}
	return fip.ID, nil
}
