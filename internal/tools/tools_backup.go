// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
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

// restoreWindow is how long after a restore the preview of backup_delete
// warns that the platform may still refuse the delete: one run of Г-16 saw
// the refusal for up to about six minutes after the restore was asked for,
// while the backup read created (a hypothesis until the third run).
const restoreWindow = 10 * time.Minute

// planBackupDelete plans the delete of a backup (D106; D26, D61, D74, D103).
// A backup is data: what it held is lost with it. The preview names the
// backup, its source and whether the source still exists — a backup of a
// resource that is gone is the last copy of its data («Риск», conclusion
// 164, У3а) — the backups that name it as their full one, whose fate evo has
// not seen (У3б), and a restore of the last minutes, which the platform
// does not let a delete through while it runs. That refusal, seen after the
// consent, is an error with its cause and the same call again, not a
// failure of the delete.
func (s *Registry) planBackupDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	const tool = "backup_delete"
	id := stringArg(args, "backup_id")
	if id == "" {
		return nil, usageErr("backup_id is required", "нужен backup_id")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	api, err := backupAPI(eng)
	if err != nil {
		return nil, err
	}
	var card map[string]any
	read := func(ctx context.Context) (map[string]any, error) { return api.GetBackup(ctx, id) }
	p := s.deletePlan(tool,
		"backup_delete permanently deletes backup %s: what it holds cannot be restored after.",
		"backup_delete безвозвратно удаляет резервную копию %s: восстановить из неё после этого нельзя.",
		id, func() (m map[string]any, err error) {
			card, err = read(ctx)
			return card, err
		},
		func(ctx context.Context) (string, bool) {
			if err := api.DeleteBackup(ctx, id); err != nil {
				return backupDeleteRefused(tool, id, args, err, s)
			}
			return deleteOutcome(ctx, args, goneWatch(tool, id, map[string]any{"backup_id": id}, "", "backup_get", read))
		})
	if p.Execute == nil {
		return p, nil
	}
	row := backupRow(card)
	will := map[string]any{}
	for _, k := range []string{"type", "size_mb", "created_at", "last_restore_time", "resource_type", "resource_id", "resource_name"} {
		if v := row[k]; v != nil {
			will[k] = v
		}
	}
	var warnings []string
	if w, _ := p.Preview["warning"].(string); w != "" {
		warnings = append(warnings, w)
	}
	if r, ok := card["resource"].(map[string]any); ok {
		if exists, ok := r["resource_exists"].(bool); ok {
			will["resource_exists"] = exists
			if !exists {
				warnings = append(warnings, i18n.T("this is a backup of a resource that no longer exists: once it is deleted, the data it holds cannot be restored from anywhere",
					"это копия ресурса, которого больше нет: после её удаления данные из неё не восстановить ниоткуда"))
			}
		}
	}
	will["restored_resources_stay"] = i18n.T("resources restored from this backup stay; its source is not touched", "ресурсы, восстановленные из этой копии, остаются; исходный ресурс не затрагивается")
	if at, err := time.Parse(time.RFC3339, fmt.Sprint(row["last_restore_time"])); err == nil && clock.Now().Sub(at) < restoreWindow {
		warnings = append(warnings, i18n.T("a restore from this backup was asked for in the last minutes: while it runs, the platform refuses the delete, though the backup reads created",
			"восстановление из этой копии запрошено в последние минуты: пока оно идёт, платформа отвергает удаление, хотя копия числится готовой"))
	}
	// The backups whose full one this is (parent_full_backup_id: a field
	// both runs of Г-16 saw, the specification does not name).
	if all, err := api.ListBackups(ctx, eng.Config.ProjectID); err == nil {
		var deps []map[string]any
		for _, b := range all {
			if b["parent_full_backup_id"] == id {
				deps = append(deps, map[string]any{"id": b["id"], "name": b["name"], "type": b["type"]})
			}
		}
		if len(deps) > 0 {
			will["dependent_backups"] = deps
			warnings = append(warnings, i18n.Tf("%d backups name this one as their full backup: what becomes of them evo has not checked",
				"%d копий называют эту своей полной: что станет с ними, evo не проверял", len(deps)))
		}
	} else {
		return nil, clierrors.Wrap(clierrors.CodeAPIError,
			i18n.Tf("backup %s: the backups of the project could not be read, so evo cannot tell which depend on it; nothing was sent", "копия %s: список копий проекта не прочитан — какие копии от неё зависят, evo сказать не может; ничего не отправлено", id),
			i18n.T("Repeat the call; backup_list shows the backups.", "Повторите вызов; копии показывает evo backup list."), err)
	}
	p.Preview["will_delete"] = will
	if len(warnings) > 0 {
		p.Preview["warning"] = strings.Join(warnings, "; ")
	}
	return p, nil
}

// backupDeleteRefused is the answer of a DELETE the platform refused. A
// restore under way (backup_restore_in_progress, one run of Г-16): the
// backup is not deleted, the same call again — without the consent, which
// does not carry over (R-85) — once the restore ends, no new reason
// («Риск», conclusion 164, answer 2). A delete already under way
// (backup_not_ready right after one): deleting (D61). Any other refusal is
// the delete's own (deleteFailed).
func backupDeleteRefused(tool, id string, args map[string]any, err error, s *Registry) (string, bool) {
	switch platformCode(err) {
	case "backup_restore_in_progress":
		ans := map[string]any{"backup_id": id, "details": map[string]any{"cause": "restore_in_progress", "platform_code": "backup_restore_in_progress"},
			"next": i18n.T("A restore from the backup is under way: the platform deletes it once the restore ends. Call backup_delete again in a few minutes.",
				"Из копии идёт восстановление: платформа удалит её, когда оно закончится. Повторите backup_delete через несколько минут.")}
		nextSameCall(ans, tool, args)
		return errAnswer(clierrors.Wrap(clierrors.CodeAPIError,
			i18n.Tf("backup %s is not deleted: a restore from it is under way", "копия %s не удалена: из неё идёт восстановление", id), "", err), ans), true
	case "backup_not_ready":
		return toJSON(map[string]any{"status": "deleting", "backup_id": id, "already_deleting": true,
			"message": i18n.Tf("backup %s is already being deleted; nothing more was done", "копия %s уже удаляется; больше ничего не сделано", id)}), false
	}
	return s.deleteFailed(tool, id, err)
}
