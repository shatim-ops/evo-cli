// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"slices"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// cloud-e2e #121 (live disk): deploy handed the VM create a boot floating
// IP still "creating" — 422 floating_ip_can_not_be_changed_from_current_state.
// The address is now waited for before the create; one not ready in time
// goes on after boot, the same address.
func TestDeploy_BootFIPCreatingIsWaitedFor(t *testing.T) {
	cloud := &faultCloud{fipSettle: 3}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("deploy: %v\nlog=%v", err, cloud.callLog())
	}
	vms, fips := cloud.liveVMs(), cloud.liveFIPs()
	if len(vms) != 1 || len(fips) != 1 || vms[0].FIPID != fips[0].ID {
		t.Fatalf("vms=%+v fips=%+v: want one VM with the boot FIP attached at create", vms, fips)
	}
}

func TestDeploy_BootFIPNotReadyGoesOnAfterBoot(t *testing.T) {
	cloud := &faultCloud{fipSettleUntilVM: true}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("deploy: %v\nlog=%v", err, cloud.callLog())
	}
	fips := cloud.liveFIPs()
	if len(cloud.liveVMs()) != 1 || len(fips) != 1 || !slices.Contains(cloud.callLog(), "AttachFloatingIP") {
		t.Fatalf("vms=%v fips=%v log=%v: want the one address attached after boot", cloud.liveVMs(), fips, cloud.callLog())
	}
	if rec := faultState(t, dir).ActiveDeployment(); rec == nil || rec.FloatingIPID != fips[0].ID {
		t.Fatalf("state %+v: want floating IP %s recorded", rec, fips[0].ID)
	}
}

// The rollback of a boot FIP still being created waits for it to settle
// and deletes it: one try answered 422 «can not be deleted from current
// state» and left it billed, «NOT released» (cloud-e2e #121: the script's
// delete went through ~45 s later).
func TestReleaseBootFIP_WaitsWhileCreating(t *testing.T) {
	cloud := &faultCloud{fips: []faultFIP{{ID: faultID(0x77), Name: "boot", IP: "203.0.113.77", State: "available", settle: 3}}}
	eng, _, _ := faultEngine(t, cloud, &faultDeployer{})
	if err := eng.releaseBootFIP(&provision.FloatingIP{ID: faultID(0x77), IPAddress: "203.0.113.77"}, faultProject); err != nil {
		t.Fatalf("release: %v\nlog=%v", err, cloud.callLog())
	}
	if len(cloud.liveFIPs()) != 0 {
		t.Fatalf("left %+v", cloud.liveFIPs())
	}
}

// CI #283 (Windows): the waits for a settling address were bounded by the
// wall clock (FIPSettleTimeout as a deadline), so three polls of a coarse
// timer outlasted the test's 20 ms and the address went on after boot.
// They are bounded by the number of polls (FIPSettleTimeout /
// FIPPollInterval): however long a poll takes, the same polls are made.
func TestBootFIPSettle_BoundedByPollsNotTheClock(t *testing.T) {
	cloud := &faultCloud{fipSettle: 3, fipReadDelay: 15 * time.Millisecond}
	eng, _, dir := faultEngine(t, cloud, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatalf("deploy: %v\nlog=%v", err, cloud.callLog())
	}
	if vms, fips := cloud.liveVMs(), cloud.liveFIPs(); len(vms) != 1 || len(fips) != 1 || vms[0].FIPID != fips[0].ID {
		t.Fatalf("vms=%+v fips=%+v: want the boot FIP attached at create after slow polls", vms, fips)
	}

	rel := &faultCloud{fips: []faultFIP{{ID: faultID(0x78), Name: "boot", IP: "203.0.113.78", State: "available", settle: 3}},
		fipReadDelay: 15 * time.Millisecond}
	eng, _, _ = faultEngine(t, rel, &faultDeployer{})
	if err := eng.releaseBootFIP(&provision.FloatingIP{ID: faultID(0x78), IPAddress: "203.0.113.78"}, faultProject); err != nil {
		t.Fatalf("release after slow tries: %v", err)
	}
}
