// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// looksUUID is a cheap check to tell a UUID from a human name.
func looksUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// resolveVM finds a VM by id or name, returning (id, zoneID).
func (s *Registry) resolveVM(ctx context.Context, ref, projectID string) (string, string, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", "", err
	}
	vms, err := eng.Cloud.ListVMsInProject(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	for _, v := range vms {
		if v.ID == ref || v.Name == ref {
			return v.ID, v.ZoneID(), nil
		}
	}
	return "", "", fmt.Errorf(i18n.T("VM %q not found", "ВМ %q не найдена"), ref)
}

// handleDiskList lists disks (block storage) with a compact projection.
func (s *Registry) handleDiskList(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	disks, err := eng.Cloud.ListDisks(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(disks))
	for _, d := range disks {
		row := map[string]any{
			"id": d["id"], "name": d["name"], "size_gb": d["size"],
			"state": d["state"], "bootable": d["bootable"],
		}
		if dt, ok := d["disk_type"].(map[string]any); ok {
			row["disk_type"] = dt["name"]
		}
		if az, ok := d["availability_zone"].(map[string]any); ok {
			row["zone"] = az["name"]
		}
		// attached VMs
		if vms, ok := d["vms"].([]any); ok && len(vms) > 0 {
			names := make([]any, 0, len(vms))
			for _, x := range vms {
				if vm, ok := x.(map[string]any); ok {
					names = append(names, vm["name"])
				}
			}
			row["attached_to"] = names
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"disks": out}), false
}

