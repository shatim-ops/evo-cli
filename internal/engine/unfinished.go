// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// unfinishedStatuses are the statuses deploy leaves a record in while it
// works: the VM being created (creating), up with its address (provisioned),
// set up over SSH — Docker, the upload, compose up (deploying).
var unfinishedStatuses = map[string]bool{"creating": true, "provisioned": true, "deploying": true}

// DeployUnfinishedError is the answer of logs and verify for a deployment
// deploy has not finished (D76): the VM may not take the key yet, and the
// service is not up by design, so neither SSH nor the probe says anything
// true of it. The way on is deploy (axis 2, D69), not a fix of SSH.
type DeployUnfinishedError struct {
	Status string
	err    *clerrors.CLIError
}

func (e *DeployUnfinishedError) Error() string { return e.err.Error() }
func (e *DeployUnfinishedError) Unwrap() error { return e.err }

// deployUnfinished is the DeployUnfinishedError of the deployment rec when
// deploy has not finished it, else nil.
func deployUnfinished(rec *state.DeploymentRecord) error {
	if rec == nil || !unfinishedStatuses[rec.Status] {
		return nil
	}
	return &DeployUnfinishedError{Status: rec.Status, err: &clerrors.CLIError{
		Code: clerrors.CodeDeployInProgress,
		Message: i18n.Tf("deploy has not finished deployment %s: its record says %s (the VM is being created or set up); its logs and its service are not there yet",
			"deploy ещё не закончил развёртывание %s: в записи — %s (ВМ создаётся или настраивается); журналов и сервиса ещё нет", rec.ID, rec.Status),
		Hint: i18n.T("Call deploy again: it takes the VM in the state and goes on from where it stopped. status shows how far it got.",
			"Вызовите deploy ещё раз: он возьмёт ВМ из state и продолжит с того места, где остановился. status покажет, докуда дошло."),
		Details: map[string]any{"deployment_status": rec.Status, "next_tool": "deploy", "next_args": map[string]any{}},
	}}
}
