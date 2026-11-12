// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
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
				return backupDeleteRefused(ctx, tool, id, args, err, s, read)
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
	// both runs of Г-16 saw, the specification does not name), read in the
	// project of the backup — not the call's, which a key of the
	// organization may name otherwise («Риск», 164, У3). A backup that
	// does not name its project: the preview says they were not checked.
	project, _ := card["project_id"].(string)
	if project == "" {
		will["dependent_backups_checked"] = false
		warnings = append(warnings, i18n.T("the backup does not name its project, so evo did not check which backups name it as their full one",
			"копия не называет свой проект — какие копии называют её своей полной, evo не проверил"))
	} else if all, err := api.ListBackups(ctx, project); err == nil {
		var deps []map[string]any
		for _, b := range all {
			if b["parent_full_backup_id"] == id {
				deps = append(deps, map[string]any{"id": b["id"], "name": b["name"], "type": b["type"]})
			}
		}
		if len(deps) > 0 {
			will["dependent_backups"] = deps
			warnings = append(warnings, i18n.Tf("backups that name this one as their full backup: %d; whether they stay and can still be restored from once this one is deleted, evo has not checked",
				"копий, которые называют эту своей полной: %d; останутся ли они и можно ли будет восстановить из них после удаления этой, evo не проверял", len(deps)))
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
// («Риск», conclusion 164, answer 2). Not ready (backup_not_ready): the
// runs saw it on a repeat of a delete only, and a backup still being
// created may be refused with it too — the backup is read: deleting is a
// delete under way (D61), gone is already_deleted, any other state an
// error that names it and the way on («Риск», 164, У1). Any other refusal
// is the delete's own (deleteFailed).
func backupDeleteRefused(ctx context.Context, tool, id string, args map[string]any, err error, s *Registry,
	read func(context.Context) (map[string]any, error)) (string, bool) {
	switch platformCode(err) {
	case "backup_restore_in_progress":
		ans := map[string]any{"backup_id": id, "details": map[string]any{"cause": "restore_in_progress", "platform_code": "backup_restore_in_progress"},
			"next": i18n.T("A restore from the backup is under way: the platform deletes it once the restore ends. Call backup_delete again in a few minutes.",
				"Из копии идёт восстановление: платформа удалит её, когда оно закончится. Повторите backup_delete через несколько минут.")}
		nextSameCall(ans, tool, args)
		return errAnswer(clierrors.Wrap(clierrors.CodeAPIError,
			i18n.Tf("backup %s is not deleted: a restore from it is under way", "копия %s не удалена: из неё идёт восстановление", id), "", err), ans), true
	case "backup_not_ready":
		card, rerr := read(ctx)
		if rerr != nil {
			if isGone(rerr) {
				return s.deleteFailed(tool, id, rerr)
			}
			return errAnswer(clierrors.Wrap(clierrors.CodeAPIError,
				i18n.Tf("backup %s is not deleted: the platform answered it is not ready, and its state could not be read", "копия %s не удалена: платформа ответила, что она не готова, а её состояние прочитать не удалось", id),
				i18n.T("backup_get shows its state.", "Состояние показывает evo backup get."), err), backupGetNext(id)), true
		}
		state, _ := card["state"].(string)
		if state == "deleting" {
			return toJSON(map[string]any{"status": "deleting", "backup_id": id, "already_deleting": true,
				"message": i18n.Tf("backup %s is already being deleted; nothing more was done", "копия %s уже удаляется; больше ничего не сделано", id)}), false
		}
		// What the platform refuses here and for how long no run has seen
		// (creatingUnseen): the answer says what is known — the refusal
		// and the state read after it — and how to check, not why.
		ans := backupGetNext(id)
		ans["details"] = map[string]any{"state": state, "platform_code": "backup_not_ready"}
		return errAnswer(clierrors.Wrap(clierrors.CodeAPIError,
			i18n.Tf("backup %s is not deleted: the platform refused the delete as not ready (backup_not_ready), and the backup reads %s", "копия %s не удалена: платформа отвергла удаление как «не готова» (backup_not_ready), копия числится %s", id, quoteCloud(state)),
			i18n.T("The backup stays and is billed. Why the platform refused it is unknown: backup_get shows its state; call backup_delete again once the state changes.",
				"Копия остаётся и тарифицируется. Почему платформа отказала, неизвестно: состояние показывает evo backup get; повторите удаление, когда оно изменится."), err), ans), true
	}
	return s.deleteFailed(tool, id, err)
}

// backupGetNext is the next step of a delete the platform refused for the
// state of the backup: backup_get shows it.
func backupGetNext(id string) map[string]any {
	return map[string]any{"backup_id": id, "next_tool": "backup_get", "next_args": map[string]any{"backup_id": id}}
}

// backupWaitLimit is how long backup_create waits for its backup in the CLI:
// a VM's took about ten minutes in both runs of Г-16, a disk's about one
// (docs/internal/briefs/backups.md); over MCP the call's budget ends it
// first (D69) and the answer names backup_get as the next step.
var backupWaitLimit = 20 * time.Minute

// backupSourceOf is the source of backup_create as it is before the
// question: its kind as the API names it, id, name, zone, its size in GB (of
// a VM, the sum of its disks) and how many disks. A source that is not
// there is NOT_FOUND, its read's error.
type backupSourceInfo struct {
	kind, id, name, zone, state string
	sizeGB, disks               int
}

func backupSourceOf(ctx context.Context, eng *engine.Engine, args map[string]any) (*backupSourceInfo, error) {
	if id := stringArg(args, "disk_id"); id != "" {
		d, err := eng.Cloud.GetDisk(ctx, id)
		if err != nil {
			return nil, err
		}
		src := &backupSourceInfo{kind: "disk", id: id, disks: 1}
		src.name, _ = d["name"].(string)
		src.state, _ = d["state"].(string)
		// The backup goes to the call's project: a disk of another one, by
		// the project its card names, is refused, so the project of the
		// write is the source's (the audit of #1544, С2).
		if pid, _ := d["project_id"].(string); pid != "" && pid != eng.Config.ProjectID {
			return nil, backupSourceElsewhere("disk", "диск", id, eng.Config.ProjectID)
		}
		src.sizeGB = int(numberOf(d["size"]))
		if z, ok := nameOf(d["availability_zone"]).(string); ok {
			src.zone = z
		}
		return src, nil
	}
	id := stringArg(args, "vm_id")
	vm, err := eng.Cloud.GetVMDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	src := &backupSourceInfo{kind: "virtual_machine", id: id, name: vm.Name, state: vm.State}
	vms, err := eng.Cloud.ListVMsInProject(ctx, eng.Config.ProjectID)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(vms, func(v *provision.VM) bool { return v.ID == id }) {
		return nil, backupSourceElsewhere("VM", "ВМ", id, eng.Config.ProjectID)
	}
	disks, err := eng.Cloud.ListDisks(ctx, eng.Config.ProjectID)
	if err != nil {
		return nil, err
	}
	for _, d := range disks {
		vms, _ := d["vms"].([]any)
		for _, x := range vms {
			if v, ok := x.(map[string]any); ok && v["id"] == id {
				src.disks++
				src.sizeGB += int(numberOf(d["size"]))
				if z, ok := nameOf(d["availability_zone"]).(string); ok && src.zone == "" {
					src.zone = z
				}
			}
		}
	}
	return src, nil
}

// backupSourceElsewhere refuses a source that is not in the call's project: the
// backup would go to another project than its source's.
func backupSourceElsewhere(what, whatRU, id, project string) error {
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("%s %s is not in project %s: a backup goes to the project of the call, and of its source", "%s %s не в проекте %s: копия создаётся в проекте вызова, и он должен быть проектом источника", i18n.T(what, whatRU), id, project),
		i18n.T("Pass project_id of the source's project.", "Передайте project_id проекта источника."))
}

// numberOf is a JSON number as a float, 0 for anything else.
func numberOf(v any) float64 {
	f, _ := v.(float64)
	return f
}

// planBackupCreate plans the backup of a VM or a disk (wave В5, D106). Before
// the question: the source is read (not there: NOT_FOUND), and a backup of
// it with the same name is the answer of a repeat — already_exists with its
// age, and the words that no new backup was made: a backup named
// «before-change» three months old is not a backup of the resource as it
// is now («Риск», conclusion 164, У1). The preview names how many backups of
// the source are kept already and their size (У2): the platform keeps them
// with no term, and each is billed.
func (s *Registry) planBackupCreate(ctx context.Context, args map[string]any) (*Plan, error) {
	name := stringArg(args, "name")
	if name == "" {
		return answered(usageT("'name' is required", "нужен 'name'"), true), nil
	}
	if err := provision.ValidName(i18n.T("backup", "резервной копии"), name); err != nil {
		return nil, err
	}
	// Exactly one source, before any request («Риск», 164, probe П2): with
	// both the call backed up the disk silently, with none it read a VM of
	// an empty id.
	switch vm, disk := stringArg(args, "vm_id"), stringArg(args, "disk_id"); {
	case vm != "" && disk != "":
		return answered(usageT("pass vm_id or disk_id, not both: one backup is of one resource", "передайте vm_id или disk_id, не оба: копия — одного ресурса"), true), nil
	case vm == "" && disk == "":
		return answered(usageT("vm_id or disk_id is required: the resource to back up", "нужен vm_id или disk_id: ресурс, копию которого сделать"), true), nil
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	api, err := backupAPI(eng)
	if err != nil {
		return nil, err
	}
	src, err := backupSourceOf(ctx, eng, args)
	if err != nil {
		return nil, err
	}
	// The backups of the source, read once: the repeat and the preview.
	ofSource := func(ctx context.Context) ([]map[string]any, error) {
		all, err := api.ListBackups(ctx, eng.Config.ProjectID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(all))
		for _, b := range all {
			if row := backupRow(b); row["resource_id"] == src.id {
				out = append(out, map[string]any{"id": row["id"], "name": row["name"], "status": row["state"],
					"created_at": row["created_at"], "size_mb": row["size_mb"]})
			}
		}
		return out, nil
	}
	kept, err := ofSource(ctx)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("list the backups of %s: %w", "список копий %s: %w"), src.id, err)
	}
	sizeMB := 0.0
	for _, b := range kept {
		if b["name"] == name {
			return answered(backupOfTheName(b)), nil
		}
		sizeMB += numberOf(b["size_mb"])
	}
	req := provision.CreateBackupReq{Name: name, Description: stringArg(args, "description"), ProjectID: eng.Config.ProjectID,
		Resource: provision.BackupResource{ResourceID: src.id, ResourceType: src.kind}}
	will := map[string]any{
		"name": name, "resource_type": src.kind, "resource_state": src.state,
		"type": i18n.T("chosen by the platform: full or incremental", "выбирает платформа: полная или инкрементальная"), "resource_id": src.id, "resource_name": src.name,
		"resource_size_gb": src.sizeGB, "resource_disks": src.disks,
		"backups_of_resource": len(kept), "backups_of_resource_size_mb": sizeMB,
		"kept":     i18n.T("until it is deleted: the platform keeps backups with no term", "пока её не удалят: платформа хранит копии бессрочно"),
		"outlives": i18n.T("the source: a backup stays, and is billed, after its source is deleted", "исходный ресурс: копия остаётся и тарифицируется после его удаления"),
	}
	if d := req.Description; d != "" {
		will["description"] = d
	}
	will["wait"] = i18n.T("a disk's backup takes about a minute, a VM's about ten", "копия диска делается около минуты, ВМ — около десяти")
	preview := map[string]any{
		"message": i18n.Tf("backup_create makes a billable backup of %s %s (%s).", "backup_create делает платную резервную копию %s %s (%s).",
			backupKindWord(src.kind), quoteCloud(src.name), src.id),
		"preview":     willPreview(engine.ResourceBackup, name, src.zone, will, []string{"resource_type", "resource_state", "type", "resource_id", "resource_name", "resource_size_gb", "resource_disks", "backups_of_resource", "backups_of_resource_size_mb", "kept", "outlives", "description", "wait"}),
		"will_create": will,
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		b, err := api.CreateBackup(ctx, req)
		id, _ := b["id"].(string)
		if err != nil || id == "" {
			if err == nil {
				err = noID("backup")
			}
			text, isErr := lostCreate(ctx, "backup_create", ofSource, i18n.T("backup", "резервная копия"), name,
				map[string]any{"name": name, "resource_id": src.id}, "backup_get backup_id",
				fmt.Errorf(i18n.T("create backup: %w", "создание копии: %w"), err))
			return backupFoundNext(text), isErr
		}
		// The id before the wait: Ctrl-C in the tenth minute leaves no billed
		// backup without its id on the screen (conclusion 164, У6).
		announce(ctx, i18n.Tf("backup %s (%s) is being made", "копия %s (%s) делается", name, id))
		return s.backupCreated(ctx, eng, api, args, id, b)
	}}, nil
}

