// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// sgRuleGoneTimeout bounds the wait for a deleted rule to leave its group.
// How long the platform keeps a rule in "deleting" is not measured (probe
// Г-11 of the 1.9 plan): the bound is the one a transient state of another
// change is given to settle.
const sgRuleGoneTimeout = 30 * time.Second

// sgRuleGone is the state waitState reports for a rule its group lists no
// more.
const sgRuleGone = "gone"

// sgRuleSource is the source of a rule in words: its prefix, or the group
// it lets in — named as a group, never as «anywhere» («Риск», conclusion
// to #1490, У2: a person who reads «22 stays open to anyone» and deletes
// the other rule loses SSH); a rule that names neither lets in any address.
func sgRuleSource(r *provision.SGRule) string {
	switch {
	case r.RemoteIPPrefix != "":
		return r.RemoteIPPrefix
	case r.RemoteSecurityGroup != nil:
		return "security group " + firstNonEmptyStr(r.RemoteSecurityGroup.Name, r.RemoteSecurityGroup.ID)
	}
	return sgrules.Anywhere
}

// sgRuleFields is a rule as a preview and an answer name it.
func sgRuleFields(r *provision.SGRule) map[string]any {
	out := map[string]any{"id": r.ID, "direction": r.Direction, "protocol": r.IPProtocol, "port_range": r.PortRange,
		"source": sgRuleSource(r), "state": r.State}
	if r.Description != "" {
		out["description"] = r.Description
	}
	return out
}

// sgRuleLetsIn reports whether r is an active ingress rule that lets
// proto/port in from its own source — the rule of «a port is open»
// (sgrules.Coverage), asked of one rule.
//
// A rule whose source is a group lets in that group's interfaces, not an
// address a person or evo comes from, and a rule still being created lets
// in nothing yet: neither counts.
func sgRuleLetsIn(r *provision.SGRule, proto string, port int) bool {
	if r.RemoteSecurityGroup != nil || r.State == "creating" {
		return false
	}
	covered, _ := sgrules.Coverage([]*provision.SGRule{r}, proto, port, sgRuleSource(r))
	return covered
}

