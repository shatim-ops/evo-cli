// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"

	"github.com/shatim-ops/evo-cli/internal/dbaas"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const kafkaHost = "https://kafka.api.cloud.ru"

func boolArg(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

// findDbaasSpec picks a spec by id, or by cpu+memory, else the first (cheapest
// after the caller sorted). Works for Kafka/Redis specs (cpu/memory fields).
func findDbaasSpec(specs []map[string]any, specID string, cpu, ram int) map[string]any {
	if specID != "" {
		for _, sp := range specs {
			if id, _ := sp["id"].(string); id == specID {
				return sp
			}
		}
		return nil
	}
	if cpu > 0 && ram > 0 {
		for _, sp := range specs {
			if int(toF(sp["cpu"])) == cpu && int(toF(sp["memory"])) == ram {
				return sp
			}
		}
		return nil
	}
	if len(specs) > 0 {
		// Default to the CHEAPEST (fewest cpu, then memory) — the API returns
		// specs unsorted, so specs[0] is arbitrary.
		sorted := make([]map[string]any, len(specs))
		copy(sorted, specs)
		sort.SliceStable(sorted, func(i, j int) bool {
			ci, cj := toF(sorted[i]["cpu"]), toF(sorted[j]["cpu"])
			if ci != cj {
				return ci < cj
			}
			return toF(sorted[i]["memory"]) < toF(sorted[j]["memory"])
		})
		return sorted[0]
	}
	return nil
}

// dbaasZone is the availability zone of a Kafka/Redis specifications
// request. The API requires availability_zone_id although the specification
// marks it optional, and the configured region is often empty (evo
// configure without --region: cloud-e2e readonly #23 got API_ERROR from
// kafka_specs and redis_specs). Order: the 'zone' argument (name or UUID),
// the configured region, else the default zone of evo init (ru.AZ-2, else
// the first listed). Returns the zone UUID and what to show for it: the
// name when known.
func dbaasZone(ctx context.Context, eng *engine.Engine, ref string) (id, shown string, err error) {
	if ref == "" && eng.Config.Region != "" {
		ref = eng.Config.Region
	}
	if ref != "" && looksUUID(ref) {
		// A UUID as given; its name, when the list has it, to show.
		if zones, err := eng.Cloud.ListAvailabilityZones(ctx); err == nil {
			for _, z := range zones {
				if z.ID == ref && z.Name != "" {
					return ref, z.Name, nil
				}
			}
		}
		return ref, ref, nil
	}
	zones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return "", "", fmt.Errorf(i18n.T("list availability zones: %w", "список зон доступности: %w"), err)
	}
	if ref != "" {
		z, err := provision.ResolveZone(zones, ref)
		if err != nil {
			return "", "", err
		}
		return z.ID, firstNonEmptyStr(z.Name, z.ID), nil
	}
	zones = engine.PreferredZoneFirst(zones)
	if len(zones) == 0 || zones[0].ID == "" {
		return "", "", errors.New(i18n.T("no availability zones listed: pass 'zone'", "список зон доступности пуст: передайте 'zone'"))
	}
	return zones[0].ID, zones[0].Name, nil
}

// dbaasSubnet picks the subnet of a new Kafka/Redis cluster first and takes
// the zone from it: the create request carries only the subnet, so the
// cluster lives in the subnet's zone, and its specification must be one of
// that zone (a flavor may be missing in a zone). subnet_id as given; else
// in the configured region the zone's main subnet (provision.PickSubnet);
// without a region the first zone with a subnet, ru.AZ-2 first (as evo
// init). Returns the subnet, the zone UUID and the zone to show.
func dbaasSubnet(ctx context.Context, eng *engine.Engine, subnetArg string) (subnetID, zoneID, zoneShown string, err error) {
	zones, _ := eng.Cloud.ListAvailabilityZones(ctx)
	subnetID, zoneID, zoneShown, err = pickDBaaSSubnet(ctx, eng, subnetArg, zones)
	if err != nil {
		return "", "", "", err
	}
	// A cluster in a zone closed for new resources never comes up and, for
	// PostgreSQL, ends FAILED, which the API does not delete (register
	// DB-21, OPS-10; final audit, N-06).
	if cerr := provision.ClosedZoneError(zones, zoneID); cerr != nil {
		return "", "", "", cerr
	}
	return subnetID, zoneID, zoneShown, nil
}

