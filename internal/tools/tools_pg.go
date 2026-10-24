// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/postgresql"
)

// specMenuCap caps how many specifications the need_specification menu returns.
// The full list (17+ specs, each ~10 fields) overflows the agent trajectory
// (trajectory-array-size-limit). Cheapest-first + a truncated count is enough
// to pick from.
const specMenuCap = 12

// compactSpecs returns a compact, cheapest-first, capped view of PG specs:
// only the fields an agent needs to choose (id, display_name, cpu, memory,
// deployment_mode, min_storage_gb). Returns the menu and how many were dropped.
func compactSpecs(specs []map[string]any, cap int) ([]map[string]any, int) {
	sorted := make([]map[string]any, len(specs))
	copy(sorted, specs)
	sort.SliceStable(sorted, func(i, j int) bool {
		ci, cj := toF(sorted[i]["cpu"]), toF(sorted[j]["cpu"])
		if ci != cj {
			return ci < cj
		}
		return toF(sorted[i]["memory"]) < toF(sorted[j]["memory"])
	})
	trunc := 0
	if len(sorted) > cap {
		trunc = len(sorted) - cap
		sorted = sorted[:cap]
	}
	out := make([]map[string]any, 0, len(sorted))
	for _, s := range sorted {
		minStorage := s["min_storage_gb"]
		if minStorage == nil {
			minStorage = s["min_storage_gigs"] // Kafka uses this field name
		}
		name := s["display_name"]
		if name == nil {
			name = s["name"] // Kafka/Redis specs use "name"
		}
		row := map[string]any{
			"id": s["id"], "display_name": name,
			"cpu": s["cpu"], "memory": s["memory"],
			"deployment_mode": s["deployment_mode"], "min_storage_gb": minStorage,
		}
		// The limits a create is checked against, when the service lists
		// them (PG max_hosts, Kafka min/max_brokers).
		for _, k := range []string{"max_hosts", "min_brokers", "max_brokers"} {
			if v, ok := s[k]; ok && v != nil {
				row[k] = v
			}
		}
		out = append(out, row)
	}
	return out, trunc
}

// pgClient builds an authenticated Managed PostgreSQL client.
func (s *Registry) pgClient(ctx context.Context) (*postgresql.Client, error) {
	cli, _, err := s.pgClientEng(ctx)
	return cli, err
}

// pgClientEng also returns the engine (for subnet resolution in pg_create).
// Uses authedEngine (not newEngine) so eng.Cloud (the compute adapter) carries a
// valid bearer — otherwise eng.Cloud.ListSubnets 403s with "RBAC: access denied".
func (s *Registry) pgClientEng(ctx context.Context) (*postgresql.Client, *engine.Engine, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	return postgresql.New(tok.Value, eng.Config.ProjectID, ""), eng, nil
}

func pgInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func toF(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

// --- Clusters: read ---

func (s *Registry) handlePgList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListClusters(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id":      m["id"],
			"name":    m["name"],
			"status":  firstNonNil(m["status"], m["state"]),
			"version": m["version"],
			"health":  m["health"],
		})
	}
	return toJSON(map[string]any{"clusters": out}), false
}

func (s *Registry) handlePgGet(ctx context.Context, args map[string]any) (string, bool) {
	// id or its alias cluster_id, as the pg_* sub-resource tools name it;
	// both and different is USAGE (QA-1).
	id, err := aliasArg(args, "id", "cluster_id")
	if err != nil {
		return errResult(err), true
	}
	if id == "" {
		return usageT("'id' is required (get it from pg_list)", "нужен 'id' (из pg_list)"), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// --- Reference ---

func (s *Registry) handlePgVersions(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListVersions(ctx)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"versions": items}), false
}

func (s *Registry) handlePgSpecs(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	// /v1/specifications requires a version_name (404 without it) → default to latest.
	version := stringArg(args, "version")
	if version == "" {
		if vers, e := cli.ListVersions(ctx); e == nil {
			version = pickLatestVersion(vers)
		}
	}
	items, err := cli.ListSpecifications(ctx, version)
	if err != nil {
		return errResult(err), true
	}
	// Cheapest-first + cap so the 17-element array can't overflow the agent
	// trajectory (trajectory-array-size-limit → non-deliverable turn).
	sort.SliceStable(items, func(i, j int) bool {
		ci, cj := toF(items[i]["cpu"]), toF(items[j]["cpu"])
		if ci != cj {
			return ci < cj
		}
		return toF(items[i]["memory"]) < toF(items[j]["memory"])
	})
	trunc := 0
	if len(items) > specMenuCap {
		trunc = len(items) - specMenuCap
		items = items[:specMenuCap]
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id":              m["id"],
			"display_name":    m["display_name"],
			"cpu":             m["cpu"],
			"memory_gb":       m["memory"],
			"deployment_mode": m["deployment_mode"],
			"flavor_type":     m["flavor_type"],
			"min_storage_gb":  m["min_storage_gb"],
			"max_hosts":       m["max_hosts"],
		})
	}
	res := map[string]any{"specifications": out}
	if trunc > 0 {
		res["specifications_truncated"] = trunc
	}
	return toJSON(res), false
}

