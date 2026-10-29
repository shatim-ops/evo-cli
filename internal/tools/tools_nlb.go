// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/nlb"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// pickVMInterface returns a VM's network interface to use as an NLB
// target — the primary one if flagged, else the first with a non-empty ID;
// nil when there is none.
func pickVMInterface(det *provision.VMDetail) *provision.VMInterface {
	if det == nil {
		return nil
	}
	for i, ni := range det.Interfaces {
		if ni.Primary && ni.ID != "" {
			return &det.Interfaces[i]
		}
	}
	for i, ni := range det.Interfaces {
		if ni.ID != "" {
			return &det.Interfaces[i]
		}
	}
	return nil
}

// tgPreview is the preview of tg_create: the targets, each VM by id and
// name, and the types of the interfaces it resolved, by VM.
func tgPreview(name string, targets []map[string]any, ifaceTypes map[string]any) engine.CreatePreview {
	will := map[string]any{"targets": targets}
	keys := []string{"targets"}
	if len(ifaceTypes) > 0 {
		will["interface_types"], keys = ifaceTypes, append(keys, "interface_types")
	}
	return willPreview(engine.ResourceTargetGroup, name, "", will, keys)
}

func (s *Registry) nlbClient(ctx context.Context) (*nlb.Client, error) {
	cli, _, err := s.nlbClientEng(ctx)
	return cli, err
}

// nlbClientEng also returns the engine (subnet resolution). Uses authedEngine so
// eng.Cloud carries a bearer (newEngine would 403 "RBAC: access denied").
func (s *Registry) nlbClientEng(ctx context.Context) (*nlb.Client, *engine.Engine, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	return nlb.New(tok.Value, eng.Config.ProjectID, ""), eng, nil
}

// --- Load balancers: read ---

func (s *Registry) handleLbList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListNLB(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		row := map[string]any{"id": m["id"], "name": m["name"], "status": m["status"], "vpc_id": m["vpc_id"]}
		if ext, ok := m["external_address"].(map[string]any); ok {
			row["external_ip"] = ext["ipv4"]
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"load_balancers": out}), false
}

func (s *Registry) handleLbGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required (from lb_list)", "нужен 'id' (из lb_list)"), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetNLB(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Registry) handleLbOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if w := pgInt(args, "wait_seconds", 0); w > 0 {
		m, err := waitForOpJSON(ctx, w, func(ctx context.Context) (map[string]any, error) {
			op, err := cli.GetOperation(ctx, id)
			if err != nil {
				return nil, err
			}
			return opToMap(op), nil
		})
		if err != nil {
			return errResult(err), true
		}
		return toJSON(m), false
	}
	op, err := cli.GetOperation(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(op), false
}

// --- Target groups ---

func (s *Registry) handleTgList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListTargetGroups(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{"id": m["id"], "name": m["name"], "status": m["status"], "vpc_id": m["vpc_id"]})
	}
	return toJSON(map[string]any{"target_groups": out}), false
}

func (s *Registry) handleTgGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required (from tg_list)", "нужен 'id' (из tg_list)"), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetTargetGroup(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// targetSpecs builds target_vm_specs from a `targets` array and/or vm_id+interface_id.
func targetSpecs(args map[string]any) []map[string]any {
	var specs []map[string]any
	if raw, ok := args["targets"].([]any); ok {
		for _, t := range raw {
			if m, ok := t.(map[string]any); ok {
				if id, _ := m["id"].(string); id != "" {
					specs = append(specs, map[string]any{"id": id, "interface_id": m["interface_id"]})
				}
			}
		}
	}
	if vm := stringArg(args, "vm_id"); vm != "" {
		specs = append(specs, map[string]any{"id": vm, "interface_id": stringArg(args, "interface_id")})
	}
	return specs
}