// handleDiskCreate creates a disk. If attach_to (a VM) is given, the disk is
// created in that VM's zone and (with confirmed) attached to it — the ergonomic
// "add a disk to my VM" path. Mutation: requires confirmed=true.
func (s *Registry) handleDiskCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	sizeGB := intArg(args, "size_gb", 0)
	// One message per argument: "'name' and 'size_gb' (>0) are required"
	// named the name that was given (QA 20.10, QA-13).
	if name == "" {
		return usageT("'name' is required", "нужен 'name'"), true
	}
	if _, ok := args["size_gb"]; !ok {
		return usageT("'size_gb' is required", "нужен 'size_gb'"), true
	}
	if sizeGB <= 0 {
		return usageT("size_gb must be greater than 0, got %d", "size_gb должен быть больше 0, передано %d", sizeGB), true
	}
	if err := provision.ValidName(i18n.T("disk", "диска"), name); err != nil {
		return errResult(err), true
	}
	if sizeGB > provision.MaxDiskGB {
		return usageT("size_gb %d is over the disk maximum of %d GB", "size_gb %d больше предельного размера диска %d ГБ", sizeGB, provision.MaxDiskGB), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	pid := eng.Config.ProjectID

	// Resolve the zone: from the attach-target VM (zones must match), else an
	// explicit zone, else the configured region.
	attachTo := stringArg(args, "attach_to")
	var zoneID, vmID string
	if attachTo != "" {
		vmID, zoneID, err = s.resolveVM(ctx, attachTo, pid)
		if err != nil {
			return errResult(err), true
		}
	} else if z := stringArg(args, "zone"); z != "" {
		if looksUUID(z) {
			zoneID = z
		} else if zs, e := eng.Cloud.ListAvailabilityZones(ctx); e == nil {
			for _, az := range zs {
				if az.Name == z {
					zoneID = az.ID
					break
				}
			}
		}
		if zoneID == "" {
			return errResult(fmt.Errorf(i18n.T("zone %q not found", "зона %q не найдена"), z)), true
		}
	} else {
		// The configured zone, by its UUID or its name (resolveVMZone, as
		// a VM's): its name went out as availability_zone_id (P-Z1).
		if _, _, err := resolveVMZone(ctx, eng); err != nil {
			return errResult(err), true
		}
		zoneID = eng.Config.Region
	}

	// Resolve disk type (name or id); default to the SSD type, as a VM's.
	req := provision.CreateDiskReq{Name: name, Size: sizeGB, AvailabilityZoneID: zoneID, ProjectID: pid}
	if dt := stringArg(args, "disk_type"); dt != "" {
		if looksUUID(dt) {
			req.DiskTypeID = dt
		} else {
			req.DiskTypeName = dt
		}
	} else if dts, e := eng.Cloud.ListDiskTypes(ctx, pid); e == nil {
		if t := provision.PickDiskType(dts, ""); t != nil {
			req.DiskTypeName = t.Name
		}
	}

	// A name taken before the call: refused before the question, so a lost
	// answer's disk found by the name is the call's own (D49).
	list := func(ctx context.Context) ([]map[string]any, error) { return eng.Cloud.ListDisks(ctx, pid) }
	if ans, refused := nameFree(ctx, list, i18n.T("disk", "диск"), name, "disk_list"); refused {
		return ans, true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		preview, err := s.completePreview(ctx, eng, diskCreatePreview(eng, req, attachTo, stringArg(args, "zone")))
		if err != nil {
			return errResult(err), true
		}
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("disk_create makes a billable disk.",
				"disk_create создаёт платный диск."),
			"will_create": map[string]any{
				"name": name, "size_gb": sizeGB, "disk_type": firstNonEmptyStr(req.DiskTypeName, req.DiskTypeID),
				"zone_id": zoneID, "attach_to": attachTo,
			},
			"preview": preview,
		}), true
	}

	disk, err := eng.Cloud.CreateDisk(ctx, req)
	if id, _ := disk["id"].(string); err != nil || id == "" {
		if err == nil {
			err = noID("disk")
		}
		return lostCreate(ctx, "disk_create", list, i18n.T("disk", "диск"), name, nil, "disk_list", fmt.Errorf(i18n.T("create disk: %w", "создание диска: %w"), err))
	}
	res := map[string]any{"status": "created", "disk": disk}
	if attachTo == "" || vmID == "" {
		return toJSON(res), false
	}

	// Auto-attach. A disk just created is "creating": attached at once,
	// Cloud.ru answered 422 "Disk has no openstack_id" and the answer still
	// said created (release audit, L-24). It is attached once available; a
	// disk that does not get there or an attach that fails is a partial
	// answer with the disk's ID, an error, and disk_attach to finish.
	diskID, _ := disk["id"].(string)
	res["disk_id"] = diskID
	partial := func(err error) (string, bool) {
		res["status"] = "partial"
		return errAnswer(clierrors.Wrap(clierrors.CodeAPIError,
			fmt.Sprintf("disk %s %s was created (it is billed), but not attached to VM %s: %v", name, diskID, attachTo, err),
			fmt.Sprintf("Attach it with disk_attach disk_id=%s vm=%s once disk_list shows it available, or delete it with disk_delete.", diskID, attachTo), err), res), true
	}
	if diskID == "" {
		return partial(fmt.Errorf("the create response has no disk id"))
	}
	if err := waitDiskAvailable(ctx, eng, diskID, pid); err != nil {
		return partial(err)
	}
	at, err := eng.Cloud.AttachDisk(ctx, diskID, vmID, pid)
	if err != nil {
		return partial(fmt.Errorf(i18n.T("attach: %w", "привязка: %w"), err))
	}
	res["attach"] = at
	res["note"] = "Disk created and attached. On the VM: partition/format the new device (e.g. mkfs.ext4 /dev/vdX) and mount it — attaching does NOT grow the existing filesystem."
	return toJSON(res), false
}

// diskReadyTimeout bounds the wait for a new disk to become available
// before it is attached. A live disk took 149 s (Grok live run on 1.3.0,
// N3): 2 minutes failed the attach of a disk about to be ready.
const diskReadyTimeout = 5 * time.Minute

