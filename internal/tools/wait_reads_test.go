// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A wait whose reads fail after one that saw the resource ends at its
// limit with the state it read last — and says when it read it and how
// many reads failed after (CLAUDE.md §6, axis 2; cloud-e2e 228, the line
// of #2216): the state named may be old.
func TestWaitState_SaysWhenItLastReadAndHowManyReadsFailed(t *testing.T) {
	old := clock.Now
	seen := time.Date(2026, 11, 17, 17, 51, 37, 0, time.UTC)
	clock.Now = func() time.Time { return seen }
	t.Cleanup(func() { clock.Now = old })
	eng := &engine.Engine{Timings: engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: 30 * time.Millisecond}}
	n := 0
	last, timedOut, _, reads, err := waitState(context.Background(), eng, time.Second,
		func(context.Context) (string, bool, error) {
			n++
			return "starting", n == 1, nil
		},
		func(st string) string { return st },
		func(string) (bool, error) { return false, nil })
	if err != nil || last != "starting" || timedOut == 0 {
		t.Fatalf("waitState: %q, %v, %v", last, timedOut, err)
	}
	if !reads.lastSeen.Equal(seen) || reads.failed != n-1 || reads.failed == 0 {
		t.Errorf("reads %+v after %d reads, want the first one seen and %d failed", reads, n, n-1)
	}
	ans := map[string]any{}
	reads.put(ans)
	if ans["last_read_at"] != "2026-11-17T17:51:37Z" || ans["failed_reads"] != n-1 {
		t.Errorf("answer %v", ans)
	}
	// No read saw the resource: no time, all the reads failed.
	none := waitReads{failed: 3}
	ans = map[string]any{}
	none.put(ans)
	if _, has := ans["last_read_at"]; has || ans["failed_reads"] != 3 {
		t.Errorf("no read seen: %v", ans)
	}
}

// vm_start at the limit of an MCP call, its reads of the VM failing after
// one saw it starting: the answer is the state so far, as before, with the
// time of that read and the reads that failed after it.
func TestVMStart_AtTheLimitSaysTheReadsThatFailed(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &faultCloud{}
	vm, err := cloud.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	cloud.setVMState(vm.ID, "stopped")
	r := budgeted(budgetComputeRegistry(t, cloud, t.TempDir(), engine.Timings{VMPollInterval: time.Millisecond}))
	cloud.failNthSticky("GetVMDetail", 4, faultTransport)
	text, isErr := r.handlers["vm_start"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	failed, _ := m["failed_reads"].(float64)
	if isErr || m["status"] != "starting" || failed == 0 || m["last_read_at"] == nil {
		t.Errorf("an answer at the limit with failed reads: %s", text)
	}
}