// tgTargetArgs checks the targets of tg_create before the cloud: vm_id or
// targets, not both (the description offers one or the other; both were
// merged without a word), and no id of only spaces — " " was sent as a VM
// id, and an id of "" in targets was dropped (release audit, REL-11).
func tgTargetArgs(args map[string]any) error {
	if err := blankArgs(args, "vm_id", "interface_id"); err != nil {
		return err
	}
	raw, _ := args["targets"].([]any)
	if len(raw) > 0 && stringArg(args, "vm_id") != "" {
		return clierrors.New(clierrors.CodeUsage,
			i18n.T("pass vm_id or targets, not both", "укажите vm_id или targets, не оба"),
			i18n.T("vm_id is one target; targets is an array of {id[, interface_id]} for several.",
				"vm_id — одна цель; targets — массив {id[, interface_id]} для нескольких."))
	}
	for i, t := range raw {
		m, _ := t.(map[string]any)
		for _, k := range []string{"id", "interface_id"} {
			if v, _ := m[k].(string); v != "" && strings.TrimSpace(v) == "" {
				return clierrors.New(clierrors.CodeUsage,
					i18n.Tf("empty value of targets[%d].%s: it is only spaces", "пустое значение targets[%d].%s: в нём только пробелы", i, k),
					i18n.T("Pass the VM UUID in id (from vm_list).", "Передайте в id UUID ВМ (из vm_list)."))
			}
		}
	}
	return nil
}

func (s *Registry) handleTgCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageT("'name' is required", "нужен 'name'"), true
	}
	if err := tgTargetArgs(args); err != nil {
		return errResult(err), true
	}
	specs := targetSpecs(args)
	if len(specs) == 0 {
		return usageT("provide targets (array of {id[, interface_id]}) or vm_id", "укажите targets (массив {id[, interface_id]}) или vm_id"), true
	}
	cli, eng, err := s.nlbClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	// A group of the name: a repeat after a lost answer made a second one
	// (fault matrix, process audit 1.2.1).
	existing, err := cli.ListTargetGroups(ctx)
	if err != nil {
		return errResult(fmt.Errorf(i18n.T("check the name is free: list target groups: %w", "проверка, что имя свободно: список целевых групп: %w"), err)), true
	}
	if id, st, ok := takenName(existing, name); ok {
		return nameTakenResult(i18n.T("target group", "целевая группа"), name, id, st, "tg_list")
	}
	// Each target VM is read: its name goes to the preview (the person
	// agrees to which VMs take the traffic, audit cli-ids-1.6, §3.2), and
	// interface_id is resolved when the caller only gave vm_id — the
	// agent has no standalone tool to fetch a VM's network interface, and
	// without this it flails (ssh/raw API). NLB targets need the VM's
	// regular VPC-subnet interface; GetVMDetail returns it.
	// The type of each interface resolved: the preview names it (D54 before
	// the probe P41, FB15-21) — a direct_ip interface is a public VM's, not
	// the private target a balancer needs; the body does not carry it.
	named := make([]map[string]any, 0, len(specs))
	ifaceTypes := map[string]any{}
	for i, sp := range specs {
		vmid, _ := sp["id"].(string)
		det, e := eng.Cloud.GetVMDetail(ctx, vmid)
		if e != nil {
			return errResult(fmt.Errorf(i18n.T("resolve interface for vm %s: %w", "разрешение интерфейса ВМ %s: %w"), vmid, e)), true
		}
		if ifid, _ := sp["interface_id"].(string); ifid == "" {
			ni := pickVMInterface(det)
			if ni == nil {
				return errResult(fmt.Errorf(i18n.T("vm %s has no usable network interface (NLB targets need a private VPC-subnet VM — provision with no_public_ip)", "у ВМ %s нет подходящего сетевого интерфейса (цель балансировщика — ВМ в подсети VPC без публичного IP: provision с no_public_ip)"), vmid)), true
			}
			specs[i]["interface_id"] = ni.ID
			if ni.Type != "" {
				ifaceTypes[vmid] = ni.Type
			}
		}
		named = append(named, map[string]any{"id": vmid, "name": det.Name, "interface_id": specs[i]["interface_id"]})
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("tg_create creates target group %q with %d target(s).",
				"tg_create создаёт группу целей %q (целей: %d).", name, len(specs)),
			"targets": specs,
			"preview": tgPreview(name, named, ifaceTypes),
		}), true
	}
	body := map[string]any{"project_id": cli.ProjectID(), "name": name, "target_vm_specs": specs}
	op, err := cli.CreateTargetGroup(ctx, body)
	if err != nil || op.ID == "" {
		if err == nil {
			err = noID("operation")
		}
		return lostCreate(ctx, "tg_create", cli.ListTargetGroups, i18n.T("target group", "целевая группа"), name, nil, "tg_list", fmt.Errorf(i18n.T("create target group: %w", "создание целевой группы: %w"), err))
	}
	out := map[string]any{"status": "creating", "operation_id": op.ID, "note": "Async — poll lb_operation(operation_id) until done=true; then tg_list for the id."}
	if op.ResourceID != "" {
		out["tg_id"] = op.ResourceID
	}
	return toJSON(out), false
}

