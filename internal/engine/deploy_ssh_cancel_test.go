// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"
	"time"
)

// cancelWatch is how long a deploy cancelled in its wait for SSH may take
// to end.
// wall-clock: a margin for a deploy that ends at the cancel.
const cancelWatch = 5 * time.Second

// FB171-2 (delta audit of 1.7.1): a call cancelled while deploy waits for
// SSH — the MCP client, Ctrl-C — wrote ssh_timeout into the record and
// dropped its ssh_wait: logs and verify then went over SSH to a VM that
// was still booting, and the next deploy began its count again. A cancel is
// not the end of the wait: the record stays unfinished, with its wait.
func TestDeploy_CancelInTheSSHPhaseKeepsTheRecordUnfinished(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dep := newMockDeployer()
	dep.hold = func(hctx context.Context, op string) error {
		if op == "connect" {
			cancel()
			<-hctx.Done()
			return hctx.Err()
		}
		return nil
	}
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	eng.Timings.SSHRetryInterval = time.Hour
	got := make(chan error, 1)
	go func() { got <- eng.Deploy(ctx, newDeployOpts(t, dir)) }()
	select {
	case err := <-got:
		if err == nil {
			t.Fatal("a cancelled deploy succeeded")
		}
	case <-time.After(cancelWatch):
		t.Fatal("deploy did not end at the cancel")
	}
	rec := readActive(t, dir)
	if rec == nil || !unfinishedStatuses[rec.Status] {
		t.Fatalf("a cancel in the wait for SSH left the record %+v: not unfinished, so logs and verify go over SSH", rec)
	}
	if rec.SSHWait == nil {
		t.Errorf("a cancel dropped the record's wait for SSH: the next deploy counts its attempts anew")
	}
	unfinished(t, "logs", rec.Status, eng.Logs(context.Background(), LogsOptions{Tail: 10}))
}
