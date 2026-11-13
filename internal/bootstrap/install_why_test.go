// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package bootstrap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A run of the install script that did not finish says why before any file
// is checked: the error of the process, the state of its context, the last
// step it called and how long it took. TestBreakR4_R4_16 failed on macOS
// under a full run at 8.01 s against the 8 s limit with only «docker.list is
// not a regular file» (Астра, 14.11): the message named neither the kill nor
// the step, so a slow runner and a broken script read the same.
func TestRunWhy_NamesTheKillTheStepAndTheTime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX sh")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	root := t.TempDir()
	script := filepath.Join(t.TempDir(), "slow.sh")
	body := `echo "apt-get update" >>"$EVO_ROOT/calls"; echo started; exec sleep 5` + "\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, sh, script)
	cmd.Env = append(os.Environ(), "EVO_ROOT="+root)
	r := runScript(ctx, cmd, root)
	if r.ok {
		t.Fatalf("a killed script reads as ok: %+v", r)
	}
	why := r.why()
	for _, want := range []string{"context deadline exceeded", "killed", "apt-get update", "started", "took"} {
		if !strings.Contains(why, want) {
			t.Errorf("why() = %q, want %q in it", why, want)
		}
	}

	// A script that finished names no kill.
	ok := filepath.Join(t.TempDir(), "ok.sh")
	if err := os.WriteFile(ok, []byte("echo done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	r = runScript(ctx2, exec.CommandContext(ctx2, sh, ok), t.TempDir())
	if !r.ok || strings.Contains(r.why(), "deadline") {
		t.Errorf("a finished script: ok=%v why=%q", r.ok, r.why())
	}
}
