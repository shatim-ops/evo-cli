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
	out := map[string]any{
		"status":      "creating",
		"vm_id":       vmID,
		"vm_status":   vmStatus,
		"name":        vmName,
		"elapsed_sec": elapsedSec,
		"next": fmt.Sprintf("The VM is still being created. Call provision again with name %s and confirmed=true from the same folder: it adopts this VM (no second one), waits and finishes it (public IP, ports); vm_get vm_id=%s shows its state.%s",
			vmName, vmID, warning),
	}
	nextStep(out, "provision", repeat)
	if password != "" {
		out["password"] = password
		out["next"] = out["next"].(string) + passwordOnce
	}
	return out
}
