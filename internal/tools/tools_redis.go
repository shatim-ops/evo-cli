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

func (s *Registry) planRedisCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	if name == "" {
		return nil, usageErr("'name' is required", "нужен 'name'")
	}
	// The shape first, before the cloud: type=FOO and shards=20 went through
	// to the preview (release audit, L-35).
	rtype, shards, replicas, err := redisShape(args)
	if err != nil {
		return answered(usageResult(err), true), nil
	}
	cli, eng, err := s.redisClientEng(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := cli.ListClusters(ctx, url.Values{})
	if err != nil {
		return nil, fmt.Errorf(i18n.T("check the name is free: list clusters: %w", "проверка, что имя свободно: список кластеров: %w"), err)
	}
	if id, st, ok := takenName(existing, name); ok {
		return answered(nameTakenResult(i18n.T("Redis cluster", "кластер Redis"), name, id, st, "redis_get cluster_id="+id)), nil
	}

	versionID := stringArg(args, "version_id")
	vers, e := cli.ListVersions(ctx, url.Values{})
	if versionID != "" {
		// D54: an id the list has not is refused before the question; a
		// list not read decides nothing.
		if e == nil {
			if err := notInCatalogue(i18n.T("Redis version", "версии Redis"), versionID, listed(vers, "id"), "redis_versions"); err != nil {
				return nil, err
			}
		}
	} else {
		if e != nil {
			return nil, fmt.Errorf(i18n.T("list versions: %w", "список версий: %w"), e)
		}
		if len(vers) == 0 {
			return nil, errors.New(i18n.T("no redis versions available", "нет доступных версий Redis"))
		}
		versionID, _ = vers[len(vers)-1]["id"].(string)
	}

	// The subnet first: its zone is the cluster's, and the specifications
	// are listed for it.
	subnetID, zoneID, zone, err := dbaasSubnet(ctx, eng, stringArg(args, "subnet_id"))
	if err != nil {
		return dbaasSubnetErr(err)
	}
	specs, err := cli.ListSpecifications(ctx, versionID, zoneID)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("list specifications: %w", "список спецификаций: %w"), err)
	}
	specArg, cpu, ram := stringArg(args, "specification_id"), pgInt(args, "cpu", 0), pgInt(args, "ram", 0)
	spec := findDbaasSpec(specs, specArg, cpu, ram)
	if spec == nil {
		menu, trunc := compactSpecs(specs, specMenuCap)
		out := map[string]any{"resolved_version_id": versionID, "availability_zone": zone, "available_specifications": menu}
		if trunc > 0 {
			out["specifications_truncated"] = trunc
		}
		return answered(needSpecResult("redis_specs", specArg, cpu, ram, out)), nil
	}
	specID, _ := spec["id"].(string)

	storage, err := specStorage(args, "storage_gb", spec, 40)
	if err != nil {
		return nil, err
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
	out := map[string]any{"message": i18n.T("redis_create provisions a BILLABLE Managed Redis cluster (async, ~minutes).",
		"redis_create создаёт ПЛАТНЫЙ кластер Managed Redis (асинхронно, несколько минут)."),
		"will_create": willCreate,
		"preview": willPreview(engine.ResourceRedis, name, zone, willCreate,
			[]string{"version_id", "type", "cpu", "memory_gb", "storage_gb", "shards", "hosts_per_shard", "subnet_id"}, warn)}
	if warn != "" {
		out["warning"] = warn
	}
	return &Plan{Preview: out, Execute: func(ctx context.Context) (string, bool) {
		body := map[string]any{
			"name": name, "project_id": cli.ProjectID(), "version_id": versionID,
			"specification_id": specID, "storage_gb": storage, "shards": shards, "replicas": redisAPIReplicas(rtype, replicas), "type": rtype,
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
			if err == nil {
				err = noID("operation")
			}
			list := func(ctx context.Context) ([]map[string]any, error) { return cli.ListClusters(ctx, url.Values{}) }
			return lostCreate(ctx, "redis_create", list, i18n.T("Redis cluster", "кластер Redis"), name, willCreate, "redis_get cluster_id", fmt.Errorf(i18n.T("create cluster: %w", "создание кластера: %w"), err))
		}
		return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "cluster_id": op.ResourceID,
			"created": willCreate, "note": "Async (~minutes). Poll redis_operation(operation_id); then redis_get for status READY + redis_hosts. redis_get and redis_list report replicas as hosts per shard, the master included (" + fmt.Sprint(replicas+1) + " here)."}), false
	}}, nil
}

// redisAPIReplicas is the create request's replicas of a cluster whose
// shard has replicas replicas without the master. The API counts them so
// for MASTER_REPLICA (2 makes a master and two replicas), but for
// STANDALONE it takes the hosts of a shard: 0 and no field are 400
// «replicas must be positive», 1 makes the one host (G140-6, register
// DB-3; cloud-e2e redis standalone probe, 1 of 1). evo sent 0, and no
// STANDALONE cluster could be made through it.
func redisAPIReplicas(rtype string, replicas int) int {
	if rtype == redisStandalone {
		return replicas + 1
	}
	return replicas
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

func (s *Registry) planRedisDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return nil, usageErr("'cluster_id' is required (from redis_list)", "нужен 'cluster_id' (из redis_list)")
	}
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return nil, err
	}
	return s.deletePlan("redis_delete", "redis_delete PERMANENTLY deletes Redis cluster %s and its data.",
		"redis_delete БЕЗВОЗВРАТНО удаляет кластер Redis %s и его данные.",
		id, func() (map[string]any, error) { return cli.GetCluster(ctx, id) },
		func(ctx context.Context) (string, bool) {
			op, err := cli.DeleteCluster(ctx, id)
			if err != nil {
				return s.deleteFailed("redis_delete", id, fmt.Errorf(i18n.T("delete cluster: %w", "удаление кластера: %w"), err))
			}
			return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
		}), nil
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
	return s.operation(ctx, "redis_operation", args, func(ctx context.Context) (any, error) { return cli.GetOperation(ctx, id) })
}
