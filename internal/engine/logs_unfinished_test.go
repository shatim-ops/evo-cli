// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/verify"
)

// cloud-e2e of rc.1 (mcp): after deploy answered deploying and status
// said creating, logs answered SSH_AUTH_FAILED — the key evo has is the
// right one, cloud-init had not put it on the VM yet. logs and verify of a
// deployment deploy has not finished (its record creating, provisioned or
// deploying) say so, with the record's status, and name deploy as the way
// on (axis 2, D69), not a failure of SSH or of the service.
func TestLogsVerify_AnUnfinishedDeploySaysSo(t *testing.T) {
	for _, status := range []string{"creating", "provisioned", "deploying"} {
		rec := verifyRecord
		rec.Status = status

		eng, dep, _ := engineWithDeployment(t, rec)
		dep.connectErr = clerrors.New(clerrors.CodeSSHAuthFailed, "ssh: unable to authenticate", "")
		err := eng.Logs(context.Background(), LogsOptions{Tail: 10})
		unfinished(t, "logs", status, err)

		eng, _, _ = engineWithDeployment(t, rec)
		probed := false
		eng.Probe = func(ctx context.Context, ip string, ports []int, d time.Duration) []verify.ProbeResult {
			probed = true
			return probeReturning(nil)(ctx, ip, ports, d)
		}
		err = eng.Verify(context.Background())
		unfinished(t, "verify", status, err)
		if probed {
			t.Errorf("verify of a %s deployment probed its ports: the service is not up yet by design", status)
		}
	}
	// A finished deployment is as before: the failure of SSH is SSH's.
	eng, dep, _ := engineWithDeployment(t, verifyRecord)
	dep.connectErr = clerrors.New(clerrors.CodeSSHAuthFailed, "ssh: unable to authenticate", "")
	var ue *DeployUnfinishedError
	if err := eng.Logs(context.Background(), LogsOptions{Tail: 10}); stderrors.As(err, &ue) || clerrors.Reason(err) != clerrors.CodeSSHAuthFailed {
		t.Errorf("logs of a running deployment whose SSH refuses: %v, want SSH_AUTH_FAILED as before", err)
	}
}

func unfinished(t *testing.T, cmd, status string, err error) {
	t.Helper()
	var ue *DeployUnfinishedError
	if !stderrors.As(err, &ue) {
		t.Errorf("%s of a %s deployment: %v, want DeployUnfinishedError", cmd, status, err)
		return
	}
	if ue.Status != status {
		t.Errorf("%s: status %q, want %q", cmd, ue.Status, status)
	}
	msg := err.Error()
	if !strings.Contains(msg, status) || !strings.Contains(msg, "deploy") {
		t.Errorf("%s of a %s deployment: %q does not say deploy has not finished", cmd, status, msg)
	}
}
