// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	stderrors "errors"
	"fmt"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
)

// The groups and the diagnosis of vm_get (docs/internal/briefs/
// vm-diagnose.md; «Риск», conclusion 209). The card of a VM does not name
// the security groups or the subnet of its interfaces: the list of the
// project's interfaces does (GET /api/v1/interfaces, narrowed to the VM).

// vmRulesShown is the most rules of one group a diagnosis names; the rest
// is counted (rules_total) and sent to list_security_groups (У6).
const vmRulesShown = 50

// Results of a check and of a diagnosis (У2, У3).
const (
	checkOK          = "ok"
	checkProblem     = "problem"
	checkUnknown     = "unknown"
	resultIncomplete = "incomplete"
)

// stateVerdicts are the states of the specification of Compute, each with
// what it says of the thing it is the state of: ok — it works; problem —
// it does not work now (the fact is the state). A state not here is
// unknown to evo (У3): named as it is, never ok or problem.
var stateVerdicts = map[string]map[string]string{
	"vm": {"running": checkOK, "stopped": checkProblem, "error": checkProblem, "error_creating": checkProblem,
		"error_deleting": checkProblem, "creating": checkProblem, "starting": checkProblem, "stopping": checkProblem,
		"rebooting": checkProblem, "deleting": checkProblem, "updating": checkProblem},
	"interface": {"created": checkOK, "error": checkProblem, "error_creating": checkProblem, "error_deleting": checkProblem,
		"creating": checkProblem, "deleting": checkProblem, "updating": checkProblem},
	"subnet": {"created": checkOK, "error": checkProblem, "error_creating": checkProblem, "error_deleting": checkProblem,
		"creating": checkProblem, "deleting": checkProblem, "updating": checkProblem},
	"security_group": {"created": checkOK, "error": checkProblem, "error_creating": checkProblem, "error_deleting": checkProblem,
		"creating": checkProblem, "deleting": checkProblem, "updating": checkProblem},
	"floating_ip": {"in_use": checkOK, "available": checkProblem, "error": checkProblem, "error_creating": checkProblem,
		"error_deleting": checkProblem, "creating": checkProblem, "deleting": checkProblem, "updating": checkProblem,
		"attaching": checkProblem, "detaching": checkProblem},
}

// stateCheck is the check of the state of what (stateVerdicts) named name.
func stateCheck(check, what, name, state string) map[string]any {
	verdict, known := stateVerdicts[what][state]
	fact := i18n.Tf("%s is %s", "%s в состоянии %s", name, state)
	if !known {
		verdict = checkUnknown
		fact = i18n.Tf("%s is in state %s, which evo does not know", "%s в состоянии %s, evo его не знает", name, quoteCloud(state))
	}
	return map[string]any{"check": check, "result": verdict, "fact": fact}
}

// vmInterfaces reads the interfaces of VM id from the list of the
// project's, by id of the interface. once: the read beside the card of a
// plain vm_get — one attempt within the limit of a preview's read, its
// failure named, not waited out (У1).
func vmInterfaces(ctx context.Context, eng *engine.Engine, id string, once bool) (map[string]*provision.Interface, error) {
	if once {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(httpretry.Once(ctx), engine.PreviewLookupTimeout)
		defer cancel()
	}
	list, err := eng.Cloud.ListInterfaces(provision.Narrowed(ctx, "/api/v1/interfaces", "vm_id", id), eng.Config.ProjectID)
	if err != nil {
		return nil, err
	}
	out := map[string]*provision.Interface{}
	for _, in := range list {
		if in != nil && in.VM != nil && in.VM.ID == id {
			out[in.ID] = in
		}
	}
	return out, nil
}

// notChecked is a check evo could not make, with why: the reason of the
// read that failed, in evo's words — a refusal of IAM by its name.
func notChecked(check string, err error) map[string]any {
	why := i18n.Tf("Cloud.ru did not answer the read (%s)", "Cloud.ru не ответил на чтение (%s)", clierrors.Reason(err))
	var ce *clierrors.CLIError
	if stderrors.As(err, &ce) && ce.Code == clierrors.CodeAuthFailed {
		why = i18n.T("the key has no role to read it (IAM, 403)", "у ключа нет роли на это чтение (IAM, 403)")
	}
	return map[string]any{"check": check, "reason": clierrors.Reason(err), "why": why}
}

