// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// D62 (#622, option A): a deployment records the compose file it runs, and
// deploy of another one — an MCP server started in one directory, an agent
// on another project — is refused before the question (USAGE, exit code 1)
// naming both, with nothing sent to the VM. A record from before 1.6 names
// none: deploy warns and records it. A VM of provision is not deploy's: a
// deploy next to it makes its own VM.
func TestDeploy_AnotherProjectsComposeIsRefused(t *testing.T) {
	write := func(t *testing.T, dir string, rec state.DeploymentRecord) {
		t.Helper()
		sf := &state.File{Version: 1}
		sf.UpsertDeployment(rec)
		if err := state.Write(state.DefaultPath(dir), sf); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("another compose file", func(t *testing.T) {
		cloud := &mockCloudAPI{}
		dep := newMockDeployer()
		eng, dir := newTestEngine(t, cloud, dep)
		rec := runningRecord()
		rec.ComposePath = filepath.Join(t.TempDir(), "other", "docker-compose.yml")
		write(t, dir, rec)
		opts := newDeployOpts(t, dir)
		err := eng.Deploy(context.Background(), opts)
		if clerrors.Reason(err) != clerrors.CodeUsage || !strings.Contains(err.Error(), rec.ComposePath) || !strings.Contains(err.Error(), composePath(opts.ComposeFile)) {
			t.Fatalf("Deploy = %v, want USAGE naming both compose files", err)
		}
		if dep.connectCalls != 0 || cloud.createCalled {
			t.Errorf("deploy went on: connects=%d create=%v", dep.connectCalls, cloud.createCalled)
		}
		eng.Config.ComposeFile = opts.ComposeFile
		if _, err := eng.DeployComposeCheck(); clerrors.Reason(err) != clerrors.CodeUsage {
			t.Errorf("the deploy tool's check before the question: %v, want USAGE", err)
		}
	})

	t.Run("a record from before 1.6", func(t *testing.T) {
		cloud := &mockCloudAPI{}
		eng, dir := newTestEngine(t, cloud, newMockDeployer())
		var out bytes.Buffer
		eng.Printer = output.NewWithWriters(&out, &out, output.ModePlain, false)
		write(t, dir, runningRecord())
		opts := newDeployOpts(t, dir)
		opts.Force = true
		if err := eng.Deploy(context.Background(), opts); err != nil {
			t.Fatalf("Deploy: %v", err)
		}
		if !strings.Contains(out.String(), "does not record its compose file") {
			t.Errorf("no warning of the record without a compose file:\n%s", out.String())
		}
		sf, _ := state.Read(state.DefaultPath(dir))
		if got := sf.ActiveDeployment().ComposePath; got != composePath(opts.ComposeFile) {
			t.Errorf("the record keeps compose_path %q, want %q", got, composePath(opts.ComposeFile))
		}
		eng.Config.ComposeFile = opts.ComposeFile
		if w, err := eng.DeployComposeCheck(); err != nil || w != "" {
			t.Errorf("after the record: warning %q, error %v; want none", w, err)
		}
	})

	t.Run("a VM of provision", func(t *testing.T) {
		cloud := &mockCloudAPI{}
		eng, dir := newTestEngine(t, cloud, newMockDeployer())
		rec := runningRecord()
		rec.Source = state.SourceProvision
		rec.ComposePath = ""
		write(t, dir, rec)
		if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
			t.Fatalf("Deploy: %v", err)
		}
		if !cloud.createCalled {
			t.Errorf("deploy updated the VM of provision instead of making its own")
		}
	})
}
