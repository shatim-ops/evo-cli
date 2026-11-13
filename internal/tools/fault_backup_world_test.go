// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// backupCloud is faultCloud with the backups of the project
// (provision.BackupAPI): the matrix world of backup_create (release plan
// 1.9, wave В5). Its reads and its create go through the same gate as the
// rest of the fake, so every fault of the matrix lands on them: a create
// whose answer is lost keeps the backup — it is billed — and one whose
// answer has no id keeps it too.
type backupCloud struct {
	*faultCloud
	bmu     sync.Mutex
	backups []map[string]any
	// disks are the disks restores made: billed, and listed by the disk
	// list of the world.
	disks []map[string]any
}

// ListDisks lists the disks the restores made.
func (c *backupCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	if err := c.gate("ListDisks"); err != nil {
		return nil, err
	}
	c.bmu.Lock()
	defer c.bmu.Unlock()
	return append([]map[string]any(nil), c.disks...), nil
}

// RestoreBackup makes a new disk of the backup, through the gate as the
// rest: a restore whose answer is lost made its disk all the same.
func (c *backupCloud) RestoreBackup(_ context.Context, id string, cfg provision.RestoreDiskConfig) (map[string]any, error) {
	err := c.gate("RestoreBackup")
	if err != nil && !errors.Is(err, errFaultLost) && !errors.Is(err, errFaultEmpty) {
		return nil, err
	}
	c.bmu.Lock()
	c.disks = append(c.disks, map[string]any{"id": faultID(0x70 + len(c.disks)), "name": cfg.Name, "state": "available", "size": float64(10)})
	c.bmu.Unlock()
	if errors.Is(err, errFaultLost) {
		return nil, lostAnswer(err, clerrors.New(clerrors.CodeAPIUnreachable, "lost response", "network"))
	}
	return map[string]any{"id": id, "state": "created"}, nil
}

func (c *backupCloud) ListBackups(context.Context, string) ([]map[string]any, error) {
	if err := c.gate("ListBackups"); err != nil {
		return nil, err
	}
	c.bmu.Lock()
	defer c.bmu.Unlock()
	return append([]map[string]any(nil), c.backups...), nil
}

func (c *backupCloud) GetBackup(_ context.Context, id string) (map[string]any, error) {
	if err := c.gate("GetBackup"); err != nil {
		return nil, err
	}
	c.bmu.Lock()
	defer c.bmu.Unlock()
	for _, b := range c.backups {
		if b["id"] == id {
			return b, nil
		}
	}
	return nil, clerrors.New(clerrors.CodeNotFound, "backup not found", "")
}

// DeleteBackup takes the backup away, through the gate as the rest.
func (c *backupCloud) DeleteBackup(_ context.Context, id string) error {
	if err := c.gate("DeleteBackup"); err != nil {
		return err
	}
	c.bmu.Lock()
	defer c.bmu.Unlock()
	c.backups = slices.DeleteFunc(c.backups, func(b map[string]any) bool { return b["id"] == id })
	return nil
}

func (c *backupCloud) CreateBackup(_ context.Context, req provision.CreateBackupReq) (map[string]any, error) {
	err := c.gate("CreateBackup")
	if err != nil && !errors.Is(err, errFaultLost) && !errors.Is(err, errFaultEmpty) {
		return nil, err
	}
	c.bmu.Lock()
	b := map[string]any{
		"id": faultID(0x60 + len(c.backups)), "name": req.Name, "state": "created", "size": float64(1024),
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"resource":   map[string]any{"resource_id": req.Resource.ResourceID, "resource_type": req.Resource.ResourceType},
	}
	c.backups = append(c.backups, b)
	c.bmu.Unlock()
	if errors.Is(err, errFaultLost) {
		return nil, lostAnswer(err, clerrors.New(clerrors.CodeAPIUnreachable, "lost response", "network"))
	}
	if errors.Is(err, errFaultEmpty) {
		return map[string]any{"name": req.Name}, nil
	}
	return b, nil
}

// backupFaultWorld is backupCloud as a matrix world: the backups it holds are
// billed, each known by its id.
type backupFaultWorld struct{ *backupCloud }

func (w backupFaultWorld) billed() []faultBilled {
	w.bmu.Lock()
	defer w.bmu.Unlock()
	var out []faultBilled
	for _, b := range w.backups {
		if b["seed"] == true {
			continue // there before the call: not its own
		}
		id := fmt.Sprint(b["id"])
		out = append(out, faultBilled{what: "backup " + id, names: []string{id}})
	}
	for _, d := range w.disks {
		id := fmt.Sprint(d["id"])
		out = append(out, faultBilled{what: "disk " + id, names: []string{id}})
	}
	return out
}

// backupCase is a case of the backup world: the source is a disk of the
// compute fake, which reads any id.
func backupCase(tool string, args map[string]any) faultMatrixCase {
	return faultMatrixCase{tool: tool, args: args, world: func(t *testing.T, cancel context.CancelFunc) (faultWorld, *Registry, string) {
		cloud := &backupCloud{faultCloud: &faultCloud{cancel: cancel}}
		r, dir := faultToolsRegistryOver(t, cloud)
		return backupFaultWorld{cloud}, r, dir
	}}
}

// faultBackupID is the backup of a disk the restore world holds before the
// call: created, of the compute fake's disk.
var faultBackupID = faultID(0x5f)

// backupRestoreCase is a case of the backup world with a created backup of
// a disk to restore from.
func backupRestoreCase(args map[string]any) faultMatrixCase {
	return faultMatrixCase{tool: "backup_restore", args: args, world: func(t *testing.T, cancel context.CancelFunc) (faultWorld, *Registry, string) {
		cloud := &backupCloud{faultCloud: &faultCloud{cancel: cancel}}
		cloud.backups = []map[string]any{{"id": faultBackupID, "name": "before-change", "state": "created", "seed": true,
			"resource": map[string]any{"resource_id": faultDisk, "resource_type": "disk", "resource_exists": true,
				"resource_state": map[string]any{"name": "data", "size": float64(10), "disk_type": map[string]any{"name": "SSD"},
					"availability_zone": map[string]any{"id": faultZone, "name": "ru.AZ-1"}}}}}
		r, dir := faultToolsRegistryOver(t, cloud)
		return backupFaultWorld{cloud}, r, dir
	}}
}
