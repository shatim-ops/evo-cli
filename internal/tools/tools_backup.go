// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"math"
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
	kind, id, name, zone string
	sizeGB, disks        int
}

func backupSourceOf(ctx context.Context, eng *engine.Engine, args map[string]any) (*backupSourceInfo, error) {
	if id := stringArg(args, "disk_id"); id != "" {
		d, err := eng.Cloud.GetDisk(ctx, id)
		if err != nil {
			return nil, err
		}
		src := &backupSourceInfo{kind: "disk", id: id, disks: 1}
		src.name, _ = d["name"].(string)
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
	src := &backupSourceInfo{kind: "virtual_machine", id: id, name: vm.Name}
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
			return answered(backupExists(b), false), nil
		}
		sizeMB += numberOf(b["size_mb"])
	}
	req := provision.CreateBackupReq{Name: name, Description: stringArg(args, "description"), ProjectID: eng.Config.ProjectID,
		Resource: provision.BackupResource{ResourceID: src.id, ResourceType: src.kind}}
	will := map[string]any{
		"name": name, "source_type": src.kind, "source_id": src.id, "source_name": src.name,
		"source_size_gb": src.sizeGB, "source_disks": src.disks,
		"backups_of_source": len(kept), "backups_of_source_size_mb": sizeMB,
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
		"preview":     willPreview(engine.ResourceBackup, name, src.zone, will, []string{"source_type", "source_id", "source_name", "source_size_gb", "source_disks", "backups_of_source", "backups_of_source_size_mb", "kept", "outlives", "description", "wait"}),
		"will_create": will,
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		b, err := api.CreateBackup(ctx, req)
		id, _ := b["id"].(string)
		if err != nil || id == "" {
			if err == nil {
				err = noID("backup")
			}
			return lostCreate(ctx, "backup_create", ofSource, i18n.T("backup", "резервная копия"), name, nil, "backup_get backup_id",
				fmt.Errorf(i18n.T("create backup: %w", "создание копии: %w"), err))
		}
		// The id before the wait: Ctrl-C in the tenth minute leaves no billed
		// backup without its id on the screen (conclusion 164, У6).
		if pp, ok := progressOf(ctx); ok {
			pp.Step(0, 0, i18n.Tf("backup %s (%s) is being made", "копия %s (%s) делается", name, id))
		}
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
				return "", false, err
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
			i18n.T("The backup in error is kept and billed: delete it in the Cloud.ru console, then make the backup again.",
				"Копия в ошибке хранится и тарифицируется: удалите её в консоли Cloud.ru, затем сделайте копию снова.")), ans), true
	default:
		ans["status"] = "creating"
		ans["state"] = state
		ans["next"] = i18n.T("The backup is still being made: backup_get shows when it is created.", "Копия ещё делается: backup_get покажет, когда она будет готова.")
		nextStep(ans, "backup_get", map[string]any{"backup_id": id})
	}
	return toJSON(ans), false
}
