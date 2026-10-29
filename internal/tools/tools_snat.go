// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// resolveZone turns an optional zone name / explicit availability_zone_id
// into a concrete zone UUID, defaulting to the configured region (the AZ the
// deploy stack targets). Returns the zone id.
func (s *Registry) resolveZone(ctx context.Context, args map[string]any) (id, name string, err error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", "", err
	}
	// A zone name was given → resolve it to a UUID.
	if name := stringArg(args, "zone"); name != "" && stringArg(args, "availability_zone_id") == "" {
		zs, e := eng.Cloud.ListAvailabilityZones(ctx)
		if e != nil {
			return "", "", e
		}
		for _, z := range zs {
			if z.Name == name {
				return z.ID, z.Name, nil
			}
		}
		return "", "", fmt.Errorf(i18n.T("availability zone %q not found", "зона доступности %q не найдена"), name)
	}
	// The id given, else the configured region, by its UUID or its name
	// (resolveVMZone, as a VM's: its name went out as the id, P-Z1); its
	// name for the preview of the billable gateway (FB123-2), none when
	// the zones cannot be read.
	if stringArg(args, "availability_zone_id") == "" {
		if _, _, err := resolveVMZone(ctx, eng); err != nil {
			return "", "", err
		}
	}
	id = firstNonEmptyStr(stringArg(args, "availability_zone_id"), eng.Config.Region)
	if id == "" {
		return "", "", errors.New(i18n.T("no zone: pass 'zone' (name) or 'availability_zone_id', or set the region", "зона не задана: передайте 'zone' (имя) или 'availability_zone_id' либо задайте region"))
	}
	if zs, e := eng.Cloud.ListAvailabilityZones(ctx); e == nil {
		for _, z := range zs {
			if z.ID == id {
				return id, z.Name, nil
			}
		}
	}
	return id, "", nil
}

// handleSnatList lists SNAT gateways (outbound-internet egress for private
// subnets, e.g. mk8s worker nodes).
func (s *Registry) handleSnatList(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	gws, err := eng.Cloud.ListNATGateways(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(gws))
	for _, g := range gws {
		out = append(out, map[string]any{
			"id":                   g["id"],
			"name":                 g["name"],
			"nat_type":             g["nat_type"],
			"status":               firstNonNil(g["status"], g["state"]),
			"availability_zone_id": gatewayZoneID(g),
			"availability_zone":    firstNonNil(g["availability_zone_name"], nested(g, "availability_zone", "name")),
		})
	}
	return toJSON(map[string]any{"nat_gateways": out}), false
}

// handleSnatCreate creates a SNAT gateway so a private subnet's nodes can reach
// the internet (pull public images, apt, outbound calls). Gated on confirmed.
func (s *Registry) handleSnatCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageT("'name' is required", "нужен 'name'"), true
	}
	if err := provision.ValidName(i18n.T("SNAT gateway", "SNAT-шлюза"), name); err != nil {
		return errResult(err), true
	}
	zoneID, zoneName, err := s.resolveZone(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	// A second gateway of the same name is a second bill: refused before
	// the consent, as every other create (fault matrix, process audit
	// 1.2.1: a repeated snat_create made one more each time).
	list := func(ctx context.Context) ([]map[string]any, error) {
		return eng.Cloud.ListNATGateways(ctx, eng.Config.ProjectID)
	}
	existing, err := list(ctx)
	if err != nil {
		return errResult(fmt.Errorf(i18n.T("check the name is free: list SNAT gateways: %w", "проверка, что имя свободно: список шлюзов SNAT: %w"), err)), true
	}
	if id, st, ok := takenName(existing, name); ok {
		return nameTakenResult(i18n.T("SNAT gateway", "шлюз SNAT"), name, id, st, "snat_list")
	}
	if id, ok := gatewayInZone(existing, zoneID); ok {
		return errAnswer(clierrors.New(clierrors.CodeUsage,
			i18n.Tf("zone %s already has SNAT gateway %s: the zone's router takes one, and Cloud.ru refuses a second", "в зоне %s уже есть шлюз SNAT %s: у маршрутизатора зоны он один, второй Cloud.ru не создаст", firstNonEmptyStr(zoneName, zoneID), id),
			i18n.Tf("Use it (snat_get id=%s) — VMs of the zone go out through it — or delete it first.", "Используйте его (snat_get id=%s) — ВМ зоны выходят через него, — или сначала удалите.", id)),
			map[string]any{"existing_id": id}), true
	}

	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		will := map[string]any{
			"name":                 name,
			"nat_type":             provision.NATTypePublicSNAT,
			"availability_zone_id": zoneID,
		}
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("snat_create makes a billable SNAT gateway.",
				"snat_create создаёт платный шлюз SNAT."),
			"will_create": will,
			"preview":     willPreview(engine.ResourceSNAT, name, zoneName, will, []string{"nat_type", "availability_zone_id"}),
		}), true
	}

	gw, err := eng.Cloud.CreateNATGateway(ctx, provision.CreateNATGatewayRequest{
		Name:               name,
		NATType:            provision.NATTypePublicSNAT,
		ProjectID:          eng.Config.ProjectID,
		AvailabilityZoneID: zoneID,
	})
	if id, _ := gw["id"].(string); err != nil || id == "" {
		if err == nil {
			err = noID("gateway")
		}
		return lostCreate(ctx, "snat_create", list, i18n.T("SNAT gateway", "шлюз SNAT"), name, nil, "snat_list", fmt.Errorf(i18n.T("create SNAT gateway: %w", "создание шлюза SNAT: %w"), err))
	}
	return toJSON(map[string]any{
		"status":      "created",
		"nat_gateway": gw,
		"note":        "SNAT gateway created — private-subnet nodes in this zone now have outbound internet. Delete it with snat_delete when no longer needed (it's billable).",
	}), false
}

