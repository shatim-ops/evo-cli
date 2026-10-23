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
		list := func(ctx context.Context) ([]map[string]any, error) { return eng.Cloud.ListDisks(ctx, pid) }
		return lostCreate(ctx, list, i18n.T("disk", "диск"), "disk_id", name, nil, "disk_list", "disk_list", fmt.Errorf("create disk: %w", err))
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
		return partial(fmt.Errorf("attach: %w", err))
	}
	res["attach"] = at
	res["note"] = "Disk created and attached. On the VM: partition/format the new device (e.g. mkfs.ext4 /dev/vdX) and mount it — attaching does NOT grow the existing filesystem."
	return toJSON(res), false
}

// diskReadyTimeout bounds the wait for a new disk to become available
// before disk_create attaches it.
const diskReadyTimeout = 2 * time.Minute

// waitDiskAvailable polls disk_list until the disk is "available", at the
// VM poll interval (Engine.Timings.VMPollInterval, 5 s by default), for at
// most diskReadyTimeout (or Timings.VMPollTimeout when shorter). A disk in
// an error state ends the wait at once.
func waitDiskAvailable(ctx context.Context, eng *engine.Engine, diskID, projectID string) error {
	every := 5 * time.Second
	if eng.Timings.VMPollInterval > 0 {
		every = eng.Timings.VMPollInterval
	}
	timeout := diskReadyTimeout
	if t := eng.Timings.VMPollTimeout; t > 0 && t < timeout {
		timeout = t
	}
	deadline := time.Now().Add(timeout)
	last := "unknown"
	for {
		if disks, err := eng.Cloud.ListDisks(ctx, projectID); err == nil {
			for _, d := range disks {
				if id, _ := d["id"].(string); id != diskID {
					continue
				}
				st, _ := d["state"].(string)
				last = st
				switch {
				case st == "available":
					return nil
				case strings.HasPrefix(st, "error"):
					return fmt.Errorf("the disk is in state %s", st)
				}
			}
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the disk did not become available within %s (state %s)", timeout, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

// handleDiskAttach attaches an existing disk to a VM.
func (s *Registry) handleDiskAttach(ctx context.Context, args map[string]any) (string, bool) {
	diskID := stringArg(args, "disk_id")
	vmRef := stringArg(args, "vm")
	if diskID == "" || vmRef == "" {
		return usageT("'disk_id' and 'vm' are required", "нужны 'disk_id' и 'vm'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.T("disk_attach changes a VM.",
			"disk_attach меняет ВМ.")}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	vmID, _, err := s.resolveVM(ctx, vmRef, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	r, err := eng.Cloud.AttachDisk(ctx, diskID, vmID, eng.Config.ProjectID)
	if err != nil && diskNotReady(err) {
		// A disk just created is not attachable yet: 422 "Disk has no
		// openstack_id" (CMP-15; live run 18.10 — disk_attach right after
		// disk_create). Wait until it is available, as disk_create
		// attach_to does (L-24), and attach once more.
		if werr := waitDiskAvailable(ctx, eng, diskID, eng.Config.ProjectID); werr != nil {
			return errResult(fmt.Errorf("attach disk: %w (waited for the new disk: %v)", err, werr)), true
		}
		r, err = eng.Cloud.AttachDisk(ctx, diskID, vmID, eng.Config.ProjectID)
	}
	if err != nil {
		return errResult(fmt.Errorf("attach disk: %w", err)), true
	}
	return toJSON(map[string]any{"status": "attached", "result": r}), false
}

// diskNotReady: the attach was refused because the disk is not ready yet
// (422 "Disk has no openstack_id", CMP-15).
func diskNotReady(err error) bool {
	var se *provision.StatusError
	return errors.As(err, &se) && se.Status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(se.Body), "openstack_id")
}

// handleDiskDetach detaches a disk from a VM.
func (s *Registry) handleDiskDetach(ctx context.Context, args map[string]any) (string, bool) {
	diskID := stringArg(args, "disk_id")
	vmRef := stringArg(args, "vm")
	if diskID == "" || vmRef == "" {
		return usageT("'disk_id' and 'vm' are required", "нужны 'disk_id' и 'vm'"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.T("disk_detach changes a VM.",
			"disk_detach меняет ВМ.")}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	vmID, _, err := s.resolveVM(ctx, vmRef, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	r, err := eng.Cloud.DetachDisk(ctx, diskID, vmID, eng.Config.ProjectID)
	if err != nil {
		return errResult(fmt.Errorf("detach disk: %w", err)), true
	}
	return toJSON(map[string]any{"status": "detached", "result": r}), false
}

// handleDiskResize grows a disk in place (PUT {size}). Only grows; shrink and
// free-tier disks are rejected by the API. Mutation: requires confirmed=true.
func (s *Registry) handleDiskResize(ctx context.Context, args map[string]any) (string, bool) {
	diskID := stringArg(args, "disk_id")
	sizeGB := intArg(args, "size_gb", 0)
	if diskID == "" {
		return usageT("'disk_id' is required", "нужен 'disk_id'"), true
	}
	if _, ok := args["size_gb"]; !ok {
		return usageT("'size_gb' is required (the new size, larger than the current one)", "нужен 'size_gb' — новый размер, больше текущего"), true
	}
	if sizeGB <= 0 {
		return usageT("size_gb must be greater than 0, got %d", "size_gb должен быть больше 0, передано %d", sizeGB), true
	}
	if sizeGB > provision.MaxDiskGB {
		return usageT("size_gb %d is over the disk maximum of %d GB", "size_gb %d больше предельного размера диска %d ГБ", sizeGB, provision.MaxDiskGB), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		preview, err := s.completePreview(ctx, nil, engine.CreatePreview{Resource: engine.ResourceDiskResize, Name: diskID,
			Disk: &deployplan.DiskSpec{SizeGB: sizeGB}})
		if err != nil {
			return errResult(err), true
		}
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.Tf("disk_resize grows disk %s to %d GB (grow only; billable). NB: after resize you must grow the partition+filesystem on the VM (e.g. growpart + resize2fs). Free-tier disks are rejected by the API.",
				"disk_resize увеличит диск %s до %d ГБ (только увеличение; платно). После этого раздел и файловую систему на ВМ нужно расширить (например, growpart и resize2fs). Диски бесплатного тарифа API не увеличивает.", diskID, sizeGB),
			"preview": preview,
		}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	d, err := eng.Cloud.ResizeDisk(ctx, diskID, sizeGB, eng.Config.ProjectID)
	if err != nil {
		return errResult(fmt.Errorf("resize disk: %w", err)), true
	}
	return toJSON(map[string]any{
		"status": "resized", "disk": d,
		"note": "Disk grown. On the VM grow the partition + filesystem to use it: e.g. `growpart /dev/vda 2 && resize2fs /dev/vda2`.",
	}), false
}

// handleDiskDelete deletes a disk by id.
func (s *Registry) handleDiskDelete(ctx context.Context, args map[string]any) (string, bool) {
	diskID := stringArg(args, "disk_id")
	if diskID == "" {
		return usageT("'disk_id' is required", "нужен 'disk_id'"), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		// A disk that is not in the project is already deleted (D26): the
		// preview asked consent for any id.
		if _, err := eng.Cloud.GetDisk(ctx, diskID); err != nil {
			return s.deleteFailed("disk_delete", diskID, err)
		}
		return toJSON(map[string]any{"status": "confirmation_required", "message": i18n.Tf("disk_delete permanently destroys disk %s and its data.",
			"disk_delete безвозвратно удаляет диск %s и его данные.", diskID)}), true
	}
	if err := eng.Cloud.DeleteDisk(ctx, diskID, eng.Config.ProjectID); err != nil {
		return s.deleteFailed("disk_delete", diskID, fmt.Errorf("delete disk: %w", err))
	}
	return toJSON(map[string]any{"status": "deleted", "disk_id": diskID}), false
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
