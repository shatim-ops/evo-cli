// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A rule a caller names whole (sg_rule_create) is compared with the rules
// of its group exactly: Coverage is the rule of «a port is open» (D52),
// and it calls a wider rule «open already» — right for a port, wrong for a
// rule a person asked for by its fields
// (docs/internal/briefs/sg-rules.md §3.3).

// Ports reads the ports of a rule as a caller gives them — one port
// («443») or a range («8000:8100») — and returns them as the API takes
// them («443:443»); false for what is neither, or is out of 1-65535.
func Ports(s string) (string, bool) {
	lo, hi, ok := portRange(s)
	if !ok || lo < 1 || hi > 65535 {
		return "", false
	}
	return fmt.Sprintf("%d:%d", lo, hi), true
}

// Prefix reads a source or a destination as a caller gives it — a prefix
// or one address — and returns it as a prefix with no bits past its
// length, and the ether type of its family. An IPv4 address written as
// IPv6 is IPv4: one address, one answer (Internet judges it the same); false for what is
// neither a prefix nor an address.
func Prefix(cidr string) (prefix, etherType string, ok bool) {
	p, perr := netip.ParsePrefix(strings.TrimSpace(cidr))
	if perr != nil {
		a, aerr := netip.ParseAddr(strings.TrimSpace(cidr))
		if aerr != nil {
			return "", "", false
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	if a := p.Addr(); a.Is4In6() {
		if p.Bits() < 96 {
			return "", "", false
		}
		p = netip.PrefixFrom(a.Unmap(), p.Bits()-96)
	}
	p = p.Masked()
	if p.Addr().Is4() {
		return p.String(), "IPv4", true
	}
	return p.String(), "IPv6", true
}

// samePrefix reports whether the prefix a rule answers is the prefix
// asked. A rule that names no prefix is not «anywhere» here: what the
// platform answers for a rule from anywhere is not verified (probe Г-11).
func samePrefix(have, want string) (same, holds bool) {
	h, _, hok := Prefix(have)
	w, _, wok := Prefix(want)
	if have == "" || !hok || !wok {
		return false, false
	}
	hp, wp := netip.MustParsePrefix(h), netip.MustParsePrefix(w)
	if hp.Addr().Is4() != wp.Addr().Is4() {
		return false, false
	}
	return hp == wp, hp.Bits() <= wp.Bits() && hp.Contains(wp.Addr())
}

// sameSource compares the source of rule r with the one want names: the
// same (same), or one that holds it (holds). A source that is a group is
// the same only as that group, and holds nothing but itself.
func sameSource(r *provision.SGRule, want provision.SGRule) (same, holds bool) {
	if want.RemoteSecurityGroupID != "" {
		is := r.RemoteSecurityGroup != nil && r.RemoteSecurityGroup.ID == want.RemoteSecurityGroupID
		return is, is
	}
	if r.RemoteSecurityGroup != nil {
		return false, false
	}
	return samePrefix(r.RemoteIPPrefix, want.RemoteIPPrefix)
}

// Same reports whether r is the rule want asks for: the direction, the
// protocol, the ports and the source, field by field. The state of r is
// the caller's to read.
func Same(r *provision.SGRule, want provision.SGRule) bool {
	if r == nil || r.Direction != want.Direction || !strings.EqualFold(r.IPProtocol, want.IPProtocol) {
		return false
	}
	rlo, rhi, rok := portRange(r.PortRange)
	wlo, whi, wok := portRange(want.PortRange)
	if !rok || !wok || rlo != wlo || rhi != whi {
		return false
	}
	same, _ := sameSource(r, want)
	return same
}

// Wider reports whether r is an active rule that lets through all want
// would and more: the same direction, the protocol of want or any, ports
// that hold want's, a source that holds want's. Such a rule is not «the
// rule is there already»: whether the narrow one is still wanted is the
// person's to say.
func Wider(r *provision.SGRule, want provision.SGRule) bool {
	if r == nil || inactive[r.State] || r.Direction != want.Direction || Same(r, want) {
		return false
	}
	if rp := strings.ToLower(r.IPProtocol); rp != strings.ToLower(want.IPProtocol) && rp != "any" {
		return false
	}
	rlo, rhi, rok := portRange(r.PortRange)
	wlo, whi, wok := portRange(want.PortRange)
	if !rok || !wok || rlo > wlo || rhi < whi {
		return false
	}
	_, holds := sameSource(r, want)
	return holds
}