// waitDiskAvailable polls disk_list until the disk is "available", at the
// VM poll interval (Engine.Timings.VMPollInterval, 5 s by default), for at
// most diskReadyTimeout (or Timings.VMPollTimeout when shorter). A disk in
// an error state ends the wait at once.
func waitDiskAvailable(ctx context.Context, eng *engine.Engine, diskID, projectID string) error {
	last, timeout, err := waitDisk(ctx, eng, diskID, projectID, diskReadyTimeout,
		i18n.T("Disk %s: %s, waiting until it is available…", "Диск %s: %s, жду, пока он станет доступен…"),
		func(st string) (bool, error) {
			switch {
			case st == "available":
				return true, nil
			case strings.HasPrefix(st, "error"):
				return true, fmt.Errorf("the disk is in state %s", st)
			}
			return false, nil
		})
	if timeout > 0 {
		return fmt.Errorf("the disk did not become available within %s (state %s)", timeout, last)
	}
	return err
}

// waitDisk polls disk_list until done says the disk's state ends the wait,
// at the VM poll interval (Engine.Timings.VMPollInterval, 5 s by default),
// for at most limit (or Timings.VMPollTimeout when shorter); it returns
// the last state read and, when the wait ran out, how long it waited. The wait says the disk's state when it changes, in
// the CLI and over MCP alike (waiting: a format of the disk and its state),
// so a wait is not silent (CLAUDE.md §6, axis 2).
func waitDisk(ctx context.Context, eng *engine.Engine, diskID, projectID string, limit time.Duration,
	waiting string, done func(state string) (bool, error)) (last string, timedOut time.Duration, err error) {
	every := 5 * time.Second
	if eng.Timings.VMPollInterval > 0 {
		every = eng.Timings.VMPollInterval
	}
	timeout := limit
	if t := eng.Timings.VMPollTimeout; t > 0 && t < timeout {
		timeout = t
	}
	deadline := time.Now().Add(timeout)
	last = "unknown"
	pp, _ := progressOf(ctx)
	for {
		if disks, err := eng.Cloud.ListDisks(ctx, projectID); err == nil {
			for _, d := range disks {
				if id, _ := d["id"].(string); id != diskID {
					continue
				}
				st, _ := d["state"].(string)
				ok, err := done(st)
				if st != last && !ok {
					pp.Step(0, 0, fmt.Sprintf(waiting, diskID, st))
				}
				last = st
				if ok {
					return st, 0, err
				}
			}
		}
		if !time.Now().Before(deadline) {
			return last, timeout, nil
		}
		select {
		case <-ctx.Done():
			return last, 0, ctx.Err()
		case <-time.After(every):
		}
	}
}

// diskTransient are the states of a disk the platform changes on its own
// and refuses to attach, detach or resize it in (422
// disk_can_not_be_detached_from_current_state: cloud-e2e of the 1.5
// candidate, run 142, #521). Any other state — known or not — is left to
// the platform, as before D53.
var diskTransient = map[string]bool{"attaching": true, "detaching": true, "extending": true, "creating": true}

// diskSettleTimeout bounds the wait before the question for a disk in a
// transient state (D53, the owner's answer to #542: 30 s, under the call
// timeout of MCP clients).
const diskSettleTimeout = 30 * time.Second

// settledDisk reads the disk before the question of disk_attach,
// disk_detach and disk_resize (D53): a disk in a transient state is waited
// for, saying its state, for at most diskSettleTimeout; one that does not
// settle is refused before the question with its state and what to do,
// not by the platform after consent. It returns the disk as last read.
func settledDisk(ctx context.Context, eng *engine.Engine, diskID string) (map[string]any, error) {
	d, err := eng.Cloud.GetDisk(ctx, diskID)
	if err != nil {
		return nil, err
	}
	if st, _ := d["state"].(string); !diskTransient[st] {
		return d, nil
	}
	last, timedOut, err := waitDisk(ctx, eng, diskID, eng.Config.ProjectID, diskSettleTimeout,
		i18n.T("Disk %s: %s, waiting until it settles…", "Диск %s: %s, жду, пока состояние устоится…"),
		func(st string) (bool, error) { return !diskTransient[st], nil })
	switch {
	case err != nil:
		return nil, err
	case timedOut > 0:
		return nil, clierrors.New(clierrors.CodeUsage,
			i18n.Tf("disk %s is in state %s: it cannot be changed until the state settles", "диск %s в состоянии %s: менять его можно, когда состояние устоится", diskID, last),
			i18n.T("Repeat the call when disk_list shows the disk available or in_use.", "Повторите вызов, когда disk_list покажет диск в available или in_use."))
	}
	return eng.Cloud.GetDisk(ctx, diskID)
}

