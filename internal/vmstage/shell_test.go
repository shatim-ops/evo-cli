// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package vmstage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The stage script on a real shell (Linux, no cloud): what the world of
// deploy (TestDeployStage_OnceAndNotCutShort) takes as given — the stage
// outlives the session that started it, one stage at a time, a stage no
// process holds reads lost — is the script's, and only a shell shows it
// (agreement with «Процесс · 1.7» on PR 4 of D69). Each case runs with
// flock and with the lock directory of a VM without it.

// localVM runs commands as the SSH session would: sh -c, with HOME and
// PATH of the test's VM, and returns when the session's command does.
type localVM struct{ home, path string }

func (v localVM) Exec(ctx context.Context, cmd string) (string, error) {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Env = []string{"HOME=" + v.home, "PATH=" + v.path}
	out, err := c.Output()
	return string(out), err
}

// stubDocker is the docker the stage runs: it writes its pid, then runs
// until SIGUSR1 (done) or exits with the code in $HOME/docker.exit.
const stubDocker = `#!/bin/sh
echo $$ >"$HOME/docker.pid"
echo pulling
if [ -f "$HOME/docker.exit" ]; then exit "$(cat "$HOME/docker.exit")"; fi
trap 'echo finished; exit 0' USR1
while :; do sleep 0.02; done
`

