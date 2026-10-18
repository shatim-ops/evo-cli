// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// CreateImageReq creates a VM image from an existing boot disk
// (POST /api/v1/images). Two conditions, both enforced by the API:
//
//   - the disk must be bootable, else
//     non_bootable_disk_can_not_be_user_to_create_image_error;
//   - the disk must be DETACHED (state "available"). Stopping the VM is not
//     enough — while the disk is still attached the API answers
//     invalid_disk_state_to_create_image.
//
// So the flow is: build the VM, power it off, detach the boot disk, wait for
// state "available", then create the image.
type CreateImageReq struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	// Description is capped at 255 characters by the API (too_long otherwise).
	Description string `json:"description,omitempty"`
	DiskID      string `json:"disk_id"`
	MinCPU      int    `json:"min_cpu,omitempty"`
	MinRAM      int    `json:"min_ram,omitempty"`
	// MinDisk is in GB and must be at least the image size, or the platform
	// refuses to boot VMs from it.
	MinDisk int `json:"min_disk,omitempty"`
	// UserDataTemplate is the cloud-init Jinja template the platform renders
	// when a VM is created from this image. Marketplace images need it: it is
	// how the buyer's inputs (SSH key, password, product settings) reach the
	// guest.
	UserDataTemplate string `json:"user_data_template,omitempty"`
	// ImageMetadata declares the fields the buyer fills in; their values are
	// substituted into UserDataTemplate. Every {{ var }} used by the template
	// must be declared here.
	ImageMetadata []ImageMetadataInput `json:"image_metadata,omitempty"`
	AvailZones    []ImageAZTarget      `json:"availability_zones"`
}

// ImageMetadataInput declares a metadata field when CREATING an image. It is
// deliberately not ImageMetadataField: the write schema differs from the read
// one — the API demands display_name and display_position, and rejects id and
// is_required with "Extra inputs are not permitted".
type ImageMetadataInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	// DisplayPosition orders the fields in the create-VM form; required.
	DisplayPosition int    `json:"display_position"`
	FieldType       string `json:"field_type,omitempty"`
	DefaultValue    string `json:"default_value,omitempty"`
	// Sensitive has no omitempty on purpose: the API requires the key to be
	// present, and omitempty would drop it whenever it is false.
	Sensitive bool `json:"sensitive"`
	// MandatoryGroup: at least one field of a group must be filled in.
	MandatoryGroup string `json:"mandatory_group,omitempty"`
}

// ImageAZTarget names an availability zone the image should be built in.
type ImageAZTarget struct {
	AvailabilityZoneID string `json:"availability_zone_id"`
}

// CreateImage builds an image from a bootable disk. The call returns as soon
// as the build is queued; the image is usable only once a zone reports state
// "created", so poll GetImage afterwards — building runs at roughly 0.7 GB per
// minute.
func (a *CloudRuAdapter) CreateImage(ctx context.Context, req CreateImageReq) (map[string]any, error) {
	if req.DiskID == "" {
		return nil, errors.New(i18n.T("disk_id is required", "нужен disk_id"))
	}
	if req.Name == "" {
		return nil, errors.New(i18n.T("name is required", "нужно имя (name)"))
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, "/api/v1/images", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// DeleteImage removes an image via DELETE /api/v1/images/{id}.
func (a *CloudRuAdapter) DeleteImage(ctx context.Context, imageID, projectID string) error {
	if imageID == "" {
		return errors.New(i18n.T("image_id is required", "нужен image_id"))
	}
	path := "/api/v1/images/" + seg(imageID)
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
