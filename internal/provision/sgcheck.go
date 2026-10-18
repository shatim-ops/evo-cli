// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"net/netip"
	"strconv"
	"strings"
)

// GroupGaps is what a security group's rules lack for a new VM.
type GroupGaps struct {
	// NoSSH: no ingress rule lets tcp 22 in (from any source), so the
	// VM's sshd is unreachable and deploy waits for SSH in vain.
	NoSSH bool
	// NoSystemEgress: no egress rule lets the VM reach the metadata
	// service (169.254.169.254/32) or DHCP (udp 67 to the subnet gateway):
	// the VM gets no IP and cloud-init does not run (docs, «Группы
	// безопасности», ограничения: «Исходящий системный трафик», 07.2026).
	// An egress rule to 0.0.0.0/0 for any protocol covers both.
	NoSystemEgress bool
}

// GroupRules returns a group's rules: from the list answer when it has
// them, else from the group's rules list.
func GroupRules(ctx context.Context, cloud CloudAPI, g *SecurityGroup) ([]SGRule, error) {
	if len(g.Rules) > 0 {
		return g.Rules, nil
	}
	listed, err := cloud.ListSecurityGroupRules(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	out := make([]SGRule, 0, len(listed))
	for _, r := range listed {
		out = append(out, *r)
	}
	return out, nil
}

// Warnings words the gaps of group name for an agent (English, as MCP
// answers are).
func (g GroupGaps) Warnings(name string) []string {
	var out []string
	if g.NoSSH {
		out = append(out, "security group "+name+" has no ingress rule for TCP 22: SSH to the VM will not come up; add an ingress TCP 22 rule or choose another group")
	}
	if g.NoSystemEgress {
		out = append(out, "security group "+name+" does not allow the VM's system egress (metadata service 169.254.169.254/32 and UDP 67 to the subnet gateway): the VM gets no IP by DHCP and cloud-init does not run; allow all egress or these two rules (Cloud.ru docs: security groups, limitations)")
	}
	return out
}

// metadataIP is the metadata service a VM reaches at boot.
var metadataIP = netip.MustParseAddr("169.254.169.254")

// CheckVMGroupRules looks for the rules a new VM needs in its group;
// gateway is the subnet's default_gateway ("" when unknown: then any
// destination of a udp 67 rule counts). What cannot be read (an unknown
// port range, a rule without a prefix) counts as allowing: the check warns
// only when the rules clearly lack it. A group without rules blocks all
// traffic (docs), so both are missing.
func CheckVMGroupRules(rules []SGRule, gateway string) GroupGaps {
	gw, gwErr := netip.ParseAddr(gateway)
	ssh, metadata, dhcp := false, false, false
	for _, r := range rules {
		if inactiveRule(r.State) {
			continue
		}
		proto := strings.ToLower(r.IPProtocol)
		switch strings.ToLower(r.Direction) {
		case "ingress":
			if (proto == "" || proto == "any" || proto == "tcp") && portInRange(r.PortRange, 22) {
				ssh = true
			}
		case "egress":
			if prefixHas(r.RemoteIPPrefix, metadataIP) &&
				(proto == "" || proto == "any" || (proto == "tcp" && portInRange(r.PortRange, 80))) {
				metadata = true
			}
			if (proto == "" || proto == "any" || proto == "udp") && portInRange(r.PortRange, 67) &&
				(gwErr != nil || prefixHas(r.RemoteIPPrefix, gw)) {
				dhcp = true
			}
		}
	}
	return GroupGaps{NoSSH: !ssh, NoSystemEgress: !metadata || !dhcp}
}

// inactiveRule: a rule that does not (or soon will not) let traffic pass.
func inactiveRule(state string) bool {
	switch state {
	case "deleting", "error", "error_creating", "error_deleting":
		return true
	}
	return false
}

// portInRange reads "22", "22:22", "80-443", "80:443"; an empty range, or
// one it cannot read, counts as any port.
func portInRange(pr string, port int) bool {
	pr = strings.TrimSpace(pr)
	if pr == "" || strings.EqualFold(pr, "any") {
		return true
	}
	lo, hi, ok := strings.Cut(strings.ReplaceAll(pr, "-", ":"), ":")
	if !ok {
		hi = lo
	}
	a, err1 := strconv.Atoi(strings.TrimSpace(lo))
	b, err2 := strconv.Atoi(strings.TrimSpace(hi))
	if err1 != nil || err2 != nil {
		return true
	}
	return a <= port && port <= b
}

// prefixHas: the rule's destination covers ip. No prefix (a rule to a
// security group, or one the answer leaves out) counts as covering.
func prefixHas(prefix string, ip netip.Addr) bool {
	if prefix == "" {
		return true
	}
	p, err := netip.ParsePrefix(prefix)
	if err != nil {
		if a, aerr := netip.ParseAddr(prefix); aerr == nil {
			return a == ip
		}
		return true
	}
	return p.Contains(ip)
}