// newVM makes the VM: a home, and a PATH of the tools the script uses
// (with flock or without) and the stub docker.
func newVM(t *testing.T, withFlock bool) localVM {
	t.Helper()
	home, bin := t.TempDir(), t.TempDir()
	tools := []string{"sh", "mkdir", "cat", "sed", "awk", "base64", "tail", "setsid", "nohup", "rm", "sleep", "printf", "mv"}
	if withFlock {
		tools = append(tools, "flock")
	}
	for _, tool := range tools {
		p, err := exec.LookPath(tool)
		if err != nil {
			// The wrapper has no other cover: on CI a missing tool fails
			// the run instead of dropping the invariant without a word
			// («Процесс · 1.7»).
			if os.Getenv("CI") != "" {
				t.Fatalf("no %s on the CI runner: the stage script is not tested", tool)
			}
			t.Skipf("no %s on this machine", tool)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(stubDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	return localVM{home: home, path: bin}
}

func (v localVM) dockerPID(t *testing.T) int {
	t.Helper()
	var pid int
	waitFor(t, "the stage's docker to start", func() bool {
		b, err := os.ReadFile(filepath.Join(v.home, "docker.pid"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil && pid > 0
	})
	return pid
}

// waitFor polls cond.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	// wall-clock: the stage is a process on this machine; it starts and
	// ends within milliseconds, the bound only ends a broken run.
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func read(t *testing.T, r Runner, name string) Stage {
	t.Helper()
	st, err := r.Read(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func forEachLock(t *testing.T, f func(t *testing.T, vm localVM)) {
	for _, withFlock := range []bool{true, false} {
		name := "flock"
		if !withFlock {
			name = "lock directory"
		}
		t.Run(name, func(t *testing.T) { f(t, newVM(t, withFlock)) })
	}
}

// The stage outlives the session that started it; a second start while it
// runs starts nothing; its end reads done with the tail of its log.
func TestStage_OutlivesTheSessionOnceAndDone(t *testing.T) {
	forEachLock(t, func(t *testing.T, vm localVM) {
		r := Over(vm)
		started, err := r.Start(context.Background(), "compose-up", "in1", "docker compose up -d")
		if err != nil || !started {
			t.Fatalf("start: %v %v", started, err)
		}
		pid := vm.dockerPID(t)
		// The session's command has returned (Exec): the stage runs on.
		if st := read(t, r, "compose-up"); st.State != "running" || st.Input != "in1" {
			t.Fatalf("after the session ended: %+v", st)
		}
		if again, err := r.Start(context.Background(), "compose-up", "in1", "docker compose up -d"); err != nil || again {
			t.Fatalf("a second start while it runs: started=%v %v", again, err)
		}
		if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the stage to end", func() bool { return read(t, r, "compose-up").State == "done" })
		st := read(t, r, "compose-up")
		if st.Exit != 0 || !strings.Contains(st.LogTail, "finished") || !strings.Contains(st.LogTail, "pulling") {
			t.Errorf("done: %+v", st)
		}
	})
}

// A stage that fails reads failed, with its code and log.
func TestStage_Failed(t *testing.T) {
	forEachLock(t, func(t *testing.T, vm localVM) {
		if err := os.WriteFile(filepath.Join(vm.home, "docker.exit"), []byte("3"), 0o644); err != nil {
			t.Fatal(err)
		}
		r := Over(vm)
		if _, err := r.Start(context.Background(), "compose-up", "in1", "docker compose up -d"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the stage to fail", func() bool { return read(t, r, "compose-up").State == "failed" })
		if st := read(t, r, "compose-up"); st.Exit != 3 || !strings.Contains(st.LogTail, "pulling") {
			t.Errorf("failed: %+v", st)
		}
	})
}

// Killed with all its processes — what a reboot does — the stage reads
// lost, not running for ever; a start after it runs it again.
func TestStage_KilledIsLost(t *testing.T) {
	forEachLock(t, func(t *testing.T, vm localVM) {
		r := Over(vm)
		if _, err := r.Start(context.Background(), "compose-up", "in1", "docker compose up -d"); err != nil {
			t.Fatal(err)
		}
		pid := vm.dockerPID(t)
		// The stage's process group: setsid made the runner its leader.
		pgid, err := syscall.Getpgid(pid)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the stage to read lost", func() bool { return read(t, r, "compose-up").State == "lost" })
		_ = os.Remove(filepath.Join(vm.home, "docker.pid"))
		started, err := r.Start(context.Background(), "compose-up", "in1", "docker compose up -d")
		if err != nil || !started {
			t.Fatalf("a start after a lost stage: started=%v %v", started, err)
		}
		_ = syscall.Kill(vm.dockerPID(t), syscall.SIGUSR1)
		waitFor(t, "the restarted stage to end", func() bool { return read(t, r, "compose-up").State == "done" })
	})
}

// The lock directory names its owner by pid, boot and start time: a live
// process that took the pid of a stage the VM lost (a reboot) is not its
// owner — the stage reads lost («Процесс · 1.7»).
func TestStage_LockDirectoryOfAForeignPIDIsLost(t *testing.T) {
	vm := newVM(t, false)
	d := filepath.Join(vm.home, ".evo", "stage", "compose-up")
	if err := os.MkdirAll(filepath.Join(d, "lockdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Skip("no boot_id")
	}
	// This test's own process: alive, but not started when the owner was.
	owner := strconv.Itoa(os.Getpid()) + " " + strings.TrimSpace(string(boot)) + " 1"
	for file, body := range map[string]string{"lockdir/owner": owner, "state": "running", "input": "in1"} {
		if err := os.WriteFile(filepath.Join(d, file), []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if st := read(t, Over(vm), "compose-up"); st.State != "lost" {
		t.Errorf("a foreign live pid in the lock directory: %+v, want lost", st)
	}
}

// A stage never started reads none.
func TestStage_None(t *testing.T) {
	vm := newVM(t, true)
	if st := read(t, Over(vm), "compose-up"); st.State != "none" {
		t.Errorf("never started: %+v", st)
	}
}

// The check of start (held) takes the lock for an instant; the stage's own
// process must not give up when it meets that instant — it waits for the
// lock («1.7 — релиз» on #769: 2 of 300 starts read lost under load). Here
// another holder keeps the lock while the stage starts: the stage waits,
// then runs.
func TestStage_RunWaitsForTheLockAStatusCheckHolds(t *testing.T) {
	vm := newVM(t, true)
	d := filepath.Join(vm.home, ".evo", "stage", "compose-up")
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	flock, err := exec.LookPath("flock")
	if err != nil {
		t.Skip("no flock")
	}
	holder := exec.Command(flock, filepath.Join(d, "lock"), "sleep", "0.5")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Wait() })
	waitFor(t, "the other holder to take the lock", func() bool {
		return exec.Command(flock, "-n", filepath.Join(d, "lock"), "true").Run() != nil
	})
	r := Over(vm)
	if _, err := r.Start(context.Background(), "compose-up", "in1", "docker compose up -d"); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()
	pid := vm.dockerPID(t)
	if st := read(t, r, "compose-up"); st.State != "running" {
		t.Fatalf("the stage gave up on a held lock: %+v", st)
	}
	_ = syscall.Kill(pid, syscall.SIGUSR1)
	waitFor(t, "the stage to end", func() bool { return read(t, r, "compose-up").State == "done" })
}

// Two starts at once: the second finds the first's stage and starts
// nothing; the first's script and log are its own («1.7 — релиз» on #769).
func TestStage_TwoStartsAtOnceRunOneStage(t *testing.T) {
	forEachLock(t, func(t *testing.T, vm localVM) {
		r := Over(vm)
		got := make(chan bool, 2)
		for _, in := range []string{"in1", "in2"} {
			go func() {
				started, err := r.Start(context.Background(), "compose-up", in, "docker compose up -d")
				if err != nil {
					t.Error(err)
				}
				got <- started
			}()
		}
		a, b := <-got, <-got
		if a == b {
			t.Fatalf("two starts at once: started %v and %v, want one", a, b)
		}
		pid := vm.dockerPID(t)
		_ = syscall.Kill(pid, syscall.SIGUSR1)
		waitFor(t, "the stage to end", func() bool { return read(t, r, "compose-up").State == "done" })
		if st := read(t, r, "compose-up"); !strings.Contains(st.LogTail, "finished") {
			t.Errorf("the log of the stage that ran was cut: %+v", st)
		}
	})
}

// The stage's files are its user's only: the script and the log may hold
// what the compose project prints («1.7 — релиз» on #769).
func TestStage_FilesAreTheUsersOnly(t *testing.T) {
	vm := newVM(t, true)
	r := Over(vm)
	if _, err := r.Start(context.Background(), "compose-up", "in1", "docker compose up -d"); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Kill(vm.dockerPID(t), syscall.SIGUSR1)
	waitFor(t, "the stage to end", func() bool { return read(t, r, "compose-up").State == "done" })
	for _, f := range []string{"script", "log", "input", "state"} {
		fi, err := os.Stat(filepath.Join(vm.home, ".evo", "stage", "compose-up", f))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is %v: readable by others", f, fi.Mode().Perm())
		}
	}
}

// The stage script is put in anew by each call while a stage runs from it:
// it is written aside and moved in — a new file, never the running one
// rewritten in place («1.7 — релиз» on #769).
func TestStage_TheScriptIsReplacedNotRewritten(t *testing.T) {
	vm := newVM(t, true)
	r := Over(vm)
	if _, err := r.Start(context.Background(), "compose-up", "in1", "docker compose up -d"); err != nil {
		t.Fatal(err)
	}
	pid := vm.dockerPID(t)
	path := filepath.Join(vm.home, ".evo", "stage", "evo-stage.sh")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	read(t, r, "compose-up")
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("the script the stage runs from was rewritten in place")
	}
	if after.Mode().Perm()&0o077 != 0 {
		t.Errorf("the script is %v: readable by others", after.Mode().Perm())
	}
	_ = syscall.Kill(pid, syscall.SIGUSR1)
	waitFor(t, "the stage to end", func() bool { return read(t, r, "compose-up").State == "done" })
}
