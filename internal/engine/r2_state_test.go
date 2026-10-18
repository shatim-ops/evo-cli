// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func r2Isolate(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{
		"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_KEY_SECRET", "CLOUDRU_PROJECT_ID",
		"CLOUDRU_API_KEY", "EVOLUTION_PROJECT_ID", "EVOLUTION_SERVICE_ACCOUNT_KEY_ID",
		"EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "MCP_BEARER_TOKEN", "NO_COLOR", "EVO_LANG",
	} {
		t.Setenv(k, "")
	}
}

// Cursor r2, R2-1: a repository may ship .evo as a symlink to another
// project's state. destroy followed it, deleted the VM recorded there and
// released its floating IP. Now it refuses, as the SSH key path does.
func TestDestroy_SymlinkedDataDirRefused(t *testing.T) {
	r2Isolate(t)
	if runtime.GOOS == "windows" {
		t.Skip("directory symlink is not a file mode Lstat can reject the same way")
	}
	const (
		vmID  = "00000000-0000-4000-8000-000000000010"
		fipID = "00000000-0000-4000-8000-0000000000f1"
		proj  = "00000000-0000-4000-8000-000000000001"
	)
	victim := t.TempDir()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{
		ID: vmID, VMID: vmID, VMIP: "203.0.113.10", FloatingIPID: fipID,
		ProjectID: proj, Status: "running",
	})
	if err := state.Write(filepath.Join(victim, "state.json"), sf); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.Symlink(victim, filepath.Join(project, ".evo")); err != nil {
		t.Skip("symlink: " + err.Error())
	}

	var deleted []string
	cloud := &mockCloudAPI{
		deleteVMFn: func(_ context.Context, id string) error {
			deleted = append(deleted, id)
			return nil
		},
		getVMFn: func(context.Context, string) (*provision.VM, error) {
			return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
		},
	}
	eng, _ := newTestEngine(t, cloud, newMockDeployer())
	eng.StateDir = project
	eng.Config.ProjectID = proj

	err := eng.Destroy(context.Background(), DestroyOptions{Force: true})
	if len(deleted) != 0 || len(cloud.deletedFIPs) != 0 {
		t.Fatalf("destroy followed .evo symlink and deleted VM %v, released FIP %v (err=%v)",
			deleted, cloud.deletedFIPs, err)
	}
	if clerrors.ExitCode(err) != clerrors.ExitAuth || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("want CONFIG_INVALID naming the symlink, got %v", err)
	}
}

// R2-2: two active records in one directory (provision next to a deploy).
// destroy without a VM deleted the first and answered "destroyed" while
// the second stayed billed. Now it refuses and names both VMs, before any
// cloud call and also with --dry-run.
func TestDestroy_SeveralActiveRefused(t *testing.T) {
	r2Isolate(t)
	const (
		vm1 = "00000000-0000-4000-8000-000000000031"
		vm2 = "00000000-0000-4000-8000-000000000032"
	)
	for _, dry := range []bool{false, true} {
		dir := t.TempDir()
		sf := &state.File{Version: 1}
		sf.UpsertDeployment(state.DeploymentRecord{ID: vm1, VMID: vm1, ProjectID: "p", Status: "running"})
		sf.UpsertDeployment(state.DeploymentRecord{ID: vm2, VMID: vm2, ProjectID: "p", Status: "creating"})
		sf.UpsertDeployment(state.DeploymentRecord{ID: "old", VMID: "old", ProjectID: "p", Status: "destroyed"})
		if err := state.Write(state.DefaultPath(dir), sf); err != nil {
			t.Fatal(err)
		}
		var calls int
		cloud := &mockCloudAPI{
			deleteVMFn: func(context.Context, string) error { calls++; return nil },
			getVMFn:    func(context.Context, string) (*provision.VM, error) { calls++; return nil, nil },
		}
		eng, _ := newTestEngine(t, cloud, newMockDeployer())
		eng.StateDir = dir
		err := eng.Destroy(context.Background(), DestroyOptions{Force: true, DryRun: dry})
		if clerrors.ExitCode(err) != clerrors.ExitAuth || !strings.Contains(err.Error(), vm1) || !strings.Contains(err.Error(), vm2) {
			t.Fatalf("dry=%v: want CONFIG_INVALID naming both VMs, got %v", dry, err)
		}
		if strings.Contains(err.Error(), "old") {
			t.Errorf("dry=%v: a destroyed record is listed: %v", dry, err)
		}
		if calls != 0 || len(cloud.deletedFIPs) != 0 {
			t.Errorf("dry=%v: %d cloud calls before the refusal", dry, calls)
		}
	}
}
