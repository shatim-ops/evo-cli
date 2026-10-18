// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// Cursor r3a, R3A-3: two deploy_apply calls of one plan overlapped on the
// check that the VM has no floating IP. The first GetVMDetail answered
// "none" and was held; the second reserved an address; the first then
// reserved another from its stale answer: one VM, two billed addresses.
// Now the second call is refused while the first runs, and a call after
// it adopts the VM with its address.
func TestDeployApply_OverlapRefused(t *testing.T) {
	cloud := &faultCloud{}
	r, dir := faultToolsRegistry(t, cloud)
	prev := deployplan.SSHPortWait
	deployplan.SSHPortWait = time.Millisecond
	t.Cleanup(func() { deployplan.SSHPortWait = prev })
	if err := deployplan.Save(dir, faultPlan()); err != nil {
		t.Fatal(err)
	}
	apply := func() (string, bool) {
		return r.handlers["deploy_apply"](context.Background(), map[string]any{
			"plan_id": "plan-0123456789ab", "confirmed": true,
		})
	}

	note, release := cloud.pauseOnce("GetVMDetail")
	defer release()
	done := make(chan struct{})
	var text1 string
	go func() {
		text1, _ = apply()
		close(done)
	}()
	<-note
	text2, isErr2 := apply()
	release()
	<-done

	if !isErr2 || !strings.Contains(text2, "another deploy_apply") {
		t.Fatalf("overlapping deploy_apply was not refused: %s", text2)
	}
	if len(cloud.liveVMs()) != 1 || len(cloud.liveFIPs()) != 1 {
		t.Fatalf("overlap left vms=%d fips=%d\nfirst: %s\nsecond: %s", len(cloud.liveVMs()), len(cloud.liveFIPs()), text1, text2)
	}
}
