package engine

import (
	"context"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// withNames returns rec with the names people know its VM, zone and flavor
// by: from the record (deploy saves them since v1.0.1), from vm when the
// caller already has it, else looked up — GET of the VM (only if fetchVM),
// the zone and flavor lists. A user with several VMs could not tell which
// one destroy was about to delete from the UUIDs alone (first real run,
// v1.0.1). Best effort and bounded by StatusRefreshTimeout: a name not
// found stays empty and the ID is shown instead.
func (e *Engine) withNames(ctx context.Context, rec state.DeploymentRecord, vm *provision.VM, fetchVM bool) state.DeploymentRecord {
	fromVM := func(vm *provision.VM) {
		if vm == nil {
			return
		}
		if rec.VMName == "" {
			rec.VMName = vm.Name
		}
		if z := vm.AvailabilityZone; rec.ZoneName == "" && z != nil && z.Name != "" && (rec.Region == "" || z.ID == rec.Region) {
			rec.ZoneName = z.Name
		}
	}
	fromVM(vm)
	needVM := fetchVM && vm == nil && rec.VMName == "" && rec.VMID != ""
	needZone := rec.ZoneName == "" && rec.Region != ""
	needFlavor := rec.FlavorName == "" && rec.VMFlavor != ""
	if !needVM && !needZone && !needFlavor || e.Auth == nil || e.Cloud == nil {
		return rec
	}
	// GET /vms/{id} carries no project: a VM of another project answers
	// 404 to this key (see CheckCloud). The lists are not per project.
	if rec.ProjectID != "" && e.Config.ProjectID != "" && rec.ProjectID != e.Config.ProjectID {
		needVM = false
	}
	cctx, cancel := context.WithTimeout(ctx, e.timings().StatusRefreshTimeout)
	defer cancel()
	tok, err := e.Auth.Token(cctx)
	if err != nil {
		return rec
	}
	e.Cloud.SetToken(tok.Value)
	if needVM {
		if got, err := e.Cloud.GetVM(cctx, rec.VMID); err == nil {
			fromVM(got)
		}
	}
	if rec.ZoneName == "" && rec.Region != "" {
		if zones, err := e.Cloud.ListAvailabilityZones(cctx); err == nil {
			for _, z := range zones {
				if z.ID == rec.Region {
					rec.ZoneName = z.Name
					break
				}
			}
		}
	}
	if needFlavor {
		if flavors, err := e.Cloud.ListFlavors(cctx); err == nil {
			for _, f := range flavors {
				if f.ID == rec.VMFlavor || f.Name == rec.VMFlavor {
					rec.FlavorName = f.Name
					break
				}
			}
		}
	}
	return rec
}

// named is "name (id)", or whichever of the two is known.
func named(name, id string) string {
	switch {
	case name == "":
		return id
	case id == "" || id == name:
		return name
	}
	return name + " (" + id + ")"
}

// vmText is a deployment's VM for people: «evo-web (a1b2c3d4-…)».
func vmText(rec *state.DeploymentRecord) string { return named(rec.VMName, rec.VMID) }

// recordFlavorText is a deployment's flavor: its name, with the ID when
// the record holds the ID (sizing picks a flavor by ID).
func recordFlavorText(rec *state.DeploymentRecord) string { return named(rec.FlavorName, rec.VMFlavor) }

// recordZoneText is a deployment's zone: its name and ID.
func recordZoneText(rec *state.DeploymentRecord) string { return named(rec.ZoneName, rec.Region) }
