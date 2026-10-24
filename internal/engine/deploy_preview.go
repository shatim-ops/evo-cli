// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/pricing"
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
	// A zone named by its name, as deploy takes it (resolveNetwork): an
	// unknown name is refused on the preview, not after the consent.
	if ref := cfg.Region; ref != "" && !isUUID(ref) && e.Auth != nil && e.Cloud != nil {
		zone, err := e.zoneOfPreview(ctx, ref)
		if err != nil {
			return nil, err
		}
		if zone != nil {
			vm.Zone = deployplan.RefSpec{ID: zone.ID, Name: zone.Name}
		}
	}
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
	if err := e.previewNetwork(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// zoneOfPreview is the zone ref names (ResolveZoneRef), read within the
// time of a preview's lookups.
func (e *Engine) zoneOfPreview(ctx context.Context, ref string) (*provision.AvailabilityZone, error) {
	ctx, cancel := context.WithTimeout(ctx, PreviewLookupTimeout)
	defer cancel()
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return ResolveZoneRef(nil, err, ref)
	}
	e.Cloud.SetToken(tok.Value)
	zones, err := e.Cloud.ListAvailabilityZones(ctx)
	return ResolveZoneRef(zones, err, ref)
}

// PlanPreview is the preview of deploy_apply: the VM of a saved plan as
// deploy_plan resolved and priced it — its name, the project, the zone,
// the flavor, the image, the boot disk, the subnet, the floating IP, the
// ports and the monthly price the plan was quoted. It is made from the plan
// alone, with no lookup and no new quote: the user approves the plan
// deploy_apply executes, at the price deploy_plan showed (break 1.2,
// B12-8). The security group stays out, as in deploy_plan's answer: the
// plan keeps its id only, and a name-less group would be a placeholder.
func PlanPreview(plan *deployplan.Plan) *CreatePreview {
	vm := plan.VM
	public := plan.Network.FloatingIP
	cost := plan.Cost
	p := &CreatePreview{Resource: ResourceVM, Name: vm.Name,
		ProjectID: plan.ProjectID, ProjectFrom: originPlan, VM: &vm, PublicIP: &public,
		SubnetID: plan.Network.SubnetID, Cost: &cost, CostNote: costNote}
	if cost.Source == pricing.SourceAPI {
		p.CostNote = apiCostNote
	}
	if len(plan.Ports) > 0 {
		ports := make([]string, len(plan.Ports))
		for i, port := range plan.Ports {
			ports[i] = fmt.Sprint(port)
		}
		p.Add("ports", strings.Join(ports, ","))
	}
	p.Add("product", plan.Product)
	return p
}

// previewNetwork adds to a deploy preview the subnet and the security group
// the new VM is plugged into, and the id of its disk type: what the preview
// of evo deploy named before the command called its tool (v1.1). Reads
// only, best effort and bounded: a choice that fails here fails deploy at
// its own step, with the warnings deploy prints there (not here: the
// preview has no output of its own).
func (e *Engine) previewNetwork(ctx context.Context, p *CreatePreview) error {
	if p.VM == nil || p.ProjectID == "" || p.VM.Zone.ID == "" || e.Auth == nil || e.Cloud == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, networkCheckTimeout)
	defer cancel()
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return nil
	}
	e.Cloud.SetToken(tok.Value)
	quiet := *e
	quiet.Printer = output.NewWithWriter(io.Discard, output.ModeJSON, false)
	n, err := quiet.resolveNetwork(ctx, p.ProjectID, p.VM.Zone.ID)
	var ce *clerrors.CLIError
	switch {
	case err == nil:
		p.SubnetID, p.SubnetName = n.SubnetID, n.SubnetName
		p.SecurityGroup = &NetworkRef{ID: n.SecurityGroupID, Name: n.SecurityGroup}
	case errors.As(err, &ce) && ce.Code == clerrors.CodeConfigInvalid:
		// A zone without a subnet or a group: deploy refuses it, so the
		// preview does, before the question (D35, G13-3). A list that
		// could not be read stays the call's to meet.
		return err
	}
	if dt, err := quiet.resolveDiskType(ctx, p.ProjectID, defaultDiskTypeName); err == nil {
		p.VM.Disk.TypeID = dt.ID
	}
	return nil
}

// DeployDetailsV11 are the details 'evo deploy --dry-run --json' gave for a
// new VM in v1.1, made from the deploy tool's preview p: the deploy tool
// keeps them in its answer beside preview, so the command's details — the
// tool's answer — still have every key they had. Deprecated, removed in
// v2.0: use preview (preview.cost, preview.vm, …).
func (e *Engine) DeployDetailsV11(p *CreatePreview) map[string]any {
	var tcp, udp []int
	if project, err := compose.Parse(e.Config.ComposeFile); err == nil {
		tcp, udp = project.Ports, project.UDPPorts
	}
	sg := map[string]string{"id": "", "name": ""}
	if p.SecurityGroup != nil {
		sg = map[string]string{"id": p.SecurityGroup.ID, "name": p.SecurityGroup.Name}
	}
	return map[string]any{
		"project_id":     p.ProjectID,
		"project_from":   p.ProjectFrom,
		"vm":             p.VM,
		"floating_ip":    true,
		"subnet_id":      p.SubnetID,
		"security_group": sg,
		"open_ports":     tcp,
		"open_udp_ports": udp,
		"cost":           p.Cost,
		"cost_note":      p.CostNote,
	}
}
