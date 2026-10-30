// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Masks of a subnet the compute API accepts (PublicSubnetCreateRequest,
// prefix_length 8..29).
const (
	minSubnetMask = 8
	maxSubnetMask = 29
)

// subnetCIDR checks the cidr of subnet_create as the compute API would: a
// private IPv4 network written by its base address, with a mask of
// 8..29. A refusal names the base address when that is all that is wrong.
func subnetCIDR(cidr string) (netip.Prefix, error) {
	if cidr == "" {
		return netip.Prefix{}, usageErr("'cidr' is required (a private network, e.g. 10.10.0.0/24)",
			"нужен 'cidr' (частная сеть, например 10.10.0.0/24)")
	}
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() {
		return netip.Prefix{}, usageErr("cidr %q is not an IPv4 network with a mask, e.g. 10.10.0.0/24",
			"cidr %q — не сеть IPv4 с маской, например 10.10.0.0/24", cidr)
	}
	if b := p.Bits(); b < minSubnetMask || b > maxSubnetMask {
		return netip.Prefix{}, usageErr("cidr %s: the mask must be /%d to /%d", "cidr %s: маска должна быть от /%d до /%d",
			cidr, minSubnetMask, maxSubnetMask)
	}
	if !p.Addr().IsPrivate() {
		return netip.Prefix{}, usageErr("cidr %s is not a private network (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)",
			"cidr %s — не частная сеть (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)", cidr)
	}
	if m := p.Masked(); m != p {
		return netip.Prefix{}, usageErr("cidr %s is not written by its base address: %s", "cidr %s записан не базовым адресом: %s", cidr, m)
	}
	return p, nil
}

// projectVPC is the VPC a new subnet goes to, or a new private DNS zone is
// linked to: vpc_id when the call gives one — a VPC of the project — else
// the project's one default VPC. evo sends it either way, so the preview
// names the VPC; two VPCs marked default are no default (the first of the
// list would depend on its order).
func (s *Registry) projectVPC(ctx context.Context, vpcID string) (id, name string, err error) {
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return "", "", err
	}
	vpcs, err := cli.ListVPCs(ctx)
	if err != nil {
		return "", "", fmt.Errorf(i18n.T("list VPCs: %w", "список VPC: %w"), err)
	}
	var defaults []map[string]any
	for _, v := range vpcs {
		vid, _ := v["id"].(string)
		vname, _ := v["name"].(string)
		if vpcID != "" && vid == vpcID {
			return vid, vname, nil
		}
		if isDefault, _ := v["default"].(bool); isDefault {
			defaults = append(defaults, v)
		}
	}
	switch {
	case vpcID == "" && len(defaults) == 1:
		vid, _ := defaults[0]["id"].(string)
		vname, _ := defaults[0]["name"].(string)
		return vid, vname, nil
	case vpcID == "" && len(defaults) > 1:
		return "", "", clierrors.New(clierrors.CodeUsage,
			i18n.Tf("the project has %d VPCs marked default: pass vpc_id", "в проекте %d VPC с признаком «по умолчанию»: передайте vpc_id", len(defaults)),
			i18n.T("vpc_list lists the project's VPCs.", "VPC проекта — в выводе vpc_list."))
	case vpcID != "":
		return "", "", clierrors.New(clierrors.CodeNotFound,
			i18n.Tf("VPC %s is not in the project", "VPC %s в проекте нет", vpcID),
			i18n.T("vpc_list lists the project's VPCs.", "VPC проекта — в выводе vpc_list."))
	case len(vpcs) == 0:
		return "", "", clierrors.New(clierrors.CodeUsage,
			i18n.T("the project has no VPC", "в проекте нет VPC"),
			i18n.T("Create one with vpc_create, then pass its id as vpc_id.", "Создайте VPC через vpc_create и передайте его id в vpc_id."))
	}
	return "", "", clierrors.New(clierrors.CodeUsage,
		i18n.T("the project has no default VPC: pass vpc_id", "в проекте нет VPC по умолчанию: передайте vpc_id"),
		i18n.T("vpc_list lists the project's VPCs.", "VPC проекта — в выводе vpc_list."))
}

