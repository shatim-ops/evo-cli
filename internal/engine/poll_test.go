// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The first poll goes at once: a VM that is already active (or already
// gone) does not cost a full interval. With an hour-long interval the old
// ticker ran into the poll timeout instead.
func TestDeploy_FirstVMPollIsImmediate(t *testing.T) {
	cloud := &mockCloudAPI{}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Timings.VMPollInterval = time.Hour
	eng.Timings.VMPollTimeout = 20 * time.Second
	start := time.Now()
	err := eng.Deploy(context.Background(), DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small",
		Region: "az-1", NonInteractive: true, Yes: true})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	// wall-clock: guards running into the poll timeout (20 s here): half of it.
	if d := time.Since(start); d > eng.Timings.VMPollTimeout/2 {
		t.Errorf("deploy took %s: the first poll waited for the interval", d)
	}
}

func TestDestroy_FirstDeletionPollIsImmediate(t *testing.T) {
	eng, _, out := destroyFixture(t, runningRecord())
	eng.Timings.DestroyPollInterval = time.Hour
	eng.Timings.DestroyPollTimeout = 20 * time.Second
	start := time.Now()
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	// wall-clock: guards running into the poll timeout (20 s here): half of it.
	if d := time.Since(start); d > eng.Timings.DestroyPollTimeout/2 {
		t.Errorf("destroy took %s: the first poll waited for the interval", d)
	}
	if !strings.Contains(out.String(), "VM deleted") {
		t.Errorf("deletion not confirmed:\n%s", out.String())
	}
}

// After the immediate tick the poll ticker goes on at the interval, and
// stop ends it.
func TestNewPollTicker(t *testing.T) {
	c, stop := newPollTicker(5 * time.Millisecond)
	defer stop()
	for i := 0; i < 3; i++ {
		select {
		case <-c:
		case <-time.After(2 * time.Second):
			t.Fatalf("tick %d did not come", i)
		}
	}
}
