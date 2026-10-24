// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// resolveZoneID turns a zone name or UUID into a zone UUID, falling back to the
// configured region when ref is empty.
func (s *Registry) resolveZoneID(ctx context.Context, ref, fallback string) (string, error) {
	if ref == "" {
		return fallback, nil
	}
	if looksUUID(ref) {
		return ref, nil
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", err
	}
	zones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return "", err
	}
	for _, z := range zones {
		if z.Name == ref {
			return z.ID, nil
		}
	}
	return "", fmt.Errorf(i18n.T("zone %q not found", "зона %q не найдена"), ref)
}

// zoneOfDisk returns the availability zone UUID the given disk lives in.
func (s *Registry) zoneOfDisk(ctx context.Context, eng *engine.Engine, diskID string) (string, error) {
	disks, err := eng.Cloud.ListDisks(ctx, eng.Config.ProjectID)
	if err != nil {
		return "", err
	}
	for _, d := range disks {
		if id, _ := d["id"].(string); id != diskID {
			continue
		}
		if az, ok := d["availability_zone"].(map[string]any); ok {
			if zid, _ := az["id"].(string); zid != "" {
				return zid, nil
			}
		}
		return "", fmt.Errorf(i18n.T("disk %s reports no availability zone; pass 'zone' explicitly", "у диска %s не указана зона доступности; передайте 'zone'"), diskID)
	}
	return "", fmt.Errorf(i18n.T("disk %s not found in project %s", "диск %s не найден в проекте %s"), diskID, eng.Config.ProjectID)
}

// handleImageCreate builds a VM image from a bootable disk — the last step of
// producing a distributable image (e.g. a Marketplace product). The source disk
// must be bootable AND detached: power the VM off, disk_detach, wait for state
// "available". A stopped-but-attached disk is refused with
// invalid_disk_state_to_create_image.
// Mutation + storage cost: requires confirmed=true.
func (s *Registry) handleImageCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	diskID := stringArg(args, "disk_id")
	if name == "" || diskID == "" {
		return usageT("'name' and 'disk_id' are required", "нужны 'name' и 'disk_id'"), true
	}
	if err := provision.ValidName(i18n.T("image", "образа"), name); err != nil {
		return errResult(err), true
	}
	// A negative minimum made a preview, then the API refused it (QA 20.10,
	// QA-7); 0 is "no minimum".
	for _, k := range []string{"min_cpu", "min_ram", "min_disk"} {
		if err := minIntArg(args, k, 0, i18n.T("0 (the default) sets no minimum.", "0 (по умолчанию) — без минимума.")); err != nil {
			return errResult(err), true
		}
	}
	req := provision.CreateImageReq{
		Name:             name,
		DisplayName:      stringArg(args, "display_name"),
		Description:      stringArg(args, "description"),
		DiskID:           diskID,
		MinCPU:           intArg(args, "min_cpu", 0),
		MinRAM:           intArg(args, "min_ram", 0),
		MinDisk:          intArg(args, "min_disk", 0),
		UserDataTemplate: stringArg(args, "user_data_template"),
	}

	// image_metadata declares the fields a buyer fills in when creating a VM;
	// accepted as a JSON array so the caller can pass the full field spec.
	if raw, ok := args["image_metadata"]; ok && raw != nil {
		// A malformed argument is the caller's (USAGE), not the API's
		// (release audit, A-003).
		blob, err := json.Marshal(raw)
		if err != nil {
			return usageResult(fmt.Errorf("image_metadata: %w", err)), true
		}
		if err := json.Unmarshal(blob, &req.ImageMetadata); err != nil {
			return usageResult(fmt.Errorf(i18n.T("image_metadata must be an array of field objects: %w", "image_metadata должен быть массивом объектов полей: %w"), err)), true
		}
	}

	// The gate comes before authentication so an unapproved call never reaches
	// the cloud at all — the same order every other mutating tool uses. The zone
	// is therefore echoed as asked for, not as resolved. When none was asked
	// for, the preview states the rule rather than naming a source it cannot
	// check here: claiming "configured region" is wrong whenever no region is
	// configured, and this preview is what an operator approves a paid action on.
	zoneRef := stringArg(args, "zone")
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("image_create makes a billable image from a disk.",
				"image_create создаёт платный образ из диска."),
			"preview": willPreview(engine.ResourceImage, name, zoneRef, args, []string{"disk_id", "min_cpu", "min_ram", "min_disk"}),
			"will_create": map[string]any{
				"name": name, "from_disk": diskID,
				"zone":    firstNonEmptyStr(zoneRef, "not given: configured region, else the source disk's zone"),
				"min_cpu": req.MinCPU, "min_ram": req.MinRAM, "min_disk": req.MinDisk,
				"has_user_data_template": req.UserDataTemplate != "",
				"metadata_fields":        len(req.ImageMetadata),
			},
		}), true
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	req.ProjectID = eng.Config.ProjectID

	// An image belongs in the zone of the disk it is made from, so that disk is
	// the default when neither an explicit zone nor a configured one exists.
	// Without this the request went out with an empty availability_zone_id and
	// the platform answered 422.
	fallback := eng.Config.Region
	if zoneRef == "" && fallback == "" {
		fallback, err = s.zoneOfDisk(ctx, eng, diskID)
		if err != nil {
			return errResult(err), true
		}
	}
	zoneID, err := s.resolveZoneID(ctx, zoneRef, fallback)
	if err != nil {
		return errResult(err), true
	}
	req.AvailZones = []provision.ImageAZTarget{{AvailabilityZoneID: zoneID}}

	img, err := eng.Cloud.CreateImage(ctx, req)
	if id, _ := img["id"].(string); err != nil || id == "" {
		if err == nil {
			err = noID("image")
		}
		list := func(ctx context.Context) ([]map[string]any, error) {
			images, err := eng.Cloud.ListImagesInProject(ctx, req.ProjectID)
			listed := make([]map[string]any, 0, len(images))
			for _, im := range images {
				listed = append(listed, map[string]any{"id": im.ID, "name": im.Name})
			}
			return listed, err
		}
		return lostCreate(ctx, "image_create", list, i18n.T("image", "образ"), name, nil, "image_get id", fmt.Errorf("create image: %w", err))
	}
	return toJSON(map[string]any{
		"status": "building",
		"image":  img,
		"note": "Image build is queued, not finished: it runs at roughly 0.7 GB per minute. " +
			"Poll image_get until it reports ready=true before booting from it.",
	}), false
}

