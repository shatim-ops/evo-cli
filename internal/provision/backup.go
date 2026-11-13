// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// BackupAPI reads the backups of VMs and disks: /api/v1/backups of the
// compute API (compute/openapi-v3.yaml; brief docs/internal/briefs/backups.md).
// It is an interface of its own, not a part of CloudAPI: the fakes of
// CloudAPI do not know backups, and a tool asks the engine's client for it
// (release plan 1.9, wave В5: the files of CloudAPI are «разработка A»'s).
type BackupAPI interface {
	ListBackups(ctx context.Context, projectID string) ([]map[string]any, error)
	GetBackup(ctx context.Context, backupID string) (map[string]any, error)
	DeleteBackup(ctx context.Context, backupID string) error
	CreateBackup(ctx context.Context, req CreateBackupReq) (map[string]any, error)
	RestoreBackup(ctx context.Context, backupID string, cfg RestoreDiskConfig) (map[string]any, error)
}

// RestoreDiskConfig is the target_config of the restore of a disk backup
// (DiskRestoreConfig): the new disk's name, its type by id or by name, and
// a description; the platform takes the zone and the size from the backup.
type RestoreDiskConfig struct {
	Name         string `json:"name"`
	DiskTypeID   string `json:"disk_type_id,omitempty"`
	DiskTypeName string `json:"disk_type_name,omitempty"`
	Description  string `json:"description,omitempty"`
}

// CreateBackupReq is the body of POST /api/v1/backups: what evo sends, and
// nothing more (CreateBackupRequestSchema: name, project_id and resource
// are required). Tags (wave В6) and a protection plan (a brief of its own)
// are not sent: docs/internal/briefs/backups.md.
type CreateBackupReq struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	ProjectID   string         `json:"project_id"`
	Resource    BackupResource `json:"resource"`
}

// BackupResource is the source of a backup: a disk or a virtual_machine.
type BackupResource struct {
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
}

var _ BackupAPI = (*CloudRuAdapter)(nil)

// ListBackups lists the backups of a project, every page (listRaw); a
// filter of the list (Narrowed) narrows the request.
func (a *CloudRuAdapter) ListBackups(ctx context.Context, projectID string) ([]map[string]any, error) {
	return listRaw(a, ctx, "/api/v1/backups", projectID)
}

// GetBackup reads one backup by id; a backup that is not there is a 404
// error (NOT_FOUND).
func (a *CloudRuAdapter) GetBackup(ctx context.Context, backupID string) (map[string]any, error) {
	resp, err := a.doWithRetry(ctx, http.MethodGet, fmt.Sprintf("/api/v1/backups/%s", seg(backupID)), nil)
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

// CreateBackup asks for a backup of a VM or a disk; the answer is the backup,
// with its id and its state (creating).
func (a *CloudRuAdapter) CreateBackup(ctx context.Context, req CreateBackupReq) (map[string]any, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, "/api/v1/backups", body)
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

// DeleteBackup asks to delete a backup; the platform answers 204 and deletes
// it after (state deleting, then 404). A DELETE is not repeated after a
// dropped connection (computeRetry).
func (a *CloudRuAdapter) DeleteBackup(ctx context.Context, backupID string) error {
	resp, err := a.doWithRetry(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/backups/%s", seg(backupID)), nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// RestoreBackup restores a disk backup into a new disk (POST
// /api/v1/backups/{id}/action, action restore). The answer is the backup,
// not the new disk: the platform does not name it (UpdateBackupResponseSchema),
// and the caller finds it by its name. The disk the backup was made of is
// not changed (two runs of Г-16). A POST is not repeated after a dropped
// connection (computeRetry).
func (a *CloudRuAdapter) RestoreBackup(ctx context.Context, backupID string, cfg RestoreDiskConfig) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{"action": "restore", "resource": map[string]any{"target_config": cfg}})
	if err != nil {
		return nil, err
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, fmt.Sprintf("/api/v1/backups/%s/action", seg(backupID)), body)
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
