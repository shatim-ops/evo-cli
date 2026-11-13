// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The backups of the parity world (provision.BackupAPI): one backup of the
// world's VM, so that the command and the tool answer the same card.
var _ provision.BackupAPI = parityCloud{}

func (parityCloud) ListBackups(context.Context, string) ([]map[string]any, error) {
	var disk map[string]any
	if err := json.Unmarshal([]byte(parityDiskBackupJSON), &disk); err != nil {
		return nil, err
	}
	return []map[string]any{parityBackup(), disk}, nil
}

// GetBackup reads the HTTP world when the parity cloud is the real adapter
// over it: after its DELETE the backup is gone there (a 404), and a delete
// that waits for that (D74) does not wait the production minute.
func (p parityCloud) GetBackup(ctx context.Context, id string) (map[string]any, error) {
	if b, ok := p.CloudAPI.(provision.BackupAPI); ok {
		return b.GetBackup(ctx, id)
	}
	if id == parityDiskBackup {
		var b map[string]any
		err := json.Unmarshal([]byte(parityDiskBackupJSON), &b)
		return b, err
	}
	b := parityBackup()
	b["id"] = id
	return b, nil
}

func parityBackup() map[string]any {
	return map[string]any{"id": parityVM, "name": "web-copy", "state": "created", "type": "full", "size": float64(20480),
		"created_at": "2026-11-12T10:00:00Z", "tags": []any{},
		"resource": map[string]any{"resource_id": parityVM, "resource_type": "virtual_machine", "resource_exists": true,
			"resource_state": map[string]any{"name": "web", "flavor": map[string]any{"name": "gen-2-4"}}}}
}

// CreateBackup goes to the HTTP world when the parity cloud is the real
// adapter over it (withJSONWorld): the write is recorded there, as every
// other tool's.
func (p parityCloud) CreateBackup(ctx context.Context, req provision.CreateBackupReq) (map[string]any, error) {
	if b, ok := p.CloudAPI.(provision.BackupAPI); ok {
		return b.CreateBackup(ctx, req)
	}
	b := parityBackup()
	b["name"], b["state"] = req.Name, "created"
	return b, nil
}

// backupKeptCloud has the backup parityArgs asks for already: the repeat of
// backup_create answers already_exists, with no question and no write.
type backupKeptCloud struct{ provision.CloudAPI }

func (c backupKeptCloud) ListBackups(context.Context, string) ([]map[string]any, error) {
	b := parityBackup()
	b["name"] = "web-backup"
	return []map[string]any{b}, nil
}

func (c backupKeptCloud) GetBackup(ctx context.Context, id string) (map[string]any, error) {
	return parityCloud{}.GetBackup(ctx, id)
}

func (c backupKeptCloud) DeleteBackup(ctx context.Context, id string) error {
	return parityCloud{}.DeleteBackup(ctx, id)
}

func (c backupKeptCloud) RestoreBackup(ctx context.Context, id string, cfg provision.RestoreDiskConfig) (map[string]any, error) {
	return parityCloud{}.RestoreBackup(ctx, id, cfg)
}

func (c backupKeptCloud) CreateBackup(ctx context.Context, req provision.CreateBackupReq) (map[string]any, error) {
	if b, ok := c.CloudAPI.(provision.BackupAPI); ok {
		return b.CreateBackup(ctx, req)
	}
	return parityCloud{}.CreateBackup(ctx, req)
}

func init() {
	consentWorlds["backup of that name kept"] = consentWorld{
		cloud:      func(c provision.CloudAPI) provision.CloudAPI { return backupKeptCloud{c} },
		alreadyFor: []string{"backup_create"},
	}
}

func (p parityCloud) DeleteBackup(ctx context.Context, id string) error {
	if b, ok := p.CloudAPI.(provision.BackupAPI); ok {
		return b.DeleteBackup(ctx, id)
	}
	return nil
}

// RestoreBackup goes to the HTTP world when the parity cloud is the real
// adapter over it, as every other write.
func (p parityCloud) RestoreBackup(ctx context.Context, id string, cfg provision.RestoreDiskConfig) (map[string]any, error) {
	if b, ok := p.CloudAPI.(provision.BackupAPI); ok {
		return b.RestoreBackup(ctx, id, cfg)
	}
	return parityBackup(), nil
}
