// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/dbaas"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

const redisHost = "https://redis.api.cloud.ru"

func (s *Registry) redisClientEng(ctx context.Context) (*dbaas.Client, *engine.Engine, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	return dbaas.New(tok.Value, eng.Config.ProjectID, redisHost), eng, nil
}

func (s *Registry) handleRedisList(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListClusters(ctx, url.Values{})
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id": m["id"], "name": m["name"], "status": m["status"],
			"type": m["type"], "shards": m["shards"], "replicas": m["replicas"], "health": m["health"],
		})
	}
	return toJSON(map[string]any{"clusters": out}), false
}

func (s *Registry) handleRedisGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageT("'cluster_id' is required (from redis_list)", "нужен 'cluster_id' (из redis_list)"), true
	}
	if res := uuidArg("cluster_id", id, "redis_list"); res != "" {
		return res, true
	}
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Registry) handleRedisVersions(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListVersions(ctx, url.Values{})
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"versions": items}), false
}

func (s *Registry) handleRedisSpecs(ctx context.Context, args map[string]any) (string, bool) {
	cli, eng, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	versionID := stringArg(args, "version_id")
	if versionID == "" {
		if vers, e := cli.ListVersions(ctx, url.Values{}); e == nil && len(vers) > 0 {
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

func (s *Registry) handleRedisCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageT("'name' is required", "нужен 'name'"), true
	}
	// The shape first, before the cloud: type=FOO and shards=20 went through
	// to the preview (release audit, L-35).
	rtype, shards, replicas, err := redisShape(args)
	if err != nil {
		return usageResult(err), true
	}
	cli, eng, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	existing, err := cli.ListClusters(ctx, url.Values{})
	if err != nil {
		return errResult(fmt.Errorf(i18n.T("check the name is free: list clusters: %w", "проверка, что имя свободно: список кластеров: %w"), err)), true
	}
	if id, st, ok := takenName(existing, name); ok {
		return nameTakenResult(i18n.T("Redis cluster", "кластер Redis"), name, id, st, "redis_get cluster_id="+id)
	}

	versionID := stringArg(args, "version_id")
	if versionID == "" {
		vers, e := cli.ListVersions(ctx, url.Values{})
		if e != nil {
			return errResult(fmt.Errorf("list versions: %w", e)), true
		}
		if len(vers) == 0 {
			return errResult(errors.New(i18n.T("no redis versions available", "нет доступных версий Redis"))), true
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
		return needSpecResult("redis_specs", specArg, cpu, ram, out)
	}
	specID, _ := spec["id"].(string)

	storage := pgInt(args, "storage_gb", 0)
	if storage == 0 {
		storage = int(toF(spec["min_storage_gb"]))
	}
	if storage == 0 {
		storage = 40
	}

	willCreate := map[string]any{"name": name, "version_id": versionID, "specification_id": specID,
		"cpu": spec["cpu"], "memory_gb": spec["memory"], "storage_gb": storage, "shards": shards, "replicas": replicas,
		"hosts_per_shard": replicas + 1, "type": rtype, "subnet_id": subnetID, "availability_zone": zone}
	warn := ""
	if rtype == redisMasterReplica {
		// Docs (Redis, «Создание кластера»): Master/Replica is a master and
		// two replicas, Multi-AZ or Single-AZ. One subnet_id puts every host
		// in its zone.
		warn = "MASTER_REPLICA is a master and two replicas; with one subnet all hosts go to zone " + zone + " (Single-AZ, which the docs allow — Multi-AZ needs a subnet per zone, not available through evo). If the cluster ends FAILED it currently cannot be deleted via the API (Cloud.ru orphan-lock) and needs console cleanup."
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		out := map[string]any{"status": "confirmation_required",
			"message": i18n.T("redis_create provisions a BILLABLE Managed Redis cluster (async, ~minutes).",
				"redis_create создаёт ПЛАТНЫЙ кластер Managed Redis (асинхронно, несколько минут)."),
			"will_create": willCreate}
		if warn != "" {
			out["warning"] = warn
		}
		return toJSON(out), true
	}

	body := map[string]any{
		"name": name, "project_id": cli.ProjectID(), "version_id": versionID,
		"specification_id": specID, "storage_gb": storage, "shards": shards, "replicas": replicas, "type": rtype,
	}
	// Use the DEPRECATED singular subnet_id, NOT subnet_ids[]: the Redis backend
	// panics (divide-by-zero) when given the subnet_ids array (it mishandles
	// zone-distribution across the list), but works with the single subnet_id.
	body["subnet_id"] = subnetID
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateCluster(ctx, body)
	if err != nil || op.ID == "" {
		list := func(ctx context.Context) ([]map[string]any, error) { return cli.ListClusters(ctx, url.Values{}) }
		if text, isErr, ok := lostCreate(ctx, list, i18n.T("Redis cluster", "кластер Redis"), name, willCreate, "redis_get cluster_id"); ok {
			return text, isErr
		}
		if err == nil {
			err = errors.New("the API answered no operation id")
		}
		return errResult(fmt.Errorf("create cluster: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "cluster_id": op.ResourceID,
		"created": willCreate, "note": "Async (~minutes). Poll redis_operation(operation_id); then redis_get for status READY + redis_hosts. redis_get and redis_list report replicas as hosts per shard, the master included (" + fmt.Sprint(replicas+1) + " here)."}), false
}

// Redis cluster types of the API (openapi-redis.yaml, type).
const (
	redisMasterReplica = "MASTER_REPLICA"
	redisStandalone    = "STANDALONE"
)

// redisShape checks type, shards and replicas of redis_create. replicas is
// what the create request takes: the replicas of a shard WITHOUT the master.
// evo raised it to 3 for MASTER_REPLICA, and the API refused every cluster:
// "replicas per shard must be in range [3:3], requested - 4"; replicas=1 is
// "requested - 2", replicas=2 is created — and that cluster reads back from
// GET with replicas: 3, the hosts of a shard (release audit, L-34). The docs
// (Redis, «Создание кластера») make Master/Replica a master and two
// replicas, so MASTER_REPLICA takes replicas=2, the default; STANDALONE has
// no replicas. Shards: the API's range [1,8].
func redisShape(args map[string]any) (rtype string, shards, replicas int, err error) {
	rtype = strings.ToUpper(stringArg(args, "type"))
	if rtype == "" {
		rtype = redisMasterReplica
	}
	defReplicas := 2
	switch rtype {
	case redisMasterReplica:
	case redisStandalone:
		defReplicas = 0
	default:
		return "", 0, 0, fmt.Errorf(i18n.T("type %q is not one of %s, %s", "type %q не входит в список: %s, %s"), stringArg(args, "type"), redisMasterReplica, redisStandalone)
	}
	shards = pgInt(args, "shards", 1)
	if shards < 1 || shards > 8 {
		return "", 0, 0, fmt.Errorf(i18n.T("shards %d is out of range: 1-8", "shards %d вне диапазона 1–8"), shards)
	}
	replicas = pgInt(args, "replicas", defReplicas)
	if replicas != defReplicas {
		if rtype == redisMasterReplica {
			return "", 0, 0, fmt.Errorf(i18n.T("replicas %d: MASTER_REPLICA takes replicas=2 (a master and two replicas per shard; replicas counts the replicas without the master — redis_get reports 3 hosts)", "replicas %d: для MASTER_REPLICA нужно replicas=2 (мастер и две реплики на шард; replicas считает реплики без мастера — redis_get покажет 3 хоста)"), replicas)
		}
		return "", 0, 0, fmt.Errorf(i18n.T("replicas %d: a STANDALONE cluster has no replicas (replicas=0)", "replicas %d: у кластера STANDALONE нет реплик (replicas=0)"), replicas)
	}
	return rtype, shards, replicas, nil
}

func (s *Registry) handleRedisDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageT("'cluster_id' is required (from redis_list)", "нужен 'cluster_id' (из redis_list)"), true
	}
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return deletePreview("redis_delete PERMANENTLY deletes Redis cluster %s and its data.",
			"redis_delete БЕЗВОЗВРАТНО удаляет кластер Redis %s и его данные.",
			id, func() (map[string]any, error) { return cli.GetCluster(ctx, id) })
	}
	op, err := cli.DeleteCluster(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete cluster: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}

func (s *Registry) handleRedisOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
	}
	cli, _, err := s.redisClientEng(ctx)
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
