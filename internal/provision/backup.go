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