// backupKindWord names the kind of a source for a person.
func backupKindWord(kind string) string {
	if kind == "disk" {
		return i18n.T("disk", "диска")
	}
	return i18n.T("VM", "ВМ")
}

// backupOfTheName is the answer when a backup of the source has the name
// already, by its state («Риск», 164, probe П1): only a backup made is
// «exists»; one being made is the one to wait for; one in error holds
// nothing to restore from; one being deleted is not there to rely on — the
// call waits for it to go, or takes another name. None is a success that
// would let an agent change the resource «under a backup» it has not got.
func backupOfTheName(b map[string]any) (string, bool) {
	id, _ := b["id"].(string)
	st, _ := b["status"].(string)
	switch st {
	case "created":
		return backupExists(b), false
	case "creating", "updating":
		ans := map[string]any{"status": "creating", "backup_id": id, "name": b["name"], "state": st, "created_at": b["created_at"],
			"note": i18n.Tf("A backup of the source with this name, asked for at %v, is still being made: no second one was asked for. backup_get shows when it is created.",
				"Копия ресурса с этим именем, запрошенная %v, ещё делается: вторая не запрошена. backup_get покажет, когда она будет готова.", b["created_at"])}
		nextStep(ans, "backup_get", map[string]any{"backup_id": id})
		return toJSON(ans), false
	case "deleting":
		return errAnswer(clierrors.New(clierrors.CodeUsage,
			i18n.Tf("a backup of the source named %s is being deleted (%s): no new backup was made", "копия ресурса с именем %s удаляется (%s): новая копия не сделана", quoteCloud(fmt.Sprint(b["name"])), id),
			i18n.T("Wait until backup_get answers it is gone, or take another name.", "Дождитесь, пока backup_get ответит, что её нет, или возьмите другое имя.")),
			map[string]any{"backup_id": id, "state": st}), true
	}
	return errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("a backup of the source named %s is in state %s: nothing can be restored from it, and no new backup was made", "копия ресурса с именем %s в состоянии %s: восстановить из неё нельзя, новая копия не сделана", quoteCloud(fmt.Sprint(b["name"])), quoteCloud(st)),
		i18n.T("Take another name. That backup is kept and billed: whether to delete it is the person's decision — ask the person; with their yes, backup_delete deletes it.",
			"Возьмите другое имя. Та копия хранится и тарифицируется: удалять ли её, решает человек — спросите человека; с его «да» её удалит evo backup delete.")),
		map[string]any{"backup_id": id, "state": st}), true
}