func (s *Registry) handlePgOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageT("'operation_id' is required", "нужен 'operation_id'"), true
	}
	cli, err := s.pgClient(ctx)
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

// --- Cluster lifecycle (mutations) ---

func pickLatestVersion(versions []map[string]any) string {
	best, bestN := "", -1
	for _, v := range versions {
		name, _ := v["name"].(string)
		if name == "" {
			continue
		}
		if n, err := strconv.Atoi(name); err == nil {
			if n > bestN {
				bestN, best = n, name
			}
		} else if best == "" {
			best = name
		}
	}
	return best
}

func findSpec(specs []map[string]any, specID string, cpu, ram int) map[string]any {
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
	}
	return nil
}

// pgMaxHosts is the most nodes a cluster of this specification takes: its
// max_hosts, else by deployment_mode as the API documents instances
// (openapi postgresql: "1-2 узла для standard, 1-3 узла для business").
// pg_create instances=5 on a standard spec went through to the preview
// (release audit, L-31). 0: not known, not checked.
func pgMaxHosts(spec map[string]any) int {
	if n := int(toF(spec["max_hosts"])); n > 0 {
		return n
	}
	switch mode, _ := spec["deployment_mode"].(string); strings.ToLower(mode) {
	case "standard":
		return 2
	case "business":
		return 3
	}
	return 0
}

