// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"strings"
	"testing"
)

// computeRead is a call of the compute world that reads; computeWrite one
// that changes the cloud.
func computeRead(call string) bool {
	return strings.HasPrefix(call, "Get") || strings.HasPrefix(call, "List")
}

func computeWrite(call string) bool {
	for _, p := range []string{"Create", "Attach", "Delete", "Update", "Start", "Stop", "Add"} {
		if strings.HasPrefix(call, p) {
			return true
		}
	}
	return false
}

// readsAfterWrite are the 1-based numbers of the calls of log that read
// after the first write: the waits and checks of a resource the call made.
func readsAfterWrite(log []string, from int) []int {
	var out []int
	wrote := false
	for i := from; i < len(log); i++ {
		switch {
		case computeWrite(log[i]):
			wrote = true
		case wrote && computeRead(log[i]):
			out = append(out, i+1)
		}
	}
	return out
}

// oneRefusalEnds are the reads after a write where one refused token
// still ends the command, "command<TAB>call" → why. Development 1.6 takes a
// line out when the read takes a new token and goes on; a line whose read
// already goes on fails the test.
var oneRefusalEnds = map[string]string{
	"deploy\tGetVM": "the VM wait stops at the first 401 with AUTH_EXPIRED «the key was refused» (R3A-4); provision of MCP goes on (its wait skips a failed poll)",
}

// The machine evo runs on sleeps in the middle of a command (CLAUDE.md
// §6, axes 2 and 3; process backlog 1.6, priority 1): after it wakes, the
// service refuses once the token evo still holds valid — the clock of a
// paused VM is behind — and evo has dropped it (G140-3), so the next
// request takes a new one. One such refusal at any read after the first
// write — the wait for the VM, its floating IP, its rules; the wait for
// its deletion — does not end deploy or destroy: the resource is there
// and billed, and an answer «the key was refused» sends the person to
// their key, not to the command again. A token refused on every read is
// the key's: TestDeploy_AuthExpiredDuringPollExitsAuth.
func TestSleep_OneRefusedTokenAfterTheWriteDoesNotEndTheCommand(t *testing.T) {
	faultIsolate(t)
	happy := &faultCloud{}
	eng, _, dir := faultEngine(t, happy, &faultDeployer{})
	if err := faultDeploy(t, eng, dir); err != nil {
		t.Fatal(err)
	}
	deployLog := happy.callLog()
	before := len(deployLog)
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	destroyLog := happy.callLog()

	seen := map[string]bool{}
	check := func(command, call string, err error) {
		key := command + "\t" + call
		seen[key] = true
		if why, declared := oneRefusalEnds[key]; declared {
			if err == nil {
				t.Errorf("%s: in oneRefusalEnds (%s), but the command goes on after one refused token; take the line out", strings.Replace(key, "\t", " ", 1), why)
			}
			return
		}
		if err != nil {
			t.Errorf("%s: one refused token after the write ends the command — %v", strings.Replace(key, "\t", " ", 1), err)
		}
	}
	reads := readsAfterWrite(deployLog, 0)
	if len(reads) == 0 {
		t.Fatalf("deploy reads nothing after its first write: the world checks nothing — %v", deployLog)
	}
	for _, k := range reads {
		cloud := &faultCloud{}
		e, _, d := faultEngine(t, cloud, &faultDeployer{})
		cloud.failAt(k, faultAuth)
		check("deploy", deployLog[k-1], faultDeploy(t, e, d))
	}
	reads = readsAfterWrite(destroyLog, before)
	if len(reads) == 0 {
		t.Fatalf("destroy reads nothing after its first delete: the world checks nothing — %v", destroyLog[before:])
	}
	for _, k := range reads {
		cloud := &faultCloud{}
		e, _, d := faultEngine(t, cloud, &faultDeployer{})
		if err := faultDeploy(t, e, d); err != nil {
			t.Fatal(err)
		}
		cloud.failAt(k, faultAuth)
		err := e.Destroy(context.Background(), DestroyOptions{Force: true})
		check("destroy", destroyLog[k-1], err)
		if err == nil && (len(cloud.liveVMs()) != 0 || len(cloud.liveFIPs()) != 0) {
			t.Errorf("destroy %s: after one refused token the command succeeded, but vms=%d fips=%d are left", destroyLog[k-1], len(cloud.liveVMs()), len(cloud.liveFIPs()))
		}
	}
	for key := range oneRefusalEnds {
		if !seen[key] {
			t.Errorf("oneRefusalEnds names %q, a read the commands no longer make after a write: remove the line", key)
		}
	}
}
