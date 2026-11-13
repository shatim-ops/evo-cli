// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"strings"
	"testing"
)

// No test opens a connection off loopback (plan 1.9: a test of deploy_apply
// waited for «port 22 did not answer» from 203.0.113.12 and failed on a
// network where that address takes TCP; CI was green because the address
// does not route there). Every night ci runs the whole go test under strace
// and offnet.sh reads each connect of a test binary; what the parser
// counts and lets through is offnet_test.sh's, which ci runs.
func TestWorkflows_OffnetWatchesTheTests(t *testing.T) {
	ws := readWorkflows(t)
	ci := ws["ci.yml"]
	j, ok := ci.Jobs["offnet"]
	if !ok {
		t.Fatal("ci: no job offnet — a test that dials off loopback goes unseen")
	}
	for _, ev := range []string{"github.event_name == 'schedule'", "github.event_name == 'workflow_dispatch'"} {
		if !strings.Contains(j.If, ev) {
			t.Errorf("ci: job offnet runs on %q, want %s among its events", j.If, ev)
		}
	}
	if p, _ := j.Permissions.(map[string]any); len(p) != 1 || p["contents"] != "read" {
		t.Errorf("ci: job offnet permissions %v, want contents: read alone", j.Permissions)
	}
	traced := false
	for _, s := range j.Steps {
		// --decode-pids=comm names the test binary of each connect; -f
		// follows go test into the binaries it starts.
		if strings.Contains(s.Run, "strace -f --seccomp-bpf --decode-pids=comm -e trace=connect") &&
			strings.Contains(s.Run, "go test") && strings.Contains(s.Run, ".github/scripts/offnet.sh") {
			traced = true
		}
	}
	if !traced {
		t.Error("ci: job offnet does not run go test under strace with the comm of each pid and read the trace with offnet.sh")
	}
	if !runsStep(ci, ".github/scripts/offnet_test.sh") {
		t.Error("ci: offnet.sh is not tested (offnet_test.sh)")
	}
}
