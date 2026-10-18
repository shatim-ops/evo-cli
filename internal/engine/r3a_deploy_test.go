// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Cursor r3a, R3A-1: CreateVM answered success with an empty id after the
// VM was already made (the array form of POST /vms, see the HTTP test).
// deploy recorded an empty id and destroy could not delete the VM. Now
// such an answer is a lost one: the VM is found by name and recorded.
func TestDeploy_EmptyVMIDAdoptedByName(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateVM", 1, faultEmpty)
	err := faultDeploy(t, eng, dir)
	if len(cloud.liveVMs()) != 1 {
		t.Fatalf("world vms=%v err=%v", cloud.liveVMs(), err)
	}
	id := cloud.liveVMs()[0].ID
	raw := sfJSON(faultState(t, dir)) + "\n" + buf.String() + "\n" + faultErrText(err)
	if !strings.Contains(raw, id) {
		t.Fatalf("VM %s is billed but neither state nor the error names it\nerr=%v\nout=%s\nstate=%s", id, err, buf.String(), sfJSON(faultState(t, dir)))
	}
	cloud.disarm()
	_ = faultDestroy(t, eng)
	if len(cloud.liveVMs()) != 0 {
		t.Fatalf("destroy left VM %s", cloud.liveVMs()[0].ID)
	}
}

// Cursor r3a, R3A-2: CreateVM was refused (409) and the release of the
// boot address then answered 500. The error said "nothing was created",
// and a later deploy reserved a second address beside the first. Now the
// error names the address left, and a later deploy reuses it.
func TestDeploy_BootFIPLeftByFailedReleaseIsReused(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNth("CreateVM", 1, faultReject)
	cloud.deleteFIP = 500
	err := faultDeploy(t, eng, dir)
	if err == nil || len(cloud.liveFIPs()) != 1 || len(cloud.liveVMs()) != 0 {
		t.Fatalf("setup err=%v vms=%v fips=%v", err, cloud.liveVMs(), cloud.liveFIPs())
	}
	id := cloud.liveFIPs()[0].ID
	if msg := faultErrText(err); !strings.Contains(msg, id) || strings.Contains(msg, "nothing was created") {
		t.Fatalf("first error does not name the address left (%s): %s", id, msg)
	}
	cloud.deleteFIP = 0
	cloud.disarm()
	if err2 := faultDeploy(t, eng, dir); err2 != nil {
		t.Fatalf("retry deploy: %v\nfirst=%v\nout=%s", err2, err, buf.String())
	}
	if n := len(cloud.liveFIPs()); n != 1 {
		t.Fatalf("retry left %d floating IPs; %s from the failed release is still billed\nfirst error: %v\nout=%s", n, id, err, buf.String())
	}
	active := faultState(t, dir).ActiveDeployment()
	if active == nil || active.FloatingIPID == "" || active.FloatingIPID != cloud.liveFIPs()[0].ID {
		t.Fatalf("state %+v fips %+v", active, cloud.liveFIPs())
	}
}

// Cursor r3a, R3A-4: a token that expires while deploy polls the VM (401 on
// every GetVM) waited out the timeout and exited 1 VM_PROVISION_FAILED. It
// is AUTH_EXPIRED, exit 2 (docs/exit-codes.md), at once; the VM stays
// recorded.
func TestDeploy_AuthExpiredDuringPollExitsAuth(t *testing.T) {
	faultIsolate(t)
	cloud := &faultCloud{}
	eng, buf, dir := faultEngine(t, cloud, &faultDeployer{})
	cloud.failNthSticky("GetVM", 1, faultAuth)
	err := faultDeploy(t, eng, dir)
	if len(cloud.liveVMs()) != 1 {
		t.Fatalf("vms=%v err=%v", cloud.liveVMs(), err)
	}
	id := cloud.liveVMs()[0].ID
	if active := faultState(t, dir).ActiveDeployment(); active == nil || active.VMID != id {
		t.Fatalf("state %+v", active)
	}
	if clerrors.ExitCode(err) != clerrors.ExitAuth || clerrors.Reason(err) != clerrors.CodeAuthExpired {
		t.Fatalf("401 during the VM wait: exit %d reason %s, want 2 AUTH_EXPIRED (%v)\nout=%s",
			clerrors.ExitCode(err), clerrors.Reason(err), err, buf.String())
	}
}
