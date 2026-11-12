// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// vmStoppedStates are the states of a VM that vm_stop leaves or goes
// through: nothing on such a VM answers — not SSH, not its service.
var vmStoppedStates = map[string]bool{"stopped": true, "stopping": true}

// VMStoppedError is the answer of logs, verify and deploy for a deployment
// whose VM is stopped or stopping (D104; «Риск», conclusion 132, У2): SSH and
// the probe would only time out on it, and a failure with no cause reads
// as a broken deployment — to be deleted and made again, with the data of
// the VM. The way on is vm_start, never a delete.
type VMStoppedError struct {
	State string
	err   *clerrors.CLIError
}

func (e *VMStoppedError) Error() string { return e.err.Error() }
func (e *VMStoppedError) Unwrap() error { return e.err }

// vmStopped reads the VM of the deployment rec before a command goes to it
// and is the VMStoppedError when the VM is stopped or stopping, else nil.
// code is the reason the command has had for a VM that does not answer
// (logs and deploy: SSH_TIMEOUT, verify: UNHEALTHY) — the cause is new,
// the reason and the exit code of a released command are not. A read that
// fails, a key that is not there: nil, the command goes its way as before.
func (e *Engine) vmStopped(ctx context.Context, rec *state.DeploymentRecord, code string) error {
	if rec == nil || rec.VMID == "" || e.Auth == nil || e.Cloud == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	tok, err := e.Auth.Token(cctx)
	if err != nil {
		return nil
	}
	e.Cloud.SetToken(tok.Value)
	vm, err := e.Cloud.GetVM(cctx, rec.VMID)
	if err != nil || vm == nil || !vmStoppedStates[vm.Status] {
		return nil
	}
	return &VMStoppedError{State: vm.Status, err: &clerrors.CLIError{
		Code: code,
		Message: i18n.Tf("VM %s of this deployment is %s: its SSH and its service do not answer until it is started",
			"ВМ %s этого развёртывания в состоянии %s: её SSH и сервис не отвечают, пока она не запущена", rec.VMID, vm.Status),
		Hint: i18n.Tf("Start it: 'evo vm start %s' (MCP: vm_start with vm_id; a VM that is stopping is waited for first), then repeat. The VM, its disks and this record are intact: nothing here needs a delete.",
			"Запустите её: 'evo vm start %s' (MCP: vm_start с vm_id; ВМ, которая останавливается, он сначала дождётся) и повторите. ВМ, её диски и эта запись целы: удалять здесь нечего.", rec.VMID),
		Details: map[string]any{
			"cause": "vm_stopped", "vm_id": rec.VMID, "vm_state": vm.Status,
			"next_tool": "vm_start", "next_args": map[string]any{"vm_id": rec.VMID},
		},
	}}
}

// DeployVMStopped is vmStopped for the deployment deploy would update: the
// deploy tool refuses with it before the question, as Deploy does.
func (e *Engine) DeployVMStopped(ctx context.Context, rec *state.DeploymentRecord) error {
	return e.vmStopped(ctx, rec, clerrors.CodeSSHTimeout)
}