// subnetOverlap is the subnet of the VPC whose network overlaps p, nil
// when none does. The spec does not say whether the list's subnet_address
// carries the mask: an address without one overlaps when p holds it. One
// that does not parse is passed over: the platform answers it with 409.
func subnetOverlap(subnets []*provision.Subnet, vpcID string, p netip.Prefix) *provision.Subnet {
	for _, sn := range subnets {
		if sn.VPCID != vpcID {
			continue
		}
		if q, err := netip.ParsePrefix(sn.SubnetAddress); err == nil && q.Overlaps(p) {
			return sn
		}
		if a, err := netip.ParseAddr(sn.SubnetAddress); err == nil && p.Contains(a) {
			return sn
		}
	}
	return nil
}

// subnetItems are subnets as the name checks read them (takenName).
func subnetItems(subnets []*provision.Subnet) []map[string]any {
	out := make([]map[string]any, 0, len(subnets))
	for _, sn := range subnets {
		out = append(out, map[string]any{"id": sn.ID, "name": sn.Name, "state": sn.State})
	}
	return out
}

// subnetDNS is the dns_servers of subnet_create: IPv4 addresses.
func subnetDNS(args map[string]any) ([]string, error) {
	out := stringSliceArg(args, "dns_servers")
	for _, v := range out {
		if a, err := netip.ParseAddr(v); err != nil || !a.Is4() {
			return nil, usageErr("dns_servers: %q is not an IPv4 address", "dns_servers: %q — не адрес IPv4", v)
		}
	}
	return out, nil
}

// planSubnetCreate plans the create of a subnet (gate.go). Everything the
// platform would refuse and evo can know without a write is refused
// before the question (CLAUDE.md §6, axis 1); the preview names every
// value of the POST (axis 4).
func (s *Registry) planSubnetCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	if name == "" {
		return nil, usageErr("'name' is required", "нужен 'name'")
	}
	if err := provision.ValidName(i18n.T("subnet", "подсети"), name); err != nil {
		return nil, err
	}
	cidr, err := subnetCIDR(stringArg(args, "cidr"))
	if err != nil {
		return nil, err
	}
	dns, err := subnetDNS(args)
	if err != nil {
		return nil, err
	}
	routed := true
	if v, ok := args["routed_network"].(bool); ok {
		routed = v
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	pid := eng.Config.ProjectID
	zoneID, zoneName, err := s.resolveZone(ctx, args)
	if err != nil {
		return nil, err
	}
	if zones, e := eng.Cloud.ListAvailabilityZones(ctx); e == nil {
		if err := provision.ClosedZoneError(zones, zoneID); err != nil {
			return nil, err
		}
	}
	vpcID, vpcName, err := s.projectVPC(ctx, stringArg(args, "vpc_id"))
	if err != nil {
		return nil, err
	}
	subnets, err := eng.Cloud.ListSubnets(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("check the name and the network are free: list subnets: %w",
			"проверка, что имя и сеть свободны: список подсетей: %w"), err)
	}
	list := func(ctx context.Context) ([]map[string]any, error) {
		sns, err := eng.Cloud.ListSubnets(ctx, pid)
		return subnetItems(sns), err
	}
	if id, st, ok := takenName(subnetItems(subnets), name); ok {
		return answered(nameTakenResult(i18n.T("subnet", "подсеть"), name, id, st, "list_subnets")), nil
	}
	if sn := subnetOverlap(subnets, vpcID, cidr); sn != nil {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("cidr %s overlaps subnet %s (%s, %s) of the same VPC", "cidr %s пересекается с подсетью %s (%s, %s) того же VPC",
				cidr, quoteCloud(sn.Name), sn.SubnetAddress, sn.ID),
			i18n.T("Pick a network that does not overlap the VPC's subnets (list_subnets).",
				"Выберите сеть, которая не пересекается с подсетями VPC (list_subnets)."))
	}
	req := provision.CreateSubnetRequest{Name: name, ProjectID: pid, VPCID: vpcID, SubnetAddress: cidr.String(),
		PrefixLength: cidr.Bits(), AvailabilityZoneID: zoneID, RoutedNetwork: routed, DNSServers: dns,
		Description: stringArg(args, "description")}
	preview := engine.CreatePreview{Resource: engine.ResourceSubnet, Name: name, ProjectID: pid,
		Zone: &deployplan.RefSpec{ID: zoneID, Name: zoneName}}
	preview.Add("cidr", req.SubnetAddress)
	preview.Add("prefix_length", req.PrefixLength)
	preview.Add("availability_zone_id", zoneID)
	preview.Add("vpc_id", vpcID)
	preview.Add("vpc_name", vpcName)
	preview.Add("routed_network", routed)
	if len(dns) > 0 {
		preview.Add("dns_servers", dns)
	}
	preview.Add("description", req.Description)
	preview.Price()
	return &Plan{
		Preview: map[string]any{
			"message": i18n.Tf("subnet_create creates subnet %q %s in VPC %s.", "subnet_create создаёт подсеть %q %s в VPC %s.",
				name, req.SubnetAddress, firstNonEmptyStr(vpcName, vpcID)),
			"preview": preview,
		},
		Execute: func(ctx context.Context) (string, bool) {
			m, err := eng.Cloud.CreateSubnet(ctx, req)
			id, _ := m["id"].(string)
			if err != nil || id == "" {
				if err == nil {
					err = noID("subnet")
				}
				return lostCreate(ctx, "subnet_create", list, i18n.T("subnet", "подсеть"), name, nil, "list_subnets",
					fmt.Errorf(i18n.T("create subnet: %w", "создание подсети: %w"), err))
			}
			return toJSON(map[string]any{"status": "created", "subnet_id": id, "subnet": m}), false
		},
	}, nil
}