// planSnatDelete plans the delete of a SNAT gateway by id (gate.go).
func (s *Registry) planSnatDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "id")
	if id == "" {
		return nil, usageErr("'id' is required (get it from snat_list)", "нужен 'id' (из snat_list)")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	return s.deletePlan("snat_delete", "snat_delete removes SNAT gateway %s — nodes in that zone lose outbound internet.",
		"snat_delete удаляет шлюз SNAT %s — узлы в этой зоне потеряют исходящий доступ в интернет.",
		id, func() (map[string]any, error) {
			gws, err := eng.Cloud.ListNATGateways(ctx, eng.Config.ProjectID)
			if err != nil {
				return nil, err
			}
			for _, gw := range gws {
				if gid, _ := gw["id"].(string); gid == id {
					return gw, nil
				}
			}
			return nil, clierrors.New(clierrors.CodeNotFound, i18n.Tf("SNAT gateway %s not found in the project", "SNAT-шлюз %s в проекте не найден", id), i18n.T("snat_list lists the project's gateways.", "Шлюзы проекта — в выводе snat_list."))
		},
		func(ctx context.Context) (string, bool) {
			if err := eng.Cloud.DeleteNATGateway(ctx, id, eng.Config.ProjectID); err != nil {
				return s.deleteFailed("snat_delete", id, fmt.Errorf(i18n.T("delete SNAT gateway: %w", "удаление шлюза SNAT: %w"), err))
			}
			return toJSON(map[string]any{"status": "deleted", "id": id}), false
		}), nil
}

// firstNonNil returns the first non-nil value.
func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// nested is m[key][field] of an API answer kept as a map (the compute
// spec nests availability_zone {id, name}; snat_list read flat fields the
// answer does not have — release QA, API inventory).
func nested(m map[string]any, key, field string) any {
	if o, ok := m[key].(map[string]any); ok {
		return o[field]
	}
	return nil
}

// gatewayInZone is the SNAT gateway of the list in zone zoneID: a zone's
// router takes one, and a second was refused after consent (422 «Router
// already …», D54, FB15-17). Only a gateway whose answer names its zone
// counts; one that does not decides nothing.
func gatewayInZone(gateways []map[string]any, zoneID string) (string, bool) {
	for _, g := range gateways {
		if id, _ := gatewayZoneID(g).(string); id != "" && id == zoneID {
			gid, _ := g["id"].(string)
			return gid, true
		}
	}
	return "", false
}

// gatewayZoneID is the zone of a gateway as an answer names it: flat
// (availability_zone_id) or nested (availability_zone.id); one reading for
// snat_list and gatewayInZone.
func gatewayZoneID(g map[string]any) any {
	return firstNonNil(g["availability_zone_id"], nested(g, "availability_zone", "id"))
}