func pickDBaaSSubnet(ctx context.Context, eng *engine.Engine, subnetArg string, zones []*provision.AvailabilityZone) (subnetID, zoneID, zoneShown string, err error) {
	subs, err := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
	if err != nil || len(subs) == 0 {
		if subnetArg == "" {
			return "", "", "", errors.New(i18n.T("resolve subnet: no subnet of the project could be read; pass subnet_id explicitly", "выбор подсети: не прочитано ни одной подсети проекта; передайте subnet_id"))
		}
		subs = nil
	}
	zoneName := func(id string) string {
		for _, z := range zones {
			if z.ID == id && z.Name != "" {
				return z.Name
			}
		}
		return id
	}
	var picked *provision.Subnet
	switch {
	case subnetArg != "":
		for _, s := range subs {
			if s.ID == subnetArg {
				picked = s
			}
		}
		if picked == nil || picked.AvailabilityZoneID == "" {
			zid, shown, zerr := dbaasZone(ctx, eng, "")
			return subnetArg, zid, shown, zerr
		}
	case eng.Config.Region != "":
		zid, shown, zerr := dbaasZone(ctx, eng, "")
		if zerr != nil {
			return "", "", "", zerr
		}
		if i := provision.PickSubnet(subs, zid, shown); i >= 0 && provision.SubnetInZone(subs[i], zid, shown) {
			return subs[i].ID, zid, shown, nil
		}
		return "", "", "", clierrors.New(clierrors.CodeUsage, i18n.Tf("no subnet of the project in zone %s", "в зоне %s нет подсети проекта", shown),
			i18n.T("Pass subnet_id, or create a subnet in this zone in the Cloud.ru console.", "Передайте subnet_id или создайте подсеть в этой зоне в консоли Cloud.ru."))
	default:
		for _, z := range engine.PreferredZoneFirst(provision.OpenZones(zones)) {
			if i := provision.PickSubnet(subs, z.ID, z.Name); i >= 0 && provision.SubnetInZone(subs[i], z.ID, z.Name) {
				return subs[i].ID, z.ID, z.Name, nil
			}
		}
		picked = subs[0]
	}
	if picked.AvailabilityZoneID == "" {
		zid, shown, zerr := dbaasZone(ctx, eng, "")
		return picked.ID, zid, shown, zerr
	}
	return picked.ID, picked.AvailabilityZoneID, zoneName(picked.AvailabilityZoneID), nil
}

func (s *Registry) kafkaClientEng(ctx context.Context) (*dbaas.Client, *engine.Engine, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	return dbaas.New(tok.Value, eng.Config.ProjectID, kafkaHost), eng, nil
}

func kafkaQuery() url.Values {
	q := url.Values{}
	q.Set("product_type", "kafka")
	return q
}

func (s *Registry) handleKafkaList(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListClusters(ctx, kafkaQuery())
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id": m["id"], "name": m["name"], "status": m["status"],
			"version": m["version_id"], "brokers": m["brokers"], "health": m["health"],
		})
	}
	return toJSON(map[string]any{"clusters": out}), false
}

func (s *Registry) handleKafkaGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageT("'cluster_id' is required (from kafka_list)", "нужен 'cluster_id' (из kafka_list)"), true
	}
	if res := uuidArg("cluster_id", id, "kafka_list"); res != "" {
		return res, true
	}
	cli, _, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Registry) handleKafkaVersions(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListVersions(ctx, kafkaQuery())
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"versions": items}), false
}

func (s *Registry) handleKafkaSpecs(ctx context.Context, args map[string]any) (string, bool) {
	cli, eng, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	versionID := stringArg(args, "version_id")
	if versionID == "" {
		if vers, e := cli.ListVersions(ctx, kafkaQuery()); e == nil && len(vers) > 0 {
			versionID, _ = vers[len(vers)-1]["id"].(string)
		}
	}
	zoneID, zone, err := dbaasZone(ctx, eng, stringArg(args, "zone"))
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListSpecifications(ctx, versionID, zoneID)
	if err != nil {
		return errResult(err), true
	}
	out, trunc := compactSpecs(items, specMenuCap)
	res := map[string]any{"resolved_version_id": versionID, "availability_zone": zone, "specifications": out}
	if trunc > 0 {
		res["specifications_truncated"] = trunc
	}
	return toJSON(res), false
}

