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
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// resolveSnatZone turns an optional zone name / explicit availability_zone_id
// into a concrete zone UUID, defaulting to the configured region (the AZ the
// deploy stack targets). Returns the zone id.
func (s *Registry) resolveSnatZone(ctx context.Context, args map[string]any) (string, error) {
	if id := stringArg(args, "availability_zone_id"); id != "" {
		return id, nil
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", err
	}
	// A zone name was given → resolve it to a UUID.
	if name := stringArg(args, "zone"); name != "" {
		zs, e := eng.Cloud.ListAvailabilityZones(ctx)
		if e != nil {
			return "", e
		}
		for _, z := range zs {
			if z.Name == name {
				return z.ID, nil
			}
		}
		return "", fmt.Errorf(i18n.T("availability zone %q not found", "зона доступности %q не найдена"), name)
	}
	// Default: the configured region (CLOUDRU_REGION = the AZ UUID).
	if eng.Config.Region != "" {
		return eng.Config.Region, nil
	}
	return "", errors.New(i18n.T("no zone: pass 'zone' (name) or 'availability_zone_id', or set the region", "зона не задана: передайте 'zone' (имя) или 'availability_zone_id' либо задайте region"))
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
			"availability_zone_id": firstNonNil(g["availability_zone_id"], nested(g, "availability_zone", "id")),
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
	zoneID, err := s.resolveSnatZone(ctx, args)
	if err != nil {
		return errResult(err), true
	}

	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "snat_create makes a billable SNAT gateway. Get the user's approval, then call again with confirmed=true.",
			"will_create": map[string]any{
				"name":                 name,
				"nat_type":             provision.NATTypePublicSNAT,
				"availability_zone_id": zoneID,
			},
		}), true
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	gw, err := eng.Cloud.CreateNATGateway(ctx, provision.CreateNATGatewayRequest{
		Name:               name,
		NATType:            provision.NATTypePublicSNAT,
		ProjectID:          eng.Config.ProjectID,
		AvailabilityZoneID: zoneID,
	})
	if err != nil {
		return errResult(fmt.Errorf("create SNAT gateway: %w", err)), true
	}
	return toJSON(map[string]any{
		"status":      "created",
		"nat_gateway": gw,
		"note":        "SNAT gateway created — private-subnet nodes in this zone now have outbound internet. Delete it with snat_delete when no longer needed (it's billable).",
	}), false
}

// handleSnatDelete deletes a SNAT gateway by id. Gated on confirmed.
func (s *Registry) handleSnatDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required (get it from snat_list)", "нужен 'id' (из snat_list)"), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return deletePreview("snat_delete removes SNAT gateway %s — nodes in that zone lose outbound internet. Confirm, then call again with confirmed=true.",
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
			})
	}
	if err := eng.Cloud.DeleteNATGateway(ctx, id, eng.Config.ProjectID); err != nil {
		return errResult(fmt.Errorf("delete SNAT gateway: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleted", "id": id}), false
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
