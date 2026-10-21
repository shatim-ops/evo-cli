// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-`)

// Params is the input to Resolve. In auto mode, requirement fields (CPU/RAMGB/
// DiskGB) drive flavor/disk selection; in manual mode the explicit *Override
// fields are used. Empty fields fall back to documented defaults.
type Params struct {
	Name        string
	Product     string
	Mode        string // "auto" | "manual"
	CPU         int    // requirement (auto-size)
	RAMGB       int    // requirement (auto-size)
	DiskGB      int
	Ports       []int // product ports to open in the security group (e.g. 80,443)
	ComposeFile string
	ExternalIP  bool

	// Manual overrides (by name).
	Flavor   string
	Zone     string
	Image    string
	DiskType string
	Login    string
	SSHKey   string // explicit public key (full "ssh-ed25519 AAAA... comment")
	// KeyLater: with no SSHKey, the plan is for the agent's deploy key all
	// the same, which is not made for it (a read-only server shows the plan
	// and neither makes the key nor saves the plan, B12-9): the key fields
	// count as filled, as images.Inputs.KeyLater has it. Never saved.
	KeyLater bool `json:"-"`
	// Meta are explicit values of the image's metadata fields
	// (image_metadata): the mandatory ones evo cannot fill. They are kept
	// in the plan file (0600, next to the deploy key) until apply.
	Meta map[string]string `json:"meta,omitempty"`
}