func (s *Registry) handlePgCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageT("'name' is required", "нужен 'name'"), true
	}
	instances := pgInt(args, "instances", 1)
	cli, eng, err := s.pgClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	existing, err := cli.ListClusters(ctx)
	if err != nil {
		return errResult(fmt.Errorf(i18n.T("check the name is free: list clusters: %w", "проверка, что имя свободно: список кластеров: %w"), err)), true
	}
	if id, st, ok := takenName(existing, name); ok {
		return nameTakenResult(i18n.T("PostgreSQL cluster", "кластер PostgreSQL"), name, id, st, "pg_get id="+id)
	}

	// Resolve version (default: latest available).
	versions, err := cli.ListVersions(ctx)
	if err != nil {
		return errResult(fmt.Errorf("list versions: %w", err)), true
	}
	version := stringArg(args, "version")
	if version == "" {
		version = pickLatestVersion(versions)
	}

	// Resolve specification (by id, or by cpu+ram).
	specs, err := cli.ListSpecifications(ctx, version)
	if err != nil {
		return errResult(fmt.Errorf("list specifications: %w", err)), true
	}
	specArg, cpu, ram := stringArg(args, "specification_id"), pgInt(args, "cpu", 0), pgInt(args, "ram", 0)
	spec := findSpec(specs, specArg, cpu, ram)
	if spec == nil {
		// Not enough info to pick a spec — hand the agent a COMPACT, capped menu.
		// Returning all 17 full spec objects overflows the agent trajectory
		// (trajectory-array-size-limit → non-deliverable turn). Keep the key
		// fields, cheapest-first, capped.
		menu, trunc := compactSpecs(specs, specMenuCap)
		out := map[string]any{
			"resolved_version":         version,
			"available_specifications": menu,
		}
		if trunc > 0 {
			out["specifications_truncated"] = trunc
		}
		return needSpecResult("pg_specs", specArg, cpu, ram, out)
	}
	specID, _ := spec["id"].(string)
	if maxHosts := pgMaxHosts(spec); maxHosts > 0 && instances > maxHosts {
		return usageResult(fmt.Errorf("instances %d: specification %s (%v) takes 1-%d nodes", instances, specID, firstNonNil(spec["deployment_mode"], spec["display_name"]), maxHosts)), true
	}

	storageGB := pgInt(args, "storage_gb", 0)
	if storageGB == 0 {
		storageGB = int(toF(spec["min_storage_gb"]))
	}
	if storageGB == 0 {
		storageGB = 50
	}
	initialDB := stringArg(args, "initial_database")
	if initialDB == "" {
		initialDB = "defaultdb"
	}

	// Backup config. Backups CANNOT be enabled/disabled after creation (API rule),
	// and without a backup spec the cluster reports "backup disabled" (manual
	// pg_backup_create + pg_backups both 400). So enable automatic backups BY
	// DEFAULT with the documented defaults (weekly Sun 03:00, 14-day retention);
	// allow overrides, or no_backup:true to opt out.
	backupEnabled := true
	if nb, _ := args["no_backup"].(bool); nb {
		backupEnabled = false
	}
	var backupSpec map[string]any
	if backupEnabled {
		schedule := stringArg(args, "backup_schedule")
		if schedule == "" {
			schedule = "0 3 * * 0"
		}
		retention := pgInt(args, "backup_retention_days", 14)
		backupSpec = map[string]any{"schedule": schedule, "retention_policy_days": retention}
	}

	// The subnet defines the zone: explicit subnet_id, else the configured
	// region's, else a subnet of an open zone (ru.AZ-2 first) — as Kafka
	// and Redis. It was the project's first subnet of any zone, a closed
	// one included (final audit, N-06).
	subnetID, _, zone, err := dbaasSubnet(ctx, eng, stringArg(args, "subnet_id"))
	if err != nil {
		return errResult(err), true
	}

	willCreate := map[string]any{
		"name":             name,
		"version":          version,
		"specification_id": specID,
		"cpu":              spec["cpu"],
		"memory_gb":        spec["memory"],
		"deployment_mode":  spec["deployment_mode"],
		"storage_gb":       storageGB,
		"instances":        instances,
		"subnet_id":        subnetID,
		"zone":             zone,
		"initial_database": initialDB,
		"backup_enabled":   backupEnabled,
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		preview, err := s.completePreview(ctx, eng, pgCreatePreview(eng.Config, willCreate))
		if err != nil {
			return errResult(err), true
		}
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("pg_create provisions a BILLABLE Managed PostgreSQL cluster (~minutes).",
				"pg_create создаёт ПЛАТНЫЙ кластер Managed PostgreSQL (несколько минут)."),
			"will_create": willCreate,
			"preview":     preview,
		}), true
	}

	body := map[string]any{
		"name":             name,
		"project_id":       cli.ProjectID(),
		"version":          version,
		"specification_id": specID,
		"storage":          map[string]any{"pg_data_gb": storageGB},
		"initial_database": initialDB,
		"instances":        instances,
		"subnet_ids":       []string{subnetID},
	}
	if backupSpec != nil {
		body["backup"] = backupSpec
	}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateCluster(ctx, body)
	if err != nil || op.ID == "" {
		if err == nil {
			err = noID("operation")
		}
		return lostCreate(ctx, "pg_create", cli.ListClusters, i18n.T("PostgreSQL cluster", "кластер PostgreSQL"), name, willCreate, "pg_get id", fmt.Errorf("create cluster: %w", err))
	}
	out := map[string]any{
		"status":       "creating",
		"operation_id": op.ID,
		"created":      willCreate,
		"note":         "Cluster creation is async (~minutes). Poll pg_operation(operation_id) until done=true; then pg_list/pg_get for status READY.",
	}
	if op.ResourceID != "" {
		out["cluster_id"] = op.ResourceID // release audit re-run, N-07
	} else {
		out["note"] = "Cluster creation is async (~minutes). Poll pg_operation(operation_id) until done=true; pg_list shows the cluster's id by its name, then pg_get for status READY."
	}
	return toJSON(out), false
}