// planSubnetDelete plans the delete of a subnet by id (gate.go). A subnet
// the platform will not delete — can_delete false: a VM, a balancer or a
// cluster is still in it, and the DELETE would be 405 — is refused before
// the question and says what to remove first (CLAUDE.md §6, axis 3).
func (s *Registry) planSubnetDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "subnet_id")
	if id == "" {
		return nil, usageErr("'subnet_id' is required (from list_subnets)", "нужен 'subnet_id' (из list_subnets)")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	m, getErr := eng.Cloud.GetSubnet(ctx, id)
	p := s.deletePlan("subnet_delete", "subnet_delete deletes subnet %s.", "subnet_delete удаляет подсеть %s.",
		id, func() (map[string]any, error) { return m, getErr },
		func(ctx context.Context) (string, bool) {
			if err := eng.Cloud.DeleteSubnet(ctx, id, eng.Config.ProjectID); err != nil {
				return s.deleteFailed("subnet_delete", id, fmt.Errorf(i18n.T("delete subnet: %w", "удаление подсети: %w"), err))
			}
			return toJSON(map[string]any{"status": "deleted", "subnet_id": id}), false
		})
	if canDelete, ok := m["can_delete"].(bool); p.Execute != nil && ok && !canDelete {
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("subnet %s cannot be deleted: Cloud.ru says resources are still in it (can_delete false)",
				"подсеть %s нельзя удалить: по данным Cloud.ru в ней ещё есть ресурсы (can_delete false)", id),
			i18n.T("Delete the VMs, balancers and clusters in the subnet first (vm_list, lb_list, pg_list, k8s_list), then delete it again.",
				"Сначала удалите ВМ, балансировщики и кластеры в подсети (vm_list, lb_list, pg_list, k8s_list), затем повторите удаление."))
	}
	return p, nil
}
