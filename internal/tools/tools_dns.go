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
	"github.com/shatim-ops/evo-cli/internal/engine"
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

// dnsDelegationWarning is what the preview of a public zone warns of: the
// CLI shows the preview in place of the message that says it.
func dnsDelegationWarning(vis string) string {
	if vis != "public" {
		return ""
	}
	return i18n.T("a public zone serves queries only after the domain is delegated to it",
		"публичная зона обслуживает запросы только после делегирования домена")
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

// dnsRecordGone reports the DNS API's answer to a record id it has no
// record for: 404, and the 400 «precondition» the live API gives on an id
// of the right form on GET and DELETE alike (register NET-15; G130-07,
// G13-31, D40). The id is a UUID by the schema (D36) and a request by id
// carries no body, so the 400 is not the call's.
func dnsRecordGone(err error) bool {
	var ae *dns.APIError
	return errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusBadRequest)
}

// dnsRecordNotFound is the refusal of a record the API answered it has no
// record for (dnsRecordGone), in the terms of evo: NOT_FOUND, which the
// delete reads as already_deleted (D26).
func dnsRecordNotFound(vis, recordID string) error {
	return clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("DNS record %s is not in the %s API", "DNS-записи %s нет в API %s зон", recordID, vis),
		i18n.T("See dns_records (zone_id) for the record ids.", "ID записей — в выводе dns_records (zone_id)."))
}

// dnsGetRecord reads record recordID in the API of vis; an answer that the
// API has no such record is NOT_FOUND (dnsRecordNotFound), any other error
// is as it is.
func dnsGetRecord(ctx context.Context, cli *dns.Client, vis, recordID string) (map[string]any, error) {
	m, err := cli.GetRecord(ctx, vis, recordID)
	if dnsRecordGone(err) {
		return nil, dnsRecordNotFound(vis, recordID)
	}
	return m, err
}

// dnsDeleteRecord deletes record recordID in the API of vis. A DELETE the
// API answers with «no such record» (dnsRecordGone) is checked by a read:
// the record gone — NOT_FOUND, the delete is done (already_deleted, D26);
// the record still there — the DELETE's error as it is: «deleted» means
// the record is not there (CLAUDE.md §6, axis 3).
func dnsDeleteRecord(ctx context.Context, cli *dns.Client, vis, recordID string) (*dns.Operation, error) {
	op, err := cli.DeleteRecord(ctx, vis, recordID)
	if !dnsRecordGone(err) {
		return op, err
	}
	if _, readErr := cli.GetRecord(ctx, vis, recordID); dnsRecordGone(readErr) {
		return nil, dnsRecordNotFound(vis, recordID)
	}
	return nil, err
}