func (s *Registry) handlePgDelete(ctx context.Context, args map[string]any) (string, bool) {
	// Both and different deleted the cluster of id (QA-1).
	id, err := aliasArg(args, "id", "cluster_id")
	if err != nil {
		return errResult(err), true
	}
	if id == "" {
		return usageT("'id' is required (from pg_list)", "нужен 'id' (из pg_list)"), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return s.deletePreview("pg_delete", "pg_delete PERMANENTLY deletes PostgreSQL cluster %s, its databases and all its backups.",
			"pg_delete БЕЗВОЗВРАТНО удаляет кластер PostgreSQL %s, его базы данных и все резервные копии.",
			id, func() (map[string]any, error) { return cli.GetCluster(ctx, id) })
	}
	op, err := cli.DeleteCluster(ctx, id)
	if err != nil {
		msg := fmt.Sprintf("delete cluster: %v", err)
		switch {
		case strings.Contains(msg, "force flag"):
			// A FAILED cluster: the public API takes no force (`?force=true`
			// is 415 at the gateway, register CRD-19; release audit re-run,
			// N-13).
			return errResult(clierrors.New(clierrors.CodeAPIError, msg,
				i18n.T("Cloud.ru deletes a FAILED cluster only with force, which its public API does not accept: delete it in the Cloud.ru console or ask support.", "Кластер в FAILED Cloud.ru удаляет только с force, а публичный API его не принимает: удалите кластер в консоли Cloud.ru или обратитесь в поддержку."))), true
		case strings.Contains(msg, "FailedPrecondition"):
			return errResult(clierrors.New(clierrors.CodeAPIError, msg,
				i18n.T("The cluster is still being created (DEPLOYING): Cloud.ru deletes it through the API once it is READY. Check it with pg_get, then call pg_delete again; a cluster that ends FAILED is deleted only in the Cloud.ru console.", "Кластер ещё создаётся (DEPLOYING): через API Cloud.ru удалит его, когда он станет READY. Проверьте его через pg_get и повторите pg_delete; кластер, оказавшийся в FAILED, удаляется только в консоли Cloud.ru."))), true
		}
		return s.deleteFailed("pg_delete", id, fmt.Errorf("delete cluster: %w", err))
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID, "note": "Async — poll pg_operation(operation_id) until done=true."}), false
}

// --- Databases ---

func (s *Registry) handlePgDatabases(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	if cid == "" {
		return usageT("'cluster_id' is required", "нужен 'cluster_id'"), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListDatabases(ctx, cid)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"databases": items}), false
}

func (s *Registry) handlePgDbCreate(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	owner := stringArg(args, "owner")
	if cid == "" || name == "" || owner == "" {
		return usageT("'cluster_id', 'name' and 'owner' are required", "нужны 'cluster_id', 'name' и 'owner'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("pg_db_create creates database %q (owner %q) in cluster %s.",
			"pg_db_create создаёт базу данных %q (владелец %q) в кластере %s.", name, owner, cid)}), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{"cluster_id": cid, "name": name, "owner": owner}
	if v := stringArg(args, "lc_collate"); v != "" {
		body["lc_collate"] = v
	}
	if v := stringArg(args, "lc_ctype"); v != "" {
		body["lc_ctype"] = v
	}
	db, err := cli.CreateDatabase(ctx, cid, body)
	if err != nil {
		return errResult(fmt.Errorf("create database: %w", err)), true
	}
	return toJSON(map[string]any{"status": "created", "database": db}), false
}

func (s *Registry) handlePgDbDelete(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	if cid == "" || name == "" {
		return usageT("'cluster_id' and 'name' are required", "нужны 'cluster_id' и 'name'"), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	// The cluster first: one that is not there is the call's error
	// (NOT_FOUND), not a repeat; the database that is not there is (D26).
	if _, err := cli.GetCluster(ctx, cid); err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		items, err := cli.ListDatabases(ctx, cid)
		if err != nil {
			return errResult(err), true
		}
		if !slices.ContainsFunc(items, func(m map[string]any) bool { return m["name"] == name }) {
			return s.deleteFailed("pg_db_delete", name, clierrors.New(clierrors.CodeNotFound,
				i18n.Tf("no database %q in cluster %s", "базы данных %q в кластере %s нет", name, cid), ""))
		}
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("pg_db_delete PERMANENTLY drops database %q in cluster %s.",
			"pg_db_delete БЕЗВОЗВРАТНО удаляет базу данных %q в кластере %s.", name, cid)}), true
	}
	if err := cli.DeleteDatabase(ctx, cid, name); err != nil {
		return s.deleteFailed("pg_db_delete", name, fmt.Errorf("delete database: %w", err))
	}
	return toJSON(map[string]any{"status": "deleted", "database": name}), false
}

// --- Users ---

func (s *Registry) handlePgUsers(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	if cid == "" {
		return usageT("'cluster_id' is required", "нужен 'cluster_id'"), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListUsers(ctx, cid)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"users": items}), false
}

