// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func breakIsolate(t *testing.T) {
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

// A repository may ship .evo as a symlink (the SSH key path refuses that:
// loadOrGenerateSSHKey / ensureKeyDir). destroy does not: it follows the
// link, deletes the VM recorded in the target and releases its floating IP.
func TestBreak_State_SymlinkDataDirDestroyFollows(t *testing.T) {
	breakIsolate(t)
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
		t.Fatalf("destroy followed .evo symlink and deleted VM %v, released FIP %v (err=%v); "+
			"a symlink data dir must be refused the way the SSH key is (ensureKeyDir)",
			deleted, cloud.deletedFIPs, err)
	}
	if err == nil {
		t.Fatal("destroy succeeded through a symlinked .evo; want a refusal and no cloud delete")
	}
}