func (s *Registry) planTgDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "id")
	if id == "" {
		return nil, usageErr("'id' is required (from tg_list)", "нужен 'id' (из tg_list)")
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return nil, err
	}
	return s.deletePlan("tg_delete", "tg_delete deletes target group %s.",
		"tg_delete удаляет группу целей %s.",
		id, func() (map[string]any, error) { return cli.GetTargetGroup(ctx, id) },
		func(ctx context.Context) (string, bool) {
			op, err := cli.DeleteTargetGroup(ctx, id)
			if err != nil {
				return s.deleteFailed("tg_delete", id, fmt.Errorf(i18n.T("delete target group: %w", "удаление целевой группы: %w"), err))
			}
			return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
		}), nil
}

// --- Load balancer create/delete ---

// planLbCreate plans the create of a load balancer (gate.go). The preview
// names the target group and the VPC by name as well as by id: the group
// is read before the question, and the person agrees to what the
// balancer serves (audit cli-ids-1.6, §3.2).
func (s *Registry) planLbCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	tgID := stringArg(args, "target_group_id")
	port := pgInt(args, "port", 0)
	if name == "" || tgID == "" || port == 0 {
		return nil, usageErr("'name', 'target_group_id' and 'port' are required", "нужны 'name', 'target_group_id' и 'port'")
	}
	targetPort := pgInt(args, "target_port", port)
	hcPort := pgInt(args, "hc_port", targetPort)
	// The check evo sets on the target: the preview names it (С4), the
	// rule sends it.
	healthCheck := map[string]any{"interval": "10s", "timeout": "5s", "healthy_threshold": 3, "unhealthy_threshold": 3}
	external := true
	if v, ok := args["external"].(bool); ok {
		external = v
	}

	cli, eng, err := s.nlbClientEng(ctx)
	if err != nil {
		return nil, err
	}
	// A second balancer of the same name is a second bill: refused before
	// the consent, as every other create (DECISIONS D21).
	existing, err := cli.ListNLB(ctx)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("check the name is free: list load balancers: %w", "проверка, что имя свободно: список балансировщиков: %w"), err)
	}
	if id, st, ok := takenName(existing, name); ok {
		return answered(nameTakenResult(i18n.T("load balancer", "балансировщик"), name, id, st, "lb_list")), nil
	}

	// VPC: explicit, else inherit from the target group (LB must share its
	// VPC). The group's VMs also name the zone the balancer serves.
	tg, e := cli.GetTargetGroup(ctx, tgID)
	if e != nil {
		return nil, fmt.Errorf(i18n.T("read target group %s: %w", "чтение целевой группы %s: %w"), tgID, e)
	}
	tgVPC, _ := tg["vpc_id"].(string)
	vpcID := stringArg(args, "vpc_id")
	if vpcID != "" && tgVPC != "" && vpcID != tgVPC {
		// The balancer shares its target group's VPC (release audit
		// re-run, N-01).
		return nil, clierrors.New(clierrors.CodeConfigInvalid,
			i18n.Tf("target group %s is in VPC %s, not in vpc_id %s; nothing was created", "целевая группа %s в VPC %s, а не в vpc_id %s; ничего не создано", tgID, tgVPC, vpcID),
			i18n.T("Omit vpc_id (the balancer takes its target group's VPC) or pass a target group of that VPC.", "Не указывайте vpc_id (балансировщик возьмёт VPC целевой группы) или передайте целевую группу этой VPC."))
	}
	if vpcID == "" {
		vpcID = tgVPC
	}
	if vpcID == "" {
		return nil, errors.New(i18n.T("could not resolve vpc_id — pass it explicitly", "не удалось определить vpc_id — передайте его"))
	}
	zone, subnetID, warnings, err := lbZoneAndSubnet(ctx, eng, args, tg, vpcID)
	if err != nil {
		return nil, err
	}

	// AZ spec requires exactly one of {id, name} (nlb API 400 otherwise);
	// zone NAMES are the canonical cross-service identifier, the id when
	// the name is unknown.
	azSpec := map[string]any{"subnet_ids": []string{subnetID}}
	if zone.Name != "" {
		azSpec["name"] = zone.Name
	} else {
		azSpec["id"] = zone.ID
	}

	rule := map[string]any{
		"name":            "rule-" + name,
		"target_group_id": tgID,
		"listeners":       []map[string]any{{"name": fmt.Sprintf("lst-%d", port), "port": port, "target_port": targetPort}},
		"health_check": map[string]any{
			"port": hcPort, "timeout": healthCheck["timeout"], "interval": healthCheck["interval"],
			"unhealthy_threshold": healthCheck["unhealthy_threshold"], "healthy_threshold": healthCheck["healthy_threshold"], "tcp": map[string]any{},
		},
	}
	body := map[string]any{
		"project_id":              cli.ProjectID(),
		"name":                    name,
		"vpc_id":                  vpcID,
		"rules":                   []map[string]any{rule},
		"availability_zone_specs": []map[string]any{azSpec},
	}
	if external {
		body["external_address_spec"] = map[string]any{"allocate": true}
	} else {
		body["internal_address_spec"] = map[string]any{"allocate": true, "subnet_id": subnetID}
	}

	willCreate := map[string]any{
		"name": name, "vpc_id": vpcID, "target_group_id": tgID,
		"listener": fmt.Sprintf("%d→%d", port, targetPort), "health_check_port": hcPort,
		"subnet_id": subnetID, "zone": firstNonEmptyStr(zone.Name, zone.ID), "external": external,
		"health_check": healthCheck,
	}
	tgName, _ := tg["name"].(string)
	preview := willPreview(engine.ResourceLB, name, firstNonEmptyStr(zone.Name, zone.ID), willCreate,
		[]string{"listener", "health_check_port", "health_check", "external", "subnet_id"}, warnings...)
	preview.Add("target_group_id", tgID)
	preview.Add("target_group_name", tgName)
	preview.Add("vpc_id", vpcID)
	preview.Add("vpc_name", s.vpcName(ctx, vpcID))
	out := map[string]any{
		"message": i18n.T("lb_create provisions a BILLABLE L4 load balancer (async).",
			"lb_create создаёт ПЛАТНЫЙ балансировщик L4 (асинхронно)."),
		"will_create": willCreate,
		"preview":     preview,
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return &Plan{Preview: out, Execute: func(ctx context.Context) (string, bool) {
		op, err := cli.CreateNLB(ctx, body)
		if err != nil || op.ID == "" {
			if err == nil {
				err = noID("operation")
			}
			return lostCreate(ctx, "lb_create", cli.ListNLB, i18n.T("load balancer", "балансировщик"), name, willCreate, "lb_get id", fmt.Errorf(i18n.T("create load balancer: %w", "создание балансировщика: %w"), err))
		}
		out := map[string]any{"status": "creating", "operation_id": op.ID, "created": willCreate, "note": "Async — poll lb_operation(operation_id) until done=true; then lb_get for the external IP + status RUNNING."}
		// The balancer's id is in the create's answer: named at once (fault
		// matrix, process audit 1.2.1; as pg_create, N-07).
		if op.ResourceID != "" {
			out["lb_id"] = op.ResourceID
		}
		return toJSON(out), false
	}}, nil
}