// planDiskAttach plans the attach of an existing disk to a VM (gate.go):
// the VM and the disk's state are read before the question (D53).
func (s *Registry) planDiskAttach(ctx context.Context, args map[string]any) (*Plan, error) {
	diskID := stringArg(args, "disk_id")
	vmRef := stringArg(args, "vm")
	if diskID == "" || vmRef == "" {
		return nil, usageErr("'disk_id' and 'vm' are required", "нужны 'disk_id' и 'vm'")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	vmID, _, err := s.resolveVM(ctx, vmRef, eng.Config.ProjectID)
	if err != nil {
		return nil, err
	}
	if _, err := settledDisk(ctx, eng, diskID); err != nil {
		return nil, err
	}
	return &Plan{Preview: map[string]any{"message": i18n.T("disk_attach changes a VM.", "disk_attach меняет ВМ.")},
		Execute: func(ctx context.Context) (string, bool) {
			r, err := eng.Cloud.AttachDisk(ctx, diskID, vmID, eng.Config.ProjectID)
			if err != nil && diskNotReady(err) {
				// A disk just created is not attachable yet: 422 "Disk has no
				// openstack_id" (CMP-15; live run 18.10 — disk_attach right after
				// disk_create). Wait until it is available, as disk_create
				// attach_to does (L-24), and attach once more.
				if werr := waitDiskAvailable(ctx, eng, diskID, eng.Config.ProjectID); werr != nil {
					return errResult(fmt.Errorf(i18n.T("attach disk: %w (waited for the new disk: %v)", "привязка диска: %w (ожидание нового диска: %v)"), err, werr)), true
				}
				r, err = eng.Cloud.AttachDisk(ctx, diskID, vmID, eng.Config.ProjectID)
			}
			if err != nil {
				return errResult(fmt.Errorf(i18n.T("attach disk: %w", "привязка диска: %w"), err)), true
			}
			return toJSON(map[string]any{"status": "attached", "result": r}), false
		}}, nil
}

// diskNotReady: the attach was refused because the disk is not ready yet
// (422 "Disk has no openstack_id", CMP-15).
func diskNotReady(err error) bool {
	var se *provision.StatusError
	return errors.As(err, &se) && se.Status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(se.Body), "openstack_id")
}