// planSGRuleDelete plans sg_rule_delete (gate.go;
// docs/internal/briefs/sg-rules.md §4): the group and its rules are read
// before the question. A group that is not there is the call's error; a
// rule no group of the project has is already deleted (D26); a rule of
// another group is a wrong call, said with the group the rule is in — not
// «already deleted». The states of the rule are the rule of every delete
// (deletePlan, D61). The preview names the rule whole, the group, and
// what stops getting through; the last rule that lets SSH in to a
// deployment of this folder is not deleted without allow_cut_ssh.
func (s *Registry) planSGRuleDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	sgRef, ruleID := stringArg(args, "security_group"), stringArg(args, "rule_id")
	if sgRef == "" || ruleID == "" {
		return nil, usageErr("'security_group' and 'rule_id' (from list_security_groups with rules=true) are required",
			"нужны 'security_group' и 'rule_id' (из list_security_groups с rules=true)")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	group, err := securityGroupByRef(ctx, eng, sgRef)
	if err != nil {
		return nil, err
	}
	rules, err := eng.Cloud.ListSecurityGroupRules(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	var rule *provision.SGRule
	for _, r := range rules {
		if r != nil && r.ID == ruleID {
			rule = r
		}
	}
	if rule == nil {
		if other := sgRuleElsewhere(ctx, eng, group.ID, ruleID); other != nil {
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("rule %s is a rule of security group %s (%s), not of %s", "правило %s принадлежит группе безопасности %s (%s), а не %s",
					ruleID, quoteCloud(other.Name), other.ID, quoteCloud(group.Name)),
				i18n.T("Name the group the rule is in, or check the rule id (list_security_groups with rules=true).",
					"Назовите группу, в которой правило, или проверьте id правила (list_security_groups с rules=true)."))
		}
		return answered(s.deleteFailed("sg_rule_delete", ruleID, clierrors.New(clierrors.CodeNotFound,
			i18n.Tf("no rule %s in security group %s", "правила %s в группе безопасности %s нет", ruleID, quoteCloud(group.Name)), ""))), nil
	}
	// SSH: evo manages a deployment over port 22. The last rule that lets
	// it in to a group of a deployment of this folder goes only with the
	// word for it — what address the caller comes from evo does not know.
	// A rule the platform is deleting already is answered as every delete
	// under way (deletePlan): there is nothing left to refuse.
	cutsSSH := !engine.DeletingState(rule.State) && sgRuleLetsIn(rule, "tcp", 22) && folderDeploymentInGroup(eng.StateDir, group.ID)
	if cutsSSH {
		for _, r := range rules {
			if r != nil && r.ID != rule.ID && sgRuleLetsIn(r, "tcp", 22) {
				cutsSSH = false
			}
		}
	}
	if cutsSSH && !boolArg(args, "allow_cut_ssh", false) {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("rule %s is the last one that lets TCP 22 in to security group %s, and a deployment of this folder is in it: without it evo (deploy, logs) and the person lose SSH to the VM",
				"правило %s — последнее, которое пропускает TCP 22 в группу безопасности %s, а в ней развёртывание этой папки: без него evo (deploy, logs) и человек теряют SSH к ВМ",
				ruleID, quoteCloud(group.Name)),
			i18n.T("Ask the person. To delete it all the same, call again with allow_cut_ssh=true: the preview will say that SSH is cut.",
				"Спросите человека. Чтобы всё же удалить, повторите вызов с allow_cut_ssh=true: превью скажет, что SSH будет отрезан."))
	}
	words := fmt.Sprintf("%s %s %s %s", rule.Direction, rule.IPProtocol, rule.PortRange, sgRuleSource(rule))
	p := s.deletePlan("sg_rule_delete",
		fmt.Sprintf("sg_rule_delete deletes rule %%s (%s) of security group %s.", words, quoteCloud(group.Name)),
		fmt.Sprintf("sg_rule_delete удаляет правило %%s (%s) группы безопасности %s.", words, quoteCloud(group.Name)),
		ruleID, func() (map[string]any, error) { return map[string]any{"state": rule.State}, nil },
		func(ctx context.Context) (string, bool) {
			if err := eng.Cloud.DeleteSecurityGroupRule(ctx, group.ID, ruleID); err != nil {
				return s.deleteFailed("sg_rule_delete", ruleID, fmt.Errorf(i18n.T("delete rule: %w", "удаление правила: %w"), err))
			}
			return s.sgRuleDeleted(ctx, eng, group, ruleID)
		})
	if p.Preview == nil {
		return p, nil
	}
	p.Preview["rule"] = sgRuleFields(rule)
	p.Preview["security_group"] = map[string]any{"id": group.ID, "name": group.Name, "default": group.Default}
	// The zone's default group holds every VM placed in the zone (how many
	// interfaces are under a group the preview will say once evo reads
	// them: the brief, §3.2).
	p.Preview["shared"] = group.Default
	if cutsSSH {
		p.Preview["cuts_ssh"] = true
	}
	if rule.Direction == "ingress" {
		p.Preview["cuts_access"] = i18n.Tf("after the delete %s %s from %s no longer gets to the VMs of the group, unless another rule lets it in",
			"после удаления %s %s с %s к ВМ группы не пройдёт, если это не разрешает другое правило", rule.IPProtocol, rule.PortRange, sgRuleSource(rule))
		still, fromGroups := sgStillAllowed(rules, rule)
		if len(still) > 0 {
			p.Preview["still_allowed_by"] = still
		}
		if len(fromGroups) > 0 {
			// Not «still allowed» for an address: only for the interfaces
			// of those groups.
			p.Preview["still_allowed_from_groups"] = fromGroups
		}
	}
	return p, nil
}

