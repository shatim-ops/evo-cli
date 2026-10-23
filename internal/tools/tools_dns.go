// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/dns"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
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

// dnsZoneVisibility is the visibility of zone zoneID: type when given,
// otherwise the list (public, then private) that holds the zone, as
// dns_zones reads them. Without type the tools went to the public API,
// and a private zone answered 403 there (live run 18.10: dns_zone_delete
// left the zone). A zone in neither list is NOT_FOUND.
func dnsZoneVisibility(ctx context.Context, cli *dns.Client, args map[string]any, zoneID string) (string, error) {
	if stringArg(args, "type") != "" {
		return dnsVisibility(args), nil
	}
	for _, vis := range []string{"public", "private"} {
		zones, err := cli.ListZones(ctx, vis)
		if err != nil {
			return "", err
		}
		for _, z := range zones {
			if fmt.Sprint(z["id"]) == zoneID {
				return vis, nil
			}
		}
	}
	return "", clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("DNS zone %s is in neither the public nor the private zone list", "DNS-зоны %s нет ни среди публичных, ни среди приватных зон", zoneID),
		i18n.T("See dns_zones for the zone ids.", "ID зон — в выводе dns_zones."))
}

// dnsRecordVisibility is the visibility of record recordID: type when
// given, otherwise the API (public, then private) that answers its GET.
// Only 403 and 404 mean "not in this API" (a foreign id gets 403, as
// AR-2, CRT-2); any other error (401, 5xx, no connection) is returned as
// is. Neither API has the record — NOT_FOUND.
func dnsRecordVisibility(ctx context.Context, cli *dns.Client, args map[string]any, recordID string) (string, error) {
	if stringArg(args, "type") != "" {
		return dnsVisibility(args), nil
	}
	for _, vis := range []string{"public", "private"} {
		_, err := cli.GetRecord(ctx, vis, recordID)
		if err == nil {
			return vis, nil
		}
		var ae *dns.APIError
		if !errors.As(err, &ae) || (ae.Status != http.StatusForbidden && ae.Status != http.StatusNotFound) {
			return "", err
		}
	}
	return "", clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("DNS record %s is found in neither the public nor the private API", "DNS-запись %s не найдена ни в публичном, ни в приватном API", recordID),
		i18n.T("See dns_records (zone_id) for the record ids.", "ID записей — в выводе dns_records (zone_id)."))
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
				// the zone's lifecycle state (dns/openapi.yaml: state; a public
				// zone also has confirmState): without it a zone being deleted
				// looked alive (live run 18.10: "Удаляется" in the console)
				"state": z["state"], "confirm_state": z["confirmState"],
			})
		}
	}
	return toJSON(map[string]any{"zones": out}), false
}

func (s *Registry) handleDnsZoneGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required (from dns_zones)", "нужен 'id' (из dns_zones)"), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	vis, err := dnsZoneVisibility(ctx, cli, args, id)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetZone(ctx, vis, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Registry) handleDnsZoneCreate(ctx context.Context, args map[string]any) (string, bool) {
	domain := stringArg(args, "domain")
	if domain == "" {
		return usageT("'domain' is required (FQDN, e.g. 'example.com.')", "нужен 'domain' (FQDN, например 'example.com.')"), true
	}
	vis := dnsVisibility(args)
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("dns_zone_create creates a %s DNS zone for %q (async). A public zone needs domain-ownership delegation to serve traffic.",
				"dns_zone_create создаёт зону DNS (%s) для %q (асинхронно). Публичная зона обслуживает запросы только после делегирования домена.", vis, domain),
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
	if err != nil || op.ID == "" {
		if err == nil {
			err = noID("operation")
		}
		list := func(ctx context.Context) ([]map[string]any, error) { return cli.ListZones(ctx, vis) }
		return lostCreate(ctx, list, i18n.T("DNS zone", "зона DNS"), "zone_id", name, nil, "dns_zone_get zone_id", "dns_zones", fmt.Errorf("create zone: %w", err))
	}
	out := map[string]any{"status": "creating", "visibility": vis, "operation_id": op.ID, "note": "Async — poll dns_operation(operation_id); then dns_zones for the id (public zones return a verification_token to delegate)."}
	// The zone's id is in the create's answer: named at once (fault matrix,
	// process audit 1.2.1; as pg_create, N-07).
	if op.ResourceID != "" {
		out["zone_id"] = op.ResourceID
	}
	return toJSON(out), false
}

