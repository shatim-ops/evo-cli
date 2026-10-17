package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/nlb"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// pickVMInterface returns the ID of a VM's network interface to use as an NLB
// target — the primary one if flagged, else the first with a non-empty ID.
func pickVMInterface(det *provision.VMDetail) string {
	if det == nil {
		return ""
	}
	for _, ni := range det.Interfaces {
		if ni.Primary && ni.ID != "" {
			return ni.ID
		}
	}
	for _, ni := range det.Interfaces {
		if ni.ID != "" {
			return ni.ID
		}
	}
	return ""
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
		return usageResult(fmt.Errorf("'id' is required (from lb_list)")), true
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
		return usageResult(fmt.Errorf("'operation_id' is required")), true
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
		return usageResult(fmt.Errorf("'id' is required (from tg_list)")), true
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

func (s *Registry) handleTgCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageResult(fmt.Errorf("'name' is required")), true
	}
	specs := targetSpecs(args)
	if len(specs) == 0 {
		return usageResult(fmt.Errorf("provide targets (array of {id[, interface_id]}) or vm_id")), true
	}
	cli, eng, err := s.nlbClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	// Auto-resolve interface_id from the VM when the caller only gave vm_id — the
	// agent has no standalone tool to fetch a VM's network interface, and without
	// this it flails (ssh/raw API). NLB targets need the VM's regular VPC-subnet
	// interface; GetVMDetail returns it.
	for i, sp := range specs {
		if ifid, _ := sp["interface_id"].(string); ifid == "" {
			vmid, _ := sp["id"].(string)
			det, e := eng.Cloud.GetVMDetail(ctx, vmid)
			if e != nil {
				return errResult(fmt.Errorf("resolve interface for vm %s: %w", vmid, e)), true
			}
			ifid := pickVMInterface(det)
			if ifid == "" {
				return errResult(fmt.Errorf("vm %s has no usable network interface (NLB targets need a private VPC-subnet VM — provision with no_public_ip)", vmid)), true
			}
			specs[i]["interface_id"] = ifid
		}
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("tg_create creates target group %q with %d target(s). Call again with confirmed=true.", name, len(specs)),
			"targets": specs,
		}), true
	}
	body := map[string]any{"project_id": cli.ProjectID(), "name": name, "target_vm_specs": specs}
	op, err := cli.CreateTargetGroup(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create target group: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "note": "Async — poll lb_operation(operation_id) until done=true; then tg_list for the id."}), false
}

func (s *Registry) handleTgDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageResult(fmt.Errorf("'id' is required (from tg_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("tg_delete deletes target group %s. Call again with confirmed=true.", id)}), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteTargetGroup(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete target group: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}

// --- Load balancer create/delete ---

func (s *Registry) handleLbCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	tgID := stringArg(args, "target_group_id")
	port := pgInt(args, "port", 0)
	if name == "" || tgID == "" || port == 0 {
		return usageResult(fmt.Errorf("'name', 'target_group_id' and 'port' are required")), true
	}
	targetPort := pgInt(args, "target_port", port)
	hcPort := pgInt(args, "hc_port", targetPort)
	external := true
	if v, ok := args["external"].(bool); ok {
		external = v
	}

	cli, eng, err := s.nlbClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}

	// VPC: explicit, else inherit from the target group (LB must share its
	// VPC). The group's VMs also name the zone the balancer serves.
	tg, e := cli.GetTargetGroup(ctx, tgID)
	if e != nil {
		return errResult(fmt.Errorf("read target group %s: %w", tgID, e)), true
	}
	vpcID := stringArg(args, "vpc_id")
	if vpcID == "" {
		vpcID, _ = tg["vpc_id"].(string)
	}
	if vpcID == "" {
		return errResult(fmt.Errorf("could not resolve vpc_id — pass it explicitly")), true
	}
	zone, subnetID, warnings, err := lbZoneAndSubnet(ctx, eng, args, tg, vpcID)
	if err != nil {
		return errResult(err), true
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
			"port": hcPort, "timeout": "5s", "interval": "10s",
			"unhealthy_threshold": 3, "healthy_threshold": 3, "tcp": map[string]any{},
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
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		out := map[string]any{
			"status":      "confirmation_required",
			"message":     "lb_create provisions a BILLABLE L4 load balancer (async). Get the user's approval, then call again with confirmed=true.",
			"will_create": willCreate,
		}
		if len(warnings) > 0 {
			out["warnings"] = warnings
		}
		return toJSON(out), true
	}
	op, err := cli.CreateNLB(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create load balancer: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "created": willCreate, "note": "Async — poll lb_operation(operation_id) until done=true; then lb_get for the external IP + status RUNNING."}), false
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
		return none, "", nil, fmt.Errorf("list availability zones: %w", err)
	}
	subs, err := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
	if err != nil {
		return none, "", nil, fmt.Errorf("resolve subnet: %w", err)
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
			return none, "", nil, clierrors.New(clierrors.CodeConfigInvalid, fmt.Sprintf("subnet %s is not in the project", subnetID),
				"Take a subnet ID from vpc_subnets, or omit subnet_id: the zone's main subnet of the balancer's VPC is taken.")
		}
	}

	var warnings []string
	var zone *provision.AvailabilityZone
	from := ""
	if ref := stringArg(args, "zone"); ref != "" {
		if zone = find(ref); zone == nil {
			return none, "", nil, clierrors.New(clierrors.CodeUsage, fmt.Sprintf("availability zone %q not found", ref),
				"See list_zones (evo list-zones) for the zone names.")
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
			"cannot tell the balancer's availability zone: the target group names no zone of its VMs and no region is configured",
			"Pass zone (name or UUID, see list_zones) or subnet_id.")
	}

	if subnet != nil {
		// A subnet of another VPC went to the API as is and got a 400
		// without the field (Grok review of PR #89, the class of L-26).
		if subnet.VPCID != "" && vpcID != "" && subnet.VPCID != vpcID {
			return none, "", nil, clierrors.New(clierrors.CodeConfigInvalid,
				fmt.Sprintf("subnet %s is not in VPC %s of the target group; nothing was created", subnetID, vpcID),
				"Pass a subnet of that VPC (vpc_subnets), or omit subnet_id.")
		}
		if from != "subnet_id" && !provision.SubnetInZone(subnet, zone.ID, zone.Name) {
			return none, "", nil, clierrors.New(clierrors.CodeConfigInvalid,
				fmt.Sprintf("subnet %s is not in zone %s; nothing was created", subnetID, firstNonEmptyStr(zone.Name, zone.ID)),
				"Pass a subnet of that zone (vpc_subnets), or omit subnet_id.")
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
			fmt.Sprintf("no subnet of VPC %s in zone %s for the balancer; nothing was created", vpcID, firstNonEmptyStr(zone.Name, zone.ID)),
			"Pass subnet_id of a subnet in that zone and VPC (vpc_subnets), or another zone.")
	}
	return *zone, inVPC[i].ID, warnings, nil
}

func (s *Registry) handleLbDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageResult(fmt.Errorf("'id' is required (from lb_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("lb_delete deletes load balancer %s. Call again with confirmed=true.", id)}), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteNLB(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete load balancer: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}
