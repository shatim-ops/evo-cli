// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// creatingVM is the VM an earlier provision of name left creating in this
// folder — cut by the budget of its MCP call or by wait=false (D69) — with
// its record: the repeat of the call adopts it and finishes it instead of
// creating a second VM. The record is the evidence the VM is this folder's
// own: a VM of the name the state does not hold is not adopted (D73). A
// record whose VM the cloud no longer has is dropped when drop is set (the
// call, not the preview, which writes nothing). nil, nil: nothing to adopt.
func creatingVM(ctx context.Context, eng *engine.Engine, name string, drop bool) (*state.DeploymentRecord, *provision.VM, error) {
	if name == "" {
		return nil, nil, nil
	}
	path := state.DefaultPath(eng.StateDir)
	sf, err := state.Read(path)
	if err != nil {
		return nil, nil, nil
	}
	var rec *state.DeploymentRecord
	for _, d := range sf.ActiveDeployments() {
		if d.Source == state.SourceProvision && d.Status == "creating" && d.VMName == name &&
			d.ProjectID == eng.Config.ProjectID && d.Region == eng.Config.Region {
			rec = d
		}
	}
	if rec == nil {
		return nil, nil, nil
	}
	vm, err := eng.Cloud.GetVM(ctx, rec.VMID)
	var ce *clierrors.CLIError
	switch {
	case stderrors.As(err, &ce) && ce.Code == clierrors.CodeVMNotFound:
		if drop {
			_ = state.Update(path, stateLockWait, func(sf *state.File) error {
				sf.RemoveDeployment(rec.ID)
				return nil
			})
		}
		return nil, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf(i18n.T("VM %s, which an earlier provision of %s left creating: %w",
			"ВМ %s, которую прошлый provision с именем %s оставил создаваться: %w"), rec.VMID, name, err)
	}
	return rec, vm, nil
}

// provisionCreating is the answer of a provision whose VM is not running
// when the call's budget or wait=false ends the wait (D69): not an error —
// the VM, the state read last and the next step, the same call, which
// adopts the VM (creatingVM). next_args leave out wait (wait=false would
// answer this again) and image_metadata (its values may be passwords; the
// repeat does not use them).
func provisionCreating(args map[string]any, vmID, vmName, vmStatus, password string, elapsedSec int, warning string) map[string]any {
	repeat := maps.Clone(args)
	delete(repeat, "wait")
	delete(repeat, "image_metadata")
	repeat["name"] = vmName
	repeat["confirmed"] = true
	// The consent goes on for this VM and no other (continuedVM, D102).
	repeat[continuedVMArg] = vmID
	out := map[string]any{
		"status":      "creating",
		"vm_id":       vmID,
		"vm_status":   vmStatus,
		"name":        vmName,
		"elapsed_sec": elapsedSec,
		"next": fmt.Sprintf("The VM is still being created. Call provision again with next_args (name %s, confirmed=true, vm_id) from the same folder: it adopts this VM (no second one), waits and finishes it (public IP, ports); if the VM is gone by then, it refuses and creates nothing. vm_get vm_id=%s shows its state.%s",
			vmName, vmID, warning),
	}
	nextStep(out, "provision", repeat)
	if password != "" {
		out["password"] = password
		out["next"] = out["next"].(string) + passwordOnce
	}
	return out
}

// continuedVMArg is the argument of provision and deploy_apply by which
// the next step of a call cut before its VM ran (D69) names that VM: the
// consent the step carries is for this VM, not for another of the name.
const continuedVMArg = "vm_id"

// continuedVMDesc is the description of continuedVMArg in a schema.
const continuedVMDesc = "next_args: its VM"

// continuedVM checks the VM a next step goes on with (D102; R-85, «Риск»,
// conclusion 125). The step of provision and deploy_apply carries the
// consent of the call it continues; the person consented to one VM. When
// that VM is gone by the time of the step — deleted in the console, by
// another agent — the step used to find nothing to adopt and make a
// second billed VM, with a floating IP, on the consent of the first. A
// call that names the VM it goes on with and does not find it makes
// nothing: the refusal says so and names no next call — whether to make a
// new VM is the person's decision, and a call without the argument asks.
// A read that fails otherwise refuses too: what is not known to be there
// is not gone on with. nil when the call names no VM (a call of its own,
// not a step) or the VM is there.
func (s *Registry) continuedVM(ctx context.Context, args map[string]any) error {
	id := stringArg(args, continuedVMArg)
	if id == "" {
		return nil
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return err
	}
	_, err = eng.Cloud.GetVM(ctx, id)
	var ce *clierrors.CLIError
	if stderrors.As(err, &ce) && ce.Code == clierrors.CodeVMNotFound {
		return clierrors.New(clierrors.CodeVMNotFound,
			i18n.Tf("the VM this call went on with (%s) is no longer there; no new VM was created", "ВМ, которую продолжал вызов (%s), больше нет; новая не создана", id),
			i18n.T("The consent was for that VM. Whether to create a new one is the person's decision: a call without "+continuedVMArg+" shows the preview and asks.",
				"Согласие было на ту ВМ. Создавать ли новую — решает человек: вызов без "+continuedVMArg+" покажет превью и спросит."))
	}
	return err
}