func (s *Registry) handleKafkaCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageT("'name' is required (2-63 chars)", "нужен 'name' (2–63 символа)"), true
	}
	if b := pgInt(args, "brokers", 0); b < 0 {
		return usageT("brokers %d: at least 1", "brokers %d: нужен хотя бы 1", b), true
	}
	cli, eng, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	existing, err := cli.ListClusters(ctx, kafkaQuery())
	if err != nil {
		return errResult(fmt.Errorf(i18n.T("check the name is free: list clusters: %w", "проверка, что имя свободно: список кластеров: %w"), err)), true
	}
	if id, st, ok := takenName(existing, name); ok {
		return nameTakenResult(i18n.T("Kafka cluster", "кластер Kafka"), name, id, st, "kafka_get cluster_id="+id)
	}

	versionID := stringArg(args, "version_id")
	if versionID == "" {
		vers, e := cli.ListVersions(ctx, kafkaQuery())
		if e != nil {
			return errResult(fmt.Errorf("list versions: %w", e)), true
		}
		if len(vers) == 0 {
			return errResult(errors.New(i18n.T("no kafka versions available", "нет доступных версий Kafka"))), true
		}
		versionID, _ = vers[len(vers)-1]["id"].(string)
	}

	// The subnet first: its zone is the cluster's, and the specifications
	// are listed for it.
	subnetID, zoneID, zone, err := dbaasSubnet(ctx, eng, stringArg(args, "subnet_id"))
	if err != nil {
		return errResult(err), true
	}
	specs, err := cli.ListSpecifications(ctx, versionID, zoneID)
	if err != nil {
		return errResult(fmt.Errorf("list specifications: %w", err)), true
	}
	specArg, cpu, ram := stringArg(args, "specification_id"), pgInt(args, "cpu", 0), pgInt(args, "ram", 0)
	spec := findDbaasSpec(specs, specArg, cpu, ram)
	if spec == nil {
		menu, trunc := compactSpecs(specs, specMenuCap)
		out := map[string]any{"resolved_version_id": versionID, "availability_zone": zone, "available_specifications": menu}
		if trunc > 0 {
			out["specifications_truncated"] = trunc
		}
		return needSpecResult("kafka_specs", specArg, cpu, ram, out)
	}
	specID, _ := spec["id"].(string)

	storage := pgInt(args, "storage_gib", 0)
	if storage == 0 {
		storage = int(toF(spec["min_storage_gigs"]))
	}
	if storage == 0 {
		storage = 40
	}
	brokers, brokersNote, err := kafkaBrokers(pgInt(args, "brokers", 0), spec)
	if err != nil {
		return usageResult(err), true
	}

	willCreate := map[string]any{"name": name, "version_id": versionID, "specification_id": specID,
		"cpu": spec["cpu"], "memory_gb": spec["memory"], "storage_gib": storage, "brokers": brokers, "subnet_id": subnetID,
		"availability_zone": zone}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		out := map[string]any{"status": "confirmation_required",
			"message": i18n.T("kafka_create provisions a BILLABLE Managed Kafka cluster (async, ~minutes).",
				"kafka_create создаёт ПЛАТНЫЙ кластер Managed Kafka (асинхронно, несколько минут)."),
			"will_create": willCreate,
			"preview": willPreview(engine.ResourceKafka, name, zone, willCreate,
				[]string{"version_id", "cpu", "memory_gb", "storage_gib", "brokers", "subnet_id"}, brokersNote)}
		if brokersNote != "" {
			out["warning"] = brokersNote
		}
		return toJSON(out), true
	}

	body := map[string]any{
		"name": name, "project_id": cli.ProjectID(), "version_id": versionID,
		"specification_id": specID, "subnet_id": subnetID, "storage_gib": storage, "brokers": brokers,
		"ui_enabled": boolArg(args, "ui_enabled", false), "cruise_control_enabled": false,
		"cruise_control_rebalance_auto_enabled": false,
	}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateCluster(ctx, body)
	if err != nil || op.ID == "" {
		if err == nil {
			err = errors.New("the API answered no operation id")
		}
		list := func(ctx context.Context) ([]map[string]any, error) { return cli.ListClusters(ctx, kafkaQuery()) }
		return lostCreate(ctx, list, i18n.T("Kafka cluster", "кластер Kafka"), name, willCreate, "kafka_get cluster_id", "kafka_list", fmt.Errorf("create cluster: %w", err))
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "cluster_id": op.ResourceID,
		"created": willCreate, "note": "Async (~minutes). Poll kafka_operation(operation_id); then kafka_get for status READY + kafka_hosts."}), false
}

// kafkaBrokers is the broker count of kafka_create: the argument, else the
// specification's min_brokers. The specification describes min_brokers and
// max_brokers (openapi-kafka.yaml), but the live API lists specifications
// without them, so the default was a silent 1 (release audit, L-33). One
// broker is a working cluster (the audit's live run created one), with no
// replication: without the argument the answer says so. The bounds are
// checked when the specification has them. note is "" when nothing needs
// saying.
func kafkaBrokers(arg int, spec map[string]any) (brokers int, note string, err error) {
	minB, maxB := int(toF(spec["min_brokers"])), int(toF(spec["max_brokers"]))
	brokers = arg
	if brokers == 0 {
		brokers = minB
	}
	if brokers == 0 {
		brokers = 1
		note = "brokers: 1 (the specification gives no minimum): a single broker keeps one copy of each partition and stops on any broker failure. Pass brokers=3 for a replicated cluster."
	}
	if minB > 0 && brokers < minB {
		return 0, "", fmt.Errorf(i18n.T("brokers %d: this specification takes at least %d", "brokers %d: для этой конфигурации нужно не меньше %d"), brokers, minB)
	}
	if maxB > 0 && brokers > maxB {
		return 0, "", fmt.Errorf(i18n.T("brokers %d: this specification takes at most %d", "brokers %d: для этой конфигурации можно не больше %d"), brokers, maxB)
	}
	return brokers, note, nil
}

func (s *Registry) handleKafkaDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageT("'cluster_id' is required (from kafka_list)", "нужен 'cluster_id' (из kafka_list)"), true
	}
	cli, _, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return deletePreview("kafka_delete PERMANENTLY deletes Kafka cluster %s and its data.",
			"kafka_delete БЕЗВОЗВРАТНО удаляет кластер Kafka %s и его данные.",
			id, func() (map[string]any, error) { return cli.GetCluster(ctx, id) })
	}
	op, err := cli.DeleteCluster(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete cluster: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}

func (s *Registry) handleKafkaOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
	}
	cli, _, err := s.kafkaClientEng(ctx)
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
