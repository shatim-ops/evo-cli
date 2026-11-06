// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// boundWorld is a deploy that ends one way without a bound: a step fails,
// or it succeeds.
type boundWorld struct {
	name  string
	dep   func() *mockDeployer
	cloud func() *mockCloudAPI
}

// boundWorlds are the ends of deploy the class invariant runs: a failure in
// each phase that writes a record, and a deploy that succeeds.
var boundWorlds = []boundWorld{
	{name: "success", dep: newMockDeployer},
	{name: "vm in error", dep: newMockDeployer, cloud: func() *mockCloudAPI {
		return &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
			return &provision.VM{ID: id, Status: "error_creating"}, nil
		}}
	}},
	{name: "ssh never answers", dep: func() *mockDeployer {
		dep := newMockDeployer()
		dep.connectErr = stderrors.New("ssh: connection timed out")
		return dep
	}},
	{name: "docker install fails", dep: func() *mockDeployer {
		dep := newMockDeployer()
		dep.streamHook = func(_ context.Context, cmd string) error {
			if strings.Contains(cmd, "base64 -d") {
				return stderrors.New("exit status 6")
			}
			return nil
		}
		return dep
	}},
	{name: "upload fails", dep: func() *mockDeployer {
		dep := newMockDeployer()
		dep.uploadErr = stderrors.New("sftp: connection lost")
		return dep
	}},
}

// boundClock is the clock of the bound of one deploy: «past» from its
// at-th reading on (0 — never). It counts the readings.
type boundClock struct {
	mu        sync.Mutex
	at, reads int
	until     time.Time
}

func (c *boundClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.at > 0 && c.reads >= c.at {
		return c.until.Add(time.Second)
	}
	return c.until.Add(-time.Hour)
}

// runAtBound runs the deploy of w with the bound coming at the at-th
// reading of its clock; it returns the answer, the record's status and how
// many times the clock was read.
func runAtBound(t *testing.T, w boundWorld, at int) (error, string, int) {
	t.Helper()
	cloud := &mockCloudAPI{}
	if w.cloud != nil {
		cloud = w.cloud()
	}
	eng, dir := newTestEngine(t, cloud, w.dep())
	eng.Timings.VMPollInterval = time.Millisecond
	eng.Timings.SSHRetryInterval = time.Millisecond
	eng.Timings.DockerWaitTimeout = 20 * time.Millisecond
	opts := newDeployOpts(t, dir)
	// The bound of the call, far on the wall clock: only the readings of
	// Engine.now bring it, so the run is the same on any runner.
	opts.Until = time.Now().Add(time.Hour)
	clk := &boundClock{at: at, until: opts.Until}
	eng.now = clk.now
	err := eng.Deploy(context.Background(), opts)
	status := ""
	if a := readActive(t, dir); a != nil {
		status = a.Status
	}
	clk.mu.Lock()
	defer clk.mu.Unlock()
	return err, status, clk.reads
}

// FB171-4, the class (release plan 1.8, Process; owner's decision 05.11,
// #918): wherever the bound of an MCP call comes in deploy, the record
// and the answer come from one decision. #914 fixed the three phases it
// knew (vm, ssh, docker) with a test for each; this runs every end of
// deploy in boundWorlds with the bound coming before each reading of its
// clock in turn — every point where deploy decides by the bound (past) —
// and checks the pair at the end against the same world without a bound:
//   - the answer «deploy goes on» (*InProgress) — the record unfinished
//     (D76: logs and verify answer DEPLOY_IN_PROGRESS), or running when
//     a running deployment is updated;
//   - any other answer — the answer and the record of the world without a
//     bound: the bound does not turn the record alone (FB171-4: ssh_timeout
//     on disk, deploying in the answer) or the answer alone.
//
// The bound comes by the count of readings, not by the wall clock: the
// run is deterministic, and a new place that decides by the bound is
// covered without a line here. A new end of deploy is a line of
// boundWorlds.
func TestDeploy_AtAnyBoundRecordAndAnswerAgree(t *testing.T) {
	decided := 0
	for _, w := range boundWorlds {
		t.Run(w.name, func(t *testing.T) {
			baseErr, baseStatus, reads := runAtBound(t, w, 0)
			if baseErr == nil && baseStatus != "running" {
				t.Errorf("no bound: no error, the record says %q", baseStatus)
			}
			decided += reads
			for at := 1; at <= reads; at++ {
				err, status, _ := runAtBound(t, w, at)
				var going *InProgress
				switch {
				case stderrors.As(err, &going):
					if !unfinishedStatuses[status] && status != "running" {
						t.Errorf("bound at reading %d of %d: the answer says deploy goes on (phase %s), the record says %q", at, reads, going.Phase, status)
					}
				case status != baseStatus || errText(err) != errText(baseErr):
					t.Errorf("bound at reading %d of %d: answer %q, record %q; without the bound — answer %q, record %q: the bound changed one of them alone",
						at, reads, errText(err), status, errText(baseErr), baseStatus)
				}
			}
		})
	}
	if decided == 0 {
		t.Fatal("no world reached a decision by the bound: boundWorlds see nothing")
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
