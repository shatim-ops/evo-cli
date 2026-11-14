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
// job 132352130794). The time of the cut run is read against the same
// case run whole on the same runner: a slow runner slows both, and only
// the production pauses tell them apart (Windows took 721 ms over a
// literal bound, #1809).
func TestDeployApply_ACutFloatingIPIsLookedForAtTheWorldsPace(t *testing.T) {
	prev := deployplan.SSHPortWait
	deployplan.SSHPortWait = time.Millisecond
	t.Cleanup(func() { deployplan.SSHPortWait = prev })
	ran := false
	for _, c := range faultMatrixCases {
		if c.tool != "deploy_apply" {
			continue
		}
		begin := time.Now()
		happy, _, _, _ := faultRun(t, c, 0, "")
		whole := time.Since(begin)
		for k, op := range happy.callLog() {
			if op != "CreateFloatingIP" {
				continue
			}
			ran = true
			start := time.Now()
			faultRun(t, c, k+1, faultCancel)
			d := time.Since(start)
			// The world's pace is milliseconds: the cut run is the whole
			// run and a few reads more. At the production interval the
			// three looks wait two seconds (floatingip.Lookup).
			// wall-clock: half of the two production pauses (deployplan.FIPLookupInterval, a second each), over the whole run.
			if d-whole > time.Second {
				t.Errorf("deploy_apply with its floating IP create cut took %s, the whole run %s: the look for the address waits a production interval", d, whole)
			}
		}
	}
	if !ran {
		t.Fatal("no deploy_apply case of the fault matrix creates a floating IP: the test checks nothing")
	}
}
