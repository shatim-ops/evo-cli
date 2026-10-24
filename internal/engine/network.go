// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"sort"
	"strings"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// vmNetwork is where a new VM is plugged in. The compute create API 500s
// without an explicit interface, so deploy always resolves one.
type vmNetwork struct {
	// ZoneID is the UUID of the zone the VM goes to (resolveNetwork).
	ZoneID          string
	ZoneName        string
	SubnetID        string
	SubnetName      string
	SecurityGroupID string
	SecurityGroup   string
}

// ResolveZoneRef is the zone a person or an agent named for a new VM — its
// UUID or its name, in any case (provision.ResolveZone) — from zones, the
// list as read, and listErr, the error of reading it. A UUID stays as
// given (nil zone, no error) when the list is unread, empty or lacks it:
// the create call answers for it, as before. A name needs the list:
// unread, it is refused with the list's error; naming no listed zone — an
// empty list too (Cursor review of 1.2.2, CUR122-4: the name reached the
// API) — with ResolveZone's USAGE. An empty ref names
// nothing. evo deploy, evo vm create and MCP provision take the zone by
// this one rule (review of the zone in 1.2.1), before the consent and
// anything created.
func ResolveZoneRef(zones []*provision.AvailabilityZone, listErr error, ref string) (*provision.AvailabilityZone, error) {
	switch {
	case ref == "":
		return nil, nil
	case listErr != nil && isUUID(ref):
		return nil, nil
	case listErr != nil:
		return nil, clerrors.Prefix(i18n.Tf("availability zone %q: list zones: ", "зона доступности %q: список зон: ", ref), listErr)
	}
	zone, err := provision.ResolveZone(zones, ref)
	if err != nil && isUUID(ref) {
		return nil, nil
	}
	return zone, err
}

// resolveNetwork picks a subnet (provision.PickSubnet: the zone's main
// subnet, then a routed one) and a security group for the zone: by the
// zone id the list returns nested (availability_zone.id), else by a name
// that contains the zone name ("Default_ru.AZ-1", "SSH-access_ru.AZ-1"),
// the same rule as the MCP vm_create path. A security group from another
// zone keeps the VM from booting, hence no blind "first one" for groups
// unless the project has a single candidate. The zone is zoneRef, its UUID
// or its name (ResolveZoneRef); n.ZoneID is its UUID.
func (e *Engine) resolveNetwork(ctx context.Context, projectID, zoneRef string) (vmNetwork, error) {
	n := vmNetwork{ZoneID: zoneRef}
	zones, err := e.Cloud.ListAvailabilityZones(ctx)
	zone, err := ResolveZoneRef(zones, err, zoneRef)
	if err != nil {
		return n, err
	}
	if zone != nil {
		n.ZoneID, n.ZoneName = zone.ID, zone.Name
	}
	if err := provision.ClosedZoneError(zones, n.ZoneID); err != nil {
		return n, err
	}
	zoneID := n.ZoneID

	subnets, err := e.Cloud.ListSubnets(ctx, projectID)
	if err != nil {
		return n, fmt.Errorf(i18n.T("list subnets: %w", "список подсетей: %w"), err)
	}
	gateway := ""
	if sn := provision.PickSubnet(subnets, zoneID, n.ZoneName); sn >= 0 {
		n.SubnetID, n.SubnetName = subnets[sn].ID, subnets[sn].Name
		gateway = subnets[sn].DefaultGateway
		if subnets[sn].NoDNS() {
			e.Printer.Warning(subnetNoDNSWarning(subnets[sn].Name))
		}
	} else {
		return n, NoSubnetError(n.ZoneName, zoneID)
	}

	sgs, err := e.Cloud.ListSecurityGroups(ctx, projectID)
	if err != nil {
		return n, fmt.Errorf(i18n.T("list security groups: %w", "список групп безопасности: %w"), err)
	}
	if want := e.Config.SecurityGroup; want != "" {
		for _, g := range sgs {
			if g.ID != want && g.Name != want {
				continue
			}
			// A group of another zone keeps the VM from booting, and deploy
			// would open the compose ports in it. A group without a zone in
			// the API answer is taken as is: nothing to check it against.
			if g.AvailabilityZoneID != "" && g.AvailabilityZoneID != zoneID {
				return n, clerrors.New(clerrors.CodeConfigInvalid,
					i18n.Tf("security group %q from security_group is not in zone %s", "группа безопасности %q из security_group не в зоне %s", want, zoneText(n.ZoneName, zoneID)),
					i18n.T("Name a group of this zone in security_group, or remove the key: evo then takes the zone's default group (SSH-access_<zone>).",
						"Укажите в security_group группу этой зоны или уберите ключ: тогда evo возьмёт группу зоны по умолчанию (SSH-access_<зона>)."))
			}
			n.SecurityGroupID, n.SecurityGroup = g.ID, g.Name
			e.warnGroupRules(ctx, g, gateway)
			return n, nil
		}
		return n, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("security group %q from security_group is not in the project", "группы безопасности %q из security_group нет в проекте", want),
			i18n.T("Check the name with 'evo tools call list_security_groups', or remove security_group from .evo.yaml.",
				"Проверьте имя ('evo tools call list_security_groups') или уберите security_group из .evo.yaml."))
	}
	if g := provision.PickSecurityGroup(sgs, zoneID, n.ZoneName); g >= 0 {
		n.SecurityGroupID, n.SecurityGroup = sgs[g].ID, sgs[g].Name
		if others := zoneGroups(sgs, zoneID, n.ZoneName); len(others) > 1 && !provision.IsDefaultGroup(sgs[g], n.ZoneName) {
			e.Printer.Warning(i18n.Tf("several security groups in zone %s (%s), none is the zone's default group: took %s; deploy opens the compose ports in it. Set security_group in .evo.yaml to choose",
				"в зоне %s несколько групп безопасности (%s), группы по умолчанию среди них нет: взята %s, в ней deploy откроет порты compose. Выбрать другую — security_group в .evo.yaml",
				zoneText(n.ZoneName, zoneID), strings.Join(others, ", "), sgs[g].Name))
		}
		e.warnGroupRules(ctx, sgs[g], gateway)
	} else {
		return n, NoSecurityGroupError(n.ZoneName, zoneID)
	}
	return n, nil
}

