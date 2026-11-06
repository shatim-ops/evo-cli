// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"testing"
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
