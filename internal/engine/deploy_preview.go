// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sizer"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// errNoSize: the flavor is auto and the compose file cannot be read, so
// there is nothing to size the VM by (deploy itself stops on the file).
var errNoSize = errors.New("no compose file to size the VM by")

// DeployPreview is the preview of the MCP deploy (spec REQ-7, owner's
// decision Q6): the VM deploy creates in a directory that has no deployment
// yet, as Deploy would make it — its name, the project, the zone, the image,
// the flavor given or sized for the compose file, the boot disk, the ports
// of the compose file — completed and priced with reads only, as
// CompletePreview does for provision (the network check aside: deploy
// resolves its network on its own). nil when the directory already has a
// deployment (or its state cannot be read): deploy then updates that VM and
// creates no new one, so there is nothing to price. The error is a refusal
// the reads found: an image the project cannot use.
func (e *Engine) DeployPreview(ctx context.Context) (*CreatePreview, error) {
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil || sf.ActiveDeployment() != nil {
		return nil, nil
	}
	cfg := e.Config
	projectName := cfg.ProjectName
	if projectName == "" {
		projectName = filepath.Base(e.StateDir)
	}
	public := true
	p := &CreatePreview{Resource: ResourceVM, Name: vmNameFor(projectName),
		ProjectID: cfg.ProjectID, ProjectFrom: cfg.ProjectOrigin, PublicIP: &public}
	vm := deployplan.VMSpec{Name: p.Name,
		Zone: deployplan.RefSpec{ID: cfg.Region},
		Disk: deployplan.DiskSpec{Type: "SSD"}}
	// The image as BootImage takes it: image_id first, else image (a name
	// or a UUID).
	switch {
	case cfg.ImageID != "":
		vm.Image.ID = cfg.ImageID
	case isUUID(cfg.Image):
		vm.Image.ID = cfg.Image
	default:
		vm.Image.Name = cfg.Image
	}
	if f := cfg.Flavor; f != "" && f != "auto" {
		if isUUID(f) {
			vm.Flavor.ID = f
		} else {
			vm.Flavor.Name = f
		}
	}
	vm.Disk.SizeGB, _ = provision.BootDiskGB(cfg.DiskSize, 0)
	p.VM = &vm
	for _, m := range []struct{ name, val string }{
		{"project_id", p.ProjectID}, {"image_id", firstNonEmpty(vm.Image.ID, vm.Image.Name)}, {"region", cfg.Region},
	} {
		if m.val == "" {
			p.Missing = append(p.Missing, m.name)
		}
	}
	project, perr := compose.Parse(cfg.ComposeFile)
	if perr != nil {
		project = nil
	}
	if project != nil {
		ports := make([]string, 0, len(project.Ports)+len(project.UDPPorts))
		for _, port := range project.Ports {
			ports = append(ports, fmt.Sprint(port))
		}
		for _, port := range project.UDPPorts {
			ports = append(ports, fmt.Sprintf("%d/udp", port))
		}
		p.Add("ports", strings.Join(ports, ","))
	}
	// The flavor deploy takes (resolveFlavor): the one given, else the
	// smallest that fits the compose file's services.
	pick := func(flavors []*provision.Flavor) (string, error) {
		if want := firstNonEmpty(vm.Flavor.ID, vm.Flavor.Name); want != "" {
			return PickVMFlavor(flavors, want)
		}
		if project == nil {
			return "", errNoSize
		}
		list := make([]provision.Flavor, len(flavors))
		for i, f := range flavors {
			list[i] = *f
		}
		match := sizer.Match(sizer.EstimateFromServices(project.Services), list)
		if match == nil {
			return "", errNoSize
		}
		return match.Flavor.ID, nil
	}
	if err := e.completePreview(ctx, p, pick, false); err != nil {
		return nil, err
	}
	return p, nil
}
