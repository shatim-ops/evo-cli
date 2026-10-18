// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/dns"
)

// dnsClient builds an authenticated Evolution DNS client (standalone → newEngine token).
func (s *Registry) dnsClient(ctx context.Context) (*dns.Client, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return dns.New(tok.Value, eng.Config.ProjectID, ""), nil
}

func dnsVisibility(args map[string]any) string {
	if v := stringArg(args, "type"); v == "private" {
		return "private"
	}
	return "public"
}

// --- Zones ---

func (s *Registry) handleDnsZones(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0)
	for _, vis := range []string{"public", "private"} {
		zones, e := cli.ListZones(ctx, vis)
		if e != nil {
			return errResult(e), true
		}
		for _, z := range zones {
			out = append(out, map[string]any{
				"id": z["id"], "name": firstNonNil(z["name"], z["domain"]),
				"domain": z["domain"], "visibility": vis,
				"valid": z["valid"], "delegated": z["delegated"],
			})
		}
	}
	return toJSON(map[string]any{"zones": out}), false
}

func (s *Registry) handleDnsZoneGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageResult(fmt.Errorf("'id' is required (from dns_zones)")), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetZone(ctx, dnsVisibility(args), id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Registry) handleDnsZoneCreate(ctx context.Context, args map[string]any) (string, bool) {
	domain := stringArg(args, "domain")
	if domain == "" {
		return usageResult(fmt.Errorf("'domain' is required (FQDN, e.g. 'example.com.')")), true
	}
	vis := dnsVisibility(args)
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("dns_zone_create creates a %s DNS zone for %q (async). A public zone needs domain-ownership delegation to serve traffic. Call again with confirmed=true.", vis, domain),
		}), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	name := stringArg(args, "name")
	if name == "" {
		name = domain
	}
	body := map[string]any{"project_id": cli.ProjectID(), "name": name, "domain": domain}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateZone(ctx, vis, body)
	if err != nil {
		return errResult(fmt.Errorf("create zone: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "visibility": vis, "operation_id": op.ID, "note": "Async — poll dns_operation(operation_id); then dns_zones for the id (public zones return a verification_token to delegate)."}), false
}

func (s *Registry) handleDnsZoneDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageResult(fmt.Errorf("'id' is required (from dns_zones)")), true
	}
	vis := dnsVisibility(args)
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("dns_zone_delete PERMANENTLY deletes %s zone %s and its records (async). Call again with confirmed=true.", vis, id)}), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteZone(ctx, vis, id)
	if err != nil {
		return errResult(fmt.Errorf("delete zone: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "visibility": vis, "id": id, "operation_id": op.ID}), false
}

// --- Operations ---

func (s *Registry) handleDnsOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageResult(fmt.Errorf("'operation_id' is required")), true
	}
	cli, err := s.dnsClient(ctx)
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

// --- Records ---

// recordValues collects values from a `values` array and/or a single `value`.
func recordValues(args map[string]any) []string {
	var vals []string
	if raw, ok := args["values"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok && s != "" {
				vals = append(vals, s)
			}
		}
	}
	if v := stringArg(args, "value"); v != "" {
		vals = append(vals, v)
	}
	return vals
}

func (s *Registry) handleDnsRecords(ctx context.Context, args map[string]any) (string, bool) {
	zoneID := stringArg(args, "zone_id")
	if zoneID == "" {
		return usageResult(fmt.Errorf("'zone_id' is required (from dns_zones)")), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListRecords(ctx, dnsVisibility(args), zoneID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{"id": m["id"], "name": m["name"], "type": m["type"], "values": m["values"], "ttl": m["ttl"]})
	}
	return toJSON(map[string]any{"records": out}), false
}

func (s *Registry) handleDnsRecordGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "record_id")
	if id == "" {
		return usageResult(fmt.Errorf("'record_id' is required (from dns_records)")), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetRecord(ctx, dnsVisibility(args), id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// dnsRecordTypes are the record types dns_record_create takes, in the
// spelling the API takes: lower case (swagger: "a, aaaa, srv, cname, mx, ns,
// spf, txt, caa"). record_type=A was a 400 "invalid argument" (release
// audit, L-16); the schema's enum matches any case and passes it on lower.
var dnsRecordTypes = []string{"a", "aaaa", "cname", "mx", "ns", "spf", "txt", "caa", "srv"}

func (s *Registry) handleDnsRecordCreate(ctx context.Context, args map[string]any) (string, bool) {
	zoneID := stringArg(args, "zone_id")
	name := stringArg(args, "name")
	rtype := strings.ToLower(stringArg(args, "record_type"))
	vals := recordValues(args)
	if zoneID == "" || name == "" || rtype == "" || len(vals) == 0 {
		return usageResult(fmt.Errorf("'zone_id', 'name', 'record_type' (a/aaaa/cname/mx/ns/spf/txt/caa/srv) and 'value'/'values' are required")), true
	}
	if !slices.Contains(dnsRecordTypes, rtype) {
		return usageResult(fmt.Errorf("record_type %q is not one of %s", stringArg(args, "record_type"), strings.Join(dnsRecordTypes, ", "))), true
	}
	ttl := pgInt(args, "ttl", 300)
	vis := dnsVisibility(args)
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("dns_record_create adds a %s %s-record %q → %v (ttl %d) in %s zone %s (async). Call again with confirmed=true.", vis, rtype, name, vals, ttl, vis, zoneID),
		}), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{
		dns.ZoneIDParam(vis): zoneID,
		"name":               name,
		"type":               rtype,
		"values":             vals,
		"ttl":                ttl,
	}
	op, err := cli.CreateRecord(ctx, vis, body)
	if err != nil {
		return errResult(fmt.Errorf("create record: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "note": "Async — poll dns_operation(operation_id)."}), false
}

func (s *Registry) handleDnsRecordDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "record_id")
	if id == "" {
		return usageResult(fmt.Errorf("'record_id' is required (from dns_records)")), true
	}
	vis := dnsVisibility(args)
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("dns_record_delete deletes %s record %s (async). Call again with confirmed=true.", vis, id)}), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteRecord(ctx, vis, id)
	if err != nil {
		return errResult(fmt.Errorf("delete record: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}
