// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
)

// sgRuleMadeTimeout bounds the wait for a new rule to become "created".
// How long the platform keeps a rule in "creating" is not measured (probe
// Г-11 of the 1.9 plan): the bound is the one the delete of a rule has.
const sgRuleMadeTimeout = sgRuleGoneTimeout

// resourceSGRule is a rule of a security group in a preview's
// price_classes.
const resourceSGRule = "security_group_rule"

// sgRuleDescriptionMax is the longest description the API takes.
const sgRuleDescriptionMax = 255

// sgRuleWanted is the rule of a call of sg_rule_create as a preview and an
// answer name it: what goes into the request, with the source group by
// its name too.
func sgRuleWanted(want provision.SGRule, sourceGroup *provision.SecurityGroup) map[string]any {
	out := map[string]any{"direction": want.Direction, "protocol": want.IPProtocol, "port_range": want.PortRange,
		"ether_type": want.EtherType}
	if sourceGroup != nil {
		out["source"] = "security group " + firstNonEmptyStr(sourceGroup.Name, sourceGroup.ID)
		out["remote_group"] = map[string]any{"id": sourceGroup.ID, "name": sourceGroup.Name}
	} else {
		out["source"] = want.RemoteIPPrefix
	}
	if want.Description != "" {
		out["description"] = want.Description
	}
	return out
}

