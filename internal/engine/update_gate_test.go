// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/compose"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A repeated deploy updates the VM of the deployment only with consent, as
// the first one creates it (owner's decision Q1, spec REQ-12): MCP deploy
// always asked for confirmed=true, evo deploy did not. No terminal and no
// --yes is exit 3 and the VM is not touched; --non-interactive refuses on a
// terminal too; --yes and an answer "y" update it; an unchanged compose
// file asks nothing (there is nothing to do).
func TestDeploy_UpdateNeedsConsent(t *testing.T) {
	record := func(t *testing.T, eng *Engine, hash string) {
		t.Helper()
		sf := &state.File{Version: 1}
		sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "127.0.0.1", Region: "az-1",
			SSHUser: "user1", Status: "running", ComposeHash: hash, CreatedAt: time.Now(), UpdatedAt: time.Now()})
		if err := state.Write(state.DefaultPath(eng.StateDir), sf); err != nil {
			t.Fatal(err)
		}
	}
	uploaded := func(eng *Engine) bool {
		d := eng.Deployer.(*mockDeployer)
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.uploadCalled
	}

	t.Run("no terminal, no --yes: exit 3, VM untouched", func(t *testing.T) {
		eng, cloud, opts, _, stderr := newDeployForConfirm(t)
		record(t, eng, "old")
		withPromptStdin(t, strings.NewReader("y\n"), false)
		err := eng.Deploy(context.Background(), opts)
		if got := clerrors.ExitCode(err); got != clerrors.ExitConfirmationRequired {
			t.Fatalf("exit code = %d (err %v), want 3", got, err)
		}
		if uploaded(eng) || cloud.createCalled {
			t.Fatal("the VM was updated without consent")
		}
		for _, want := range []string{"About to update VM vm-1", "upload the compose file to the existing VM", "no new resources are created"} {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("preview lacks %q:\n%s", want, stderr.String())
			}
		}
	})
	t.Run("--non-interactive on a terminal: exit 3", func(t *testing.T) {
		eng, _, opts, _, _ := newDeployForConfirm(t)
		record(t, eng, "old")
		withPromptStdin(t, strings.NewReader("y\n"), true)
		opts.NonInteractive = true
		if got := clerrors.ExitCode(eng.Deploy(context.Background(), opts)); got != clerrors.ExitConfirmationRequired || uploaded(eng) {
			t.Fatalf("exit code = %d, uploaded = %v; want 3 and no upload", got, uploaded(eng))
		}
	})
	t.Run("--yes: updated", func(t *testing.T) {
		eng, cloud, opts, _, stderr := newDeployForConfirm(t)
		record(t, eng, "old")
		withPromptStdin(t, strings.NewReader(""), false)
		opts.Yes = true
		if err := eng.Deploy(context.Background(), opts); err != nil {
			t.Fatalf("deploy --yes: %v", err)
		}
		if !uploaded(eng) || cloud.createCalled || !strings.Contains(stderr.String(), "Confirmed: update VM vm-1") {
			t.Fatalf("uploaded = %v, created = %v:\n%s", uploaded(eng), cloud.createCalled, stderr.String())
		}
	})
	t.Run("terminal, answer y: updated", func(t *testing.T) {
		eng, _, opts, _, stderr := newDeployForConfirm(t)
		record(t, eng, "old")
		withPromptStdin(t, strings.NewReader("y\n"), true)
		if err := eng.Deploy(context.Background(), opts); err != nil {
			t.Fatalf("deploy: %v", err)
		}
		if !uploaded(eng) || !strings.Contains(stderr.String(), "[y/N]") {
			t.Fatalf("uploaded = %v, prompt:\n%s", uploaded(eng), stderr.String())
		}
	})
	t.Run("--force on an unchanged compose file: asked too", func(t *testing.T) {
		eng, _, opts, _, stderr := newDeployForConfirm(t)
		p, err := parseCompose(opts.ComposeFile)
		if err != nil {
			t.Fatal(err)
		}
		record(t, eng, p)
		withPromptStdin(t, strings.NewReader(""), false)
		opts.Force = true
		if got := clerrors.ExitCode(eng.Deploy(context.Background(), opts)); got != clerrors.ExitConfirmationRequired || uploaded(eng) {
			t.Fatalf("exit code = %d, uploaded = %v; want 3", got, uploaded(eng))
		}
		if !strings.Contains(stderr.String(), "the compose file has not changed: redeployed because of --force") {
			t.Errorf("no --force line in the preview:\n%s", stderr.String())
		}
	})
	t.Run("unchanged compose file after a failed run: a retry, asked", func(t *testing.T) {
		eng, _, opts, _, stderr := newDeployForConfirm(t)
		p, err := parseCompose(opts.ComposeFile)
		if err != nil {
			t.Fatal(err)
		}
		record(t, eng, p)
		sf, _ := state.Read(state.DefaultPath(eng.StateDir))
		rec := sf.ActiveDeployment()
		rec.Status = "ssh_timeout"
		sf.UpsertDeployment(*rec)
		if err := state.Write(state.DefaultPath(eng.StateDir), sf); err != nil {
			t.Fatal(err)
		}
		withPromptStdin(t, strings.NewReader(""), false)
		if got := clerrors.ExitCode(eng.Deploy(context.Background(), opts)); got != clerrors.ExitConfirmationRequired || uploaded(eng) {
			t.Fatalf("exit code = %d, uploaded = %v; want 3", got, uploaded(eng))
		}
		if !strings.Contains(stderr.String(), "the last deploy did not finish (ssh_timeout), this one retries it") {
			t.Errorf("no retry line in the preview:\n%s", stderr.String())
		}
	})
	t.Run("unchanged compose file: nothing to do, nothing asked", func(t *testing.T) {
		eng, _, opts, stdout, _ := newDeployForConfirm(t)
		p, err := parseCompose(opts.ComposeFile)
		if err != nil {
			t.Fatal(err)
		}
		record(t, eng, p)
		withPromptStdin(t, strings.NewReader(""), false)
		if err := eng.Deploy(context.Background(), opts); err != nil {
			t.Fatalf("unchanged: %v", err)
		}
		if uploaded(eng) || !strings.Contains(stdout.String(), "No changes detected") {
			t.Fatalf("uploaded = %v:\n%s", uploaded(eng), stdout.String())
		}
	})
}

