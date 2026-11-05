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
// deploy has not finished it, else nil. Its way on is the record's own
// (wayOn, D82).
func deployUnfinished(rec *state.DeploymentRecord) error {
	if rec == nil || !unfinishedStatuses[rec.Status] {
		return nil
	}
	details := map[string]any{"deployment_status": rec.Status}
	tool, args, hint := wayOn(rec)
	if tool != "" {
		details["next_tool"], details["next_args"] = tool, args
	}
	return &DeployUnfinishedError{Status: rec.Status, err: &clerrors.CLIError{
		Code: clerrors.CodeDeployInProgress,
		Message: i18n.Tf("deploy has not finished deployment %s: its record says %s (the VM is being created or set up); its logs and its service are not there yet",
			"deploy ещё не закончил развёртывание %s: в записи — %s (ВМ создаётся или настраивается); журналов и сервиса ещё нет", rec.ID, rec.Status),
		Hint:    hint,
		Details: details,
	}}
}

// wayOn is the step that goes on with the unfinished deployment rec (D82;
// «Риск», conclusion 10): the tool, its arguments, and the hint that says
// it. One consent, as D69's step: confirmed — only for a record deploy
// began that knows the source its ports were opened from (R-36: the
// repeat opens the missing ones from it, not wider), and for a record of
// deploy_apply with its plan (R-35). A record whose way on evo cannot tell
// — provision's, deploy_apply's without a plan, one with neither a source
// nor a compose file — gets no tool, and the hint says why.
func wayOn(rec *state.DeploymentRecord) (string, map[string]any, string) {
	switch {
	case rec.Source == state.SourceDeployApply && rec.PlanID != "":
		return "deploy_apply", map[string]any{"plan_id": rec.PlanID, "confirmed": true},
			i18n.Tf("Call deploy_apply again with plan_id %s: it takes the VM of the plan and goes on. status shows how far it got.",
				"Вызовите deploy_apply ещё раз с plan_id %s: он возьмёт ВМ плана и продолжит. status покажет, докуда дошло.", rec.PlanID)
	case rec.Source == "" && rec.ComposePath != "":
		args := map[string]any{"compose_file": rec.ComposePath}
		// The consent goes on for the file it was given for: another file
		// now is asked again («Риск», R-38).
		if rec.ComposeHash != "" {
			args["compose_hash"] = rec.ComposeHash
		}
		hint := i18n.T("Call deploy again with this compose file: it takes the VM in the state and goes on from where it stopped. status shows how far it got.",
			"Вызовите deploy ещё раз с этим compose-файлом: он возьмёт ВМ из state и продолжит с того места, где остановился. status покажет, докуда дошло.")
		if rec.Cidr != "" {
			args["cidr"], args["confirmed"] = rec.Cidr, true
		} else {
			hint += " " + i18n.T("The record does not say which addresses its ports were opened to: the call shows its preview, with cidr, before it goes on.",
				"Запись не говорит, каким адресам открыты её порты: вызов покажет превью с cidr, прежде чем продолжить.")
		}
		return "deploy", args, hint
	case rec.Source == state.SourceProvision:
		return "", nil, i18n.T("This VM is provision's: call provision again with its name, and it takes the VM it left creating. status shows how far it got.",
			"Эта ВМ — от provision: вызовите provision ещё раз с её именем, и он возьмёт ВМ, оставленную в creating. status покажет, докуда дошло.")
	}
	return "", nil, i18n.T("The record does not say which call made it (a record of deploy_apply from before 1.8, or one without its plan): status shows the deployment; go on with the call that made it.",
		"Запись не говорит, каким вызовом она сделана (запись deploy_apply до 1.8 или без плана): status покажет развёртывание; продолжите тем вызовом, который её сделал.")
}
