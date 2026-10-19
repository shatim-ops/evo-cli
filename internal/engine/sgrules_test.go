// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

func readState(t *testing.T, dir string) *state.File {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return sf
}

// deploy records the group and the IDs of the rules it created itself — not
// the rule for a port that was already open.
func TestDeploy_RecordsCreatedRuleIDsAndGroup(t *testing.T) {
	cloud := &mockCloudAPI{
		listSecurityGroupsFn: func(context.Context, string) ([]*provision.SecurityGroup, error) {
			return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru-moscow-1", Rules: []provision.SGRule{
				{ID: "theirs", Direction: "ingress", IPProtocol: "tcp", PortRange: "443:443"},
			}}}, nil
		},
	}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	opts := newDeployOpts(t, dir)
	opts.ComposeFile = writeCompose(t, dir, "services:\n  web:\n    image: nginx\n    ports:\n      - \"80:80\"\n      - \"443:443\"\n")
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	rec := readActive(t, dir)
	if rec.SecurityGroupID != "sg-1" {
		t.Errorf("security_group_id = %q", rec.SecurityGroupID)
	}
	if len(rec.OpenedRules) != 1 || rec.OpenedRules[0] != (state.SGRule{ID: "rule-1", SecurityGroupID: "sg-1", Port: "80/tcp"}) {
		t.Errorf("opened_rules = %+v", rec.OpenedRules)
	}
	if strings.Join(rec.SecurityGroupPorts, ",") != "80/tcp,443/tcp" {
		t.Errorf("security_group_ports = %v", rec.SecurityGroupPorts)
	}
}

// BACKLOG [qa-engine]: a deploy interrupted before the ports were opened
// left no group in state, so the re-run never opened them. The group is
// saved with the first record now, and the re-run opens what is missing.
func TestDeploy_RerunAfterInterruptOpensPorts(t *testing.T) {
	cloud := &mockCloudAPI{}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "203.0.113.5", SSHUser: "user1",
		Status: "provisioned", SecurityGroupID: "sg-1", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if cloud.createCalled {
		t.Fatal("a new VM was created")
	}
	if len(cloud.addedRules) != 2 || cloud.addedRules[0].PortRange != "5432:5432" || cloud.addedRules[1].PortRange != "8080:8080" {
		t.Fatalf("rules added on re-run = %+v", cloud.addedRules)
	}
	if rec := readActive(t, dir); len(rec.OpenedRules) != 2 {
		t.Errorf("opened_rules = %+v", rec.OpenedRules)
	}
}

// The group ID reaches state with the first record, before the VM is up.
func TestDeploy_GroupSavedWithFirstRecord(t *testing.T) {
	var mu sync.Mutex
	var seen string
	cloud := &mockCloudAPI{}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	cloud.getVMFn = func(context.Context, string) (*provision.VM, error) {
		mu.Lock()
		defer mu.Unlock()
		if seen == "" {
			if rec := readActive(t, dir); rec != nil {
				seen = rec.SecurityGroupID + "/" + rec.Status
			}
		}
		return &provision.VM{ID: "vm-123", Status: "active"}, nil
	}
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatal(err)
	}
	if seen != "sg-1/creating" {
		t.Errorf("record while creating = %q", seen)
	}
}

func rulesRecord() state.DeploymentRecord {
	rec := runningRecord()
	rec.SecurityGroupID = "sg-1"
	rec.SecurityGroupPorts = []string{"80/tcp", "53/udp"}
	rec.OpenedRules = []state.SGRule{
		{ID: "rule-80", SecurityGroupID: "sg-1", Port: "80/tcp"},
		{ID: "rule-53", SecurityGroupID: "sg-1", Port: "53/udp"},
	}
	return rec
}

// destroy deletes exactly the rules deploy created, after the VM is gone.
func TestDestroy_DeletesOwnRulesAfterVM(t *testing.T) {
	eng, cloud, out := destroyFixture(t, rulesRecord())
	var order []string
	cloud.deleteVMFn = func(context.Context, string) error { order = append(order, "vm"); return nil }
	cloud.deleteRuleFn = func(_ context.Context, _, id string) error { order = append(order, id); return nil }

	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if strings.Join(order, ",") != "vm,rule-80,rule-53" {
		t.Errorf("order = %v", order)
	}
	if strings.Join(cloud.deletedRules, ",") != "sg-1/rule-80,sg-1/rule-53" {
		t.Errorf("deleted = %v", cloud.deletedRules)
	}
	if !strings.Contains(out.String(), "Closed ports 80,53/udp in the security group") {
		t.Errorf("output:\n%s", out.String())
	}
	if sf := readState(t, eng.StateDir); len(sf.Deployments) != 0 {
		t.Errorf("state left: %+v", sf.Deployments)
	}
}

// A VM that cannot be deleted keeps serving: its ports stay open.
func TestDestroy_VMDeleteFailsKeepsRules(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, rulesRecord())
	cloud.deleteVMFn = func(context.Context, string) error { return stderrors.New("API returned status 500") }

	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err == nil {
		t.Fatal("destroy succeeded")
	}
	if len(cloud.deletedRules) != 0 {
		t.Errorf("rules deleted while the VM is still there: %v", cloud.deletedRules)
	}
	if rec := readActive(t, eng.StateDir); rec == nil || len(rec.OpenedRules) != 2 {
		t.Errorf("record = %+v", rec)
	}
}