// backupExists is the answer of a repeat: the backup of the source by the
// name, its age, and that no new backup was made (conclusion 164, У1).
func backupExists(b map[string]any) string {
	ans := map[string]any{"status": "already_exists", "backup_id": b["id"], "name": b["name"], "state": b["status"], "created_at": b["created_at"]}
	if at, err := time.Parse(time.RFC3339, fmt.Sprint(b["created_at"])); err == nil {
		days := int(math.Floor(clock.Now().Sub(at).Hours() / 24))
		ans["age_days"] = days
		ans["note"] = i18n.Tf("No new backup was made: this one, of the same name, is %d days old; what changed on the source since is not in it. Another name makes a new one.",
			"Новая копия не сделана: эта, с тем же именем, сделана %d дн. назад; изменений источника после неё в ней нет. Новое имя — новая копия.", days)
	} else {
		ans["note"] = i18n.T("No new backup was made: one of the same name exists; what changed on the source since it is not in it. Another name makes a new one.",
			"Новая копия не сделана: копия с этим именем уже есть; изменений источника после неё в ней нет. Новое имя — новая копия.")
	}
	return toJSON(ans)
}

// backupCreated waits for the backup the call asked for (wait=false: not at
// all) and answers it: created, or still creating with backup_get as the
// next step, or the state of an error.
func (s *Registry) backupCreated(ctx context.Context, eng *engine.Engine, api provision.BackupAPI, args map[string]any, id string, b map[string]any) (string, bool) {
	created := map[string]any{"backup_id": id}
	ans := map[string]any{"backup_id": id, "created": created}
	state, _ := b["state"].(string)
	if boolArg(args, "wait", true) && state != "created" {
		read := func(ctx context.Context) (string, bool, error) {
			cur, err := api.GetBackup(ctx, id)
			if err != nil {
				// Only a backup that is gone ends the wait: a failed read
				// (a 5xx, the network) is read again (the audit, С5).
				if isGone(err) {
					return "", false, err
				}
				return "", false, nil
			}
			st, _ := cur["state"].(string)
			b = cur
			return st, true, nil
		}
		waiting := func(st string) string {
			return i18n.Tf("backup %s: %s", "копия %s: %s", id, quoteCloud(st))
		}
		done := func(st string) (bool, error) { return st != "creating" && st != "updating", nil }
		last, _, _, err := waitState(ctx, eng, backupWaitLimit, read, waiting, done)
		if err != nil {
			return errAnswer(err, ans), true
		}
		state = last
	}
	ans["backup"] = backupRow(b)
	switch state {
	case "created":
		ans["status"] = "created"
	case "error", "error_creating":
		ans["state"] = state
		return errAnswer(clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("Cloud.ru did not make backup %s: it is %s", "Cloud.ru не сделал копию %s: она в состоянии %s", id, quoteCloud(state)),
			i18n.T("The backup in error is kept and billed. Whether to delete it is the person's decision: ask the person; with their yes, backup_delete deletes it (whether the platform deletes a backup in error no run has shown yet: if it refuses, the Cloud.ru console or support). Then make the backup again.",
				"Копия в ошибке хранится и тарифицируется. Удалять ли её — решает человек: спросите человека; с его «да» её удалит evo backup delete (удаляет ли платформа копию в ошибке, ни один прогон ещё не показал: если откажет — консоль Cloud.ru или поддержка). Затем сделайте копию снова.")), ans), true
	default:
		ans["status"] = "creating"
		ans["state"] = state
		ans["next"] = i18n.T("The backup is still being made: backup_get shows when it is created.", "Копия ещё делается: backup_get покажет, когда она будет готова.")
		nextStep(ans, "backup_get", map[string]any{"backup_id": id})
	}
	return toJSON(ans), false
}

// announce says msg to the person while the call goes on, in every mode of
// the CLI: a line of progress, and in --json a {"progress": msg} line on
// stderr, which Step leaves out (the audit of #1544, Б3); over MCP, the
// line of progress.
func announce(ctx context.Context, msg string) {
	pp, ok := progressOf(ctx)
	if !ok {
		return
	}
	if pp.IsJSON() {
		_ = json.NewEncoder(pp.ErrWriter()).Encode(map[string]string{"progress": msg})
		return
	}
	pp.Step(0, 0, msg)
}

// backupFoundNext is the answer of a lost create whose backup was found
// (lostCreate): its read, backup_get, as the next step (the audit, С4).
func backupFoundNext(text string) string {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return text
	}
	id, _ := m["backup_id"].(string)
	if id == "" || m["status"] == nil || m["status"] == "error" {
		return text
	}
	nextStep(m, "backup_get", map[string]any{"backup_id": id})
	return toJSON(m)
}
