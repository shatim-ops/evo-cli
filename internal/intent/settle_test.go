// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// deadPID is the pid of a process that ran and is gone.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// Settle removed every record of the same tool, resource, project and key
// but its own — not only the earlier ones and not only those of calls that
// were cut: the answer of one of two calls in flight, even a refusal,
// erased the record of the other, and a call killed after that left no
// trace (the interim delta audit of 1.9, FB19I-34; «Риск», conclusion 177,
// У6 and У8). A record is removed only when it is of the same call, began
// no later than the one that answered, and its call is not in flight: it
// said it ended, or its process is gone.
func TestSettle_OnlyAnEarlierCallThatIsNotInFlight(t *testing.T) {
	key := &state.Maker{KeyID: "k"}
	canon := []byte(`{"name":"data","size_gb":10}`)
	t0 := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	gone := deadPID(t)
	for _, c := range []struct {
		name    string
		in      Intent
		ended   bool
		removed bool
	}{
		{"a cut call of a process that is gone", Intent{PID: gone, StartedAt: t0, Canon: canon}, false, true},
		{"a call of this process that returned", Intent{PID: os.Getpid(), StartedAt: t0, Canon: canon}, true, true},
		{"a call of this process in flight", Intent{PID: os.Getpid(), StartedAt: t0, Canon: canon}, false, false},
		{"a call of another live process in flight", Intent{PID: os.Getppid(), StartedAt: t0, Canon: canon}, false, false},
		{"a call of another live process that returned", Intent{PID: os.Getppid(), StartedAt: t0, Canon: canon}, true, true},
		{"a later call", Intent{PID: gone, StartedAt: t0.Add(2 * time.Minute), Canon: canon}, true, false},
		{"another call on the same resource", Intent{PID: gone, StartedAt: t0, Canon: []byte(`{"name":"data","size_gb":20}`)}, true, false},
		{"an operation going on", Intent{PID: gone, StartedAt: t0, Canon: canon, Held: true}, false, false},
		{"a call whose key is not known", Intent{PID: gone, StartedAt: t0, Canon: canon, Key: nil}, true, false},
		// A record of evo before the print: the same call by its name,
		// closed by a later call only.
		{"a record without a print", Intent{PID: gone, StartedAt: t0}, false, true},
		{"a later record without a print", Intent{PID: gone, StartedAt: t0.Add(2 * time.Minute)}, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := Dir(t.TempDir())
			c.in.Tool, c.in.Name, c.in.ProjectID = "disk_create", "data", "p"
			if c.name != "a call whose key is not known" {
				c.in.Key = key
			}
			rec, err := Open(dir, c.in)
			if err != nil {
				t.Fatal(err)
			}
			if c.in.Held {
				Hold(rec)
			}
			if c.ended {
				End(rec)
			}
			done, err := Open(dir, Intent{Tool: "disk_create", Name: "data", ProjectID: "p", Key: key, PID: os.Getpid(),
				StartedAt: t0.Add(time.Minute), Canon: canon})
			if err != nil {
				t.Fatal(err)
			}
			Close(done)
			Settle(dir, done)
			if removed := len(Pending(dir)) == 0; removed != c.removed {
				t.Errorf("removed %v, want %v: %+v", removed, c.removed, Pending(dir))
			}
		})
	}
}

// The same arguments on another resource are another call: a name that
// now resolves to another group (renamed, its name taken by a new one) —
// the call there says nothing of the cut call on the first, whose record
// stays (R-101). Same arguments and the same target, or a record with no
// target, are settled as before.
func TestSettle_TheSameArgumentsOnAnotherTargetAreAnotherCall(t *testing.T) {
	key := &state.Maker{KeyID: "k"}
	canon := []byte(`{"security_group":"db","ports":"8080"}`)
	t0 := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	gone := deadPID(t)
	for _, c := range []struct {
		name, was, now string
		removed        bool
	}{
		{"another target", "security_group=g1", "security_group=g2", false},
		{"the same target", "security_group=g1", "security_group=g1", true},
		{"the record names no target", "", "security_group=g2", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := Dir(t.TempDir())
			if _, err := Open(dir, Intent{Tool: "sg_rule_create", Target: c.was, ProjectID: "p", Key: key, PID: gone, StartedAt: t0, Canon: canon}); err != nil {
				t.Fatal(err)
			}
			done, err := Open(dir, Intent{Tool: "sg_rule_create", Target: c.now, ProjectID: "p", Key: key, PID: os.Getpid(),
				StartedAt: t0.Add(time.Minute), Canon: canon})
			if err != nil {
				t.Fatal(err)
			}
			Close(done)
			Settle(dir, done)
			if removed := len(Pending(dir)) == 0; removed != c.removed {
				t.Errorf("removed %v, want %v: %+v", removed, c.removed, Pending(dir))
			}
		})
	}
}

// The print tells the same arguments in the same record and shows nothing
// between two records: the id of the record is in the hash.
func TestPrint_OfTheRecordNotOfTheArgumentsAlone(t *testing.T) {
	dir := Dir(t.TempDir())
	canon := []byte(`{"name":"data"}`)
	a, _ := Open(dir, Intent{Tool: "disk_create", Name: "data", Canon: canon})
	b, _ := Open(dir, Intent{Tool: "disk_create", Name: "data", Canon: canon})
	if a.Print == "" || a.Print == b.Print {
		t.Errorf("two records of the same call have the prints %q and %q", a.Print, b.Print)
	}
	for _, in := range Pending(dir) {
		if !in.SameCall(canon) || in.SameCall([]byte(`{"name":"other"}`)) || in.SameCall(nil) {
			t.Errorf("the record read back does not tell its call: %+v", in)
		}
	}
	// The file of a record is the owner's alone, as the state is.
	if info, err := os.Stat(recordPath(dir, a.ID)); err != nil || (info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/') {
		t.Errorf("the file of the record: %v, %v", info.Mode().Perm(), err)
	}
}