// NoSubnetError: the project has no subnet in the VM's zone. A subnet of
// another zone keeps the VM from booting (error_creating) while its boot
// FIP is billed, so deploy and the MCP provision refuse before creating
// anything.
func NoSubnetError(zoneName, zoneID string) error {
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("no subnet for zone %s in the project", "в проекте нет подсети для зоны %s", zoneText(zoneName, zoneID)),
		i18n.T("Create a network and a subnet in this zone in the Cloud.ru console, or pick another zone with 'evo init'.",
			"Создайте сеть и подсеть в этой зоне в консоли Cloud.ru или выберите другую зону ('evo init')."))
}

// NoSecurityGroupError: the project has groups, none in the VM's zone; a
// group of another zone keeps the VM from booting.
func NoSecurityGroupError(zoneName, zoneID string) error {
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("no security group for zone %s in the project", "в проекте нет группы безопасности для зоны %s", zoneText(zoneName, zoneID)),
		i18n.T("Create a security group whose name contains the zone name (e.g. 'web_ru.AZ-1') in the Cloud.ru console.",
			"Создайте в консоли Cloud.ru группу безопасности, в имени которой есть имя зоны (например, 'web_ru.AZ-1')."))
}

// warnGroupRules warns, before the preview, when the VM's group lacks what
// a new VM needs (provision.CheckVMGroupRules): ingress tcp 22 — else SSH
// never comes up and deploy waits for it in vain while the VM is billed —
// and egress system traffic — else the VM gets no IP and cloud-init does
// not run. The rules come from the list answer, else from the rules list;
// when neither can be read, there is no warning. A warning, not a refusal:
// the group may be meant for it (a bastion, a VPN).
func (e *Engine) warnGroupRules(ctx context.Context, g *provision.SecurityGroup, gateway string) {
	rules, err := provision.GroupRules(ctx, e.Cloud, g)
	if err != nil {
		return
	}
	gaps := provision.CheckVMGroupRules(rules, gateway)
	if gaps.NoSSH {
		e.Printer.Warning(groupNoSSHWarning(g.Name) + i18n.T(" (security_group in .evo.yaml); without SSH deploy cannot install Docker",
			" (security_group в .evo.yaml); без SSH deploy не установит Docker"))
	}
	if gaps.NoSystemEgress {
		e.Printer.Warning(groupNoEgressWarning(g.Name))
	}
}

func groupNoSSHWarning(name string) string {
	return i18n.Tf("security group %s has no ingress rule for TCP 22: SSH to the VM will not come up. Add an ingress TCP 22 rule in the Cloud.ru console or choose another group",
		"в группе безопасности %s нет входящего правила для TCP 22: SSH к ВМ не поднимется. Добавьте входящее правило TCP 22 в консоли Cloud.ru или выберите другую группу", name)
}

func groupNoEgressWarning(name string) string {
	return i18n.Tf("security group %s does not allow the VM's system egress (the metadata service 169.254.169.254/32 and UDP 67 to the subnet gateway): the VM gets no IP by DHCP and cloud-init does not run. Allow all egress, or these two rules (Cloud.ru docs, security groups, limitations)",
		"группа безопасности %s не разрешает исходящий системный трафик ВМ (сервис метаданных 169.254.169.254/32 и UDP 67 до шлюза подсети): ВМ не получит IP по DHCP, cloud-init не запустится. Разрешите весь исходящий трафик или эти два правила (документация Cloud.ru, «Группы безопасности», ограничения)", name)
}

