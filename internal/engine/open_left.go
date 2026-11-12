// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"strings"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
)

// A rule a person or a tool added to the group of a VM (sg_rule_create,
// open_ports, the console) is not the deployment's: destroy does not
// delete it, and a port open to the whole internet would outlive the VM
// unseen («Риск», conclusion 146, У4). The preview of every delete of a VM
// says what stays open to the internet in the VM's groups — a fact, not an
// advice to delete: in a shared group other VMs use those rules.

// MaxOpenLeftRules is how many rules of a group the preview names.
const MaxOpenLeftRules = 10

// OpenLeft is what stays open to the internet in the security groups of a
// VM after its delete.
type OpenLeft struct {
	Groups []OpenLeftGroup `json:"groups,omitempty"`
	// Unread is the class of the error when evo could not read it: the
	// preview says so instead of saying nothing.
	Unread string `json:"unread,omitempty"`
}

// OpenLeftGroup is a group and its rules that stay.
type OpenLeftGroup struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Shared bool   `json:"shared"`
	// Interfaces is how many interfaces are in the group; nil when the
	// list was not read.
	Interfaces *int           `json:"interfaces,omitempty"`
	Rules      []OpenLeftRule `json:"rules"`
	// More is how many more rules stay than Rules names.
	More int `json:"more,omitempty"`
}

// OpenLeftRule is a rule that stays, by its fields.
type OpenLeftRule struct {
	ID        string `json:"id"`
	Protocol  string `json:"protocol"`
	PortRange string `json:"port_range"`
	Source    string `json:"source"`
}

// OpenLeft reads what stays open to the internet after the delete of VM
// vmID: the active ingress rules of its groups whose source is the
// internet (sgrules.Internet — the threshold, not two literals) and that
// this delete does not close (closing: the ids of the rules it deletes).
// groupIDs are the groups the records of the VM name; with none, the
// groups of the VM's interfaces. nil when nothing stays and all was read.
func (e *Engine) OpenLeft(ctx context.Context, projectID, vmID string, groupIDs []string, closing map[string]bool) *OpenLeft {
	if e.Cloud == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	interfaces, ifErr := e.Cloud.ListInterfaces(ctx, projectID)
	if len(groupIDs) == 0 {
		if ifErr != nil {
			return &OpenLeft{Unread: clerrors.Reason(ifErr)}
		}
		seen := map[string]bool{}
		for _, in := range interfaces {
			if in == nil || in.VM == nil || in.VM.ID != vmID {
				continue
			}
			for _, g := range in.SecurityGroups {
				if !seen[g.ID] {
					seen[g.ID] = true
					groupIDs = append(groupIDs, g.ID)
				}
			}
		}
	}
	if len(groupIDs) == 0 {
		return nil
	}
	groups, err := e.Cloud.ListSecurityGroups(ctx, projectID)
	if err != nil {
		return &OpenLeft{Unread: clerrors.Reason(err)}
	}
	byID := map[string]*provision.SecurityGroup{}
	for _, g := range groups {
		if g != nil {
			byID[g.ID] = g
		}
	}
	out := &OpenLeft{}
	for _, id := range groupIDs {
		group := byID[id]
		if group == nil {
			// Not a group of this project any more: nothing of it to say.
			continue
		}
		rules, err := e.Cloud.ListSecurityGroupRules(ctx, id)
		if err != nil {
			out.Unread = clerrors.Reason(err)
			continue
		}
		left := OpenLeftGroup{ID: id, Name: group.Name}
		for _, r := range rules {
			if r == nil || closing[r.ID] || !sgrules.OpenToInternet(r) {
				continue
			}
			if len(left.Rules) == MaxOpenLeftRules {
				left.More++
				continue
			}
			left.Rules = append(left.Rules, OpenLeftRule{ID: r.ID, Protocol: r.IPProtocol, PortRange: r.PortRange, Source: r.RemoteIPPrefix})
		}
		if len(left.Rules) == 0 {
			continue
		}
		use := sgrules.Use(group, interfaces, ifErr == nil)
		left.Shared = use.Shared
		if use.Counted {
			n := use.Interfaces
			left.Interfaces = &n
		}
		out.Groups = append(out.Groups, left)
	}
	if len(out.Groups) == 0 && out.Unread == "" {
		return nil
	}
	return out
}

// Lines says OpenLeft in the lines of a preview: one per group, and one
// for what was not read.
func (o *OpenLeft) Lines() []string {
	if o == nil {
		return nil
	}
	var lines []string
	for _, g := range o.Groups {
		rules := make([]string, 0, len(g.Rules))
		for _, r := range g.Rules {
			rules = append(rules, fmt.Sprintf("%s %s %s", r.Protocol, r.PortRange, i18n.Tf("from %s", "с %s", r.Source)))
		}
		list := strings.Join(rules, "; ")
		if g.More > 0 {
			list += i18n.Tf("; and %d more", "; и ещё %d", g.More)
		}
		use := ""
		switch {
		case g.Shared && g.Interfaces != nil:
			use = i18n.Tf(" (shared: %d interface(s) in it)", " (общая: интерфейсов в ней — %d)", *g.Interfaces)
		case g.Shared:
			use = i18n.T(" (shared)", " (общая)")
		}
		lines = append(lines, i18n.Tf("stay open to the internet in security group %q%s after the delete: %s",
			"останутся открыты в интернет в группе безопасности %q%s после удаления: %s", g.Name, use, list))
	}
	if o.Unread != "" {
		lines = append(lines, i18n.Tf("evo could not read what stays open to the internet in the VM's security group (%s)",
			"что останется открытым в интернет в группе безопасности ВМ, evo не узнал (%s)", o.Unread))
	}
	return lines
}

// ClosingRules are the ids of the rules a delete of a VM deletes.
func ClosingRules(planned []sgrules.Planned) map[string]bool {
	closing := map[string]bool{}
	for _, p := range planned {
		if p.KeptFor == "" && p.Rule.ID != "" {
			closing[p.Rule.ID] = true
		}
	}
	return closing
}