// dnsRecordVisibility is the visibility of record recordID: type when
// given, otherwise the API (public, then private) that answers its GET.
// Only 403 and «no such record» (dnsRecordGone: 404, the live API's 400)
// mean "not in this API" (a foreign id gets 403, as AR-2, CRT-2); any
// other error (401, 5xx, no connection) is returned as is. Neither API has
// the record — NOT_FOUND.
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
		if !dnsRecordGone(err) && (!errors.As(err, &ae) || ae.Status != http.StatusForbidden) {
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
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	name := stringArg(args, "name")
	if name == "" {
		name = domain
	}
	// A name taken before the call is refused before the question (D49),
	// so a lost answer's zone found by the name is the call's own.
	list := func(ctx context.Context) ([]map[string]any, error) { return cli.ListZones(ctx, vis) }
	if ans, refused := nameFree(ctx, list, i18n.T("DNS zone", "зона DNS"), name, "dns_zones"); refused {
		return ans, true
	}
	// A private zone resolves only in the VPCs linked to it (release plan
	// 1.6, #597 п. 15; axis 4): evo links the VPC of vpc_id, else the
	// project's default one, and names it before the question.
	will := map[string]any{"name": stringArg(args, "name"), "visibility": vis}
	var vpcID string
	switch {
	case vis == "private":
		id, vname, err := s.projectVPC(ctx, stringArg(args, "vpc_id"))
		if err != nil {
			return errResult(err), true
		}
		vpcID, will["vpc_id"], will["vpc_name"] = id, id, vname
	case stringArg(args, "vpc_id") != "":
		return usageT("vpc_id links a private zone (type=private); a public zone is served on the internet, not in a VPC",
			"vpc_id привязывает приватную зону (type=private); публичная зона обслуживается в интернете, а не в VPC"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("dns_zone_create creates a %s DNS zone for %q (async). A public zone needs domain-ownership delegation to serve traffic.",
				"dns_zone_create создаёт зону DNS (%s) для %q (асинхронно). Публичная зона обслуживает запросы только после делегирования домена.", vis, domain),
			"preview": willPreview(engine.ResourceDNSZone, domain, "", will,
				[]string{"name", "visibility", "vpc_id", "vpc_name"}, dnsDelegationWarning(vis)),
		}), true
	}
	body := map[string]any{"project_id": cli.ProjectID(), "name": name, "domain": domain}
	if vpcID != "" {
		body["linkedVpcIds"] = []string{vpcID}
	}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateZone(ctx, vis, body)
	if err != nil || op.ID == "" {
		if err == nil {
			err = noID("operation")
		}
		return lostCreate(ctx, "dns_zone_create", list, i18n.T("DNS zone", "зона DNS"), name, nil, "dns_zone_get zone_id", fmt.Errorf(i18n.T("create zone: %w", "создание зоны: %w"), err))
	}
	out := map[string]any{"status": "creating", "visibility": vis, "operation_id": op.ID, "note": "Async — poll dns_operation(operation_id); then dns_zones for the id (public zones return a verification_token to delegate)."}
	// The zone's id is in the create's answer: named at once (fault matrix,
	// process audit 1.2.1; as pg_create, N-07).
	if op.ResourceID != "" {
		out["zone_id"] = op.ResourceID
	}
	return toJSON(out), false
}

func (s *Registry) planDnsZoneDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "id")
	if id == "" {
		return nil, usageErr("'id' is required (from dns_zones)", "нужен 'id' (из dns_zones)")
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return nil, err
	}
	vis, err := dnsZoneVisibility(ctx, cli, args, id)
	if err != nil {
		return answered(s.deleteFailed("dns_zone_delete", id, err)), nil
	}
	// With type the zone was not looked up: a zone that is not there
	// is already deleted (D26).
	if stringArg(args, "type") != "" {
		if _, err := cli.GetZone(ctx, vis, id); err != nil {
			return answered(s.deleteFailed("dns_zone_delete", id, err)), nil
		}
	}
	preview := map[string]any{"message": i18n.Tf("dns_zone_delete PERMANENTLY deletes %s zone %s and its records (async).",
		"dns_zone_delete БЕЗВОЗВРАТНО удаляет зону (%s) %s и её записи (асинхронно).", vis, id)}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		op, err := cli.DeleteZone(ctx, vis, id)
		if err != nil {
			return s.deleteFailed("dns_zone_delete", id, fmt.Errorf(i18n.T("delete zone: %w", "удаление зоны: %w"), err))
		}
		return toJSON(map[string]any{"status": "deleting", "visibility": vis, "id": id, "operation_id": op.ID}), false
	}}, nil
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
	return s.operation(ctx, "dns_operation", args, func(ctx context.Context) (any, error) { return cli.GetOperation(ctx, id) })
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
	m, err := dnsGetRecord(ctx, cli, vis, id)
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

