// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// The answer of a failed docker compose up sends the person to the logs of
// the services on the VM («Check your compose file and service logs on the
// VM»), and its record stays deploying so that a re-run retries
// (TestDeploy_ComposeUpFailsOnNewVM_RerunRetries). logs and verify read
// deploying as a deploy not finished (D76) and answer DEPLOY_IN_PROGRESS:
// the logs the answer points to are refused, and the agent is told the
// deploy goes on when it is over (FB171-4, the class of #1042: «Процесс ·
// 1.8 (2)»). After a failed compose up, logs reads the services' logs.
func TestLogs_AfterComposeUpFailedReadsTheServices(t *testing.T) {
	dep := newMockDeployer()
	upCmd := "cd /opt/app && docker compose -f docker-compose.yml up -d"
	dep.streamErrs = map[string][]error{upCmd: {stderrors.New("pull access denied")}}
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err == nil {
		t.Fatal("deploy with a failing compose up succeeded")
	}
	err := eng.Logs(context.Background(), LogsOptions{Tail: 10})
	var ue *DeployUnfinishedError
	if stderrors.As(err, &ue) {
		t.Errorf("logs after a failed compose up: %v — the deploy is over and its answer points to these logs; the record says %q", err, ue.Status)
	}
}

// D87: a deploy that ended at the upload or at compose up is over —
// upload_failed, compose_failed — and logs and verify read it as such:
// verify probes the service (UNHEALTHY when it does not answer), not
// DEPLOY_IN_PROGRESS.
func TestVerify_AfterTheDeployFailedProbesTheService(t *testing.T) {
	for _, status := range []string{"upload_failed", "compose_failed"} {
		rec := verifyRecord
		rec.Status = status
		eng, _, _ := engineWithDeployment(t, rec)
		eng.Probe = probeReturning(map[int]bool{})
		err := eng.Verify(context.Background())
		if r := clerrors.Reason(err); r != clerrors.CodeUnhealthy {
			t.Errorf("verify of a record %s: %v (%s), want UNHEALTHY: the deploy is over", status, err, r)
		}
	}
}

// D87: the answer of a failed upload is UPLOAD_FAILED, not the SSH error
// it carries, and the record says upload_failed; a re-run uploads again.
func TestDeploy_UploadFailedIsItsOwnReasonAndARerunRetries(t *testing.T) {
	dep := newMockDeployer()
	dep.uploadErr = stderrors.New("scp: write failed")
	eng, dir := newTestEngine(t, &mockCloudAPI{}, dep)
	err := eng.Deploy(context.Background(), newDeployOpts(t, dir))
	if r := clerrors.Reason(err); r != clerrors.CodeUploadFailed {
		t.Fatalf("deploy with a failing upload: %v (%s), want UPLOAD_FAILED", err, r)
	}
	if a := readActive(t, dir); a == nil || a.Status != "upload_failed" {
		t.Fatalf("record %+v, want upload_failed", a)
	}
	dep.uploadErr = nil
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("re-run after a failed upload: %v", err)
	}
	if a := readActive(t, dir); a == nil || a.Status != "running" {
		t.Errorf("record after the re-run %+v, want running", a)
	}
}