// Resolve performs all read-only discovery and returns a costed plan. It never
// mutates Cloud.ru. The caller is expected to Save() the returned plan.
func Resolve(ctx context.Context, cloud provision.CloudAPI, projectID string, p Params) (*Plan, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if err := provision.CheckBootDiskGB(p.DiskGB); err != nil {
		return nil, err
	}
	var warnings []string
	// The SSH public key to inject (if any) is decided by the caller (the MCP
	// handler defaults it to the agent's deploy key). p.SSHKey == "" means no
	// key — fall back to a generated password.

	// --- Zone ---
	// The caller fills p.Zone from the call or the configured region; with
	// neither the plan says which zone it took (release audit, A-002).
	if p.Zone == "" {
		warnings = append(warnings, "no zone given and no region configured: planning in ru.AZ-2 (pass zone, or set region in the profile or .evo.yaml)")
	}
	zoneInput := firstNonEmpty(p.Zone, "ru.AZ-2")
	zone := provision.AvailabilityZone{ID: zoneInput, Name: zoneInput}
	zones, zErr := cloud.ListAvailabilityZones(ctx)
	if zErr == nil {
		for _, z := range zones {
			if z.ID == zoneInput || z.Name == zoneInput {
				zone = *z
				break
			}
		}
	}
	if !uuidRe.MatchString(zone.ID) {
		return nil, fmt.Errorf("could not resolve availability zone %q to a UUID", zoneInput)
	}
	if err := provision.ClosedZoneError(zones, zone.ID); err != nil {
		return nil, err
	}

	// --- Flavor ---
	flavors, err := cloud.ListFlavors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list flavors: %w", err)
	}
	flavor, fwarn, err := selectFlavor(flavors, p)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, fwarn...)

	// --- Image ---
	image, err := selectImage(ctx, cloud, projectID, p.Image)
	if err != nil {
		return nil, err
	}
	// Docker publishes no packages for the domestic distributions: a
	// compose deploy gets no plan, so deploy_apply does not bill a VM Docker
	// cannot go on (as for a missing metadata field).
	if p.ComposeFile != "" {
		if family := images.NoDockerFamily(image.Name, image.DisplayName, image.OS); family != "" {
			return nil, images.NoDockerError(clierrors.CodeUsage, image.Name, family)
		}
	}

	// --- Disk type ---
	minDisk := 0
	if d, e := cloud.GetImage(ctx, image.ID); e == nil {
		minDisk = d.MinDisk
	}
	diskGB, diskNote := provision.BootDiskGB(p.DiskGB, minDisk)
	if diskNote != "" {
		warnings = append(warnings, diskNote)
	}
	diskName, diskID := "SSD", ""
	if dts, e := cloud.ListDiskTypes(ctx, projectID); e == nil && len(dts) > 0 {
		want := firstNonEmpty(p.DiskType, "SSD")
		picked := dts[0]
		for _, dt := range dts {
			if strings.EqualFold(dt.Name, want) {
				picked = dt
				break
			}
		}
		diskName, diskID = picked.Name, picked.ID
	} else if p.DiskType != "" {
		diskName = p.DiskType
	}

	// --- Subnet (zone-matched) ---
	subnets, err := cloud.ListSubnets(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list subnets: %w", err)
	}
	subnet := pickSubnet(subnets, zone.ID, zone.Name)
	if subnet == nil {
		return nil, fmt.Errorf("no subnet found for project/zone %s — create a network+subnet in the Cloud.ru console first", zone.Name)
	}
	subnetID := subnet.ID
	if subnet.NoDNS() {
		warnings = append(warnings, fmt.Sprintf("subnet %s has no DNS servers: the VM will not resolve names — Docker and images will not download; add a DNS server to the subnet in the Cloud.ru console", subnet.Name))
	}

	// --- Security group (the default group of the zone, then the first by name) ---
	// None in the zone or an unreadable list: no plan, as deploy and
	// provision — a VM without a group is billed and unreachable by SSH.
	sgs, err := cloud.ListSecurityGroups(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list security groups: %w", err)
	}
	i := provision.PickSecurityGroup(sgs, zone.ID, zone.Name)
	if i < 0 {
		return nil, clierrors.New(clierrors.CodeConfigInvalid,
			fmt.Sprintf("no security group for zone %s in the project", zone.Name),
			"Create a security group whose name contains the zone name (e.g. 'web_"+zone.Name+"') in the Cloud.ru console, or plan in another zone.")
	}
	sgID := sgs[i].ID
	// The rules a new VM needs (ingress TCP 22, system egress): a warning
	// in the plan, before anything is billed.
	if rules, re := provision.GroupRules(ctx, cloud, sgs[i]); re == nil {
		warnings = append(warnings, provision.CheckVMGroupRules(rules, subnet.DefaultGateway).Warnings(sgs[i].Name)...)
	}
	if len(p.Ports) > 0 {
		warnings = append(warnings, fmt.Sprintf("ports %v will be opened (ingress, 0.0.0.0/0) on the shared security group of this zone — other VMs later placed in it would share those open ports", p.Ports))
	}

	// --- Floating IP: evo's own, created at apply. A free FIP of the user
	// is not taken over — destroy releases the deployment's FIP (QA r3 R9).
	wantFIP := p.ExternalIP
	attachFIPID := ""

	name := p.Name
	if name == "" {
		base := firstNonEmpty(p.Product, "vm")
		name = sanitizeName(base) + "-" + time.Now().Format("20060102-1504")
	}
	// A name the compute API rejects: no plan (release QA, B-lifecycle).
	if err := provision.ValidName(i18n.T("VM", "ВМ"), name); err != nil {
		return nil, err
	}

	// --- Access: image metadata fields + SSH model ---
	metaFields, metaStatic, login, keyName, sErr, refuse := resolveAccess(ctx, cloud, image.ID, name, p)
	if refuse != nil {
		// A value the image does not take, or a mandatory field nobody
		// filled: no plan, so deploy_apply cannot create the VM.
		return nil, refuse
	}
	if sErr != nil {
		warnings = append(warnings, "could not read image metadata template: "+sErr.Error())
	}

	vm := VMSpec{
		Name:   name,
		Flavor: FlavorSpec{ID: flavor.ID, Name: flavor.Name, VCPUs: flavor.VCPUs, RAMGB: flavor.RAMGB, FreeTier: flavor.FreeTier},
		Image:  RefSpec{ID: image.ID, Name: image.Name},
		Zone:   RefSpec{ID: zone.ID, Name: zone.Name},
		Disk:   DiskSpec{SizeGB: diskGB, Type: diskName, TypeID: diskID},
	}

	now := time.Now()
	plan := &Plan{
		PlanID:    newPlanID(),
		CreatedAt: now,
		ExpiresAt: now.Add(planTTL),
		Product:   p.Product,
		Mode:      firstNonEmpty(p.Mode, "auto"),
		ProjectID: projectID,
		VM:        vm,
		Network: NetworkSpec{
			SubnetID:        subnetID,
			SecurityGroupID: sgID,
			FloatingIP:      wantFIP,
			AttachFIPID:     attachFIPID,
		},
		Ports:           p.Ports,
		SSH:             SSHSpec{Login: login, KeyName: keyName},
		Cost:            PriceVM(ctx, cloud, PriceInput{ProjectID: projectID, VM: vm, PublicIP: wantFIP, Marketplace: image.Type == provision.ImageTypeMarketplace}),
		Warnings:        warnings,
		ComposeRequired: p.ComposeFile != "",
		ComposeFile:     p.ComposeFile,
		MetaFields:      metaFields,
		MetaStatic:      metaStatic,
		PublicKey:       p.SSHKey,
		SourceParams:    p,
	}
	return plan, nil
}

