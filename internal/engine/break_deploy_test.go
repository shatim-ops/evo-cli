// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// CreateVM answers success with an empty id after the VM is already in the
// world (the array form of POST /vms does this; see the HTTP test). The
// billed VM must be recorded under its real id and destroy must delete it.
func TestBreak_Deploy_EmptyVMIDOrphansVM(t *testing.T) {
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

// CreateVM is refused (409) and DeleteFloatingIP of the boot address then
// returns 500. A later deploy with the same input must not leave that
// address billed beside a new one.
func TestBreak_Deploy_BootFIPRelease500OrphansOnRetry(t *testing.T) {
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

// A token that expires while deploy polls the VM (401 on every GetVM) is
// AUTH_EXPIRED, exit 2 (docs/exit-codes.md: 401 from the compute API during
// deploy). The VM is already billed and must stay recorded.
func TestBreak_Deploy_AuthExpiredDuringPoll(t *testing.T) {
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
