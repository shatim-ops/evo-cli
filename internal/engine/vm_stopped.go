// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	stderrors "errors"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// vmStoppedStates are the states of a VM that vm_stop leaves or goes
// through: nothing on such a VM answers — not SSH, not its service.
var vmStoppedStates = map[string]bool{"stopped": true, "stopping": true}

// vmTransitStates are the states vm_start and vm_reboot take a VM through
// on its way to running: SSH and the service may not answer yet. A command
// goes its way and waits as it did (D104), and a failure then names the
// state as its cause (VMInTransitError; the interim delta audit of 1.9,
// FB19I-5).
var vmTransitStates = map[string]bool{"starting": true, "rebooting": true}

// VMStateCovered reports whether logs, verify and deploy say something of a
// VM in state beyond the failure of SSH or of the probe: stopped, or on its
// way up. Every state a power tool goes through or leads to must be
// covered — the tools test checks it against their actions.
func VMStateCovered(state string) bool { return vmStoppedStates[state] || vmTransitStates[state] }

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
// and is the VMStoppedError when the VM is stopped or stopping, else nil;
// transit is the state of a VM on its way up (vmTransitStates), "" for any
// other. code is the reason the command has had for a VM that does not
// answer (logs and deploy: SSH_TIMEOUT, verify: UNHEALTHY) — the cause is
// new, the reason and the exit code of a released command are not. A read
// that fails, a key that is not there: nil and "", the command goes its
// way as before.
func (e *Engine) vmStopped(ctx context.Context, rec *state.DeploymentRecord, code string) (transit string, err error) {
	if rec == nil || rec.VMID == "" || e.Auth == nil || e.Cloud == nil {
		return "", nil
	}
	cctx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	tok, err := e.Auth.Token(cctx)
	if err != nil {
		return "", nil
	}
	e.Cloud.SetToken(tok.Value)
	vm, err := e.Cloud.GetVM(cctx, rec.VMID)
	if err != nil || vm == nil || !VMStateCovered(vm.Status) {
		return "", nil
	}
	if vmTransitStates[vm.Status] {
		return vm.Status, nil
	}
	return "", &VMStoppedError{State: vm.Status, err: &clerrors.CLIError{
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

// VMInTransitError is a failure of logs or verify on a VM that was on its
// way up when the command began (vmTransitStates): the failure itself,
// with its reason and exit code, and the state as its cause. The way on is
// to wait for running (vm_get) and repeat, never a delete.
type VMInTransitError struct {
	State string
	err   *clerrors.CLIError
}

func (e *VMInTransitError) Error() string { return e.err.Error() }
func (e *VMInTransitError) Unwrap() error { return e.err }

// inTransit is err of a command on the VM of rec that was in state transit
// when it began, with the state named as its cause; err as it is when
// there is no failure, the VM was not on its way up, the command was
// interrupted, or err carries no reason of its own — the cause never
// changes the reason or the exit code of the failure.
func inTransit(err error, rec *state.DeploymentRecord, transit string) error {
	var ce *clerrors.CLIError
	if err == nil || transit == "" || !stderrors.As(err, &ce) || ce.Code == "" ||
		clerrors.ExitCode(err) == clerrors.ExitInterrupted {
		return err
	}
	return &VMInTransitError{State: transit, err: &clerrors.CLIError{
		Code: ce.Code,
		Message: i18n.Tf("%s (VM %s of this deployment was %s when the command began: SSH and its service may not answer until it is running)",
			"%s (ВМ %s этого развёртывания была в состоянии %s, когда команда началась: SSH и сервис могут не отвечать, пока она не запущена)",
			ce.Message, rec.VMID, transit),
		Hint: i18n.Tf("Wait until 'evo vm get %s' shows running (MCP: vm_get with vm_id), then repeat. The VM, its disks and this record are intact: nothing here needs a delete.",
			"Дождитесь, пока 'evo vm get %s' покажет running (MCP: vm_get с vm_id), и повторите. ВМ, её диски и эта запись целы: удалять здесь нечего.", rec.VMID),
		Err: err,
		Details: map[string]any{
			"cause": "vm_in_transition", "vm_id": rec.VMID, "vm_state": transit,
			"next_tool": "vm_get", "next_args": map[string]any{"vm_id": rec.VMID},
		},
	}}
}

// DeployVMStopped is vmStopped for the deployment deploy would update: the
// deploy tool refuses with it before the question, as Deploy does.
func (e *Engine) DeployVMStopped(ctx context.Context, rec *state.DeploymentRecord) error {
	// A VM on its way up is deploy's own wait for SSH, as it was.
	_, err := e.vmStopped(ctx, rec, clerrors.CodeSSHTimeout)
	return err
}
