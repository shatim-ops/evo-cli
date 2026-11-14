// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
)

// deploy_apply whose create of the floating IP is cut looks for the
// address by its name (floatingip.Reserve) at the pace of the world, not at
// a production second per look: the interval was a literal second, the
// world could not shorten it, and the case of the fault matrix took 2 s —
// over its limit on a loaded Windows runner («Процесс · 1.9 (3)», #1759,
// job 132352130794).
func TestDeployApply_ACutFloatingIPIsLookedForAtTheWorldsPace(t *testing.T) {
	prev := deployplan.SSHPortWait
	deployplan.SSHPortWait = time.Millisecond
	t.Cleanup(func() { deployplan.SSHPortWait = prev })
	ran := false
	for _, c := range faultMatrixCases {
		if c.tool != "deploy_apply" {
			continue
		}
		happy, _, _, _ := faultRun(t, c, 0, "")
		for k, op := range happy.callLog() {
			if op != "CreateFloatingIP" {
				continue
			}
			ran = true
			start := time.Now()
			faultRun(t, c, k+1, faultCancel)
			// wall-clock: half of one production look for the address (a
			// second): the world's pace is milliseconds.
			if d := time.Since(start); d > 500*time.Millisecond {
				t.Errorf("deploy_apply with its floating IP create cut took %s: the look for the address waits a production interval", d)
			}
		}
	}
	if !ran {
		t.Fatal("no deploy_apply case of the fault matrix creates a floating IP: the test checks nothing")
	}
}
