// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"context"
	stderrors "errors"
	"strings"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// RuleLabel is the description of a port rule evo opens for a VM (P6):
// "evo: port 80 (vm <id>)". The VM's id in it lets a delete of that VM
// from any folder find and close the rule, which the folder's state alone
// cannot: a rule opened from folder A outlived 'vm delete' from folder B.
func RuleLabel(label, vmID string) string {
	return "evo: port " + label + " (vm " + vmID + ")"
}

// labelVM is the VM id of a RuleLabel, "" for any other description.
func labelVM(desc string) string {
	rest, ok := strings.CutPrefix(desc, "evo: port ")
	if !ok {
		return ""
	}
	_, vm, ok := strings.Cut(rest, " (vm ")
	if !ok || !strings.HasSuffix(vm, ")") {
		return ""
	}
	return strings.TrimSuffix(vm, ")")
}

// FindLabeled reads the rules of the project's security groups and returns
// the ingress rules labelled for vmID that no record of sf holds (P6), as
// Planned deletes with no RecordID. A
// rule a record holds goes by that record (Close): kept for another
// deployment, or marked for the next destroy. Reads only; an unread list
// is the error, and nothing is found then.
func FindLabeled(ctx context.Context, cloud provision.CloudAPI, projectID, vmID string, sf *state.File) ([]Planned, error) {
	if vmID == "" || cloud == nil {
		return nil, nil
	}
	held := map[string]bool{}
	if sf != nil {
		for _, d := range sf.Deployments {
			for _, r := range d.OpenedRules {
				held[r.ID] = true
			}
		}
	}
	groups, err := cloud.ListSecurityGroups(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var out []Planned
	for _, g := range groups {
		if g == nil {
			continue
		}
		rules, err := cloud.ListSecurityGroupRules(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range rules {
			if r == nil || r.ID == "" || held[r.ID] || r.Direction != "ingress" || labelVM(r.Description) != vmID {
				continue
			}
			if inactive[r.State] {
				continue
			}
			lo, _, _ := portRange(r.PortRange)
			out = append(out, Planned{Rule: state.SGRule{ID: r.ID, SecurityGroupID: g.ID, Port: PortKey(strings.ToLower(r.IPProtocol), lo)}, VMID: vmID})
		}
	}
	return out, nil
}

// CloseLabeled deletes the rules FindLabeled found, once their VM is gone
// or deleted by this call. A rule already gone (404) counts as closed; a
// failed delete is reported: no record holds it, and the next delete of the
// same VM finds it again by its label.
func CloseLabeled(ctx context.Context, cloud provision.CloudAPI, rules []Planned) CloseResult {
	var res CloseResult
	for _, p := range rules {
		// The port and the VM of its label: the agent of another folder
		// whose port went finds why (P6).
		label := Label(p.Rule.Port) + " (vm " + p.VMID + ")"
		err := cloud.DeleteSecurityGroupRule(ctx, p.Rule.SecurityGroupID, p.Rule.ID)
		var cliErr *clerrors.CLIError
		if err == nil || (stderrors.As(err, &cliErr) && (cliErr.Code == clerrors.CodeNotFound || cliErr.Code == clerrors.CodeVMNotFound)) {
			res.Closed = append(res.Closed, label)
			continue
		}
		res.Failed = append(res.Failed, label+": "+err.Error())
	}
	return res
}
