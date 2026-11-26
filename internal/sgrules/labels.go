// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"context"

	"strings"

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

// IsRuleLabel reports whether desc reads as a RuleLabel: a description a
// caller gives a rule of its own must not be one — a delete of the VM it
// names would take the rule for the deployment's and close it.
func IsRuleLabel(desc string) bool { return labelVM(strings.TrimSpace(desc)) != "" }

// FindLabeled reads the rules of the project's security groups and returns
// the ingress rules labelled for vmID that no record of sf holds (P6), as
// Planned deletes with no RecordID. A rule a record holds goes by that
// record (Close): kept for another deployment, or marked for the next
// destroy. zoneID or zoneName, when the VM's zone is known (the VM still
// there), limits the reads to that zone's groups — the VM's ports open in
// its zone's group; a VM already gone has every group read. Reads only. A
// list it could not read is the error, beside what the other groups gave:
// the caller says the rules were not all checked.
func FindLabeled(ctx context.Context, cloud provision.CloudAPI, projectID, vmID, zoneID, zoneName string, sf *state.File) ([]Planned, error) {
	if vmID == "" || cloud == nil {
		return nil, nil
	}
	held := map[[2]string]bool{}
	if sf != nil {
		for _, d := range sf.Deployments {
			for _, r := range d.OpenedRules {
				held[[2]string{r.SecurityGroupID, r.ID}] = true
			}
		}
	}
	groups, err := cloud.ListSecurityGroups(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var out []Planned
	var firstErr error
	for _, g := range groups {
		if g == nil || !inZone(g, zoneID, zoneName) {
			continue
		}
		rules, err := provision.ReadSecurityGroupRulesForCleanup(ctx, cloud, g.ID, projectID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, r := range rules {
			if r == nil || r.ID == "" || held[[2]string{g.ID, r.ID}] || r.Direction != "ingress" || labelVM(r.Description) != vmID {
				continue
			}
			lo, _, _ := portRange(r.PortRange)
			out = append(out, Planned{Rule: state.SGRule{ID: r.ID, SecurityGroupID: g.ID, Port: PortKey(strings.ToLower(r.IPProtocol), lo)}, VMID: vmID})
		}
	}
	return out, firstErr
}

// inZone: group g is in the zone given, or no zone is given, or g names
// none (it is read rather than missed).
func inZone(g *provision.SecurityGroup, zoneID, zoneName string) bool {
	if zoneID == "" && zoneName == "" {
		return true
	}
	if g.AvailabilityZoneID == "" && g.AvailabilityZoneName == "" {
		return true
	}
	return (zoneID != "" && (g.AvailabilityZoneID == zoneID || strings.EqualFold(g.AvailabilityZoneName, zoneID))) ||
		(zoneName != "" && (strings.EqualFold(g.AvailabilityZoneName, zoneName) || g.AvailabilityZoneID == zoneName))
}

// TrackLabeled adds only the approved exact IDs, before issuing a DELETE.
// The target keeps failed receipts even if its VM came from another folder.
func TrackLabeled(sf *state.File, rec *state.DeploymentRecord, rules []Planned) {
	for _, p := range rules {
		if p.VMID == "" || p.VMID != rec.VMID || p.Rule.ID == "" || p.Rule.SecurityGroupID == "" {
			continue
		}
		held := false
		for _, d := range sf.Deployments {
			for _, r := range d.OpenedRules {
				if r.ID == p.Rule.ID && r.SecurityGroupID == p.Rule.SecurityGroupID {
					held = true
					break
				}
			}
			if held {
				break
			}
		}
		if !held {
			rule := p.Rule
			rule.LabelVMID = p.VMID
			addRule(rec, rule)
		}
	}
}
