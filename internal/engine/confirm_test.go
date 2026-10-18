// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// newDeployForConfirm returns an engine with separate stdout/stderr and a
// compose file, ready for a first (VM-creating) deploy.
func newDeployForConfirm(t *testing.T) (*Engine, *mockCloudAPI, DeployOptions, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cloud := &mockCloudAPI{}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	var stdout, stderr bytes.Buffer
	eng.Printer = output.NewWithWriters(&stdout, &stderr, output.ModePlain, false)
	opts := DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small", Region: "az-1"}
	return eng, cloud, opts, &stdout, &stderr
}

func TestDeploy_DryRun_PreviewWithCostNoVM(t *testing.T) {
	eng, cloud, opts, stdout, _ := newDeployForConfirm(t)
	opts.DryRun = true

	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if cloud.createCalled {
		t.Fatal("--dry-run created a VM")
	}
	out := stdout.String()
	for _, want := range []string{"Dry run: would create VM", "small (2 vCPU, 2 GB RAM)", "ru-moscow-1", "Ubuntu 22.04", "10 GB", "RUB/month", costNote} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
	sf, _ := state.Read(state.DefaultPath(eng.StateDir))
	if len(sf.Deployments) != 0 {
		t.Errorf("--dry-run wrote state: %+v", sf.Deployments)
	}
}

func TestDeploy_NoTTYNoYes_Code3NoVM(t *testing.T) {
	eng, cloud, opts, _, stderr := newDeployForConfirm(t)
	withPromptStdin(t, strings.NewReader("\n"), false)

	err := eng.Deploy(context.Background(), opts)
	if got := clerrors.ExitCode(err); got != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit code = %d (err %v), want 3", got, err)
	}
	if cloud.createCalled {
		t.Fatal("VM created without consent")
	}
	if !strings.Contains(stderr.String(), "About to create VM") || !strings.Contains(stderr.String(), costNote) {
		t.Errorf("preview missing from stderr:\n%s", stderr.String())
	}
}

func TestDeploy_NonInteractiveWithoutYes_Code3(t *testing.T) {
	eng, cloud, opts, _, _ := newDeployForConfirm(t)
	withPromptStdin(t, strings.NewReader("y\n"), true) // a terminal, but prompts are off
	opts.NonInteractive = true

	err := eng.Deploy(context.Background(), opts)
	if got := clerrors.ExitCode(err); got != clerrors.ExitConfirmationRequired || cloud.createCalled {
		t.Fatalf("exit code = %d, created = %v; want 3 and no VM", got, cloud.createCalled)
	}
}

func TestDeploy_Yes_CreatesWithOneLinePreview(t *testing.T) {
	eng, cloud, opts, _, stderr := newDeployForConfirm(t)
	withPromptStdin(t, strings.NewReader(""), false)
	opts.Yes = true

	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("deploy --yes: %v", err)
	}
	if !cloud.createCalled {
		t.Fatal("--yes did not create the VM")
	}
	if !strings.Contains(stderr.String(), "Confirmed: create VM") {
		t.Errorf("one-line preview missing from stderr:\n%s", stderr.String())
	}
}

func TestDeploy_TTYAnswerYes_Creates(t *testing.T) {
	eng, cloud, opts, _, stderr := newDeployForConfirm(t)
	withPromptStdin(t, strings.NewReader("y\n"), true)

	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !cloud.createCalled || !strings.Contains(stderr.String(), "[y/N]") {
		t.Fatalf("created = %v, prompt:\n%s", cloud.createCalled, stderr.String())
	}
}

func TestDeploy_DryRunOnExistingVM_TouchesNothing(t *testing.T) {
	eng, cloud, opts, stdout, _ := newDeployForConfirm(t)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "203.0.113.5", Status: "running",
		ComposeHash: "old", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err := state.Write(state.DefaultPath(eng.StateDir), sf); err != nil {
		t.Fatal(err)
	}
	eng.Auth = nil // any cloud access panics
	opts.DryRun = true

	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if cloud.createCalled || !strings.Contains(stdout.String(), "would update VM vm-1") {
		t.Fatalf("created = %v, out:\n%s", cloud.createCalled, stdout.String())
	}
}

func TestDestroy_DryRun_ListsResourcesTouchesNothing(t *testing.T) {
	e, buf := destroyEngineWithVM(t)
	sf, _ := state.Read(state.DefaultPath(e.StateDir))
	rec := sf.ActiveDeployment()
	rec.VMIP = "203.0.113.7"
	sf.UpsertDeployment(*rec)
	_ = state.Write(state.DefaultPath(e.StateDir), sf)

	if err := e.Destroy(context.Background(), DestroyOptions{DryRun: true}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	for _, want := range []string{"Dry run: would destroy VM vm-1", "release floating IP 203.0.113.7", "local state"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("preview lacks %q:\n%s", want, buf.String())
		}
	}
	assertVMStillInState(t, e)
}

func TestDestroy_JSONNoTTY_StatusOnStdout(t *testing.T) {
	e, _ := destroyEngineWithVM(t)
	var stdout, stderr bytes.Buffer
	e.Printer = output.NewWithWriters(&stdout, &stderr, output.ModeJSON, false)
	withPromptStdin(t, strings.NewReader(""), false)

	err := e.Destroy(context.Background(), DestroyOptions{})
	if got := clerrors.ExitCode(err); got != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit code = %d, want 3", got)
	}
	var env struct {
		Status  string `json:"status"`
		Preview struct {
			Action  string `json:"action"`
			Details struct {
				Resources []destroyResource `json:"resources"`
			} `json:"details"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not JSON: %q", stdout.String())
	}
	if env.Status != "confirmation_required" || env.Preview.Action != "destroy" {
		t.Fatalf("got %+v", env)
	}
	if len(env.Preview.Details.Resources) == 0 || env.Preview.Details.Resources[0].ID != "vm-1" {
		t.Errorf("resources = %+v", env.Preview.Details.Resources)
	}
	assertVMStillInState(t, e)
}

func TestDestroy_NothingToDestroy_NoTTY_IsSuccess(t *testing.T) {
	var buf bytes.Buffer
	e := &Engine{Printer: output.NewWithWriter(&buf, output.ModePlain, false), StateDir: t.TempDir()}
	withPromptStdin(t, strings.NewReader(""), false)

	if err := e.Destroy(context.Background(), DestroyOptions{}); err != nil {
		t.Fatalf("nothing to destroy must not need consent: %v", err)
	}
}
