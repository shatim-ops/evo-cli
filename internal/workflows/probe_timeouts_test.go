// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

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

// A request of cloud-live.sh to an API of Cloud.ru — the token, a probe,
// a sweep — has a limit of time for the connection and for the answer. A
// request that hangs would keep the paid resources of the run until the
// limit of the job cuts it, with no sweep after (risk note 185, the debt
// named with it; R-89).
func TestScripts_CloudRequestsHaveTimeLimits(t *testing.T) {
	// The sweep of the stand (e2e_stand.sh) sends the same token for up to
	// half an hour: its requests had no limit (review of #1658).
	// cloud-live uses five cloud curl sites: IAM, AR, DNS and the two
	// branches of api_once, which also handle the network cleanup.
	for _, f := range []struct {
		name string
		min  int
	}{{"cloud-live.sh", 5}, {"e2e_stand.sh", 3}} {
		cloudRequestsHaveTimeLimits(t, f.name, f.min)
	}
}

func cloudRequestsHaveTimeLimits(t *testing.T, name string, min int) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "scripts", name))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "#") || !strings.Contains(l, "curl ") {
			continue
		}
		// A command goes on to the next line: the header with the token
		// is often there.
		for j := i; strings.HasSuffix(strings.TrimSpace(lines[j]), "\\") && j+1 < len(lines); j++ {
			l += " " + lines[j+1]
		}
		// The local evo mcp of the run is not the cloud (its request has a
		// limit of its own).
		if strings.Contains(l, "http://127.0.0.1") || (!strings.Contains(l, "Authorization: Bearer") && !strings.Contains(l, "api.cloud.ru")) {
			continue
		}
		checked++
		if !strings.Contains(l, "--connect-timeout ") || !strings.Contains(l, "--max-time ") {
			t.Errorf("%s:%d: a request to the cloud without --connect-timeout and --max-time: %s", name, i+1, strings.TrimSpace(l))
		}
	}
	if checked < min {
		t.Fatalf("%s: %d requests to the cloud read, want at least %d: the scan sees too few", name, checked, min)
	}
}

// The net cleanup fixture uses Linux CI tooling. Dropping its runner guard
// or running the shell without a deadline changes the environment contract.
func TestWorkflows_NetCleanupFixture(t *testing.T) {
	const script = ".github/scripts/cloud_live_net_cleanup_test.sh"
	found := false
	for _, step := range readWorkflows(t)["ci.yml"].Jobs["test"].Steps {
		if !strings.Contains(step.Run, script) {
			continue
		}
		found = true
		if step.If != "runner.os == 'Linux'" || strings.TrimSpace(step.Run) != "timeout 120 "+script || step.ContinueOnError {
			t.Errorf("net cleanup fixture must run on Linux with a 120s deadline and propagate failure: if=%q run=%q continue-on-error=%t", step.If, step.Run, step.ContinueOnError)
		}
	}
	if !found {
		t.Error("net cleanup fixture missing from ci test job")
	}
}

// Exercise the local entry point without cloud access or waiting 120s. The
// tool that enforces the deadline must receive the real fixture and its exit
// status must reach the caller; an unsupported kernel exits before any probe.
func TestNetCleanup_LocalPreflight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("standalone check uses a POSIX shell")
	}
	for _, tc := range []struct {
		kernel string
		exit   int
	}{
		{"Linux", 124}, {"Darwin", 77},
	} {
		t.Run(tc.kernel, func(t *testing.T) {
			bin := t.TempDir()
			for name, body := range map[string]string{
				"uname":   "echo " + tc.kernel,
				"bash":    "exit 1",
				"jq":      "exit 1",
				"timeout": "[ \"$*\" = '-k 5 120 .github/scripts/cloud_live_net_cleanup_test.sh' ] && [ -x \"$4\" ] || exit 2\nexit 124",
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", "../../scripts/test-net-cleanup.sh")
			out, err := cmd.CombinedOutput()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != tc.exit {
				t.Errorf("exit %v, want %d: %s", err, tc.exit, out)
			}
		})
	}
}
