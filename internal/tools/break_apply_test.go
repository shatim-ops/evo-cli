// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// Two deploy_apply calls overlap on the check that the VM has no floating
// IP. The first GetVMDetail has already answered "none" and is held on a
// channel; the second reserves an address; the first then reserves another
// from that stale answer. One VM must not get two addresses.
func TestBreak_DeployApply_OverlapAdoptsAndBillsSecondFIP(t *testing.T) {
	faultToolsIsolate(t)
	prev := faultSSHWait
	faultSSHWait = time.Millisecond
	t.Cleanup(func() { faultSSHWait = prev })
	cloud := &faultCloud{}
	// The first GetVMDetail has already snapshotted "no floating IP". Hold
	// it there while the second apply reserves an address, then let the
	// first continue with that snapshot.
	note, release := cloud.pauseOnce("GetVMDetail")
	defer release()
	plan := faultPlan()
	done := make(chan struct{})
	var err1 error
	go func() {
		_, err1 = deployplan.ApplyWithTimeout(context.Background(), cloud, plan, 50*time.Millisecond)
		close(done)
	}()
	<-note
	_, err2 := deployplan.ApplyWithTimeout(context.Background(), cloud, plan, 50*time.Millisecond)
	release()
	<-done
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("overlap left vms=%d fips=%d (err1=%v err2=%v)", len(cloud.liveVMs()), len(cloud.liveFIPs()), err1, err2)
	}
}
