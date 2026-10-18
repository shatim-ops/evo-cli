// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

const projectA = "00000000-0000-0000-0000-00000000000a"

// A new deployment records the project its VM was created in.
func TestDeploy_RecordsProject(t *testing.T) {
	eng, dir := newTestEngine(t, &mockCloudAPI{}, newMockDeployer())
	opts := DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small", Region: "az-1", NonInteractive: true, Yes: true}
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	a := readActive(t, dir)
	if a == nil || a.ProjectID != eng.Config.ProjectID {
		t.Fatalf("record %+v, want project_id %s", a, eng.Config.ProjectID)
	}
}

// destroy works in the deployment's project, not the one the config names
// now: with the config on project B, the FIP of project A is released in A
// (QA P1). A 404 from B used to read as "already released".
func TestDestroy_UsesRecordedProject(t *testing.T) {
	rec := runningRecord()
	rec.ProjectID = projectA
	eng, cloud, _ := destroyFixture(t, rec)
	var gotProject string
	cloud.deleteFloatingIPFn = func(_ context.Context, _, projectID string) error {
		gotProject = projectID
		return nil
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if gotProject != projectA {
		t.Errorf("DeleteFloatingIP project = %q, want the recorded %s", gotProject, projectA)
	}
}

// Without floating_ip_id the FIP is looked up in the recorded project too.
func TestDestroy_LooksUpFIPInRecordedProject(t *testing.T) {
	rec := runningRecord()
	rec.FloatingIPID = ""
	rec.ProjectID = projectA
	eng, cloud, _ := destroyFixture(t, rec)
	var listed string
	cloud.listFloatingIPsFn = func(_ context.Context, projectID string) ([]*provision.FloatingIP, error) {
		listed = projectID
		return []*provision.FloatingIP{{ID: "fip-a", IPAddress: rec.VMIP}}, nil
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if listed != projectA || len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-a" {
		t.Errorf("listed in %q, released %v; want %s and [fip-a]", listed, cloud.deletedFIPs, projectA)
	}
}

// Records from older versions have no project: the configured one is used,
// as before.
func TestDestroy_OldRecordUsesConfiguredProject(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, runningRecord())
	var gotProject string
	cloud.deleteFloatingIPFn = func(_ context.Context, _, projectID string) error {
		gotProject = projectID
		return nil
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if gotProject != eng.Config.ProjectID {
		t.Errorf("DeleteFloatingIP project = %q, want the configured %s", gotProject, eng.Config.ProjectID)
	}
}

// A re-run of deploy with another project configured stops: the compose
// file would go to project A's VM and its ports would be opened with
// project B (QA P3).
func TestDeploy_ExistingDeploymentInOtherProject(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, dir := newTestEngine(t, cloud, dep)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{
		ID: "vm-a", VMID: "vm-a", VMIP: "198.51.100.10", ProjectID: projectA,
		SSHUser: "user1", Status: "running", ComposeHash: "old", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	opts := DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small", NonInteractive: true, Yes: true}
	err := eng.Deploy(context.Background(), opts)
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || cliErr.Code != clerrors.CodeConfigInvalid {
		t.Fatalf("Deploy = %v, want CONFIG_INVALID", err)
	}
	dep.mu.Lock()
	connected := dep.connectCalled
	dep.mu.Unlock()
	if connected || cloud.createCalled {
		t.Error("deploy touched a VM despite the project mismatch")
	}

	// The recorded project itself is fine.
	eng.Config.ProjectID = projectA
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("Deploy with the recorded project: %v", err)
	}
}

// The previews name the project: with a key that sees several projects the
// person approving must see which one is changed (QA P6).
func TestPreviews_NameTheProject(t *testing.T) {
	eng, _, opts, stdout, _ := newDeployForConfirm(t)
	opts.DryRun = true
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if want := "project:   " + eng.Config.ProjectID; !strings.Contains(stdout.String(), want) {
		t.Errorf("deploy preview lacks %q:\n%s", want, stdout.String())
	}

	rec := runningRecord()
	rec.ProjectID = projectA
	p := destroyPreview(&rec, &state.File{}, nil, projectA, originRecord)
	if want := "project:   " + projectA + " (where the deployment was created)"; len(p.Lines) == 0 || p.Lines[0] != want {
		t.Errorf("destroy preview lines = %q, want %q first", p.Lines, want)
	}
	if d, _ := p.Details.(map[string]any); d["project_id"] != projectA || d["project_from"] != originRecord {
		t.Errorf("destroy preview details = %v, want project_id", p.Details)
	}
}

// The preview says where the project comes from (best practice from
// 'aws configure list'; none of the compared CLIs does it in a preview).
func TestPreview_ProjectSource(t *testing.T) {
	eng, _, opts, stdout, _ := newDeployForConfirm(t)
	eng.Config.ProjectOrigin = config.OriginEnv
	opts.DryRun = true
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if want := eng.Config.ProjectID + " (from CLOUDRU_PROJECT_ID)"; !strings.Contains(stdout.String(), want) {
		t.Errorf("deploy preview lacks %q:\n%s", want, stdout.String())
	}
}
