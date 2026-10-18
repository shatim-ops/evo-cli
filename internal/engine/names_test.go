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
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

const demoVMID = "a1b2c3d4-0000-4000-8000-000000000001"

// demoRecord is a record of v1.0.0: UUIDs only, no names.
func demoRecord() state.DeploymentRecord {
	return state.DeploymentRecord{
		ID: demoVMID, VMID: demoVMID, VMIP: "203.0.113.10", Region: "az-2", VMFlavor: "f-small",
		SSHUser: "user1", Status: "running", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

// demoCloud knows the VM by its name; after the delete it is gone.
func demoCloud() *mockCloudAPI {
	deleted := false
	c := &mockCloudAPI{}
	c.getVMFn = func(_ context.Context, id string) (*provision.VM, error) {
		if deleted {
			return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
		}
		return &provision.VM{ID: id, Name: "evo-web", Status: "running"}, nil
	}
	c.deleteVMFn = func(context.Context, string) error { deleted = true; return nil }
	return c
}

// A user with several VMs could not tell from the UUIDs which one destroy
// was about to delete (first real run, v1.0.1): the step line, the plan
// and its delete line name the VM and the zone, the IDs in parentheses.
func TestRussian_DestroyNamesVMAndZone(t *testing.T) {
	russian(t)
	eng, dir := newTestEngine(t, demoCloud(), newMockDeployer())
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModePlain, false)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(demoRecord())
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	t.Logf("\n%s", buf.String())
	for _, want := range []string{
		"    ВМ evo-web (" + demoVMID + "), 203.0.113.10, зона ru-moscow-2\n",
		"План: удалить ВМ evo-web (" + demoVMID + ") в зоне ru-moscow-2",
		"  удалить ВМ evo-web (" + demoVMID + ") и все данные на её дисках\n",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("destroy lacks %q", want)
		}
	}
}

// The lookups are best effort: without a client the IDs are shown, and a
// record with names (deploy since v1.0.1) needs none.
func TestDestroy_NamesFallBackToIDs(t *testing.T) {
	eng, dir := newTestEngine(t, demoCloud(), newMockDeployer())
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModePlain, false)
	eng.Auth, eng.Cloud = nil, nil // any cloud access panics
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(demoRecord())
	_ = state.Write(state.DefaultPath(dir), sf)
	if err := eng.Destroy(context.Background(), DestroyOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "VM "+demoVMID+", 203.0.113.10, zone az-2\n") ||
		!strings.Contains(buf.String(), "destroy VM "+demoVMID+" in az-2") {
		t.Errorf("fallback to IDs:\n%s", buf.String())
	}

	buf.Reset()
	rec := demoRecord()
	rec.VMName, rec.ZoneName = "web", "ru.AZ-2"
	sf.UpsertDeployment(rec)
	_ = state.Write(state.DefaultPath(dir), sf)
	if err := eng.Destroy(context.Background(), DestroyOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "destroy VM web ("+demoVMID+") in ru.AZ-2") {
		t.Errorf("names from the record:\n%s", buf.String())
	}
}

// status shows the flavor and the zone by name, not as UUIDs, with one GET
// of the VM; --json adds vm_name, zone_name and flavor_name.
func TestStatus_ShowsNames(t *testing.T) {
	cloud := demoCloud()
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModePlain, false)
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(demoRecord())
	_ = state.Write(state.DefaultPath(dir), sf)
	if err := eng.Status(context.Background(), StatusOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("\n%s", buf.String())
	rows := map[string]string{}
	for _, line := range strings.Split(buf.String(), "\n") {
		if k, v, ok := strings.Cut(line, "  "); ok {
			rows[k] = strings.TrimSpace(v)
		}
	}
	for k, want := range map[string]string{"VM name": "evo-web", "Flavor": "small (f-small)", "Region": "ru-moscow-2 (az-2)", "VM ID": demoVMID} {
		if rows[k] != want {
			t.Errorf("status %s = %q, want %q", k, rows[k], want)
		}
	}
	if cloud.getCalls != 1 {
		t.Errorf("GET calls = %d, want 1", cloud.getCalls)
	}

	buf.Reset()
	eng.Printer = output.NewWithWriter(&buf, output.ModeJSON, false)
	if err := eng.Status(context.Background(), StatusOptions{}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["vm_name"] != "evo-web" || got["zone_name"] != "ru-moscow-2" || got["flavor_name"] != "small" ||
		got["vm_flavor"] != "f-small" || got["region"] != "az-2" || got["vm_id"] != demoVMID {
		t.Errorf("status --json: %v", got)
	}
}

// A port another rule already opens: deploy creates no rule, says so and
// records it; destroy then says it leaves the port (v1.0.1: both were
// silent).
func TestDeployDestroy_PortAlreadyOpenIsSaid(t *testing.T) {
	cloud := &mockCloudAPI{
		listSecurityGroupsFn: func(context.Context, string) ([]*provision.SecurityGroup, error) {
			return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru-moscow-1", Rules: []provision.SGRule{
				{ID: "theirs", Direction: "ingress", IPProtocol: "tcp", PortRange: "443:443"},
			}}}, nil
		},
	}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModePlain, false)
	opts := newDeployOpts(t, dir)
	opts.ComposeFile = writeCompose(t, dir, "services:\n  web:\n    image: nginx\n    ports:\n      - \"80:80\"\n      - \"443:443\"\n")
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !strings.Contains(buf.String(), "port 443 is already open in group SSH-access_ru-moscow-1 — no rule created, destroy will not touch it") {
		t.Errorf("deploy output:\n%s", buf.String())
	}
	rec := readActive(t, dir)
	if strings.Join(rec.PortsAlreadyOpen, ",") != "443/tcp" || rec.SecurityGroupName != "SSH-access_ru-moscow-1" ||
		rec.VMName == "" || rec.FlavorName != "small" {
		t.Errorf("record: %+v", rec)
	}

	// A re-run finds 443 open again and its own rule on 80: still only 443
	// is someone else's.
	buf.Reset()
	opts.Force = true
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("re-deploy: %v", err)
	}
	if rec := readActive(t, dir); strings.Join(rec.PortsAlreadyOpen, ",") != "443/tcp" {
		t.Errorf("re-run: ports_already_open = %v\n%s", rec.PortsAlreadyOpen, buf.String())
	}

	buf.Reset()
	if err := eng.Destroy(context.Background(), DestroyOptions{DryRun: true}); err != nil {
		t.Fatalf("Destroy --dry-run: %v", err)
	}
	if want := "keep port 443 open in security group SSH-access_ru-moscow-1 (sg-1) (not opened by this deploy)"; !strings.Contains(buf.String(), want) {
		t.Errorf("destroy plan lacks %q:\n%s", want, buf.String())
	}
	buf.Reset()
	cloud.getVMFn = func(context.Context, string) (*provision.VM, error) {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if want := "port 443 was not opened by this deploy — left open"; !strings.Contains(buf.String(), want) {
		t.Errorf("destroy lacks %q:\n%s", want, buf.String())
	}
}