func (s *Registry) handleDnsZoneDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return usageT("'id' is required (from dns_zones)", "нужен 'id' (из dns_zones)"), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	vis, err := dnsZoneVisibility(ctx, cli, args, id)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("dns_zone_delete PERMANENTLY deletes %s zone %s and its records (async).",
			"dns_zone_delete БЕЗВОЗВРАТНО удаляет зону (%s) %s и её записи (асинхронно).", vis, id)}), true
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
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
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
// recordValueArgs checks the values of dns_record_create before the
// preview: value or values, not both (the description offers one or the
// other; both were merged without a word), and no value of only spaces —
// " " went to the API as the record's value (release audit, REL-11).
func recordValueArgs(args map[string]any) error {
	if err := blankArgs(args, "value"); err != nil {
		return err
	}
	raw, _ := args["values"].([]any)
	if len(raw) > 0 && stringArg(args, "value") != "" {
		return clierrors.New(clierrors.CodeUsage,
			i18n.T("pass value or values, not both", "укажите value или values, не оба"),
			i18n.T("value is one record value; values is an array for several.",
				"value — одно значение записи; values — массив для нескольких."))
	}
	for i, v := range raw {
		if s, _ := v.(string); s != "" && strings.TrimSpace(s) == "" {
			return clierrors.New(clierrors.CodeUsage,
				i18n.Tf("empty value of values[%d]: it is only spaces", "пустое значение values[%d]: в нём только пробелы", i),
				i18n.T("Pass the record values, or leave out the empty ones.", "Передайте значения записи или уберите пустые."))
		}
	}
	return nil
}

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
		return usageT("'zone_id' is required (from dns_zones)", "нужен 'zone_id' (из dns_zones)"), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	vis, err := dnsZoneVisibility(ctx, cli, args, zoneID)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListRecords(ctx, vis, zoneID)
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
		return usageT("'record_id' is required (from dns_records)", "нужен 'record_id' (из dns_records)"), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	vis, err := dnsRecordVisibility(ctx, cli, args, id)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetRecord(ctx, vis, id)
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
	if err := recordValueArgs(args); err != nil {
		return errResult(err), true
	}
	vals := recordValues(args)
	if zoneID == "" || name == "" || rtype == "" || len(vals) == 0 {
		return usageT("'zone_id', 'name', 'record_type' (a/aaaa/cname/mx/ns/spf/txt/caa/srv) and 'value'/'values' are required", "нужны 'zone_id', 'name', 'record_type' (a/aaaa/cname/mx/ns/spf/txt/caa/srv) и 'value' или 'values'"), true
	}
	if !slices.Contains(dnsRecordTypes, rtype) {
		return usageT("record_type %q is not one of %s", "record_type %q не входит в список: %s", stringArg(args, "record_type"), strings.Join(dnsRecordTypes, ", ")), true
	}
	ttl := pgInt(args, "ttl", 300)
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		// the preview stays offline: without type the zone's visibility is
		// looked up on the confirmed call
		vis := dnsVisibility(args)
		if stringArg(args, "type") == "" {
			vis = i18n.T("(public or private: found by id)", "публичную или приватную — найдём по id")
		}
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("dns_record_create adds a %s-record %q → %v (ttl %d) in %s zone %s (async).",
				"dns_record_create добавляет запись %s %q → %v (ttl %d) в зону (%s) %s (асинхронно).", rtype, name, vals, ttl, vis, zoneID),
		}), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	vis, err := dnsZoneVisibility(ctx, cli, args, zoneID)
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
		return usageT("'record_id' is required (from dns_records)", "нужен 'record_id' (из dns_records)"), true
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	vis, err := dnsRecordVisibility(ctx, cli, args, id)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("dns_record_delete deletes %s record %s (async).",
			"dns_record_delete удаляет запись (%s) %s (асинхронно).", vis, id)}), true
	}
	op, err := cli.DeleteRecord(ctx, vis, id)
	if err != nil {
		return errResult(fmt.Errorf("delete record: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}
