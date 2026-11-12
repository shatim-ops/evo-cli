// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"net/netip"
	"sort"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The rule «the whole internet on a shared group» (D105; owner's decision
// Вп-4 and 12.11; docs/internal/briefs/sg-rules.md §2, §3.2): a tool that
// changes the rules of an existing group does not open a port to the
// internet on a group other VMs are in without the person's own word for
// it. One decision for every such tool — open_ports, sg_rule_create —
// which a refusal in one of them alone would only send the caller to the
// other.

// internetShorterThan are the prefix lengths below which a source is the
// internet: a refusal that only knew 0.0.0.0/0 would be passed with
// 0.0.0.0/1 and 128.0.0.0/1 («Риск», conclusion 146, У1).
const (
	internetShorterThanV4 = 8
	internetShorterThanV6 = 32
)

// privateRanges are the ranges that are not the internet however wide: a
// prefix that lies wholly in one of them is a private source.
var privateRanges = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", // RFC 1918
		"100.64.0.0/10",                 // RFC 6598
		"127.0.0.0/8", "169.254.0.0/16", // loopback, link-local
		"fc00::/7", "fe80::/10", "::1/128",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// Internet reports whether a source is the internet: its prefix is shorter
// than /8 (IPv4) or /32 (IPv6) and does not lie wholly in a private range.
// A bare address is the prefix of that one address; a source that does not
// parse is not one evo can call the internet — the schema refuses it.
func Internet(cidr string) bool {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	p = p.Masked()
	limit := internetShorterThanV4
	if p.Addr().Is6() && !p.Addr().Is4In6() {
		limit = internetShorterThanV6
	}
	if p.Bits() >= limit {
		return false
	}
	for _, private := range privateRanges {
		if private.Bits() <= p.Bits() && private.Contains(p.Addr()) {
			return false
		}
	}
	return true
}

// GroupUse is who a rule of a group acts on: whether the group is shared,
// and the interfaces and the VMs under it as far as evo read them.
type GroupUse struct {
	// Shared: the zone's default group, or more than one interface is in
	// the group. With the interfaces unread, a group the API says has
	// interfaces counts as shared: a failed read does not turn a shared
	// group into one of its own («Риск», conclusion 146, У3).
	Shared bool
	// Counted is false when the list of interfaces was not read:
	// Interfaces and VMs say nothing then.
	Counted    bool
	Interfaces int
	// VMs are the names (ids, when unnamed) of the VMs under the group,
	// sorted, at most MaxVMsNamed; MoreVMs is how many more there are.
	VMs     []string
	MoreVMs int
}

// MaxVMsNamed is how many VMs under a group a preview names.
const MaxVMsNamed = 10

// Use is the GroupUse of group by the project's interfaces; read is false
// when they could not be read.
func Use(group *provision.SecurityGroup, interfaces []*provision.Interface, read bool) GroupUse {
	if group == nil {
		return GroupUse{}
	}
	if !read {
		return GroupUse{Shared: group.Default || group.HasInterfaces}
	}
	u := GroupUse{Counted: true}
	vms := map[string]bool{}
	for _, in := range interfaces {
		if in == nil {
			continue
		}
		for _, g := range in.SecurityGroups {
			if g.ID != group.ID {
				continue
			}
			u.Interfaces++
			if in.VM != nil {
				name := in.VM.Name
				if name == "" {
					name = in.VM.ID
				}
				if name != "" {
					vms[name] = true
				}
			}
			break
		}
	}
	for name := range vms {
		u.VMs = append(u.VMs, name)
	}
	sort.Strings(u.VMs)
	if len(u.VMs) > MaxVMsNamed {
		u.MoreVMs = len(u.VMs) - MaxVMsNamed
		u.VMs = u.VMs[:MaxVMsNamed]
	}
	u.Shared = group.Default || u.Interfaces > 1
	return u
}