// A rule delete that fails is a warning: destroy succeeds, the record stays
// (not active) with the rule marked, and the next destroy finishes it.
func TestDestroy_RuleDeleteFailureWarnsAndRetries(t *testing.T) {
	eng, cloud, out := destroyFixture(t, rulesRecord())
	var errBuf bytes.Buffer
	eng.Printer = output.NewWithWriters(out, &errBuf, output.ModePlain, false)
	cloud.deleteRuleFn = func(_ context.Context, _, id string) error {
		if id == "rule-53" {
			return clerrors.New(clerrors.CodeAPIError, "API returned status 422: rule state", "")
		}
		return nil
	}

	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if !strings.Contains(errBuf.String(), "Could not delete the security group rules for ports 53/udp") ||
		!strings.Contains(errBuf.String(), "re-run 'evo destroy'") {
		t.Errorf("warning:\n%s", errBuf.String())
	}
	sf := readState(t, eng.StateDir)
	if sf.ActiveDeployment() != nil {
		t.Fatalf("still active: %+v", sf.ActiveDeployment())
	}
	if len(sf.Deployments) != 1 || sf.Deployments[0].Status != "destroyed" ||
		len(sf.Deployments[0].OpenedRules) != 1 || sf.Deployments[0].OpenedRules[0].DeleteError == "" {
		t.Fatalf("state = %+v", sf.Deployments)
	}
	if len(cloud.deletedFIPs) != 1 {
		t.Errorf("floating IP not released: %v", cloud.deletedFIPs)
	}

	// The preview of the retry names what is left.
	out.Reset()
	if err := eng.Destroy(context.Background(), DestroyOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "delete the rule rule-53 opening port 53/udp in security group sg-1 (left by an earlier destroy)") {
		t.Errorf("retry preview:\n%s", out.String())
	}

	cloud.deleteRuleFn = nil
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("second Destroy: %v", err)
	}
	if cloud.deletedRules[len(cloud.deletedRules)-1] != "sg-1/rule-53" {
		t.Errorf("deleted = %v", cloud.deletedRules)
	}
	if sf := readState(t, eng.StateDir); len(sf.Deployments) != 0 {
		t.Errorf("state left: %+v", sf.Deployments)
	}
	if len(cloud.deletedFIPs) != 1 {
		t.Errorf("second destroy touched the FIP again: %v", cloud.deletedFIPs)
	}
}

// Another active deployment in this state shares the security group:
// destroy without a VM refuses (R2-2) and deletes no rule. Each one goes by
// its VM ID (MCP destroy vm_id), which keeps a rule the other still needs
// (sgrules.Close, TestClose_KeepsRuleNeededByAnotherDeployment).
func TestDestroy_SharedGroupWithAnotherDeploymentRefused(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, rulesRecord())
	sf := readState(t, eng.StateDir)
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-2", VMID: "vm-2", Status: "running",
		SecurityGroupID: "sg-1", SecurityGroupPorts: []string{"80/tcp"}, CreatedAt: time.Now()})
	if err := state.Write(state.DefaultPath(eng.StateDir), sf); err != nil {
		t.Fatal(err)
	}

	for _, opts := range []DestroyOptions{{DryRun: true}, {Force: true}} {
		err := eng.Destroy(context.Background(), opts)
		if clerrors.ExitCode(err) != clerrors.ExitAuth || !strings.Contains(err.Error(), "vm-2") {
			t.Fatalf("%+v: want CONFIG_INVALID naming vm-2, got %v", opts, err)
		}
	}
	if len(cloud.deletedRules) != 0 || len(cloud.deletedFIPs) != 0 {
		t.Errorf("deleted rules %v, FIPs %v", cloud.deletedRules, cloud.deletedFIPs)
	}
	if n := len(readState(t, eng.StateDir).ActiveDeployments()); n != 2 {
		t.Errorf("active records = %d, want 2", n)
	}
}

// A rule deleted by hand in the console (404) is not an error.
func TestDestroy_RuleAlreadyGone(t *testing.T) {
	eng, cloud, _ := destroyFixture(t, rulesRecord())
	cloud.deleteRuleFn = func(context.Context, string, string) error {
		return clerrors.New(clerrors.CodeNotFound, "resource not found", "")
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if sf := readState(t, eng.StateDir); len(sf.Deployments) != 0 {
		t.Errorf("state left: %+v", sf.Deployments)
	}
}

// The JSON result says what happened to the rules.
func TestDestroy_JSONReportsRules(t *testing.T) {
	eng, _, _ := destroyFixture(t, rulesRecord())
	var stdout bytes.Buffer
	eng.Printer = output.NewWithWriters(&stdout, &bytes.Buffer{}, output.ModeJSON, false)
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Rules struct {
			Closed []string `json:"closed"`
		} `json:"security_group_rules"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || strings.Join(got.Rules.Closed, ",") != "80,53/udp" {
		t.Errorf("json (%v): %s", err, stdout.String())
	}
}

// The deploy preview says the ports are opened and that destroy closes them.
func TestDeployPreview_SaysDestroyClosesPorts(t *testing.T) {
	eng, _, opts, _, stderr := newDeployForConfirm(t)
	opts.DryRun = true
	opts.Yes = false
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String()+eng.Printer.Writer().(*bytes.Buffer).String(), "destroy deletes only the rules this deploy creates") {
		t.Errorf("preview:\n%s", stderr.String())
	}
}