func (s *Registry) handlePgUserCreate(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	password := stringArg(args, "password")
	if cid == "" || name == "" || password == "" {
		return usageT("'cluster_id', 'name' and 'password' are required", "нужны 'cluster_id', 'name' и 'password'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("pg_user_create creates user %q in cluster %s.",
			"pg_user_create создаёт пользователя %q в кластере %s.", name, cid)}), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{"cluster_id": cid, "name": name, "password": password}
	u, err := cli.CreateUser(ctx, cid, body)
	if err != nil {
		return errResult(fmt.Errorf("create user: %w", err)), true
	}
	// Never echo the password back.
	return toJSON(map[string]any{"status": "created", "user": u}), false
}

func (s *Registry) handlePgUserDelete(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	if cid == "" || name == "" {
		return usageT("'cluster_id' and 'name' are required", "нужны 'cluster_id' и 'name'"), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	// The cluster first: one that is not there is the call's error
	// (NOT_FOUND), not a repeat; the user that is not there is (D26).
	if _, err := cli.GetCluster(ctx, cid); err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		items, err := cli.ListUsers(ctx, cid)
		if err != nil {
			return errResult(err), true
		}
		if !slices.ContainsFunc(items, func(m map[string]any) bool { return m["name"] == name }) {
			return s.deleteFailed("pg_user_delete", name, clierrors.New(clierrors.CodeNotFound,
				i18n.Tf("no user %q in cluster %s", "пользователя %q в кластере %s нет", name, cid), ""))
		}
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("pg_user_delete PERMANENTLY deletes user %q in cluster %s.",
			"pg_user_delete БЕЗВОЗВРАТНО удаляет пользователя %q в кластере %s.", name, cid)}), true
	}
	if err := cli.DeleteUser(ctx, cid, name); err != nil {
		return s.deleteFailed("pg_user_delete", name, fmt.Errorf("delete user: %w", err))
	}
	return toJSON(map[string]any{"status": "deleted", "user": name}), false
}

// --- Backups ---

func (s *Registry) handlePgBackups(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	if cid == "" {
		return usageT("'cluster_id' is required", "нужен 'cluster_id'"), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListBackups(ctx, cid)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"backups": items}), false
}

func (s *Registry) handlePgBackupCreate(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	if cid == "" || name == "" {
		return usageT("'cluster_id' and 'name' are required", "нужны 'cluster_id' и 'name'"), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	// A backup of the name in the cluster: a repeat after a lost answer
	// made a second one (fault matrix, process audit 1.2.1).
	list := func(ctx context.Context) ([]map[string]any, error) { return cli.ListBackups(ctx, cid) }
	existing, err := list(ctx)
	if err != nil {
		return errResult(fmt.Errorf(i18n.T("check the name is free: list backups: %w", "проверка, что имя свободно: список резервных копий: %w"), err)), true
	}
	if id, st, ok := takenName(existing, name); ok {
		return nameTakenResult(i18n.T("backup", "резервная копия"), name, id, st, "pg_backups cluster_id="+cid)
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("pg_backup_create starts a manual backup %q of cluster %s.",
			"pg_backup_create запускает ручное резервное копирование %q кластера %s.", name, cid),
			"preview": willPreview(engine.ResourcePGBackup, name, "", map[string]any{"cluster_id": cid}, []string{"cluster_id"})}), true
	}
	op, err := cli.CreateBackup(ctx, cid, name)
	if err != nil || op.ID == "" {
		if err == nil {
			err = noID("operation")
		}
		return lostCreate(ctx, "pg_backup_create", list, i18n.T("backup", "резервная копия"), name, nil, "pg_backups cluster_id="+cid, fmt.Errorf("create backup: %w", err))
	}
	out := map[string]any{"status": "backing_up", "operation_id": op.ID, "note": "Async — poll pg_operation(operation_id)."}
	if op.ResourceID != "" {
		out["backup_id"] = op.ResourceID
	}
	return toJSON(out), false
}

// pgCreatePreview is the preview of pg_create: the cluster as resolved.
// evo has no rate card for Managed PostgreSQL: the preview links its tariff (Price).
func pgCreatePreview(cfg config.Config, will map[string]any) engine.CreatePreview {
	name, _ := will["name"].(string)
	p := engine.CreatePreview{Resource: engine.ResourcePGCluster, Name: name,
		ProjectID: cfg.ProjectID, ProjectFrom: cfg.ProjectOrigin}
	for _, k := range []string{"version", "cpu", "memory_gb", "storage_gb", "instances", "deployment_mode",
		"subnet_id", "initial_database", "backup_enabled"} {
		p.Add(k, will[k])
	}
	p.Price()
	return p
}
