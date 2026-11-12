// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The backups of VMs and disks (wave В5, docs/internal/briefs/backups.md):
// a resource of its own, with its own id and list, of two kinds of source.
// The facts the brief stands on are of the published compute specification;
// what one run only showed is not in a text of these tools.

// backupStates are the states of a backup (BackupState of the
// specification; the list names them CheckpointState).
var backupStates = []string{"creating", "created", "error_creating", "updating", "error", "deleting", "error_deleting"}

// backupAPI is the client of the backups of eng (provision.BackupAPI): the
// compute client of the engine, which a fake of CloudAPI may not be.
func backupAPI(eng *engine.Engine) (provision.BackupAPI, error) {
	if b, ok := eng.Cloud.(provision.BackupAPI); ok {
		return b, nil
	}
	return nil, clierrors.New(clierrors.CodeConfigInvalid,
		i18n.T("this compute client does not read backups", "этот клиент Compute не читает резервные копии"), "")
}

// backupRow is the row of a backup in backup_list: what the item of the list
// carries (BackupListItemResponse), its size in MB as the platform gives it.
func backupRow(b map[string]any) map[string]any {
	row := map[string]any{
		"id": b["id"], "name": b["name"], "state": b["state"], "type": b["type"],
		"size_mb": b["size"], "created_at": b["created_at"], "last_restore_time": b["last_restore_time"],
	}
	if r, ok := b["resource"].(map[string]any); ok {
		row["resource_type"], row["resource_id"] = r["resource_type"], r["resource_id"]
		if st, ok := r["resource_state"].(map[string]any); ok {
			row["resource_name"] = st["name"]
		}
	}
	return row
}

// handleBackupList lists the backups of the project.
func (s *Registry) handleBackupList(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	api, err := backupAPI(eng)
	if err != nil {
		return errResult(err), true
	}
	items, err := api.ListBackups(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, b := range items {
		out = append(out, backupRow(b))
	}
	return toJSON(map[string]any{"backups": out}), false
}

// handleBackupGet answers the card of one backup: the row of the list, and
// what the card adds — its description, its plan, whether its source still
// exists, the source as the backup holds it.
func (s *Registry) handleBackupGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "backup_id")
	if id == "" {
		return usageT("'backup_id' is required (from backup_list)", "нужен 'backup_id' (из backup_list)"), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	api, err := backupAPI(eng)
	if err != nil {
		return errResult(err), true
	}
	b, err := api.GetBackup(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	card := backupRow(b)
	card["description"] = b["description"]
	if p, ok := b["protection_plan"].(map[string]any); ok {
		card["protection_plan"] = map[string]any{"id": p["id"], "name": p["name"]}
	}
	if names := backupTagNames(b["tags"]); len(names) > 0 {
		card["tags"] = names
	}
	if r, ok := b["resource"].(map[string]any); ok {
		if exists, ok := r["resource_exists"].(bool); ok {
			card["resource_exists"] = exists
			if !exists {
				card["note"] = i18n.T("The source of this backup no longer exists: the backup is kept, and billed, until it is deleted.",
					"Исходного ресурса этой копии больше нет: копия хранится и тарифицируется, пока её не удалят.")
			}
		}
		if st, ok := r["resource_state"].(map[string]any); ok {
			card["source"] = backupSource(r["resource_type"], st)
		}
	}
	return toJSON(card), false
}

// backupSource is the source of a backup as the backup holds it: of a disk,
// its size, type and zone; of a VM, its flavor, disks and interfaces.
func backupSource(kind any, st map[string]any) map[string]any {
	src := map[string]any{"name": st["name"], "zone": nameOf(st["availability_zone"])}
	switch kind {
	case "disk":
		src["size_gb"], src["disk_type"] = st["size"], nameOf(st["disk_type"])
	case "virtual_machine":
		src["flavor"] = nameOf(st["flavor"])
		if disks, ok := st["disks"].([]any); ok {
			out := make([]map[string]any, 0, len(disks))
			for _, x := range disks {
				if d, ok := x.(map[string]any); ok {
					out = append(out, map[string]any{"name": d["name"], "size_gb": d["size"], "disk_type": nameOf(d["disk_type"]), "bootable": d["bootable"]})
				}
			}
			src["disks"] = out
		}
		if ifs, ok := st["interfaces"].([]any); ok {
			out := make([]map[string]any, 0, len(ifs))
			for _, x := range ifs {
				if i, ok := x.(map[string]any); ok {
					out = append(out, map[string]any{"name": i["name"], "subnet": nameOf(i["subnet"]), "ip_address": i["ip_address"]})
				}
			}
			src["interfaces"] = out
		}
	}
	return src
}

// nameOf is the name of a nested object of the API ({id, name, …}), else
// the value itself.
func nameOf(v any) any {
	if m, ok := v.(map[string]any); ok {
		return m["name"]
	}
	return v
}

// backupTagNames are the names of the tags of a backup.
func backupTagNames(v any) []any {
	tags, _ := v.([]any)
	out := make([]any, 0, len(tags))
	for _, x := range tags {
		if t, ok := x.(map[string]any); ok && t["name"] != nil {
			out = append(out, t["name"])
		}
	}
	return out
}
