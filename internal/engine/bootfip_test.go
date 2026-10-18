// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// fipCalls records CreateFloatingIP requests.
type fipCalls struct {
	mu   sync.Mutex
	reqs []provision.CreateFloatingIPRequest
}

func (f *fipCalls) record(req provision.CreateFloatingIPRequest) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return len(f.reqs)
}

// The public IP is reserved before the VM and attached in the create
// request, so the VM boots with internet (cloud-init sets up the image's
// apt mirror at first boot); no second FIP is made after boot.
func TestDeploy_FIPAttachedAtCreation(t *testing.T) {
	var calls fipCalls
	cloud := &mockCloudAPI{createFloatingIPFn: func(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		calls.record(req)
		return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.7", State: "available"}, nil
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())

	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(calls.reqs) != 1 {
		t.Fatalf("CreateFloatingIP called %d times, want 1 (before the VM)", len(calls.reqs))
	}
	if calls.reqs[0].InterfaceID != "" {
		t.Errorf("boot FIP created with interface %q, want a free one", calls.reqs[0].InterfaceID)
	}
	if got := cloud.createReq.Interfaces[0].AttachExternalIPID; got != "fip-boot" {
		t.Errorf("create request attaches %q, want fip-boot", got)
	}
	a := readActive(t, dir)
	if a == nil || a.FloatingIPID != "fip-boot" || a.VMIP == "" {
		t.Fatalf("state: %+v, want FIP fip-boot with an IP", a)
	}
}

// A VM that was not created leaves no billed FIP behind.
func TestDeploy_CreateVMFails_ReleasesBootFIP(t *testing.T) {
	cloud := &mockCloudAPI{createVMFn: func(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
		return nil, stderrors.New("422 quota")
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())

	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err == nil {
		t.Fatal("deploy succeeded, want the CreateVM error")
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-1" {
		t.Errorf("released FIPs %v, want [fip-1]", cloud.deletedFIPs)
	}
}

// When the IP cannot be reserved up front for a reason other than quota,
// deploy goes on, attaches one after boot and says the first boot may be
// slow.
func TestDeploy_BootFIPFails_AttachesAfterBoot(t *testing.T) {
	var calls fipCalls
	cloud := &mockCloudAPI{createFloatingIPFn: func(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		if calls.record(req) == 1 {
			return nil, stderrors.New("503 service unavailable")
		}
		return &provision.FloatingIP{ID: "fip-late", IPAddress: "203.0.113.8", State: "in_use"}, nil
	}}
	var out bytes.Buffer
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)

	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if got := cloud.createReq.Interfaces[0].AttachExternalIPID; got != "" {
		t.Errorf("create request attaches %q, want none", got)
	}
	if len(calls.reqs) != 2 || calls.reqs[1].InterfaceID == "" {
		t.Fatalf("FIP calls %+v, want a failed reservation then one on the interface", calls.reqs)
	}
	if !strings.Contains(out.String(), "attached after boot") {
		t.Errorf("no warning about the late IP:\n%s", out.String())
	}
	if a := readActive(t, dir); a == nil || a.FloatingIPID != "fip-late" {
		t.Errorf("state: %+v, want FIP fip-late", a)
	}
}

// The create call fails but the VM was made (the answer was lost): the VM
// and its FIP go into the state for deploy or destroy to pick up, and the
// FIP, attached to that VM, is not released.
func TestDeploy_CreateVMFailsButVMExists_Recorded(t *testing.T) {
	cloud := &mockCloudAPI{
		createVMFn: func(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
			return nil, stderrors.New("context deadline exceeded")
		},
	}
	cloud.listVMsInProjectFn = func(context.Context, string) ([]*provision.VM, error) {
		return []*provision.VM{{ID: "vm-lost", Name: cloud.createReq.Name, CreatedAt: time.Now()}}, nil
	}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())

	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	if err == nil || !strings.Contains(err.Error(), "vm-lost") {
		t.Fatalf("deploy error %v, want one naming vm-lost", err)
	}
	if len(cloud.deletedFIPs) != 0 {
		t.Errorf("released FIPs %v, want none: the FIP belongs to the VM", cloud.deletedFIPs)
	}
	a := readActive(t, dir)
	if a == nil || a.VMID != "vm-lost" || a.FloatingIPID != "fip-1" || a.Status != "creating" {
		t.Fatalf("state: %+v, want vm-lost creating with fip-1", a)
	}
}

// No public IP can be had (quota): the VM would be billed with no way in,
// since the IP after boot hits the same quota — stop before creating it.
func TestDeploy_FIPQuotaExhausted_CreatesNothing(t *testing.T) {
	cloud := &mockCloudAPI{createFloatingIPFn: func(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		return nil, stderrors.New("403 organization_quota_exceeded")
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())

	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	if err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("deploy error %v, want the quota", err)
	}
	if cloud.createCalled {
		t.Error("VM created although no public IP could be reserved")
	}
}