// planDnsRecordCreate plans dns_record_create (D35): the zone and its
// records are read before the question — a zone that is not there is
// NOT_FOUND and a record of the same name and type is the taken name, both
// before consent, not after it (W6, G13-3).
func (s *Registry) planDnsRecordCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	zoneID := stringArg(args, "zone_id")
	name := stringArg(args, "name")
	rtype := strings.ToLower(stringArg(args, "record_type"))
	if err := recordValueArgs(args); err != nil {
		return nil, err
	}
	vals := recordValues(args)
	if zoneID == "" || name == "" || rtype == "" || len(vals) == 0 {
		return nil, usageErr("'zone_id', 'name', 'record_type' (a/aaaa/cname/mx/ns/spf/txt/caa/srv) and 'value'/'values' are required", "нужны 'zone_id', 'name', 'record_type' (a/aaaa/cname/mx/ns/spf/txt/caa/srv) и 'value' или 'values'")
	}
	if !slices.Contains(dnsRecordTypes, rtype) {
		return nil, usageErr("record_type %q is not one of %s", "record_type %q не входит в список: %s", stringArg(args, "record_type"), strings.Join(dnsRecordTypes, ", "))
	}
	ttl := pgInt(args, "ttl", 300)
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return nil, err
	}
	vis, err := dnsZoneVisibility(ctx, cli, args, zoneID)
	if err != nil {
		return nil, err
	}
	// A record is known by its name and type in the zone: a repeat after a
	// lost answer added a second one (fault matrix, process audit 1.2.1).
	key := name + " " + rtype
	list := func(ctx context.Context) ([]map[string]any, error) {
		records, err := cli.ListRecords(ctx, vis, zoneID)
		listed := make([]map[string]any, 0, len(records))
		for _, r := range records {
			listed = append(listed, map[string]any{"id": r["id"], "name": fmt.Sprint(r["name"]) + " " + strings.ToLower(fmt.Sprint(r["type"]))})
		}
		return listed, err
	}
	existing, err := list(ctx)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("check the record is new: list records: %w", "проверка, что записи ещё нет: список записей: %w"), err)
	}
	if id, st, ok := takenName(existing, key); ok {
		return answered(nameTakenResult(i18n.T("DNS record", "запись DNS"), key, id, st, "dns_records zone_id="+zoneID)), nil
	}
	return &Plan{
		Preview: map[string]any{
			"message": i18n.Tf("dns_record_create adds a %s-record %q → %v (ttl %d) in %s zone %s (async).",
				"dns_record_create добавляет запись %s %q → %v (ttl %d) в зону (%s) %s (асинхронно).", rtype, name, vals, ttl, vis, zoneID),
			"will_create": map[string]any{"name": name, "type": rtype, "values": vals, "ttl": ttl},
		},
		Execute: func(ctx context.Context) (string, bool) {
			body := map[string]any{
				dns.ZoneIDParam(vis): zoneID,
				"name":               name,
				"type":               rtype,
				"values":             vals,
				"ttl":                ttl,
			}
			op, err := cli.CreateRecord(ctx, vis, body)
			if err != nil || op.ID == "" {
				if err == nil {
					err = noID("operation")
				}
				return lostCreate(ctx, "dns_record_create", list, i18n.T("DNS record", "запись DNS"), key, nil, "dns_records zone_id="+zoneID, fmt.Errorf(i18n.T("create record: %w", "создание записи: %w"), err))
			}
			out := map[string]any{"status": "creating", "operation_id": op.ID, "note": "Async — poll dns_operation(operation_id)."}
			if op.ResourceID != "" {
				out["record_id"] = op.ResourceID
			}
			return toJSON(out), false
		},
	}, nil
}

func (s *Registry) planDnsRecordDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "record_id")
	if id == "" {
		return nil, usageErr("'record_id' is required (from dns_records)", "нужен 'record_id' (из dns_records)")
	}
	cli, err := s.dnsClient(ctx)
	if err != nil {
		return nil, err
	}
	vis, err := dnsRecordVisibility(ctx, cli, args, id)
	if err != nil {
		return answered(s.deleteFailed("dns_record_delete", id, err)), nil
	}
	// With type the record was not looked up: one that is not there is
	// already deleted (D26).
	if stringArg(args, "type") != "" {
		if _, err := dnsGetRecord(ctx, cli, vis, id); err != nil {
			return answered(s.deleteFailed("dns_record_delete", id, err)), nil
		}
	}
	preview := map[string]any{"message": i18n.Tf("dns_record_delete deletes %s record %s (async).",
		"dns_record_delete удаляет запись (%s) %s (асинхронно).", vis, id)}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		op, err := dnsDeleteRecord(ctx, cli, vis, id)
		if err != nil {
			return s.deleteFailed("dns_record_delete", id, fmt.Errorf(i18n.T("delete record: %w", "удаление записи: %w"), err))
		}
		return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
	}}, nil
}