// planSGRuleCreate plans sg_rule_create (gate.go;
// docs/internal/briefs/sg-rules.md §3): one rule of a security group,
// named whole by the caller — no default for the direction or the source.
// The group, its rules and the interfaces under it are read before the
// question. The same rule there already is already_exists with no
// question and no write; a wider one is named (covered_by) and the
// question asked. An ingress rule from the whole internet on a shared
// group is refused without the person's word for it — the decision of
// open_ports (D105, internetOnSharedGroup).
func (s *Registry) planSGRuleCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	sgRef, direction, proto := stringArg(args, "security_group"), stringArg(args, "direction"), stringArg(args, "protocol")
	if sgRef == "" || direction == "" || proto == "" || stringArg(args, "ports") == "" {
		return nil, usageErr("'security_group', 'direction', 'protocol' and 'ports' are required",
			"нужны 'security_group', 'direction', 'protocol' и 'ports'")
	}
	ports, ok := sgrules.Ports(stringArg(args, "ports"))
	if !ok {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("ports %q is neither a port nor a range of ports within 1-65535", "ports %q — не порт и не диапазон портов в пределах 1-65535", stringArg(args, "ports")),
			i18n.T("Pass one port (443) or a range (8000:8100).", "Укажите один порт (443) или диапазон (8000:8100)."))
	}
	source, sourceArg, err := oneOfArgs(args, clierrors.New(clierrors.CodeUsage,
		i18n.T("pass cidr or remote_group, not both", "укажите cidr или remote_group, не оба"),
		i18n.T("A rule has one source: a CIDR, or another security group.", "У правила один источник: CIDR или другая группа безопасности.")),
		"cidr", "remote_group")
	if err != nil {
		return nil, err
	}
	if sourceArg == "" {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.T("the source of the rule is required: 'cidr' or 'remote_group'", "нужен источник правила: 'cidr' или 'remote_group'"),
			i18n.T("evo has no default source: ask the person which addresses the rule is for (for egress — the destination).",
				"Источника по умолчанию нет: спросите человека, для каких адресов правило (для egress — назначение)."))
	}
	description := stringArg(args, "description")
	if utf8.RuneCountInString(description) > sgRuleDescriptionMax {
		return nil, usageErr(fmt.Sprintf("'description' is longer than %d characters", sgRuleDescriptionMax),
			fmt.Sprintf("'description' длиннее %d знаков", sgRuleDescriptionMax))
	}
	want := provision.SGRule{Direction: direction, IPProtocol: proto, PortRange: ports, EtherType: "IPv4", Description: description}
	if sourceArg == "cidr" {
		if want.RemoteIPPrefix, want.EtherType, ok = sgrules.Prefix(source); !ok {
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("cidr %q is neither a CIDR nor an address", "cidr %q — не CIDR и не адрес", source),
				i18n.T("Pass a CIDR, e.g. 203.0.113.0/24.", "Укажите CIDR, например 203.0.113.0/24."))
		}
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	group, err := securityGroupByRef(ctx, eng, sgRef)
	if err != nil {
		return nil, err
	}
	var sourceGroup *provision.SecurityGroup
	if sourceArg == "remote_group" {
		if sourceGroup, err = securityGroupByRef(ctx, eng, source); err != nil {
			return nil, err
		}
		want.RemoteSecurityGroupID = sourceGroup.ID
	}
	rules, err := eng.Cloud.ListSecurityGroupRules(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	// The repeat (§3.3): the same rule, by its state.
	var failed []string
	var coveredBy []map[string]any
	for _, r := range rules {
		switch {
		case r == nil:
		case sgrules.Same(r, want):
			switch {
			case r.State == "creating":
				// Being created by an earlier call: its outcome, with no
				// second request.
				return answered(s.sgRuleCreated(ctx, eng, group, r.ID, sgRuleFields(r))), nil
			case engine.DeletingState(r.State):
				// Going away: not there for the caller; a new one is asked.
			case sgRuleFailed(r.State):
				failed = append(failed, r.ID)
			default:
				return answered(toJSON(map[string]any{"status": "already_exists", "rule_id": r.ID, "rule": sgRuleFields(r),
					"security_group": group.Name, "security_group_id": group.ID,
					"message": i18n.Tf("security group %s has this rule already (%s): nothing was created",
						"в группе безопасности %s такое правило уже есть (%s): ничего не создано", quoteCloud(group.Name), r.ID)}), false), nil
			}
		case sgrules.Wider(r, want):
			coveredBy = append(coveredBy, sgRuleFields(r))
		}
	}
	use := sgGroupUse(ctx, eng, group)
	internet := direction == "ingress" && sourceArg == "cidr" && sgrules.Internet(want.RemoteIPPrefix)
	if direction == "ingress" && sourceArg == "cidr" {
		if err := internetOnSharedGroup(group, use, ports+"/"+proto, want.RemoteIPPrefix, boolArg(args, allowInternetArg, false)); err != nil {
			return nil, err
		}
	}
	wanted := sgRuleWanted(want, sourceGroup)
	will := map[string]any{"rule": wanted,
		"security_group": map[string]any{"id": group.ID, "name": group.Name, "default": group.Default},
		// No tariff evo has read says a rule is free: the class of the
		// price and where the rates are, not «0 ₽» (CLAUDE.md §6, axis 4).
		"price_classes": []engine.ResourcePrice{{Resource: resourceSGRule, Class: engine.PricePaidUnpriced,
			Note: i18n.T("price unknown, the resource is billable: no line of the tariffs at tariff_url names a rule of a security group",
				"цена неизвестна, ресурс платный: в тарифах по tariff_url нет строки о правиле группы безопасности")}},
		"tariff_url": engine.TariffURL(engine.ResourceVPC),
	}
	sgUseFields(will, use)
	if internet {
		will["open_to_internet"] = true
		will["warning"] = i18n.Tf("open to the internet: any address will reach %s/%s of every VM of the group",
			"открыто в интернет: любой адрес сможет обратиться к %s/%s каждой ВМ группы", ports, proto)
	}
	if direction == "egress" {
		will["egress_note"] = i18n.T("an egress rule: what leaves the group with no egress rule, and what the first one changes, evo has not verified",
			"исходящее правило: что выходит из группы без исходящих правил и что меняет первое из них, evo не проверял")
	}
	if len(coveredBy) > 0 {
		// A wider rule lets this through already: whether the narrow one
		// is still wanted is the person's to say.
		will["covered_by"] = coveredBy
	}
	if len(failed) > 0 {
		will["failed_rules"] = failed
	}
	message := i18n.Tf("sg_rule_create adds the rule %s %s %s %s to security group %s.", "sg_rule_create добавляет правило %s %s %s %s в группу безопасности %s.",
		direction, proto, ports, wanted["source"], quoteCloud(group.Name))
	return &Plan{Preview: map[string]any{"message": message, "preview": will}, Execute: func(ctx context.Context) (string, bool) {
		made, err := eng.Cloud.AddSecurityGroupRule(ctx, group.ID, eng.Config.ProjectID, want)
		id := ""
		if err == nil && made != nil {
			id = made.ID
		}
		if id == "" {
			// The answer was lost, or named no rule: the rule may be
			// there. The rules are read again — the same rule, there now
			// and not before the request, is the one this call made.
			var refused *provision.StatusError
			errors.As(err, &refused)
			// A refusal of the platform (4xx) made nothing; anything else
			// — a lost answer, a 5xx, an answer with no id — leaves the
			// outcome unknown.
			refusedByPlatform := refused != nil && refused.Status >= 400 && refused.Status < 500
			again, listErr := eng.Cloud.ListSecurityGroupRules(ctx, group.ID)
			there := sgRuleSame(again, want)
			if there == nil && !refusedByPlatform {
				// The list may be behind the write: one read that does not
				// show the rule is not «not made». The call waits for it to
				// be listed before it says the outcome is unknown.
				there = s.sgRuleListed(ctx, eng, group, want)
			}
			if there != nil && refused != nil && refused.Status == http.StatusConflict {
				// The platform refused the write as a conflict and the
				// rule is there: another call made it between the read of
				// the plan and the write — not this one.
				return toJSON(map[string]any{"status": "already_exists", "rule_id": there.ID, "rule": sgRuleFields(there),
					"security_group": group.Name, "security_group_id": group.ID}), false
			}
			if there != nil {
				id = there.ID
			}
			if id == "" {
				if err == nil {
					err = noID("rule")
				}
				fields := map[string]any{"security_group": group.Name, "security_group_id": group.ID}
				if code := platformCode(err); code != "" {
					fields["details"] = map[string]any{"platform_code": code}
				}
				if refusedByPlatform && listErr == nil {
					return errAnswer(fmt.Errorf(i18n.T("create rule: %w", "создание правила: %w"), err), fields), true
				}
				// The outcome is unknown: the error sends to the read, not
				// to the same call again — that would be a second rule
				// (everySendIsAChange asks a consented repeat again).
				err = clierrors.Wrap(clierrors.Reason(err), i18n.Tf("create rule: %v", "создание правила: %v", err),
					i18n.T("The rule may have been created: list_security_groups with rules=true shows the rules of the group. Do not send the call again before reading them.",
						"Правило могло быть создано: правила группы показывает list_security_groups с rules=true. Не повторяйте вызов, пока не прочитаете их."), err)
				return errAnswer(err, fields), true
			}
		}
		if made != nil && made.ID == id && made.State == "created" {
			// The answer of the write says the rule is in force: no read.
			return toJSON(map[string]any{"status": "created", "rule_id": id, "rule": wanted,
				"security_group": group.Name, "security_group_id": group.ID}), false
		}
		return s.sgRuleCreated(ctx, eng, group, id, wanted)
	}}, nil
}

