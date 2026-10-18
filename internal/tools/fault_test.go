// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/stack"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func faultPlan() *deployplan.Plan {
	return &deployplan.Plan{
		PlanID:    "plan-0123456789ab",
		ProjectID: faultProject,
		ExpiresAt: time.Now().Add(time.Hour),
		VM: deployplan.VMSpec{
			Name:   "web",
			Flavor: deployplan.FlavorSpec{ID: faultFlavor, Name: "low-1-1"},
			Image:  deployplan.RefSpec{ID: faultImage, Name: "ubuntu-22.04"},
			Zone:   deployplan.RefSpec{ID: faultZone, Name: "ru.AZ-1"},
			Disk:   deployplan.DiskSpec{SizeGB: 10, Type: "SSD"},
		},
		Network: deployplan.NetworkSpec{SubnetID: faultSubnet, SecurityGroupID: faultSG, FloatingIP: true},
		SSH:     deployplan.SSHSpec{Login: "user1"},
	}
}

func faultToolsIsolate(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{
		"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_KEY_SECRET", "CLOUDRU_PROJECT_ID",
		"CLOUDRU_API_KEY", "EVOLUTION_PROJECT_ID", "MCP_BEARER_TOKEN", "NO_COLOR", "EVO_LANG",
	} {
		t.Setenv(k, "")
	}
}

func faultToolsTimings() engine.Timings {
	return engine.Timings{
		VMPollInterval:        time.Millisecond,
		VMPollTimeout:         40 * time.Millisecond,
		VMProgressInterval:    time.Hour,
		IfacePollInterval:     time.Millisecond,
		FIPRetryInterval:      time.Millisecond,
		FIPLookupInterval:     time.Millisecond,
		FIPPollInterval:       time.Millisecond,
		SSHRetryInterval:      time.Millisecond,
		SettleDelay:           time.Millisecond,
		VerifyTimeout:         time.Millisecond,
		DockerPollInterval:    time.Millisecond,
		DockerWaitTimeout:     25 * time.Millisecond,
		DestroyPollInterval:   time.Millisecond,
		DestroyPollTimeout:    40 * time.Millisecond,
		DeletableWaitTimeout:  25 * time.Millisecond,
		DeletablePollInterval: time.Millisecond,
		StatusRefreshTimeout:  20 * time.Millisecond,
	}
}

func faultToolsRegistry(t *testing.T, cloud *faultCloud) (*Registry, string) {
	t.Helper()
	faultToolsIsolate(t)
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config: config.Config{
				ProjectID: faultProject,
				Region:    faultZone,
				ImageID:   faultImage,
				Flavor:    "low-1-1",
				DiskSize:  10,
				KeyID:     "k",
				Secret:    "s",
			},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("fake-key"),
			SSHPubKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest test@test",
			Timings:   faultToolsTimings(),
		}, nil
	})
	return r, dir
}

func faultJSON(t *testing.T, text string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("json: %v\n%s", err, text)
	}
	return m
}

func TestFault_Provision_HappyThenDestroy(t *testing.T) {
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if isErr {
		t.Fatalf("provision: %s\nlog=%v", text, cloud.callLog())
	}
	m := faultJSON(t, text)
	id, _ := m["vm_id"].(string)
	if id == "" || len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("answer %s vms=%d fips=%d", text, len(cloud.liveVMs()), len(cloud.liveFIPs()))
	}
	if !strings.Contains(text, id) {
		t.Fatalf("answer omits %s", id)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	active := sf.ActiveDeployment()
	if active == nil || active.VMID != id || active.FloatingIPID == "" {
		t.Fatalf("state %+v", active)
	}
	dtext, dErr := r.handlers["destroy"](context.Background(), map[string]any{"confirmed": true, "vm_id": id})
	if dErr {
		t.Fatalf("destroy: %s", dtext)
	}
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("left vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
}

func TestFault_Provision_LostCreateIsRecorded(t *testing.T) {
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	cloud.failNth("CreateVM", 1, faultLost)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr {
		t.Fatalf("lost create succeeded: %s", text)
	}
	if len(cloud.liveVMs()) != 1 {
		t.Fatalf("vms=%v text=%s", cloud.liveVMs(), text)
	}
	id := cloud.liveVMs()[0].ID
	if !strings.Contains(text, id) {
		t.Fatalf("answer does not name %s: %s", id, text)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if sf.ActiveDeployment() == nil || sf.ActiveDeployment().VMID != id {
		t.Fatalf("state %+v", sf.Deployments)
	}
	cloud.disarm()
	text2, isErr2 := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("retry left vms=%d fips=%d\nfirst=%s\nsecond=%s err=%v", len(cloud.liveVMs()), len(cloud.liveFIPs()), text, text2, isErr2)
	}
}

func TestFault_Provision_RejectReleasesBootFIP(t *testing.T) {
	cloud := &faultCloud{}
	r, _ := faultToolsRegistry(t, cloud)
	cloud.failNth("CreateVM", 1, faultReject)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr || len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("err=%v vms=%v fips=%v text=%s", isErr, cloud.liveVMs(), cloud.liveFIPs(), text)
	}
}