// interfaceGroups are the groups of in as an answer names them.
func interfaceGroups(in *provision.Interface) []map[string]any {
	groups := make([]map[string]any, 0, len(in.SecurityGroups))
	for _, g := range in.SecurityGroups {
		groups = append(groups, map[string]any{"id": g.ID, "name": g.Name, "state": g.State})
	}
	return groups
}

// interfaceSubnet is the subnet of in as an answer names it; nil when the
// list names none.
func interfaceSubnet(in *provision.Interface) map[string]any {
	if in.Subnet == nil || in.Subnet.ID == "" {
		return nil
	}
	return map[string]any{"id": in.Subnet.ID, "name": in.Subnet.Name, "cidr": in.Subnet.Address, "state": in.Subnet.State}
}

// vmGroupsOnRows puts the groups and the subnet of each interface of det
// into its row of the answer, from listed; an interface the list does not
// name is said in not_checked rather than shown with no groups.
func vmGroupsOnRows(rows []map[string]any, det *provision.VMDetail, listed map[string]*provision.Interface) (missing []map[string]any) {
	for i, in := range det.Interfaces {
		li := listed[in.ID]
		if li == nil {
			missing = append(missing, map[string]any{"check": "security_groups:" + in.ID, "reason": "not_listed",
				"why": i18n.T("the list of the project's interfaces does not name this interface",
					"список интерфейсов проекта не называет этот интерфейс")})
			continue
		}
		rows[i]["security_groups"] = interfaceGroups(li)
		if sub := interfaceSubnet(li); sub != nil {
			rows[i]["subnet"] = sub
		}
	}
	return missing
}

