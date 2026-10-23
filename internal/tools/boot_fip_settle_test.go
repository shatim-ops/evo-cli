// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// cloud-e2e #121 (live disk): the boot floating IP was handed to the VM
// create while it was still "creating", and the create answered 422
// floating_ip_can_not_be_changed_from_current_state; the rollback could
// not delete it either (422) and answered "NOT released". Now provision
// waits for the address to be ready before the create; one that is not
// ready in time goes on after boot — the same address, no second one.
func TestFault_Provision_BootFIPCreatingIsWaitedFor(t *testing.T) {
	cloud := &faultCloud{fipSettle: 3}
	r, _ := faultToolsRegistry(t, cloud)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if isErr {
		t.Fatalf("provision: %s\nlog=%v", text, cloud.callLog())
	}
	vms, fips := cloud.liveVMs(), cloud.liveFIPs()
	if len(vms) != 1 || len(fips) != 1 || vms[0].FIPID != fips[0].ID {
		t.Fatalf("vms=%+v fips=%+v: want one VM with the boot FIP attached at create", vms, fips)
	}
	if slices.Contains(cloud.callLog(), "AttachFloatingIP") {
		t.Errorf("the ready address was attached after boot: %v", cloud.callLog())
	}
}

func TestFault_Provision_BootFIPNotReadyGoesOnAfterBoot(t *testing.T) {
	cloud := &faultCloud{fipSettleUntilVM: true}
	r, dir := faultToolsRegistry(t, cloud)
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if isErr || strings.Contains(text, "NOT released") {
		t.Fatalf("provision: %s\nlog=%v", text, cloud.callLog())
	}
	fips := cloud.liveFIPs()
	if len(cloud.liveVMs()) != 1 || len(fips) != 1 {
		t.Fatalf("vms=%v fips=%v: want one VM and its one address", cloud.liveVMs(), fips)
	}
	n := 0
	for _, op := range cloud.callLog() {
		if op == "CreateFloatingIP" {
			n++
		}
	}
	if n != 1 || !slices.Contains(cloud.callLog(), "AttachFloatingIP") {
		t.Errorf("want the one address attached after boot: %v", cloud.callLog())
	}
	if !strings.Contains(text, fips[0].ID) && !strings.Contains(stateText(t, dir), fips[0].ID) {
		t.Errorf("floating IP %s is neither in the answer nor in the state: %s", fips[0].ID, text)
	}
}