// handleFipAttach binds an existing free floating IP to a VM's primary
// interface. Needed whenever a VM ends up without a public address — e.g. when
// provisioning from a large custom image takes longer than the provision
// wizard's wait and it returns before assigning one.
// Mutation: requires confirmed=true.
func (s *Registry) handleFipAttach(ctx context.Context, args map[string]any) (string, bool) {
	fipRef := stringArg(args, "fip")
	vmRef := stringArg(args, "vm")
	if fipRef == "" || vmRef == "" {
		return usageT("'fip' (address or UUID) and 'vm' (name or UUID) are required", "нужны 'fip' (адрес или UUID) и 'vm' (имя или UUID)"), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("fip_attach gives the VM a public address.",
				"fip_attach даёт ВМ публичный адрес."),
			"will_attach": map[string]any{"fip": fipRef, "vm": vmRef},
		}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	pid := eng.Config.ProjectID

	// Resolve the floating IP by address or UUID, and refuse one already in use.
	fips, err := eng.Cloud.ListFloatingIPs(ctx, pid)
	if err != nil {
		return errResult(fmt.Errorf(i18n.T("list floating ips: %w", "список floating IP: %w"), err)), true
	}
	fipID, fipName := "", ""
	for _, f := range fips {
		if f.ID == fipRef || f.IPAddress == fipRef {
			if f.OnNATGateway() {
				return errResult(fmt.Errorf(i18n.T("floating IP %q belongs to a SNAT gateway and cannot be attached to a VM", "floating IP %q принадлежит SNAT-шлюзу, к ВМ его не привязать"), fipRef)), true
			}
			fipID, fipName = f.ID, f.Name
			break
		}
	}
	if fipID == "" {
		return errResult(fmt.Errorf(i18n.T("floating IP %q not found", "floating IP %q не найден"), fipRef)), true
	}

	// Find the VM's primary interface — that is what the address binds to.
	vmID, _, err := s.resolveVM(ctx, vmRef, pid)
	if err != nil {
		return errResult(err), true
	}
	detail, err := eng.Cloud.GetVMDetail(ctx, vmID)
	if err != nil {
		return errResult(fmt.Errorf("get vm detail: %w", err)), true
	}
	ifaceID := ""
	for _, itf := range detail.Interfaces {
		if itf.Primary {
			ifaceID = itf.ID
			break
		}
	}
	if ifaceID == "" && len(detail.Interfaces) > 0 {
		ifaceID = detail.Interfaces[0].ID
	}
	if ifaceID == "" {
		return errResult(fmt.Errorf(i18n.T("VM %s has no interface to attach to", "у ВМ %s нет интерфейса для привязки"), vmRef)), true
	}

	// The API rejects an empty name ("Value should have at least 1 item"), so
	// fall back to the address's existing name, then to the VM's.
	name := fipName
	if name == "" {
		name = provision.FitName("fip-"+detail.Name, "")
	}
	res, err := eng.Cloud.AttachFloatingIP(ctx, fipID, pid, ifaceID, name)
	if err != nil {
		return errResult(fmt.Errorf("attach floating ip: %w", err)), true
	}
	return toJSON(map[string]any{"status": "attached", "floating_ip": res, "vm_id": vmID, "interface_id": ifaceID}), false
}

// zoneStates maps each zone name to the image's build state there.
func zoneStates(img *provision.ImageDetail) map[string]string {
	states := make(map[string]string, len(img.AvailZones))
	for _, az := range img.AvailZones {
		name := az.AvailabilityZoneName
		if name == "" {
			name = az.AvailabilityZoneID
		}
		states[name] = az.State
	}
	return states
}

// handleImageDelete removes an image. Mutation: requires confirmed=true.
func (s *Registry) handleImageDelete(ctx context.Context, args map[string]any) (string, bool) {
	imageID := stringArg(args, "image_id")
	if imageID == "" {
		return usageT("'image_id' is required", "нужен 'image_id'"), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		// An image that is not there is already deleted (D26).
		if _, err := eng.Cloud.GetImage(ctx, imageID); err != nil {
			return s.deleteFailed("image_delete", imageID, err)
		}
		return toJSON(map[string]any{
			"status": "confirmation_required",
			"message": i18n.T("image_delete permanently removes the image. VMs already running keep working, but no new VM can be created from it.",
				"image_delete безвозвратно удаляет образ. Уже работающие ВМ продолжат работать, но новую ВМ из него не создать."),
			"will_delete": map[string]any{"image_id": imageID},
		}), true
	}
	if err := eng.Cloud.DeleteImage(ctx, imageID, eng.Config.ProjectID); err != nil {
		return s.deleteFailed("image_delete", imageID, fmt.Errorf("delete image: %w", err))
	}
	return toJSON(map[string]any{"status": "deleted", "image_id": imageID}), false
}
