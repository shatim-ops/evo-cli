// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
)

func TestReadWrite_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	now := time.Now().Truncate(time.Second)
	original := &File{
		Version: 1,
		Project: "my-project",
		Deployments: []DeploymentRecord{
			{
				ID:           "dep-1",
				VMID:         "vm-abc",
				VMIP:         "10.0.0.1",
				VMFlavor:     "m1.small",
				Region:       "ru-1",
				Status:       "running",
				ComposeHash:  "sha256:aabbcc",
				ExposedPorts: []int{80, 443},
				CreatedAt:    now,
				UpdatedAt:    now,
			},
		},
	}

	if err := Write(path, original); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if got.Version != original.Version {
		t.Errorf("Version = %d, want %d", got.Version, original.Version)
	}
	if got.Project != original.Project {
		t.Errorf("Project = %q, want %q", got.Project, original.Project)
	}
	if len(got.Deployments) != 1 {
		t.Fatalf("Deployments count = %d, want 1", len(got.Deployments))
	}

	d := got.Deployments[0]
	if d.ID != "dep-1" {
		t.Errorf("ID = %q, want %q", d.ID, "dep-1")
	}
	if d.VMID != "vm-abc" {
		t.Errorf("VMID = %q, want %q", d.VMID, "vm-abc")
	}
	if d.VMIP != "10.0.0.1" {
		t.Errorf("VMIP = %q, want %q", d.VMIP, "10.0.0.1")
	}
	if d.Status != "running" {
		t.Errorf("Status = %q, want %q", d.Status, "running")
	}
	if len(d.ExposedPorts) != 2 || d.ExposedPorts[0] != 80 || d.ExposedPorts[1] != 443 {
		t.Errorf("ExposedPorts = %v, want [80 443]", d.ExposedPorts)
	}
}

func TestRead_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent", "state.json")

	f, err := Read(path)
	if err != nil {
		t.Fatalf("Read returned error for missing file: %v", err)
	}
	if f.Version != 1 {
		t.Errorf("Version = %d, want 1", f.Version)
	}
	if len(f.Deployments) != 0 {
		t.Errorf("Deployments should be empty, got %d", len(f.Deployments))
	}
}

func TestRead_CorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := os.WriteFile(path, []byte("{invalid json!!!"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}

	_, err := Read(path)
	if err == nil {
		t.Fatal("Read should return error for corrupted JSON")
	}

	var cliErr *clierr.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error should be *CLIError, got %T", err)
	}
	if cliErr.Code != clierr.CodeStateCorrupted {
		t.Errorf("Code = %q, want %q", cliErr.Code, clierr.CodeStateCorrupted)
	}
}

func TestWrite_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "deep", "state.json")

	f := &File{Version: 1, Deployments: []DeploymentRecord{}}
	if err := Write(path, f); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should exist after Write: %v", err)
	}
}

func TestWrite_AtomicRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	f := &File{Version: 1, Deployments: []DeploymentRecord{}}
	if err := Write(path, f); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// After a successful write, the .tmp file should not remain.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp file should not exist after successful write")
	}

	// The final file must exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state file should exist: %v", err)
	}
}

func TestActiveDeployment(t *testing.T) {
	f := &File{
		Version: 1,
		Deployments: []DeploymentRecord{
			{ID: "dep-1", Status: "destroyed"},
			{ID: "dep-2", Status: "running"},
			{ID: "dep-3", Status: "creating"},
		},
	}

	active := f.ActiveDeployment()
	if active == nil {
		t.Fatal("ActiveDeployment returned nil, want dep-2")
	}
	if active.ID != "dep-2" {
		t.Errorf("ActiveDeployment ID = %q, want %q", active.ID, "dep-2")
	}
}

func TestActiveDeployment_None(t *testing.T) {
	f := &File{
		Version: 1,
		Deployments: []DeploymentRecord{
			{ID: "dep-1", Status: "destroyed"},
			{ID: "dep-2", Status: "destroyed"},
		},
	}

	if active := f.ActiveDeployment(); active != nil {
		t.Errorf("ActiveDeployment should be nil when all destroyed, got %q", active.ID)
	}
}

func TestUpsertDeployment(t *testing.T) {
	f := &File{Version: 1, Deployments: []DeploymentRecord{}}

	// Add new.
	f.UpsertDeployment(DeploymentRecord{ID: "dep-1", Status: "creating", VMIP: "10.0.0.1"})
	if len(f.Deployments) != 1 {
		t.Fatalf("Deployments count = %d, want 1", len(f.Deployments))
	}
	if f.Deployments[0].VMIP != "10.0.0.1" {
		t.Errorf("VMIP = %q, want %q", f.Deployments[0].VMIP, "10.0.0.1")
	}

	// Update existing.
	f.UpsertDeployment(DeploymentRecord{ID: "dep-1", Status: "running", VMIP: "10.0.0.2"})
	if len(f.Deployments) != 1 {
		t.Fatalf("Deployments count = %d after update, want 1", len(f.Deployments))
	}
	if f.Deployments[0].Status != "running" {
		t.Errorf("Status = %q, want %q", f.Deployments[0].Status, "running")
	}
	if f.Deployments[0].VMIP != "10.0.0.2" {
		t.Errorf("VMIP = %q after update, want %q", f.Deployments[0].VMIP, "10.0.0.2")
	}
}

func TestRemoveDeployment(t *testing.T) {
	f := &File{
		Version: 1,
		Deployments: []DeploymentRecord{
			{ID: "dep-1", Status: "running"},
			{ID: "dep-2", Status: "destroyed"},
			{ID: "dep-3", Status: "creating"},
		},
	}

	f.RemoveDeployment("dep-2")

	if len(f.Deployments) != 2 {
		t.Fatalf("Deployments count = %d, want 2", len(f.Deployments))
	}
	if f.FindDeployment("dep-2") != nil {
		t.Error("dep-2 should have been removed")
	}
	if f.FindDeployment("dep-1") == nil {
		t.Error("dep-1 should still exist")
	}
	if f.FindDeployment("dep-3") == nil {
		t.Error("dep-3 should still exist")
	}
}