// DeployUpdateOf is the update the deploy tool previews: the record, its
// project, whether the compose file changed, what it creates — and
// "unchanged" exactly when Deploy would skip.
func TestDeployUpdateOf(t *testing.T) {
	eng, _, opts, _, _ := newDeployForConfirm(t)
	eng.Config.ComposeFile = opts.ComposeFile
	if eng.Config.ProjectID == "" {
		eng.Config.ProjectID = "p-1"
	}
	if u := eng.DeployUpdateOf(false); u != nil {
		t.Fatalf("no deployment: %+v", u)
	}
	hash, err := parseCompose(opts.ComposeFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, hash, status, ip string
		force                  bool
		changed, unchanged     bool
		creates                string
	}{
		{"changed", "old", "running", "127.0.0.1", false, true, false, ""},
		{"same, running", hash, "running", "127.0.0.1", false, false, true, ""},
		{"same, --force", hash, "running", "127.0.0.1", true, false, false, ""},
		{"same, failed before", hash, "ssh_timeout", "127.0.0.1", false, false, false, ""},
		{"no public IP", "old", "running", "", false, true, false, "floating_ip"},
	} {
		sf := &state.File{Version: 1}
		sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: c.ip, Status: c.status, ComposeHash: c.hash, ProjectID: eng.Config.ProjectID})
		if err := state.Write(state.DefaultPath(eng.StateDir), sf); err != nil {
			t.Fatal(err)
		}
		u := eng.DeployUpdateOf(c.force)
		if u == nil || u.ComposeChanged == nil || *u.ComposeChanged != c.changed || u.Unchanged != c.unchanged ||
			u.ProjectID != eng.Config.ProjectID || u.Deployment.VMID != "vm-1" || strings.Join(u.Creates, ",") != c.creates {
			t.Errorf("%s: %+v", c.name, u)
		}
	}
	// A record of another project: Deploy refuses it before it would skip.
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "127.0.0.1", Status: "running", ComposeHash: hash, ProjectID: "p-other"})
	if err := state.Write(state.DefaultPath(eng.StateDir), sf); err != nil {
		t.Fatal(err)
	}
	if u := eng.DeployUpdateOf(false); u == nil || u.Unchanged || u.ProjectID != "p-other" {
		t.Errorf("another project: %+v", u)
	}
}

// parseCompose is the hash deploy records for the compose file at path.
func parseCompose(path string) (string, error) {
	p, err := compose.Parse(path)
	if err != nil {
		return "", err
	}
	return p.Hash, nil
}