// sgRuleSame is the first rule of rules that is the rule want and is not
// going away or failed: of several the same, one is named.
func sgRuleSame(rules []*provision.SGRule, want provision.SGRule) *provision.SGRule {
	for _, r := range rules {
		if r != nil && sgrules.Same(r, want) && !engine.DeletingState(r.State) && !sgRuleFailed(r.State) {
			return r
		}
	}
	return nil
}

// sgRuleListed waits, after a write whose outcome is unknown, for the
// rule want to be listed in group: the list of rules may be behind the
// write. nil when the wait — its own limit, or that of an MCP call — ends
// without the rule.
func (s *Registry) sgRuleListed(ctx context.Context, eng *engine.Engine, group *provision.SecurityGroup, want provision.SGRule) *provision.SGRule {
	const listed, absent = "listed", "not listed"
	var found *provision.SGRule
	_, _, _, _ = waitState(ctx, eng, sgRuleMadeTimeout,
		func(ctx context.Context) (string, bool, error) {
			rules, err := eng.Cloud.ListSecurityGroupRules(ctx, group.ID)
			if err != nil {
				return "", false, nil
			}
			if found = sgRuleSame(rules, want); found != nil {
				return listed, true, nil
			}
			return absent, true, nil
		},
		func(string) string {
			return i18n.T("the answer of the create was lost: waiting for the rule to be listed…", "ответ на создание потерян: жду, появится ли правило в списке…")
		},
		func(st string) (bool, error) { return st == listed, nil })
	return found
}

// sgRuleCreated waits for rule id of group to become "created" and
// answers: created; at the limit — of the wait or of an MCP call — the
// rule is still being created, and the read of the group's rules is the
// next step; a rule that failed is an error that names it, for
// sg_rule_delete to remove.
func (s *Registry) sgRuleCreated(ctx context.Context, eng *engine.Engine, group *provision.SecurityGroup, id string, rule map[string]any) (string, bool) {
	last, timedOut, atLimit, err := waitState(ctx, eng, sgRuleMadeTimeout,
		func(ctx context.Context) (string, bool, error) {
			rules, err := eng.Cloud.ListSecurityGroupRules(ctx, group.ID)
			if err != nil {
				return "", false, nil
			}
			for _, r := range rules {
				if r != nil && r.ID == id {
					// A rule listed with no state is listed as it is.
					return firstNonEmptyStr(r.State, "created"), true, nil
				}
			}
			// Not listed yet: the write was taken, the list is behind.
			return "creating", true, nil
		},
		func(st string) string {
			return fmt.Sprintf(i18n.T("rule %s: %s, waiting…", "правило %s: %s, жду…"), id, st)
		},
		func(st string) (bool, error) {
			switch {
			case st == "created":
				return true, nil
			case sgRuleFailed(st) || engine.DeletingState(st):
				return true, clierrors.New(clierrors.CodeAPIError,
					i18n.Tf("the new rule of security group %s is in state %s after the create", "новое правило группы безопасности %s после создания в состоянии %s", quoteCloud(group.Name), st),
					i18n.T("Ask the person whether to remove it: sg_rule_delete takes the rule_id of this answer.", "Спросите человека, удалять ли его: sg_rule_delete принимает rule_id из этого ответа."))
			}
			return false, nil
		})
	ans := map[string]any{"rule_id": id, "rule": rule, "security_group": group.Name, "security_group_id": group.ID}
	switch {
	case err != nil:
		ans["state"] = last
		return errAnswer(err, ans), true
	case atLimit || timedOut > 0:
		ans["status"], ans["state"] = "creating", last
		ans["next"] = fmt.Sprintf("Rule %s is still being created. list_security_groups with rules=true shows its state.", id)
		nextStep(ans, "list_security_groups", map[string]any{"rules": true})
		return toJSON(ans), false
	}
	ans["status"] = "created"
	return toJSON(ans), false
}

// sgRuleFailed reports a state of a rule the platform left in an error.
func sgRuleFailed(state string) bool { return strings.HasPrefix(state, "error") }
