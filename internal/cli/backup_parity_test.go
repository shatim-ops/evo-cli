// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The backups of the parity world (provision.BackupAPI): one backup of the
// world's VM, so that the command and the tool answer the same card.
var _ provision.BackupAPI = parityCloud{}

func (parityCloud) ListBackups(context.Context, string) ([]map[string]any, error) {
	return []map[string]any{parityBackup()}, nil
}

func (parityCloud) GetBackup(_ context.Context, id string) (map[string]any, error) {
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

func (parityCloud) CreateBackup(_ context.Context, req provision.CreateBackupReq) (map[string]any, error) {
	b := parityBackup()
	b["name"], b["state"] = req.Name, "created"
	return b, nil
}