// sgStillAllowed are the other active rules of the group that let the port
// of rule in — with their source: who still gets in is the person's to
// read. byPrefix let addresses in; fromGroups let in only the interfaces
// of their source group, and are named apart — never as «anywhere». Only
// for a rule of one tcp or udp port: a range and a rule of any protocol
// are not compared.
func sgStillAllowed(rules []*provision.SGRule, rule *provision.SGRule) (byPrefix, fromGroups []map[string]any) {
	var port int
	if _, err := fmt.Sscanf(rule.PortRange, "%d:%d", &port, new(int)); err != nil || rule.PortRange != fmt.Sprintf("%d:%d", port, port) {
		return nil, nil
	}
	if rule.IPProtocol != "tcp" && rule.IPProtocol != "udp" {
		return nil, nil
	}
	for _, r := range rules {
		if r == nil || r.ID == rule.ID {
			continue
		}
		if r.RemoteSecurityGroup != nil {
			// The same rule of «a port is open», asked without the source.
			asPrefix := *r
			asPrefix.RemoteSecurityGroup = nil
			if sgRuleLetsIn(&asPrefix, rule.IPProtocol, port) {
				fromGroups = append(fromGroups, sgRuleFields(r))
			}
			continue
		}
		if sgRuleLetsIn(r, rule.IPProtocol, port) {
			byPrefix = append(byPrefix, sgRuleFields(r))
		}
	}
	return byPrefix, fromGroups
}

// sgRuleElsewhere is the other group of the project that has the rule,
// nil when none does (or the groups cannot be read: the rule is then
// answered as not there, as before the lookup).
func sgRuleElsewhere(ctx context.Context, eng *engine.Engine, notIn, ruleID string) *provision.SecurityGroup {
	groups, err := eng.Cloud.ListSecurityGroups(ctx, eng.Config.ProjectID)
	if err != nil {
		return nil
	}
	for _, g := range groups {
		if g == nil || g.ID == notIn {
			continue
		}
		rules, err := eng.Cloud.ListSecurityGroupRules(ctx, g.ID)
		if err != nil {
			continue
		}
		for _, r := range rules {
			if r != nil && r.ID == ruleID {
				return g
			}
		}
	}
	return nil
}

// folderDeploymentInGroup reports whether a deployment recorded in the
// folder has its VM in the group.
func folderDeploymentInGroup(stateDir, groupID string) bool {
	if stateDir == "" {
		return false
	}
	sf, err := state.Read(state.DefaultPath(stateDir))
	if err != nil {
		return false
	}
	for _, d := range sf.Deployments {
		if d.SecurityGroupID == groupID && d.Status != "destroyed" {
			return true
		}
	}
	return false
}

// sgRuleDeleted waits for the deleted rule to leave its group and answers:
// deleted once the group lists it no more; at the limit — of the wait or
// of an MCP call — the rule is still being deleted, and the read of the
// group's rules is the next step.
func (s *Registry) sgRuleDeleted(ctx context.Context, eng *engine.Engine, group *provision.SecurityGroup, ruleID string) (string, bool) {
	last, timedOut, atLimit, err := waitState(ctx, eng, sgRuleGoneTimeout,
		func(ctx context.Context) (string, bool, error) {
			rules, err := eng.Cloud.ListSecurityGroupRules(ctx, group.ID)
			if err != nil {
				return "", false, nil
			}
			for _, r := range rules {
				if r != nil && r.ID == ruleID {
					return r.State, true, nil
				}
			}
			return sgRuleGone, true, nil
		},
		func(st string) string {
			return fmt.Sprintf(i18n.T("rule %s: %s, waiting…", "правило %s: %s, жду…"), ruleID, st)
		},
		func(st string) (bool, error) {
			switch st {
			case sgRuleGone:
				return true, nil
			case "error", "error_deleting":
				return true, clierrors.New(clierrors.CodeAPIError,
					i18n.Tf("rule %s is in state %s after the delete", "правило %s после удаления в состоянии %s", ruleID, st),
					i18n.T("list_security_groups with rules=true shows it.", "Его показывает list_security_groups с rules=true."))
			}
			return false, nil
		})
	ans := map[string]any{"rule_id": ruleID, "security_group": group.Name, "security_group_id": group.ID}
	switch {
	case err != nil:
		return errAnswer(err, ans), true
	case atLimit || timedOut > 0:
		ans["status"], ans["state"] = "deleting", last
		ans["next"] = fmt.Sprintf("Rule %s is still being deleted. list_security_groups with rules=true shows whether it is gone.", ruleID)
		nextStep(ans, "list_security_groups", map[string]any{"rules": true})
		return toJSON(ans), false
	}
	ans["status"] = "deleted"
	return toJSON(ans), false
}