// NetworkCheck is what the preview of a VM found wrong with the network
// the call will pick (the same choice as provision): a subnet without DNS
// servers, a security group without ingress TCP 22 or system egress.
// Warnings, not refusals.
type NetworkCheck struct {
	Subnet         string `json:"subnet,omitempty"`
	SubnetNoDNS    bool   `json:"subnet_no_dns,omitempty"`
	SecurityGroup  string `json:"security_group,omitempty"`
	NoSSHIngress   bool   `json:"no_ssh_ingress,omitempty"`
	NoSystemEgress bool   `json:"no_system_egress,omitempty"`
}

// checkNetwork resolves the subnet and the group as provision does (the
// zone's main subnet or subnet_id; the zone's default group) and checks
// them; nil when nothing is wrong or nothing could be read. A zone the
// lists show without a subnet or a group is the error provision refuses
// with (NoSubnetError, NoSecurityGroupError): the preview refuses it before
// the question, as an image the project cannot use (D35, G13-3).
func (e *Engine) checkNetwork(ctx context.Context, projectID, zoneID, zoneName, subnetID string) (*NetworkCheck, error) {
	if projectID == "" || zoneID == "" {
		return nil, nil
	}
	var c NetworkCheck
	gateway := ""
	if subnets, err := e.Cloud.ListSubnets(ctx, projectID); err == nil {
		var sn *provision.Subnet
		if i := provision.PickSubnet(subnets, zoneID, zoneName); i >= 0 {
			sn = subnets[i]
		} else if subnetID == "" {
			return nil, NoSubnetError(zoneName, zoneID)
		}
		if subnetID != "" {
			sn = nil
			for _, s := range subnets {
				if s.ID == subnetID {
					sn = s
				}
			}
		}
		if sn != nil {
			gateway = sn.DefaultGateway
			if sn.NoDNS() {
				c.Subnet, c.SubnetNoDNS = sn.Name, true
			}
		}
	}
	sgs, err := e.Cloud.ListSecurityGroups(ctx, projectID)
	if err == nil && provision.PickSecurityGroup(sgs, zoneID, zoneName) < 0 {
		return nil, NoSecurityGroupError(zoneName, zoneID)
	}
	if g := pickGroup(ctx, e, projectID, zoneID, zoneName); g != nil {
		if rules, err := provision.GroupRules(ctx, e.Cloud, g); err == nil {
			gaps := provision.CheckVMGroupRules(rules, gateway)
			if gaps.NoSSH || gaps.NoSystemEgress {
				c.SecurityGroup, c.NoSSHIngress, c.NoSystemEgress = g.Name, gaps.NoSSH, gaps.NoSystemEgress
			}
		}
	}
	if c == (NetworkCheck{}) {
		return nil, nil
	}
	return &c, nil
}

// pickGroup is the zone's group provision takes; nil when the list
// fails or no group is in the zone.
func pickGroup(ctx context.Context, e *Engine, projectID, zoneID, zoneName string) *provision.SecurityGroup {
	sgs, err := e.Cloud.ListSecurityGroups(ctx, projectID)
	if err != nil {
		return nil
	}
	if i := provision.PickSecurityGroup(sgs, zoneID, zoneName); i >= 0 {
		return sgs[i]
	}
	return nil
}

// lines words the check for the preview.
func (c *NetworkCheck) lines() []string {
	if c == nil {
		return nil
	}
	var out []string
	if c.SubnetNoDNS {
		out = append(out, subnetNoDNSWarning(c.Subnet))
	}
	if c.NoSSHIngress {
		out = append(out, groupNoSSHWarning(c.SecurityGroup))
	}
	if c.NoSystemEgress {
		out = append(out, groupNoEgressWarning(c.SecurityGroup))
	}
	return out
}

