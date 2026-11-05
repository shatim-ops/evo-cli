// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// D83 (owner's decision 05.11, #941): destroy without a target in a folder
// with several records — provision's VM next to deploy's — refuses before
// the question with the code it gave after it, CONFIG_INVALID (2), with
// and without consent, on every surface; nothing is deleted. Without
// --yes, evo tools call destroy gave 3 (the question) — the breaking part,
// «Миграция» 1.8.
func TestRun_DestroySeveralRecordsIsConfigInvalidBeforeTheQuestion(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	t.Setenv("CLOUDRU_API_KEY", "")
	oldTTY := confirm.StdinIsTTY
	confirm.StdinIsTTY = func() bool { return false }
	t.Cleanup(func() { confirm.StdinIsTTY = oldTTY })

	now := time.Now()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-a", VMID: "vm-a", Status: "running", Source: state.SourceProvision, CreatedAt: now})
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-b", VMID: "vm-b", Status: "running", ComposePath: "/srv/app/docker-compose.yml", CreatedAt: now})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	want := clerrors.ExitCode(clerrors.New(clerrors.CodeConfigInvalid, "m", ""))
	for _, args := range [][]string{
		{"destroy"},
		{"destroy", "--yes"},
		{"tools", "call", "destroy"},
		{"tools", "call", "destroy", "--yes"},
	} {
		if got := run(args); got != want {
			t.Errorf("run(%v) = %d, want %d (CONFIG_INVALID, before the question)", args, got, want)
		}
	}
	after, _ := state.Read(state.DefaultPath(dir))
	if n := len(after.ActiveDeployments()); n != 2 {
		t.Fatalf("records changed: %d active", n)
	}
}