// EstimateVM is the rate-card estimate for a resolved VM: the price of
// deploy_plan and of the CLI previews (PriceVM) when the price-calculation
// API gives none, so they never disagree about the price.
func EstimateVM(vm VMSpec) pricing.Estimate {
	return pricing.EstimateFor(vm.Flavor.Name, vm.Flavor.VCPUs, vm.Flavor.RAMGB, vm.Disk.SizeGB, vm.Disk.Type)
}

// selectFlavor validates an explicit flavor by name, or auto-sizes from CPU/RAM,
// or falls back to a sane default. Free-tier flavors are skipped for auto-size.
func selectFlavor(flavors []*provision.Flavor, p Params) (provision.Flavor, []string, error) {
	var warnings []string
	byName := func(name string) *provision.Flavor {
		for _, f := range flavors {
			if f.Name == name {
				return f
			}
		}
		return nil
	}

	if p.Flavor != "" {
		if f := byName(p.Flavor); f != nil {
			return *f, warnings, nil
		}
		return provision.Flavor{}, warnings, fmt.Errorf(i18n.T("flavor %q not found (use the list_flavors tool to see options)", "конфигурация %q не найдена (варианты — в выводе инструмента list_flavors)"), p.Flavor)
	}

	// Auto-size from requirements.
	if p.CPU > 0 || p.RAMGB > 0 {
		var cands []*provision.Flavor
		for _, f := range flavors {
			if f.FreeTier || strings.Contains(strings.ToLower(f.Name), "free") {
				continue
			}
			if f.VCPUs >= p.CPU && f.RAMGB >= p.RAMGB {
				cands = append(cands, f)
			}
		}
		if len(cands) > 0 {
			sort.Slice(cands, func(i, j int) bool {
				if cands[i].VCPUs != cands[j].VCPUs {
					return cands[i].VCPUs < cands[j].VCPUs
				}
				return cands[i].RAMGB < cands[j].RAMGB
			})
			return *cands[0], warnings, nil
		}
		warnings = append(warnings, fmt.Sprintf("no flavor satisfies %d vCPU / %d GB — falling back to default", p.CPU, p.RAMGB))
	}

	if f := byName("lowcost10-2-4"); f != nil {
		return *f, warnings, nil
	}
	// Last resort: smallest non-free flavor.
	var fallback *provision.Flavor
	for _, f := range flavors {
		if f.FreeTier {
			continue
		}
		if fallback == nil || f.VCPUs < fallback.VCPUs {
			fallback = f
		}
	}
	if fallback != nil {
		return *fallback, warnings, nil
	}
	return provision.Flavor{}, warnings, errors.New(i18n.T("no usable flavor available", "нет подходящей конфигурации ВМ"))
}

// selectImage resolves an image by name or UUID (or default Ubuntu 24.04 non-GPU).
//
// An explicit choice is looked up in TWO places. A bare /api/v1/images call
// returns only the shared catalogue: images built inside the project — exactly
// the ones image_create produces — are invisible there (see the comment on
// CloudRuAdapter.ListImages). Without the second lookup the engine cannot
// deploy what it has itself just built, and it says "not found" about an image
// that list_images plainly shows.
//
// The default branch deliberately stays on the shared catalogue: it guesses a
// base OS, and a project's own product images have no business being guessed.
func selectImage(ctx context.Context, cloud provision.CloudAPI, projectID, want string) (provision.Image, error) {
	img, own, err := pickImage(ctx, cloud, projectID, want)
	if err != nil {
		return img, err
	}
	// A shared image the project cannot use: the create call would answer
	// 422 wrong_image after the plan is approved — refuse the plan.
	if !own && (images.Catalog{Cloud: cloud, ProjectID: projectID}).MissingFromProject(ctx, img.ID, img.Name) {
		return provision.Image{}, images.NotInProjectError(img.Name)
	}
	return img, nil
}