// planDiskDetach plans the detach of a disk from a VM (gate.go): the VM
// and the disk's state are read before the question (D53).
func (s *Registry) planDiskDetach(ctx context.Context, args map[string]any) (*Plan, error) {
	diskID := stringArg(args, "disk_id")
	vmRef := stringArg(args, "vm")
	if diskID == "" || vmRef == "" {
		return nil, usageErr("'disk_id' and 'vm' are required", "нужны 'disk_id' и 'vm'")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	vmID, _, err := s.resolveVM(ctx, vmRef, eng.Config.ProjectID)
	if err != nil {
		return nil, err
	}
	if _, err := settledDisk(ctx, eng, diskID); err != nil {
		return nil, err
	}
	return &Plan{Preview: map[string]any{"message": i18n.T("disk_detach changes a VM.", "disk_detach меняет ВМ.")},
		Execute: func(ctx context.Context) (string, bool) {
			r, err := eng.Cloud.DetachDisk(ctx, diskID, vmID, eng.Config.ProjectID)
			if err != nil {
				return errResult(fmt.Errorf(i18n.T("detach disk: %w", "отвязка диска: %w"), err)), true
			}
			return toJSON(map[string]any{"status": "detached", "result": r}), false
		}}, nil
}

// planDiskResize plans the growth of a disk in place (PUT {size}, gate.go).
// Only grows: a smaller or the same size is refused before the question;
// free-tier disks are rejected by the API.
func (s *Registry) planDiskResize(ctx context.Context, args map[string]any) (*Plan, error) {
	diskID := stringArg(args, "disk_id")
	sizeGB := intArg(args, "size_gb", 0)
	if diskID == "" {
		return nil, usageErr("'disk_id' is required", "нужен 'disk_id'")
	}
	if _, ok := args["size_gb"]; !ok {
		return nil, usageErr("'size_gb' is required (the new size, larger than the current one)", "нужен 'size_gb' — новый размер, больше текущего")
	}
	if sizeGB <= 0 {
		return nil, usageErr("size_gb must be greater than 0, got %d", "size_gb должен быть больше 0, передано %d", sizeGB)
	}
	if sizeGB > provision.MaxDiskGB {
		return nil, usageErr("size_gb %d is over the disk maximum of %d GB", "size_gb %d больше предельного размера диска %d ГБ", sizeGB, provision.MaxDiskGB)
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	// The disk before the question: a shrink the API refuses, the same
	// size changes nothing — both failed after consent (G13-3, W5); a
	// transient state is waited for or refused (D53).
	d, err := settledDisk(ctx, eng, diskID)
	if err != nil {
		return nil, err
	}
	if cur, ok := numberValue(d["size"]); ok {
		switch {
		case float64(sizeGB) < cur:
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("disk %s has %v GB: disk_resize only grows it, %d GB is smaller", "у диска %s %v ГБ: disk_resize только увеличивает, %d ГБ меньше", diskID, cur, sizeGB),
				i18n.T("Pass a size larger than the current one.", "Укажите размер больше текущего."))
		case float64(sizeGB) == cur:
			return nil, clierrors.New(clierrors.CodeUsage,
				i18n.Tf("disk %s already has %d GB: nothing to change", "у диска %s уже %d ГБ: менять нечего", diskID, sizeGB),
				i18n.T("Pass a size larger than the current one.", "Укажите размер больше текущего."))
		}
	}
	preview, err := s.completePreview(ctx, nil, engine.CreatePreview{Resource: engine.ResourceDiskResize, Name: diskID,
		Disk: &deployplan.DiskSpec{SizeGB: sizeGB}})
	if err != nil {
		return nil, err
	}
	return &Plan{Preview: map[string]any{
		"message": i18n.Tf("disk_resize grows disk %s to %d GB (grow only; billable). NB: after resize you must grow the partition+filesystem on the VM (e.g. growpart + resize2fs). Free-tier disks are rejected by the API.",
			"disk_resize увеличит диск %s до %d ГБ (только увеличение; платно). После этого раздел и файловую систему на ВМ нужно расширить (например, growpart и resize2fs). Диски бесплатного тарифа API не увеличивает.", diskID, sizeGB),
		"preview": preview,
	}, Execute: func(ctx context.Context) (string, bool) {
		d, err := eng.Cloud.ResizeDisk(ctx, diskID, sizeGB, eng.Config.ProjectID)
		if err != nil {
			return errResult(fmt.Errorf(i18n.T("resize disk: %w", "изменение размера диска: %w"), err)), true
		}
		return toJSON(map[string]any{
			"status": "resized", "disk": d,
			"note": "Disk grown. On the VM grow the partition + filesystem to use it: e.g. `growpart /dev/vda 2 && resize2fs /dev/vda2`.",
		}), false
	}}, nil
}

// planDiskDelete plans the delete of a disk by id (gate.go).
func (s *Registry) planDiskDelete(ctx context.Context, args map[string]any) (*Plan, error) {
	diskID := stringArg(args, "disk_id")
	if diskID == "" {
		return nil, usageErr("'disk_id' is required", "нужен 'disk_id'")
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	// A disk that is not in the project is already deleted (D26): the
	// preview asked consent for any id.
	disk, err := eng.Cloud.GetDisk(ctx, diskID)
	if err != nil {
		return answered(s.deleteFailed("disk_delete", diskID, err)), nil
	}
	if vms, err := diskAttachedError(diskID, disk); err != nil {
		return answered(errAnswer(err, map[string]any{"disk_id": diskID, "attached_vms": vms}), true), nil
	}
	preview := map[string]any{"message": i18n.Tf("disk_delete permanently destroys disk %s and its data.",
		"disk_delete безвозвратно удаляет диск %s и его данные.", diskID)}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
		if err := eng.Cloud.DeleteDisk(ctx, diskID, eng.Config.ProjectID); err != nil {
			return s.deleteFailed("disk_delete", diskID, fmt.Errorf(i18n.T("delete disk: %w", "удаление диска: %w"), err))
		}
		return toJSON(map[string]any{"status": "deleted", "disk_id": diskID}), false
	}}, nil
}

