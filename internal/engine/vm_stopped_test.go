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

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/verify"
)

// vm_stop makes a state evo did not have: the record of a deployment is
// there, its VM is stopped. logs, verify and deploy of such a record used
// to fail on SSH or on the probe with no cause, and the next thing an
// agent tries then is delete and deploy again — the VM's data is gone
// («Риск», conclusion 132, У2). They read the VM before SSH and say the
// cause and vm_start, with the reason and the exit code each had.

func vmIn(status string) *mockCloudAPI {
	return &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
		return &provision.VM{ID: id, Status: status}, nil
	}}
}

// stoppedRefusal checks err is the refusal of cmd for a VM in status: the
// cause, vm_start as the step, the reason the command had, and no advice
// to delete.
func stoppedRefusal(t *testing.T, cmd, status, reason string, err error) {
	t.Helper()
	var se *VMStoppedError
	if !stderrors.As(err, &se) {
		t.Fatalf("%s of a deployment whose VM is %s: %v, want VMStoppedError", cmd, status, err)
	}
	var ce *clerrors.CLIError
	if !stderrors.As(err, &ce) {
		t.Fatalf("%s: %v is not a CLIError", cmd, err)
	}
	if ce.Code != reason {
		t.Errorf("%s: reason %s, want %s (the one the command had on a VM that does not answer)", cmd, ce.Code, reason)
	}
	if se.State != status || ce.Details["vm_state"] != status || ce.Details["cause"] != "vm_stopped" {
		t.Errorf("%s: state %q, details %v, want cause vm_stopped and vm_state %s", cmd, se.State, ce.Details, status)
	}
	args, _ := ce.Details["next_args"].(map[string]any)
	if ce.Details["next_tool"] != "vm_start" || args["vm_id"] != "vm-1" || len(args) != 1 {
		t.Errorf("%s: next step %v %v, want vm_start of vm-1 and no consent in it", cmd, ce.Details["next_tool"], args)
	}
	text := ce.Message + " " + ce.Hint
	if !strings.Contains(text, status) || !strings.Contains(text, "vm start") {
		t.Errorf("%s: %q does not say the VM is %s and how to start it", cmd, text, status)
	}
	for _, bad := range []string{"destroy", "evo vm delete", "deploy again"} {
		if strings.Contains(ce.Hint, bad) {
			t.Errorf("%s: the hint advises %q: %s", cmd, bad, ce.Hint)
		}
	}
}

func stoppedEngine(t *testing.T, cloud *mockCloudAPI) (*Engine, *mockDeployer, string) {
	t.Helper()
	dep := newMockDeployer()
	dep.connectErr = clerrors.New(clerrors.CodeSSHAuthFailed, "ssh: unable to authenticate", "")
	eng, dir := newTestEngine(t, cloud, dep)
	rec := verifyRecord
	rec.ProjectID = eng.Config.ProjectID
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(rec)
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	return eng, dep, dir
}

func TestLogsVerifyDeploy_AStoppedVMSaysSoAndNamesVMStart(t *testing.T) {
	for _, status := range []string{"stopped", "stopping"} {
		eng, dep, _ := stoppedEngine(t, vmIn(status))
		err := eng.Logs(context.Background(), LogsOptions{Tail: 10})
		stoppedRefusal(t, "logs", status, clerrors.CodeSSHTimeout, err)
		if dep.connectCalls != 0 {
			t.Errorf("logs went to SSH of a %s VM", status)
		}

		eng, _, _ = stoppedEngine(t, vmIn(status))
		probed := false
		eng.Probe = func(ctx context.Context, ip string, ports []int, d time.Duration) []verify.ProbeResult {
			probed = true
			return probeReturning(nil)(ctx, ip, ports, d)
		}
		err = eng.Verify(context.Background())
		stoppedRefusal(t, "verify", status, clerrors.CodeUnhealthy, err)
		if probed {
			t.Errorf("verify probed the ports of a %s VM", status)
		}

		// deploy: refused before the question (axis 1) — the gate is not
		// asked, nothing is dialed.
		eng, dep, dir := stoppedEngine(t, vmIn(status))
		asked, dialed := false, false
		eng.SSHDial = func(context.Context, string) error { dialed = true; return nil }
		opts := DeployOptions{ComposeFile: writeComposeFile(t, dir), Flavor: "small", NonInteractive: true, Yes: true}
		opts.Gate = func(context.Context, *confirm.Preview) (bool, error) { asked = true; return true, nil }
		err = eng.Deploy(context.Background(), opts)
		stoppedRefusal(t, "deploy", status, clerrors.CodeSSHTimeout, err)
		if asked || dialed || dep.connectCalls != 0 {
			t.Errorf("deploy to a %s VM: asked %v, dialed %v, SSH connects %d — want a refusal before all three", status, asked, dialed, dep.connectCalls)
		}
	}
}

// A VM that runs, and a state evo could not read, are the path as it was:
// the failure of SSH is SSH's, the probe is the probe's.
func TestLogsVerify_ARunningVMOrAFailedReadIsAsBefore(t *testing.T) {
	unread := &mockCloudAPI{getVMFn: func(context.Context, string) (*provision.VM, error) {
		return nil, clerrors.New(clerrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", "")
	}}
	gone := &mockCloudAPI{getVMFn: func(_ context.Context, id string) (*provision.VM, error) {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "VM "+id+" not found", "")
	}}
	// starting: the VM is on its way up — the command's own wait for SSH
	// is the way, as it was.
	for name, cloud := range map[string]*mockCloudAPI{"running": vmIn("running"), "starting": vmIn("starting"),
		"error": vmIn("error"), "unread": unread, "gone": gone} {
		eng, dep, _ := stoppedEngine(t, cloud)
		err := eng.Logs(context.Background(), LogsOptions{Tail: 10})
		var se *VMStoppedError
		if stderrors.As(err, &se) || clerrors.Reason(err) != clerrors.CodeSSHAuthFailed || dep.connectCalls != 1 {
			t.Errorf("%s: logs = %v (SSH connects %d), want the SSH failure as before", name, err, dep.connectCalls)
		}

		eng, _, _ = stoppedEngine(t, cloud)
		eng.Probe = probeReturning(map[int]bool{80: true, 8080: true})
		if err := eng.Verify(context.Background()); err != nil {
			t.Errorf("%s: verify = %v, want the probe's answer as before", name, err)
		}
	}
	// No token: the read is not made, the path is as before.
	eng, dep, _ := stoppedEngine(t, vmIn("stopped"))
	eng.Auth = &mockTokenProvider{err: clerrors.New(clerrors.CodeAuthRequired, "no key", "")}
	eng.Probe = probeReturning(map[int]bool{80: true, 8080: true})
	if err := eng.Verify(context.Background()); err != nil {
		t.Errorf("verify without a token = %v, want the probe's answer as before", err)
	}
	if dep.connectCalls != 0 {
		t.Errorf("verify connected over SSH %d time(s)", dep.connectCalls)
	}
}