// vmDiagnosis is what diagnose adds to the answer of vm_get: the groups of
// the VM with their rules, the checks, the exposure and the result.
func vmDiagnosis(ctx context.Context, eng *engine.Engine, det *provision.VMDetail, listed map[string]*provision.Interface,
	notCheckedSoFar []map[string]any) map[string]any {
	checks := []map[string]any{stateCheck("vm_state", "vm", i18n.T("the VM", "ВМ"), det.State)}
	missing := notCheckedSoFar
	hasPrimaryAddress := false
	for _, in := range det.Interfaces {
		name := i18n.Tf("interface %s", "интерфейс %s", quoteCloud(cmpName(in.Name, in.ID)))
		checks = append(checks, stateCheck("interface_state:"+in.ID, "interface", name, in.State))
		if in.Primary && in.IPAddress != "" {
			hasPrimaryAddress = true
		}
		if in.FloatingIP != nil && in.FloatingIP.ID != "" {
			checks = append(checks, stateCheck("public_address_state:"+in.FloatingIP.ID, "floating_ip",
				i18n.Tf("public address %s", "публичный адрес %s", in.FloatingIP.IPAddress), in.FloatingIP.State))
		}
		if li := listed[in.ID]; li != nil && li.Subnet != nil && li.Subnet.ID != "" {
			checks = append(checks, stateCheck("subnet_state:"+li.Subnet.ID, "subnet",
				i18n.Tf("subnet %s", "подсеть %s", quoteCloud(cmpName(li.Subnet.Name, li.Subnet.ID))), li.Subnet.State))
		}
	}
	if len(det.Interfaces) > 0 {
		primary := map[string]any{"check": "primary_address", "result": checkOK,
			"fact": i18n.T("the primary interface has an address", "у основного интерфейса есть адрес")}
		if !hasPrimaryAddress {
			primary["result"] = checkProblem
			primary["fact"] = i18n.T("no primary interface with an address", "нет основного интерфейса с адресом")
		}
		checks = append(checks, primary)
	}

	// The groups of the VM, each once, in the order the interfaces name them.
	var order []provision.InterfaceGroup
	seen := map[string]bool{}
	for _, in := range det.Interfaces {
		if li := listed[in.ID]; li != nil {
			for _, g := range li.SecurityGroups {
				if !seen[g.ID] {
					seen[g.ID] = true
					order = append(order, g)
				}
			}
		}
	}
	var groups []map[string]any
	var internet, port22 []map[string]any
	for _, g := range order {
		name := cmpName(g.Name, g.ID)
		checks = append(checks, stateCheck("group_state:"+g.ID, "security_group",
			i18n.Tf("security group %s", "группа безопасности %s", quoteCloud(name)), g.State))
		row := map[string]any{"id": g.ID, "name": g.Name, "state": g.State}
		rules, err := eng.Cloud.ListSecurityGroupRules(ctx, g.ID)
		if err != nil {
			missing = append(missing, notChecked("rules:"+g.ID, err))
			groups = append(groups, row)
			continue
		}
		shown := make([]map[string]any, 0, min(len(rules), vmRulesShown))
		for _, r := range rules {
			if r == nil {
				continue
			}
			if len(shown) < vmRulesShown {
				shown = append(shown, vmRule(r))
			}
			if sgrules.OpenToInternet(r) {
				internet = append(internet, map[string]any{"security_group": g.Name, "protocol": r.IPProtocol, "port_range": r.PortRange,
					"source": r.RemoteIPPrefix})
			}
			if sgrules.Admits(r, "tcp", 22) {
				port22 = append(port22, map[string]any{"security_group": g.Name, "source": ruleSource(r)})
			}
		}
		row["rules"] = shown
		row["rules_total"] = len(rules)
		if len(rules) > len(shown) {
			row["rules_more"] = i18n.Tf("and %d more: list_security_groups", "и ещё %d: list_security_groups", len(rules)-len(shown))
		}
		groups = append(groups, row)
	}

	// Exposure: facts, no judgement and no next step (У4). A rule from a
	// group lets in that group's interfaces, and a rule with no source is
	// not judged (sgrules.OpenToInternet): said, not counted.
	exposure := map[string]any{
		"open_to_internet": orEmpty(internet),
		"port_22":          orEmpty(port22),
		"not_counted": i18n.T("a rule whose source is a security group lets in only that group's interfaces, and a rule that names no source is not judged: neither is counted as open to the internet",
			"правило с группой-источником пускает только интерфейсы этой группы, правило без источника не оценивается: ни то ни другое не считается открытым в интернет"),
	}

	result := checkOK
	for _, c := range checks {
		switch c["result"] {
		case checkProblem:
			result = checkProblem
		case checkUnknown:
			if result == checkOK {
				result = resultIncomplete
			}
		}
	}
	if len(missing) > 0 && result == checkOK {
		result = resultIncomplete
	}
	out := map[string]any{"checks": checks, "exposure": exposure, "result": result, "security_groups": orEmpty(groups)}
	if len(missing) > 0 {
		out["not_checked"] = missing
	}
	return out
}

// vmRule is a rule of a group as the diagnosis names it.
func vmRule(r *provision.SGRule) map[string]any {
	row := map[string]any{"direction": r.Direction, "protocol": r.IPProtocol, "port_range": r.PortRange,
		"source": ruleSource(r), "state": r.State}
	if r.Description != "" {
		row["description"] = r.Description
	}
	return row
}

// ruleSource is where a rule lets traffic from (to, for egress): its
// prefix, or the group it names.
func ruleSource(r *provision.SGRule) string {
	if r.RemoteSecurityGroup != nil {
		return fmt.Sprintf("security group %s", cmpName(r.RemoteSecurityGroup.Name, r.RemoteSecurityGroup.ID))
	}
	return r.RemoteIPPrefix
}

// cmpName is name, or id when the cloud names none.
func cmpName(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

// orEmpty is list, an empty list for none: «none» and «not read» differ
// in an answer (the latter is in not_checked).
func orEmpty(list []map[string]any) []map[string]any {
	if list == nil {
		return []map[string]any{}
	}
	return list
}