// diskAttachedError refuses the delete of a disk a VM still has (D59):
// Cloud.ru answers it with 422 after consent («Primary disk can not be
// deleted without vm»), and the disk's VMs are in the GetDisk answer the
// plan reads anyway. The hint names the way out per VM: a boot disk goes
// with its VM (destroy), a data disk is detached first (disk_detach); vms
// are the VMs, for the answer's attached_vms (untrustedFields). nil for a
// disk no VM has.
func diskAttachedError(diskID string, disk map[string]any) ([]map[string]any, error) {
	list, _ := disk["vms"].([]any)
	var names, ways []string
	var vms []map[string]any
	for _, v := range list {
		vm, _ := v.(map[string]any)
		if vm == nil {
			continue
		}
		id, _ := vm["id"].(string)
		name, _ := vm["name"].(string)
		primary, _ := vm["primary"].(bool)
		vms = append(vms, map[string]any{"id": id, "name": name, "primary": primary})
		label := quoteCloud(firstNonEmptyStr(name, id))
		names = append(names, label)
		if primary {
			ways = append(ways, i18n.Tf("It is the boot disk of VM %s: delete the VM with destroy vm_id=%s ('evo vm delete %s'), its boot disk goes with it.",
				"Это загрузочный диск ВМ %s: удалите ВМ — destroy vm_id=%s ('evo vm delete %s'), загрузочный диск уйдёт вместе с ней.", label, id, id))
		} else {
			ways = append(ways, i18n.Tf("Detach it from VM %s with disk_detach disk_id=%s vm=%s ('evo disk detach %s --vm %s'), then delete it.",
				"Отключите его от ВМ %s — disk_detach disk_id=%s vm=%s ('evo disk detach %s --vm %s'), затем удалите.", label, diskID, id, diskID, id))
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	return vms, clierrors.New(clierrors.CodeUsage,
		i18n.Tf("disk %s is attached to VM %s: Cloud.ru does not delete an attached disk; nothing was deleted",
			"диск %s подключён к ВМ %s: Cloud.ru не удаляет подключённый диск; ничего не удалено", diskID, strings.Join(names, ", ")),
		strings.Join(ways, " "))
}

// diskCreatePreview is the preview of disk_create: the disk as resolved,
// the zone (by name when the call gave one) and its price.
func diskCreatePreview(eng *engine.Engine, req provision.CreateDiskReq, attachTo, zoneArg string) engine.CreatePreview {
	zone := &deployplan.RefSpec{ID: req.AvailabilityZoneID}
	if zoneArg != "" && !looksUUID(zoneArg) {
		zone.Name = zoneArg
	}
	p := engine.CreatePreview{Resource: engine.ResourceDisk, Name: req.Name,
		ProjectID: req.ProjectID, ProjectFrom: eng.Config.ProjectOrigin, Zone: zone,
		Disk: &deployplan.DiskSpec{SizeGB: req.Size, Type: firstNonEmptyStr(req.DiskTypeName, req.DiskTypeID)}}
	p.Add("attach_to", attachTo)
	p.Price()
	return p
}
