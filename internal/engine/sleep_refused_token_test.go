// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
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
var oneRefusalEnds = map[string]string{}

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

// sleepPath is a wait on a path of failure or interruption: setup makes
// the world it starts from (what the command made before), run is the
// wait, ok whether it reached its end.
type sleepPath struct {
	name  string
	setup func(t *testing.T, c *faultCloud) any
	run   func(e *Engine, c *faultCloud, made any) bool
}

func sleepVM(t *testing.T, c *faultCloud) *provision.VM {
	t.Helper()
	vm, err := c.CreateVM(context.Background(), provision.CreateVMRequest{Name: "evo-app", ProjectID: faultProject})
	if err != nil {
		t.Fatal(err)
	}
	return vm
}

func sleepFIP(t *testing.T, c *faultCloud) *provision.FloatingIP {
	t.Helper()
	fip, err := c.CreateFloatingIP(context.Background(), provision.CreateFloatingIPRequest{Name: "fip-evo-app", ProjectID: faultProject})
	if err != nil {
		t.Fatal(err)
	}
	return fip
}

var sleepPaths = []sleepPath{
	{
		// The floating IP made after boot: the wait for the VM's interface,
		// the address, its attach.
		name:  "assignFloatingIP",
		setup: func(t *testing.T, c *faultCloud) any { return sleepVM(t, c) },
		run: func(e *Engine, c *faultCloud, made any) bool {
			ip, _, err := assignFloatingIP(context.Background(), c, e.timings(), nil, made.(*provision.VM).ID, faultProject, faultZone)
			return err == nil && ip != ""
		},
	},
	{
		// After a Ctrl-C of the create: the VM is deleted, deploy waits for
		// it to go before it releases the address.
		name: "waitVMGone",
		setup: func(t *testing.T, c *faultCloud) any {
			vm := sleepVM(t, c)
			if err := c.DeleteVM(context.Background(), vm.ID); err != nil {
				t.Fatal(err)
			}
			return vm
		},
		run: func(e *Engine, c *faultCloud, made any) bool {
			return e.waitVMGone(context.Background(), made.(*provision.VM).ID)
		},
	},
	{
		// After a Ctrl-C of the create: the boot address made before it is
		// looked up by name (three lookups).
		name:  "findBootFIP",
		setup: func(t *testing.T, c *faultCloud) any { return sleepFIP(t, c) },
		run: func(e *Engine, c *faultCloud, made any) bool {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			f := e.findBootFIP(ctx, faultProject, made.(*provision.FloatingIP).Name)
			return f != nil && f.ID == made.(*provision.FloatingIP).ID
		},
	},
	{
		// The VM was not made: its boot address is released, retried while
		// it is still being created.
		name:  "releaseBootFIP",
		setup: func(t *testing.T, c *faultCloud) any { return sleepFIP(t, c) },
		run: func(e *Engine, c *faultCloud, made any) bool {
			return e.releaseBootFIP(made.(*provision.FloatingIP), faultProject) == nil && len(c.liveFIPs()) == 0
		},
	},
}

// oneRefusalEndsPath are the waits of sleepPaths where one refused token
// still ends the wait, "function<TAB>call" → why; a line whose wait goes
// on fails the test.
var oneRefusalEndsPath = map[string]string{}

// The waits on the paths of failure and interruption — the floating IP
// after boot, the VM going after a Ctrl-C, the boot address looked up and
// released — meet one refused token after the sleep at each of their
// calls, and go on to their end.
func TestSleep_OneRefusedTokenOnThePathsOfFailure(t *testing.T) {
	faultIsolate(t)
	seen := map[string]bool{}
	for _, p := range sleepPaths {
		happy := &faultCloud{}
		e, _, _ := faultEngine(t, happy, &faultDeployer{})
		made := p.setup(t, happy)
		from := len(happy.callLog())
		if !p.run(e, happy, made) {
			t.Fatalf("%s: the happy path does not reach its end — %v", p.name, happy.callLog()[from:])
		}
		log := happy.callLog()
		if len(log) == from {
			t.Fatalf("%s: calls nothing: the world checks nothing", p.name)
		}
		for k := from + 1; k <= len(log); k++ {
			call := log[k-1]
			if !computeRead(call) && !strings.HasPrefix(call, "Delete") {
				continue
			}
			key := p.name + "\t" + call
			seen[key] = true
			c := &faultCloud{}
			e, _, _ := faultEngine(t, c, &faultDeployer{})
			made := p.setup(t, c)
			c.failAt(k, faultAuth)
			ok := p.run(e, c, made)
			if why, declared := oneRefusalEndsPath[key]; declared {
				if ok {
					t.Errorf("%s: in oneRefusalEndsPath (%s), but the wait goes on after one refused token; take the line out", strings.Replace(key, "\t", " ", 1), why)
				}
				continue
			}
			if !ok {
				t.Errorf("%s (call %d): one refused token after the sleep ends the wait — %v", strings.Replace(key, "\t", " ", 1), k, c.callLog())
			}
		}
	}
	for key := range oneRefusalEndsPath {
		if !seen[key] {
			t.Errorf("oneRefusalEndsPath names %q, a call the wait no longer makes: remove the line", key)
		}
	}
}