// zoneGroups lists the names of the zone's groups, for the warning.
func zoneGroups(sgs []*provision.SecurityGroup, zoneID, zoneName string) []string {
	var out []string
	for _, g := range sgs {
		if inZone(g.AvailabilityZoneID, g.Name, zoneID, zoneName) {
			out = append(out, g.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// inZone: the zone by ID, else by a name containing the zone name.
func inZone(itemZoneID, name, zoneID, zoneName string) bool {
	return (itemZoneID != "" && itemZoneID == zoneID) || (zoneName != "" && strings.Contains(name, zoneName))
}

// subnetNoDNSWarning: a subnet without DNS servers leaves the VM unable to
// resolve names (docs, troubleshooting «Temporary failure in name
// resolution»): apt, Docker and image pulls fail. A warning, not a refusal:
// the user may resolve names otherwise.
func subnetNoDNSWarning(name string) string {
	return i18n.Tf("subnet %s has no DNS servers: the VM will not resolve names — Docker and images will not download. Add a DNS server to the subnet in the Cloud.ru console",
		"в подсети %s не указаны DNS-серверы: ВМ не будет разрешать имена — Docker и образы не скачаются. Добавьте DNS-сервер в настройках подсети в консоли Cloud.ru", name)
}

func zoneText(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

// openPorts adds ingress rules for the compose ports (tcp and udp) to the
// VM's security group, skipping ports already open for that protocol, and
// records in rec the rules it created — destroy deletes those. The group is
// shared by every VM placed in it — the deploy preview says so before
// anything is created. Failures are reported, not fatal: the VM exists and
// the ports can be opened in the console.
func (e *Engine) openPorts(ctx context.Context, projectID, sgID string, tcp, udp []int, rec *state.DeploymentRecord) {
	res := sgrules.Open(ctx, e.Cloud, projectID, sgID, tcp, udp,
		func(label string) string { return "evo: port " + label }, rec)
	group := rec.SecurityGroupName
	if group == "" {
		group = sgID
	}
	ShowOpenedPorts(e.Printer, res, rec, group)
}

// ShowOpenedPorts tells on p what sgrules.Open did with the ports of rec in
// the security group named group: the ports opened, the ports another rule
// opens already, the ports it could not open or cannot close later. deploy
// and evo vm create say it in the same words.
func ShowOpenedPorts(p *output.Printer, res sgrules.OpenResult, rec *state.DeploymentRecord, group string) {
	if len(res.Opened) > 0 {
		p.StepDone(i18n.Tf("Opened ports %s in the security group", "Открыты порты %s в группе безопасности", strings.Join(res.Opened, ",")))
	}
	// A port someone else's rule opens gets no rule from deploy and so
	// none for destroy to delete: say so, it was silent (v1.0.1).
	if theirs := res.NotOurs(rec); len(theirs) > 0 {
		p.StepDone(alreadyOpenText(theirs, group))
	}
	if len(res.Failed) > 0 {
		p.Warning(i18n.Tf("Could not open ports %s — open them in the security group in the Cloud.ru console",
			"Не удалось открыть порты %s — откройте их в группе безопасности в консоли Cloud.ru", strings.Join(res.Failed, ",")))
	}
	if len(res.Untracked) > 0 {
		p.Warning(i18n.Tf("Ports %s were opened, but the API returned no rule ID: destroy will not close them — delete those rules in the Cloud.ru console after destroy",
			"Порты %s открыты, но API не вернул ID правил: destroy их не закроет — после destroy удалите эти правила в консоли Cloud.ru",
			strings.Join(res.Untracked, ",")))
	}
}

// alreadyOpenText is the deploy line about ports another rule opens.
func alreadyOpenText(labels []string, group string) string {
	if len(labels) == 1 {
		return i18n.Tf("port %s is already open in group %s — no rule created, destroy will not touch it",
			"порт %s уже открыт в группе %s — правило не создавалось, destroy его не тронет", labels[0], group)
	}
	return i18n.Tf("ports %s are already open in group %s — no rules created, destroy will not touch them",
		"порты %s уже открыты в группе %s — правила не создавались, destroy их не тронет", strings.Join(labels, ","), group)
}

// flavorAPIName turns the flavor picked by sizing (an ID) or given by the
// user (an ID or a name) into the name the create API expects: it rejects
// UUIDs in flavor_name with 422.
func (e *Engine) flavorAPIName(ctx context.Context, ref string) (string, error) {
	flavors, err := e.Cloud.ListFlavors(ctx)
	if err != nil {
		return "", fmt.Errorf(i18n.T("list flavors: %w", "список flavor: %w"), err)
	}
	for _, f := range flavors {
		if f.ID == ref || f.Name == ref {
			return f.Name, nil
		}
	}
	return "", clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("flavor %q not found", "flavor %q не найден", ref),
		i18n.T("Use 'flavor: auto' or a flavor name from the Cloud.ru console.",
			"Укажите 'flavor: auto' или имя flavor из консоли Cloud.ru ('evo vm flavors')."))
}

// randomPassword satisfies the image's complexity rules: one of each class,
// the rest from all of them, crypto/rand throughout.
func randomPassword(n int) string {
	const (
		lower   = "abcdefghijkmnpqrstuvwxyz"
		upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digit   = "23456789"
		special = "!@#$%_-"
	)
	all := lower + upper + digit + special
	rb := make([]byte, n)
	_, _ = crand.Read(rb)
	out := make([]byte, n)
	for i := range out {
		set := all
		switch i {
		case 0:
			set = lower
		case 1:
			set = upper
		case 2:
			set = digit
		case 3:
			set = special
		}
		out[i] = set[int(rb[i])%len(set)]
	}
	return string(out)
}
