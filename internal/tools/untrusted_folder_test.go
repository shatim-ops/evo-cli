// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/state"
)

type markedWorkload struct{ faultDeployer }

func (*markedWorkload) ExecStream(_ context.Context, _ string, stdout, _ io.Writer) error {
	_, err := io.WriteString(stdout, "workload\n"+cloudMark)
	return err
}

// Exercise the same handler from an empty folder and from a folder holding a
// deployment, plan or job. The marker comes from a VM, workload, catalogue
// label or operation result. It is never supplied as a tool argument.
func checkFolderCloudValue(t *testing.T, tool string) {
	// The engine has a fake CloudAPI; service clients reached by helpers
	// also stay inside this world.
	savedTransport := httpretry.DefaultBase
	httpretry.DefaultBase = &oddAPI{status: 403, body: []byte(`{"message":"fixture refuses unconfigured reads"}`)}
	t.Cleanup(func() { httpretry.DefaultBase = savedTransport })
	vmID := faultID(0x42)
	value := "from-" + tool + "\n" + cloudMark
	cloud := &faultCloud{vms: []faultVM{{ID: vmID, Name: value, Status: "running"}}}
	r, dir := faultToolsRegistry(t, cloud)
	compose := writeTestCompose(t)
	factory := r.newEngine
	r.newEngine = func(overrides map[string]any) (*engine.Engine, error) {
		e, err := factory(overrides)
		if err == nil {
			e.Config.ComposeFile = compose
			e.Deployer = &markedWorkload{}
		}
		return e, err
	}
	call, _ := r.Lookup(tool)
	args := map[string]any{}
	switch tool {
	case "deploy_apply":
		args["plan_id"] = "plan-0123456789ab"
	case "stack_apply", "stack_status", "stack_destroy":
		args["job_id"] = "stk-0123456789ab"
	}
	// deploy with only a compose file can ask to create a VM. The other
	// tools have no record to read. Neither path can see the stored marker.
	if text, _ := call.Handler(context.Background(), args); strings.Contains(text, cloudMark) {
		t.Fatalf("empty folder exposed stored marker: %s", text)
	}
	switch tool {
	case "deploy", "status", "logs":
		rec := state.DeploymentRecord{ID: "dep-class", ProjectID: faultProject, VMID: vmID, VMName: value, VMIP: "192.0.2.4", Status: "running", SSHUser: "user1", ComposeHash: "prior"}
		if err := state.Write(state.DefaultPath(dir), &state.File{Deployments: []state.DeploymentRecord{rec}}); err != nil {
			t.Fatal(err)
		}
	case "deploy_apply":
		plan := faultPlan()
		plan.VM.Flavor.Name = value
		if err := writePlanFile(dir, plan.PlanID, plan); err != nil {
			t.Fatal(err)
		}
		args["plan_id"] = plan.PlanID
	case "stack_apply", "stack_status", "stack_destroy":
		job := planJob(t, `{"name":"class-job","vpc":{"name":"private-network"}}`)
		job.ProjectID = faultProject
		step := job.Steps[0]
		if tool == "stack_destroy" {
			step.Status = "done"
			step.ResourceID = value
			job.Outputs[step.Key] = value
			job.Status = "done"
		} else {
			step.Status = "error"
			step.Detail = value
			job.Error = value
			job.Status = "error"
		}
		if err := job.Save(); err != nil {
			t.Fatal(err)
		}
		args["job_id"] = job.ID
	}
	text, _ := call.Handler(context.Background(), args)
	if !strings.Contains(text, cloudMark) {
		t.Fatalf("%s did not return its stored or workload value: %s", tool, text)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		if tool != "logs" || !call.UntrustedOutput || !strings.Contains(call.Description, untrustedOutputNote) {
			t.Fatalf("unlabelled raw answer: %s", text)
		}
		return
	}
	if leaks := classLeaks(answer); len(leaks) > 0 {
		t.Fatalf("unmarked fields %v: %s", leaks, text)
	}
	// Removing the declaration must expose at least one field of this actual
	// response; a generic refusal or a quoted diagnostic alone is insufficient.
	delete(answer, "_untrusted")
	if len(classLeaks(answer)) == 0 {
		t.Fatalf("marker did not exercise field provenance: %s", text)
	}
}