func TestFault_Provision_Release500NamesFIP(t *testing.T) {
	cloud := &faultCloud{}
	r, _ := faultToolsRegistry(t, cloud)
	cloud.failNth("CreateVM", 1, faultReject)
	cloud.deleteFIP = 500
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !isErr || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("err=%v fips=%v text=%s", isErr, cloud.liveFIPs(), text)
	}
	id := cloud.liveFIPs()[0].ID
	if !strings.Contains(text, id) {
		t.Fatalf("MCP answer does not name floating IP %s: %s", id, text)
	}
}

func TestFault_DeployApply_RetryReusesFIP(t *testing.T) {
	faultToolsIsolate(t)
	prev := deployplan.SSHPortWait
	deployplan.SSHPortWait = time.Millisecond
	t.Cleanup(func() { deployplan.SSHPortWait = prev })
	cloud := &faultCloud{}
	plan := faultPlan()
	if _, err := deployplan.ApplyWithTimeout(context.Background(), cloud, plan, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := deployplan.ApplyWithTimeout(context.Background(), cloud, plan, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("sequential retry vms=%d fips=%d", len(cloud.liveVMs()), len(cloud.liveFIPs()))
	}
}

func TestFault_DeployApply_PlanDeleted(t *testing.T) {
	cloud := &faultCloud{noDial: true}
	r, _ := faultToolsRegistry(t, cloud)
	text, isErr := r.handlers["deploy_apply"](context.Background(), map[string]any{
		"plan_id": "plan-0123456789ab", "confirmed": true,
	})
	if !isErr {
		t.Fatalf("missing plan succeeded: %s", text)
	}
	if len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0 {
		t.Fatalf("missing plan created vms=%v fips=%v", cloud.liveVMs(), cloud.liveFIPs())
	}
	if !strings.Contains(text, "plan-0123456789ab") {
		t.Fatalf("answer: %s", text)
	}
}

func TestFault_Stack_PartialDoesNotCreateSecondVM(t *testing.T) {
	cloud := &faultCloud{fipDown: true}
	r, _ := faultToolsRegistry(t, cloud)
	job := &stack.Job{
		ID:      "stk-aabbccddee",
		Status:  "running",
		Outputs: map[string]string{},
		Steps: []*stack.Step{{
			Key:    "vm",
			Tool:   "provision",
			Status: "pending",
			Args:   map[string]any{"name": "web", "confirmed": true},
		}},
	}
	r.advance(context.Background(), job)
	if job.Status != "error" || job.Steps[0].ResourceID == "" {
		t.Fatalf("after partial: status=%s step=%+v", job.Status, job.Steps[0])
	}
	vms, fips := cloud.creates()
	r.advance(context.Background(), job)
	if v2, f2 := cloud.creates(); v2 != vms || f2 != fips {
		t.Fatalf("second crank created vm %d→%d fip %d→%d step=%+v", vms, v2, fips, f2, job.Steps[0])
	}
	if len(cloud.liveVMs()) != 1 {
		t.Fatalf("vms=%v", cloud.liveVMs())
	}
}

func TestFault_Stack_TakenByNameErrorAndDeleting(t *testing.T) {
	faultToolsIsolate(t)
	r := New(func(map[string]any) (*engine.Engine, error) { return nil, nil })
	for _, status := range []string{"ERROR", "DELETING", "error_creating"} {
		r.handlers["vm_list"] = func(context.Context, map[string]any) (string, bool) {
			return toJSON(map[string]any{"vms": []any{
				map[string]any{"name": "web", "id": faultID(1), "status": status},
			}}), false
		}
		id, err := r.takenByName(context.Background(), &stack.Step{
			FindTool: "vm_list", FindField: "vms", FindName: "web",
		})
		if err != nil || id != faultID(1) {
			t.Fatalf("status %s: id=%q err=%v", status, id, err)
		}
	}
	r.handlers["vm_list"] = func(context.Context, map[string]any) (string, bool) {
		return toJSON(map[string]any{"vms": []any{
			map[string]any{"name": "web", "id": faultID(1), "status": "DELETED"},
			map[string]any{"name": "web", "id": faultID(2), "status": "ERASED"},
		}}), false
	}
	id, err := r.takenByName(context.Background(), &stack.Step{
		FindTool: "vm_list", FindField: "vms", FindName: "web",
	})
	if err != nil || id != "" {
		t.Fatalf("DELETED/ERASED counted as taken: %q %v", id, err)
	}
}
