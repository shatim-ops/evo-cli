// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/vpc"
)

// vpcClient builds an authenticated VPC client (standalone → newEngine token is enough).
func (s *Registry) vpcClient(ctx context.Context) (*vpc.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return vpc.New(tok.Value, eng.Config.ProjectID, ""), nil
}

// --- VPCs: read ---

func (s *Registry) handleVpcList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListVPCs(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{"id": m["id"], "name": m["name"], "default": m["default"], "type": m["type"]})
	}
	return toJSON(map[string]any{"vpcs": out}), false
}

func (s *Registry) handleVpcGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required (from vpc_list)", "нужен 'id' (из vpc_list)"), true
	}
	if res := uuidArg("id", id, "vpc_list"); res != "" {
		return res, true
	}
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetVPC(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// --- VPCs: mutations ---

func (s *Registry) handleVpcCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageT("'name' is required", "нужен 'name'"), true
	}
	if err := plainName("VPC", "name", name); err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("vpc_create creates a new VPC %q (async). Call again with confirmed=true after the user approves.",
				"vpc_create создаёт новый VPC %q (асинхронно). Получив согласие пользователя, вызовите снова с confirmed=true.", name),
		}), true
	}
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{"projectId": cli.ProjectID(), "name": name}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateVPC(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create vpc: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "note": "Async — poll vpc_operation(operation_id); then vpc_list for the id."}), false
}

func (s *Registry) handleVpcDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required (from vpc_list)", "нужен 'id' (из vpc_list)"), true
	}
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return deletePreview("vpc_delete deletes VPC %s (async). Its subnets/resources must be removed first. Call again with confirmed=true.",
			"vpc_delete удаляет VPC %s (асинхронно). Сначала нужно удалить его подсети и ресурсы. Вызовите снова с confirmed=true.",
			id, func() (map[string]any, error) { return cli.GetVPC(ctx, id) })
	}
	op, err := cli.DeleteVPC(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete vpc: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}

func (s *Registry) handleVpcOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
	}
	cli, err := s.vpcClient(ctx)
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

// --- Static routes ---

func (s *Registry) handleVpcRoutes(ctx context.Context, args map[string]any) (string, bool) {
	vpcID := stringArg(args, "vpc_id")
	if vpcID == "" {
		return usageT("'vpc_id' is required (from vpc_list)", "нужен 'vpc_id' (из vpc_list)"), true
	}
	if res := uuidArg("vpc_id", vpcID, "vpc_list"); res != "" {
		return res, true
	}
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListRoutes(ctx, vpcID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{"id": m["id"], "subnet": m["subnet"], "next_hop_type": m["nextHopType"], "status": m["status"]})
	}
	return toJSON(map[string]any{"routes": out}), false
}

// routeNextHop builds the nextHop object from vm_id+interface_id, vip_id, or magic_router_id.
// Two next hops given, or one of only spaces, is USAGE: the first of vm,
// vip and magic router was taken and the others dropped without a word
// (break r4, R4-11, as vm_get).
func routeNextHop(args map[string]any) (map[string]any, error) {
	ref, key, err := oneOfArgs(args, clierrors.New(clierrors.CodeUsage,
		i18n.T("pass one next hop: next_hop_vm_id, next_hop_vip_id or next_hop_magic_router_id",
			"укажите один следующий узел: next_hop_vm_id, next_hop_vip_id или next_hop_magic_router_id"), ""),
		"next_hop_vm_id", "next_hop_vip_id", "next_hop_magic_router_id")
	if err != nil {
		return nil, err
	}
	switch key {
	case "next_hop_vm_id":
		iface := stringArg(args, "next_hop_interface_id")
		if iface == "" {
			return nil, errors.New(i18n.T("next_hop_interface_id is required with next_hop_vm_id", "с next_hop_vm_id нужен next_hop_interface_id"))
		}
		return map[string]any{"virtualMachine": map[string]any{
			"virtualMachine": map[string]any{"id": ref},
			"interface":      map[string]any{"id": iface},
		}}, nil
	case "next_hop_vip_id":
		return map[string]any{"vip": map[string]any{"vips": []map[string]any{{"id": ref}}}}, nil
	case "next_hop_magic_router_id":
		return map[string]any{"magicRouter": map[string]any{
			"magicRouter": map[string]any{"id": ref},
			"azName":      stringArg(args, "next_hop_az"),
		}}, nil
	}
	return nil, errors.New(i18n.T("a next hop is required: next_hop_vm_id+next_hop_interface_id, or next_hop_vip_id, or next_hop_magic_router_id+next_hop_az", "нужен следующий узел: next_hop_vm_id+next_hop_interface_id, next_hop_vip_id или next_hop_magic_router_id+next_hop_az"))
}

func (s *Registry) handleVpcRouteCreate(ctx context.Context, args map[string]any) (string, bool) {
	vpcID := stringArg(args, "vpc_id")
	subnet := stringArg(args, "subnet")
	if vpcID == "" || subnet == "" {
		return usageT("'vpc_id' and 'subnet' (destination CIDR, e.g. 192.168.0.0/24) are required", "нужны 'vpc_id' и 'subnet' (CIDR назначения, например 192.168.0.0/24)"), true
	}
	nextHop, err := routeNextHop(args)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("vpc_route_create adds a static route %s in VPC %s (async). Call again with confirmed=true.",
				"vpc_route_create добавляет статический маршрут %s в VPC %s (асинхронно). Вызовите снова с confirmed=true.", subnet, vpcID),
		}), true
	}
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{"vpcId": vpcID, "projectId": cli.ProjectID(), "subnet": subnet, "nextHop": nextHop}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateRoute(ctx, vpcID, body)
	if err != nil {
		return errResult(fmt.Errorf("create route: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "note": "Async — poll vpc_operation(operation_id)."}), false
}

func (s *Registry) handleVpcRouteDelete(ctx context.Context, args map[string]any) (string, bool) {
	vpcID := stringArg(args, "vpc_id")
	routeID := stringArg(args, "route_id")
	if vpcID == "" || routeID == "" {
		return usageT("'vpc_id' and 'route_id' (from vpc_routes) are required", "нужны 'vpc_id' и 'route_id' (из vpc_routes)"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("vpc_route_delete removes static route %s from VPC %s (async). Call again with confirmed=true.",
			"vpc_route_delete удаляет статический маршрут %s из VPC %s (асинхронно). Вызовите снова с confirmed=true.", routeID, vpcID)}), true
	}
	cli, err := s.vpcClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteRoute(ctx, vpcID, routeID)
	if err != nil {
		return errResult(fmt.Errorf("delete route: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "route_id": routeID, "operation_id": op.ID}), false
}