// vpcName is the name of VPC id for a preview, "" when it cannot be read
// within engine.PreviewLookupTimeout: the id stays named either way.
func (s *Registry) vpcName(ctx context.Context, id string) string {
	ctx, cancel := context.WithTimeout(ctx, engine.PreviewLookupTimeout)
	defer cancel()
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return ""
	}
	m, err := cli.GetVPC(ctx, id)
	if err != nil {
		return ""
	}
	name, _ := m["name"].(string)
	return name
}

// lbZoneAndSubnet picks the zone the balancer serves and its subnet there.
// Zone: the 'zone' argument (name or UUID), else the zone of subnet_id,
// else the zone of the target group's VMs, else the configured region; none
// is USAGE before any create. Subnet: subnet_id as given (of that zone),
// else the zone's main subnet of the balancer's VPC. Without a zone in the
// configuration the request went with an empty availability_zone_specs[0]
// (API 400), and the subnet was the project's first one, of any zone and
// VPC (release audit, L-26).
func lbZoneAndSubnet(ctx context.Context, eng *engine.Engine, args, tg map[string]any, vpcID string) (provision.AvailabilityZone, string, []string, error) {
	var none provision.AvailabilityZone
	zones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return none, "", nil, fmt.Errorf(i18n.T("list availability zones: %w", "список зон доступности: %w"), err)
	}
	subs, err := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
	if err != nil {
		return none, "", nil, fmt.Errorf(i18n.T("resolve subnet: %w", "выбор подсети: %w"), err)
	}
	find := func(ref string) *provision.AvailabilityZone {
		for _, z := range zones {
			if ref != "" && (z.ID == ref || strings.EqualFold(z.Name, ref)) {
				return z
			}
		}
		return nil
	}
	subnetID := stringArg(args, "subnet_id")
	var subnet *provision.Subnet
	if subnetID != "" {
		for _, sn := range subs {
			if sn.ID == subnetID {
				subnet = sn
			}
		}
		if subnet == nil {
			return none, "", nil, clierrors.New(clierrors.CodeConfigInvalid, i18n.Tf("subnet %s is not in the project", "подсети %s нет в проекте", subnetID),
				i18n.T("Take a subnet ID from list_subnets, or omit subnet_id: the zone's main subnet of the balancer's VPC is taken.", "Возьмите ID подсети из list_subnets или не указывайте subnet_id: возьмётся основная подсеть зоны в VPC балансировщика."))
		}
	}

	var warnings []string
	var zone *provision.AvailabilityZone
	from := ""
	if ref := stringArg(args, "zone"); ref != "" {
		if zone = find(ref); zone == nil {
			return none, "", nil, clierrors.New(clierrors.CodeUsage, i18n.Tf("availability zone %q not found", "зона доступности %q не найдена", ref),
				i18n.T("See list_zones (evo list-zones) for the zone names.", "Имена зон — в выводе list_zones (evo list-zones)."))
		}
		from = "zone"
	}
	if zone == nil && subnet != nil && subnet.AvailabilityZoneID != "" {
		zone, from = find(subnet.AvailabilityZoneID), "subnet_id"
		if zone == nil {
			zone = &provision.AvailabilityZone{ID: subnet.AvailabilityZoneID}
		}
	}
	if zone == nil {
		// The zones of the group's VMs (target_vms[].zone_name/zone_id).
		var tzones []string
		vms, _ := tg["target_vms"].([]any)
		for _, v := range vms {
			m, _ := v.(map[string]any)
			ref := firstNonEmptyStr(stringArg(m, "zone_id"), stringArg(m, "zone_name"))
			if ref == "" {
				continue
			}
			z := find(ref)
			if z == nil {
				z = &provision.AvailabilityZone{ID: stringArg(m, "zone_id"), Name: stringArg(m, "zone_name")}
			}
			if zone == nil {
				zone, from = z, "target group"
			}
			if n := firstNonEmptyStr(z.Name, z.ID); !slices.Contains(tzones, n) {
				tzones = append(tzones, n)
			}
		}
		if len(tzones) > 1 {
			warnings = append(warnings, fmt.Sprintf("the target group's VMs are in zones %s; the balancer serves %s only (pass zone for another)",
				strings.Join(tzones, ", "), tzones[0]))
		}
	}
	if zone == nil && eng.Config.Region != "" {
		zone, from = find(eng.Config.Region), "region"
		if zone == nil {
			zone = &provision.AvailabilityZone{ID: eng.Config.Region}
		}
	}
	if zone == nil {
		return none, "", nil, clierrors.New(clierrors.CodeUsage,
			i18n.T("cannot tell the balancer's availability zone: the target group names no zone of its VMs and no region is configured", "зона доступности балансировщика неизвестна: в целевой группе нет зоны её ВМ, и region не задан"),
			i18n.T("Pass zone (name or UUID, see list_zones) or subnet_id.", "Передайте zone (имя или UUID, см. list_zones) или subnet_id."))
	}

	if subnet != nil {
		// A subnet of another VPC went to the API as is and got a 400
		// without the field (Grok review of PR #89, the class of L-26).
		if subnet.VPCID != "" && vpcID != "" && subnet.VPCID != vpcID {
			return none, "", nil, clierrors.New(clierrors.CodeConfigInvalid,
				i18n.Tf("subnet %s is not in VPC %s of the balancer; nothing was created", "подсеть %s не в VPC %s балансировщика; ничего не создано", subnetID, vpcID),
				i18n.T("Pass a subnet of that VPC (list_subnets), or omit subnet_id.", "Передайте подсеть этой VPC (list_subnets) или не указывайте subnet_id."))
		}
		if from != "subnet_id" && !provision.SubnetInZone(subnet, zone.ID, zone.Name) {
			return none, "", nil, clierrors.New(clierrors.CodeConfigInvalid,
				i18n.Tf("subnet %s is not in zone %s; nothing was created", "подсеть %s не в зоне %s; ничего не создано", subnetID, firstNonEmptyStr(zone.Name, zone.ID)),
				i18n.T("Pass a subnet of that zone (list_subnets), or omit subnet_id.", "Передайте подсеть этой зоны (list_subnets) или не указывайте subnet_id."))
		}
		return *zone, subnetID, warnings, nil
	}
	var inVPC []*provision.Subnet
	for _, sn := range subs {
		if sn.VPCID == "" || sn.VPCID == vpcID {
			inVPC = append(inVPC, sn)
		}
	}
	i := provision.PickSubnet(inVPC, zone.ID, zone.Name)
	if i < 0 {
		return none, "", nil, clierrors.New(clierrors.CodeConfigInvalid,
			i18n.Tf("no subnet of VPC %s in zone %s for the balancer; nothing was created", "в зоне %s нет подсети VPC %s для балансировщика; ничего не создано", vpcID, firstNonEmptyStr(zone.Name, zone.ID)),
			i18n.T("Pass subnet_id (list: list_subnets) of a subnet in that zone and VPC, or another zone; if the project has no subnet in any zone, create one in the Cloud.ru console.",
				"Передайте subnet_id (список: list_subnets) подсети этой зоны и VPC или другую зону; если в проекте нет подсети ни в одной зоне — создайте её в консоли Cloud.ru."))
	}
	return *zone, inVPC[i].ID, warnings, nil
}

func (s *Registry) planLbDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "id")
	if id == "" {
		return nil, usageErr("'id' is required (from lb_list)", "нужен 'id' (из lb_list)")
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return nil, err
	}
	return s.deletePlan("lb_delete", "lb_delete deletes load balancer %s.",
		"lb_delete удаляет балансировщик %s.",
		id, func() (map[string]any, error) { return cli.GetNLB(ctx, id) },
		func(ctx context.Context) (string, bool) {
			op, err := cli.DeleteNLB(ctx, id)
			if err != nil {
				return s.deleteFailed("lb_delete", id, fmt.Errorf(i18n.T("delete load balancer: %w", "удаление балансировщика: %w"), err))
			}
			return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
		}), nil
}