// pickImage is the image of the plan: by exact name or UUID in the shared
// catalogue, then among the project's own (the bool is true for those); by
// default Ubuntu 24.04.
func pickImage(ctx context.Context, cloud provision.CloudAPI, projectID, want string) (provision.Image, bool, error) {
	images, err := cloud.ListImages(ctx)
	if err != nil {
		return provision.Image{}, false, fmt.Errorf("list images: %w", err)
	}
	if want != "" {
		for _, img := range images {
			if img.Name == want || img.ID == want {
				return *img, false, nil
			}
		}
		// Ошибка выборки по проекту глотается намеренно: у токена может не быть
		// прав на проектные образы, и тогда честный ответ — «такого образа нет»,
		// а не «не удалось прочитать проект».
		if own, e := cloud.ListImagesInProject(ctx, projectID); e == nil {
			for _, img := range own {
				if img.Name == want || img.ID == want {
					return *img, true, nil
				}
			}
		}
		return provision.Image{}, false, fmt.Errorf(
			i18n.T("image %q not found in the shared catalogue or in project %s (use the list_images tool to see options)",
				"образ %q не найден ни в общем каталоге, ни в проекте %s (варианты — в выводе инструмента list_images)"),
			want, projectID)
	}
	// Default: prefer ubuntu 24.04 non-gpu.
	for _, img := range images {
		n := strings.ToLower(img.Name)
		if strings.Contains(n, "ubuntu") && strings.Contains(n, "24.04") && !strings.Contains(n, "gpu") {
			return *img, false, nil
		}
	}
	if len(images) > 0 {
		return *images[0], false, nil
	}
	return provision.Image{}, false, fmt.Errorf("no images available")
}

// resolveAccess fills the image's metadata by its declared fields
// (internal/images, the same rules as the CLI deploy and MCP provision) and
// returns:
//   - dynamic: the password fields generated at apply time — the plan is
//     saved to disk and must not hold the password;
//   - static:  every other field, sent verbatim (the key, the hostname, the
//     login, the image's defaults — the API rejects the request with
//     image_metadata_fields_missing if these are omitted);
//   - login and a human label for the SSH model;
//   - readErr: the image could not be read (a warning, defaults are used);
//   - refuse: a value the image does not take, or mandatory fields no value
//     was found for — no plan is made.
func resolveAccess(ctx context.Context, cloud provision.CloudAPI, imageID, hostname string, p Params) (fields []string, static map[string]string, login, keyName string, readErr, refuse error) {
	login = firstNonEmpty(p.Login, "user1")
	withKey := p.SSHKey != "" || p.KeyLater

	detail, err := cloud.GetImage(ctx, imageID)
	if err != nil {
		// Fallback for Ubuntu-style images if the template can't be read.
		fields := []string{"name", "hostname", "linux_password"}
		key := "generated-password"
		if withKey {
			fields = append(fields, "public_key")
			key = "user-key"
		}
		static := map[string]string{}
		for k, v := range p.Meta {
			static[k] = v
		}
		return fields, static, login, key, err, nil
	}

	static, filled, err := images.FillMetadata(detail.MetadataFields, images.Inputs{
		PublicKey: p.SSHKey, // empty: no key field is sent (an empty public_key triggers a 500)
		Hostname:  hostname,
		KeyLater:  p.KeyLater,
		Login:     login,
		Values:    p.Meta,
	})
	if err != nil {
		return nil, nil, login, "", nil, err
	}
	if len(filled.Missing) > 0 {
		return nil, nil, login, "", nil, images.MissingError(clierrors.CodeUsage, filled.Missing,
			`image_metadata {"%s": "<value>"} in deploy_plan`)
	}

	keyName = "generated-password"
	if withKey {
		keyName = "ssh-key (deploy)"
	} else if len(filled.PasswordFields) == 0 {
		keyName = "image-default"
	}
	return filled.PasswordFields, static, login, keyName, nil, nil
}

// pickSubnet is the subnet deploy takes (provision.PickSubnet: the zone's
// main subnet, then a routed one, then the API order); nil when none fits.
func pickSubnet(subnets []*provision.Subnet, zoneID, zoneName string) *provision.Subnet {
	if i := provision.PickSubnet(subnets, zoneID, zoneName); i >= 0 {
		return subnets[i]
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "vm"
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}
