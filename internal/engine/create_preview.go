// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/images"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Resources of a CreatePreview.
const (
	ResourceVM          = "vm"
	ResourceDisk        = "disk"
	ResourceDiskResize  = "disk_resize"
	ResourcePGCluster   = "pg_cluster"
	ResourceK8sCluster  = "k8s_cluster"
	ResourceK8sNodePool = "k8s_nodepool"
	ResourceK8sAddon    = "k8s_addon"
	ResourceS3Bucket    = "s3_bucket"
)

// CreatePreview is what a tool that creates a billable resource is about to
// do, as resolved values. The tool completes it with lookups
// (CompletePreview) and puts it into its confirmation_required answer as
// "preview" (English, for agents as well): every surface — the command,
// 'evo tools call', MCP — gets the same preview, and the CLI only shows it
// (ShowCreatePreview) in the lines of the deploy preview (spec REQ-7).
// Values that are not known are left out rather than filled with a
// placeholder.
type CreatePreview struct {
	// Resource is what is created: vm, disk, disk_resize, pg_cluster,
	// k8s_cluster, k8s_nodepool, k8s_addon or s3_bucket.
	Resource string `json:"resource"`
	Name     string `json:"name,omitempty"`
	// ProjectFrom is where the project comes from: profile, project, env,
	// flag (the configuration's origins) or argument (the call's own).
	ProjectID   string              `json:"project_id,omitempty"`
	ProjectFrom string              `json:"project_from,omitempty"`
	Zone        *deployplan.RefSpec `json:"zone,omitempty"` // of a resource other than a VM
	VM          *deployplan.VMSpec  `json:"vm,omitempty"`
	PublicIP    *bool               `json:"public_ip,omitempty"`
	SubnetID    string              `json:"subnet_id,omitempty"`
	// SubnetName and SecurityGroup: the network deploy plugs its new VM
	// into (DeployPreview), as the preview of evo deploy named it.
	SubnetName    string               `json:"subnet_name,omitempty"`
	SecurityGroup *NetworkRef          `json:"security_group,omitempty"`
	Disk          *deployplan.DiskSpec `json:"disk,omitempty"` // of disk and disk_resize
	// Params are the other key parameters, in the order they are shown.
	Params []PreviewParam `json:"params,omitempty"`
	// Missing names the required values that are not set (project_id,
	// image_id, region): the call will fail without them.
	Missing []string          `json:"missing,omitempty"`
	Cost    *pricing.Estimate `json:"cost,omitempty"`
	// CostNote says where Cost comes from (the price-calculation API, or
	// the rate card: an estimate), or why there is none.
	CostNote string `json:"cost_note,omitempty"`
	// NetworkCheck: what the preview found wrong with the VM's subnet and
	// security group (CompletePreview).
	NetworkCheck *NetworkCheck `json:"network_check,omitempty"`
}

// NetworkRef is a security group of a preview, by id and name.
type NetworkRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PreviewParam is one key parameter of a CreatePreview.
type PreviewParam struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// Add appends a parameter unless its value is empty.
func (p *CreatePreview) Add(key string, v any) {
	switch x := v.(type) {
	case nil:
		return
	case string:
		if x == "" {
			return
		}
	}
	p.Params = append(p.Params, PreviewParam{Key: key, Value: v})
}

// AddArgs appends the call's arguments not shown elsewhere, by name.
func (p *CreatePreview) AddArgs(args map[string]any, skip ...string) {
	skipped := map[string]bool{"confirmed": true, "project_id": true}
	for _, k := range skip {
		skipped[k] = true
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		if !skipped[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		p.Add(k, args[k])
	}
}

const noRateCard = "not estimated: evo has no rate card for this resource"

// Price sets the cost estimate where evo can make one: a VM of a known
// size (the estimator of deploy and deploy_plan) and a disk.
func (p *CreatePreview) Price() {
	p.Cost, p.CostNote = nil, noRateCard
	switch {
	case p.Resource == ResourceVM && p.VM != nil:
		if p.VM.Flavor.VCPUs == 0 {
			p.CostNote = "not estimated: flavor size unknown"
			return
		}
		cost := deployplan.EstimateVM(*p.VM)
		p.Cost = &cost
	case p.Resource == ResourceDisk && p.Disk != nil && p.Disk.SizeGB > 0:
		cost := pricing.EstimateFor("", 0, 0, p.Disk.SizeGB, p.Disk.Type)
		p.Cost = &cost
	default:
		return
	}
	p.CostNote = costNote
}

// priceFromAPI prices a VM preview that has talked to the cloud: the
// price-calculation API's price (deployplan.QuoteVM) in place of the
// rate-card estimate of Price, or, when the API gives none, the estimate
// with the reason (PriceAPIError).
func (p *CreatePreview) priceFromAPI(ctx context.Context, cloud provision.CloudAPI, marketplace bool) {
	p.Price()
	if p.Resource != ResourceVM || p.VM == nil {
		return
	}
	cost, err := deployplan.QuoteVM(ctx, cloud, deployplan.PriceInput{ProjectID: p.ProjectID, VM: *p.VM,
		PublicIP: p.PublicIP == nil || *p.PublicIP})
	switch {
	case err == nil:
		p.Cost, p.CostNote = &cost, apiCostNote
	case p.Cost != nil:
		p.Cost.PriceAPIError = err.Error()
	}
	if marketplace && p.Cost != nil {
		p.Cost.LicenseNote = deployplan.LicenseNote(*p.Cost)
	}
}

// previewLookupTimeout bounds the lookups of CompletePreview: a slow API
// must not hold the question back for long.
const previewLookupTimeout = 20 * time.Second

// networkCheckTimeout bounds the network check of the preview on its own,
// after the price: a slow rules list must not leave the preview without a
// price (ревью Fable, A7).
const networkCheckTimeout = 5 * time.Second

// CompletePreview fills what a tool's preview cannot know from the call and
// the configuration alone, with reads only (the tool calls it before it
// answers confirmation_required, spec REQ-7): the configured project, zone
// names, the flavor auto picks with its size, the image name and the boot
// disk it needs, then the price — of a VM from the price-calculation API
// when it answers (within 5 s, as deploy_plan), else the rate card with
// the API's error (price_api_error). Lookups are
// best effort: what cannot be read stays as it is. The one error is a
// refusal: the VM's image is in the catalogue but the project cannot use it
// (images.NotInProjectError) — the call would be refused after consent.
func (e *Engine) CompletePreview(ctx context.Context, p *CreatePreview) error {
	return e.completePreview(ctx, p, nil, true)
}

// completePreview is CompletePreview with the flavor pick of the call:
// pick chooses the flavor from the catalogue (nil: PickVMFlavor with the
// flavor of the preview, as provision does); network adds the check of the
// subnet and the security group provision would take.
func (e *Engine) completePreview(ctx context.Context, p *CreatePreview, pick func([]*provision.Flavor) (string, error), network bool) error {
	if p.ProjectID == "" && p.Resource != ResourceS3Bucket && !contains(p.Missing, "project_id") {
		p.ProjectID, p.ProjectFrom = e.Config.ProjectID, e.Config.ProjectOrigin
		if p.ProjectID == "" {
			p.Missing = append(p.Missing, "project_id")
		}
	}
	priced := false
	defer func() {
		if !priced {
			p.Price()
		}
	}()
	zone := p.Zone
	if p.VM != nil {
		zone = &p.VM.Zone
	}
	needZone := zone != nil && zone.ID != "" && (zone.Name == "" || zone.Name == zone.ID)
	if (!needZone && p.VM == nil) || e.Auth == nil || e.Cloud == nil {
		return nil
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, previewLookupTimeout)
	defer cancel()
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return nil
	}
	e.Cloud.SetToken(tok.Value)
	if needZone {
		if zones, err := e.Cloud.ListAvailabilityZones(ctx); err == nil {
			for _, z := range zones {
				if z.ID == zone.ID {
					zone.Name = z.Name
					break
				}
			}
		}
	}
	if p.VM == nil {
		return nil
	}
	vm := p.VM
	if pick == nil {
		pick = func(flavors []*provision.Flavor) (string, error) {
			return PickVMFlavor(flavors, firstNonEmpty(vm.Flavor.ID, vm.Flavor.Name))
		}
	}
	if flavors, err := e.Cloud.ListFlavors(ctx); err == nil {
		if id, err := pick(flavors); err == nil {
			for _, f := range flavors {
				if f.ID == id {
					vm.Flavor = deployplan.FlavorSpec{ID: f.ID, Name: f.Name, VCPUs: f.VCPUs, RAMGB: f.RAMGB, FreeTier: f.FreeTier}
					break
				}
			}
		}
	}
	cat := images.Catalog{Cloud: e.Cloud, ProjectID: p.ProjectID}
	checked, marketplace := false, false
	if vm.Image.ID == "" && vm.Image.Name != "" {
		// image by name: the call resolves it the same way.
		if img, err := cat.Resolve(ctx, vm.Image.Name); err == nil {
			if img.NotInProject {
				return images.NotInProjectError(img.Name)
			}
			vm.Image.ID, vm.Image.Name, checked = img.ID, img.Name, true
			marketplace = img.Type == provision.ImageTypeMarketplace
		}
	}
	if vm.Image.ID != "" {
		if img, err := e.Cloud.GetImage(ctx, vm.Image.ID); err == nil {
			if !checked {
				cat.MarkInProject(ctx, img)
				if img.NotInProject {
					return images.NotInProjectError(img.Name)
				}
			}
			if img.Name != "" {
				vm.Image.Name = img.Name
			}
			if img.Type != "" {
				marketplace = img.Type == provision.ImageTypeMarketplace
			}
			vm.Disk.SizeGB, _ = provision.BootDiskGB(vm.Disk.SizeGB, img.MinDisk)
		}
	}
	p.priceFromAPI(ctx, e.Cloud, marketplace)
	priced = true
	if !network {
		return nil
	}
	nctx, ncancel := context.WithTimeout(parent, networkCheckTimeout)
	defer ncancel()
	p.NetworkCheck = e.checkNetwork(nctx, p.ProjectID, vm.Zone.ID, vm.Zone.Name, p.SubnetID)
	return nil
}

// PickVMFlavor is the flavor provision creates a VM with: a name or UUID
// as given, or for "" and "auto" the smallest lowcost flavor (free tier is
// limited to one VM, usually in use, so it is skipped). It returns the
// flavor's UUID.
func PickVMFlavor(flavors []*provision.Flavor, want string) (string, error) {
	if want != "" && want != "auto" {
		for _, f := range flavors {
			if f.Name == want || f.ID == want {
				return f.ID, nil
			}
		}
		if isUUID(want) {
			return want, nil
		}
		names := make([]string, len(flavors))
		for i, f := range flavors {
			names[i] = f.Name + " (" + f.ID + ")"
		}
		return "", fmt.Errorf("flavor %q not found. Available: %s", want, strings.Join(names, ", "))
	}
	var best *provision.Flavor
	for _, f := range flavors {
		name := strings.ToLower(f.Name)
		if f.FreeTier || strings.Contains(name, "free") || !strings.Contains(name, "low") {
			continue
		}
		if best == nil || f.VCPUs < best.VCPUs || (f.VCPUs == best.VCPUs && f.RAMGB < best.RAMGB) {
			best = f
		}
	}
	if best != nil {
		return best.ID, nil
	}
	for _, f := range flavors {
		if !f.FreeTier {
			return f.ID, nil
		}
	}
	if len(flavors) > 0 {
		return flavors[0].ID, nil
	}
	return "", nil
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
	}
	return true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ShowCreatePreview is the human form of a CreatePreview, in the lines of
// the deploy preview: what is created, the project and where it comes from,
// the zone by name, the key parameters and the price marked as an estimate
// (or a note that there is none). action names the operation in the
// refusal message.
func ShowCreatePreview(action string, p CreatePreview) confirm.Preview {
	zone := p.Zone
	if p.VM != nil {
		zone = &p.VM.Zone
	}
	zoneName := ""
	if zone != nil {
		zoneName = firstNonEmpty(zone.Name, zone.ID)
	}

	var lines []string
	lines = append(lines, createdLine(p))
	if p.ProjectID != "" {
		lines = append(lines, previewLabel("project", "проект")+projectText(p.ProjectID, p.ProjectFrom))
	}
	if vm := p.VM; vm != nil {
		if vm.Flavor.Name != "" || vm.Flavor.ID != "" {
			f := vm.Flavor
			f.Name = firstNonEmpty(f.Name, f.ID)
			lines = append(lines, previewLabel("flavor", "flavor")+flavorText(f))
		}
		if zoneName != "" {
			lines = append(lines, previewLabel("zone", "зона")+refText(*zone))
		}
		if vm.Image.ID != "" || vm.Image.Name != "" {
			lines = append(lines, previewLabel("image", "образ")+refText(vm.Image))
		}
		if vm.Disk.SizeGB > 0 {
			lines = append(lines, bootDiskLine(vm.Disk.SizeGB))
		}
		switch {
		case p.SecurityGroup != nil:
			// deploy: the network it resolved, as its preview said it.
			lines = append(lines, previewLabel("network", "сеть")+i18n.Tf("subnet %s, security group %s", "подсеть %s, группа безопасности %s",
				firstNonEmpty(p.SubnetName, p.SubnetID), firstNonEmpty(p.SecurityGroup.Name, p.SecurityGroup.ID)))
		case p.SubnetID != "":
			lines = append(lines, previewLabel("network", "сеть")+i18n.Tf("subnet %s", "подсеть %s", p.SubnetID))
		}
		if p.PublicIP != nil {
			lines = append(lines, publicIPLine(*p.PublicIP))
		}
		for _, prm := range p.Params {
			v := paramValue(prm.Value)
			switch {
			case v == "":
			case prm.Key == "ports":
				lines = append(lines, previewLabel("ports", "порты")+i18n.Tf("%s opened to 0.0.0.0/0 in the VM's security group (shared by every VM in it); destroy closes them",
					"%s открываются для 0.0.0.0/0 в группе безопасности ВМ (она общая для всех ВМ в ней); destroy их закроет", v))
			case prm.Key == "image_metadata":
				lines = append(lines, previewLabel("fields", "поля")+i18n.Tf("%s of the image, as given", "%s — поля образа с заданными значениями", v))
			default:
				lines = append(lines, paramLine(prm))
			}
		}
	} else {
		if zoneName != "" {
			lines = append(lines, previewLabel("zone", "зона")+refText(*zone))
		}
		if d := p.Disk; d != nil && p.Resource == ResourceDisk {
			size := i18n.Tf("%d GB", "%d ГБ", d.SizeGB)
			if d.Type != "" {
				size += i18n.Tf(", type %s", ", тип %s", d.Type)
			}
			lines = append(lines, previewLabel("disk", "диск")+size)
		}
		for _, prm := range p.Params {
			if l := paramLine(prm); l != "" {
				lines = append(lines, l)
			}
		}
		if p.Resource == ResourceDiskResize {
			lines = append(lines, i18n.T("then grow the partition and the file system on the VM (growpart, resize2fs)",
				"после этого расширьте раздел и файловую систему на ВМ (growpart, resize2fs)"))
		}
	}
	if len(p.Missing) > 0 {
		names := make([]string, len(p.Missing))
		for i, m := range p.Missing {
			names[i] = missingText(m)
		}
		lines = append(lines, previewLabel("not set", "не задано")+strings.Join(names, ", ")+
			i18n.T(": the call will fail without it", ": без этого команда завершится ошибкой"))
	}
	lines = append(lines, createCostLines(p)...)
	for _, w := range p.NetworkCheck.lines() {
		lines = append(lines, previewLabel("warning", "внимание")+w)
	}

	return confirm.Preview{Action: action, Summary: createSummary(p, zoneName), Lines: lines}
}

// createSummary is the heading of the preview, phrased as for deploy.
func createSummary(p CreatePreview, zone string) string {
	inZone := ""
	if zone != "" {
		inZone = i18n.Tf(" in %s", " в зоне %s", zone)
	}
	switch p.Resource {
	case ResourceVM:
		s := i18n.Tf("create VM %s", "создать ВМ %s", p.Name)
		if p.VM != nil && p.VM.Flavor.Name != "" {
			s += " (" + p.VM.Flavor.Name + ")"
		}
		return s + inZone
	case ResourceDisk:
		s := i18n.Tf("create disk %s", "создать диск %s", p.Name)
		if p.Disk != nil {
			s += i18n.Tf(" (%d GB)", " (%d ГБ)", p.Disk.SizeGB)
		}
		return s + inZone
	case ResourceDiskResize:
		size := 0
		if p.Disk != nil {
			size = p.Disk.SizeGB
		}
		return i18n.Tf("grow disk %s to %d GB", "увеличить диск %s до %d ГБ", p.Name, size)
	case ResourcePGCluster:
		return i18n.Tf("create PostgreSQL cluster %s", "создать кластер PostgreSQL %s", p.Name) + inZone
	case ResourceK8sCluster:
		return i18n.Tf("create Kubernetes cluster %s", "создать кластер Kubernetes %s", p.Name) + inZone
	case ResourceK8sNodePool:
		return i18n.Tf("add node pool %s to a Kubernetes cluster", "добавить группу узлов %s в кластер Kubernetes", p.Name) + inZone
	case ResourceK8sAddon:
		return i18n.Tf("install addon %s into a Kubernetes cluster", "установить плагин %s в кластер Kubernetes", p.Name)
	case ResourceS3Bucket:
		return i18n.Tf("create bucket %s", "создать бакет %s", p.Name)
	}
	return i18n.Tf("create %s %s", "создать %s %s", p.Resource, p.Name)
}

// createdLine is the first line: what will be created (and billed).
func createdLine(p CreatePreview) string {
	var what []string
	switch p.Resource {
	case ResourceVM:
		what = append(what, i18n.Tf("VM %s", "ВМ %s", p.Name))
		if p.VM != nil && p.VM.Disk.SizeGB > 0 {
			what = append(what, i18n.Tf("a %d GB boot disk", "загрузочный диск %d ГБ", p.VM.Disk.SizeGB))
		}
		if p.PublicIP == nil || *p.PublicIP {
			what = append(what, i18n.T("a floating IP", "floating IP"))
		}
	case ResourceDisk:
		what = append(what, i18n.Tf("disk %s", "диск %s", p.Name))
	case ResourceDiskResize:
		size := 0
		if p.Disk != nil {
			size = p.Disk.SizeGB
		}
		return i18n.Tf("Will change: disk %s grows to %d GB (it cannot shrink back)",
			"Будет изменено: диск %s увеличится до %d ГБ (уменьшить обратно нельзя)", p.Name, size)
	case ResourcePGCluster:
		what = append(what, i18n.Tf("Managed PostgreSQL cluster %s", "кластер Managed PostgreSQL %s", p.Name))
	case ResourceK8sCluster:
		what = append(what, i18n.Tf("Managed Kubernetes cluster %s", "кластер Managed Kubernetes %s", p.Name))
	case ResourceK8sNodePool:
		what = append(what, i18n.Tf("worker node pool %s", "группа рабочих узлов %s", p.Name))
	case ResourceK8sAddon:
		return i18n.Tf("Will install: addon %s into the cluster (the cluster's nodes run it)",
			"Будет установлено: плагин %s в кластер (работает на узлах кластера)", p.Name)
	case ResourceS3Bucket:
		what = append(what, i18n.Tf("Object Storage bucket %s", "бакет Object Storage %s", p.Name))
	default:
		what = append(what, p.Resource+" "+p.Name)
	}
	return i18n.T("Will create: ", "Будет создано: ") + strings.Join(what, ", ")
}

// createCostLines is the price: in the words of deploy (the API's price or
// the estimate, with its source), or why there is none.
func createCostLines(p CreatePreview) []string {
	if p.Cost != nil {
		if p.Resource == ResourceVM {
			return vmCostLines(*p.Cost, true)
		}
		return []string{previewLabel("cost", "цена") + i18n.Tf("~%.2f %s/month (disk), %s", "~%.2f %s в месяц (диск), %s",
			p.Cost.TotalMonthlyRub, p.Cost.Currency, costNoteText())}
	}
	if p.Resource == ResourceVM {
		return []string{previewLabel("cost", "цена") + i18n.T("not estimated: the flavor size is unknown (see 'evo vm flavors')",
			"не оценена: размер flavor неизвестен (см. 'evo vm flavors')")}
	}
	return []string{previewLabel("cost", "цена") + i18n.T("not estimated: evo has no rate card for this resource",
		"не оценивается: у evo нет тарифа для этого ресурса")}
}

func missingText(m string) string {
	switch m {
	case "project_id":
		return i18n.T("project (--project-id or CLOUDRU_PROJECT_ID)", "проект (--project-id или CLOUDRU_PROJECT_ID)")
	case "image_id":
		return i18n.T("image (--image, --image-id or CLOUDRU_IMAGE_ID)", "образ (--image, --image-id или CLOUDRU_IMAGE_ID)")
	case "region":
		return i18n.T("zone (--region or CLOUDRU_REGION)", "зона (--region или CLOUDRU_REGION)")
	}
	return m
}

// paramLabels name the key parameters in both languages; gb marks a size
// in gigabytes.
var paramLabels = map[string]struct {
	en, ru string
	gb     bool
}{
	"version":           {"version", "версия", false},
	"cpu":               {"vCPU", "vCPU", false},
	"memory_gb":         {"RAM", "RAM", true},
	"ram":               {"RAM", "RAM", true},
	"ram_gb":            {"RAM", "RAM", true},
	"storage_gb":        {"storage", "хранилище", true},
	"disk_gb":           {"node disk", "диск узла", true},
	"instances":         {"nodes", "узлов", false},
	"count":             {"nodes", "узлов", false},
	"master_count":      {"masters", "мастеров", false},
	"deployment_mode":   {"mode", "режим", false},
	"subnet_id":         {"subnet", "подсеть", false},
	"nodes_subnet_id":   {"subnet", "подсеть", false},
	"security_group_id": {"SG", "группа SG", false},
	"initial_database":  {"database", "база", false},
	"backup_enabled":    {"backups", "бэкапы", false},
	"flavor":            {"flavor", "flavor", false},
	"kube_api_internet": {"public API", "API извне", false},
	"cluster_sa_id":     {"account", "аккаунт", false},
	"cluster_id":        {"cluster", "кластер", false},
	"product":           {"product", "продукт", false},
	"region":            {"region", "регион", false},
	"attach_to":         {"attach to", "к ВМ", false},
	"disk_id":           {"disk", "диск", false},
	"description":       {"about", "описание", false},
	"cluster_name":      {"cluster name", "имя кластера", false},
	"addon_version":     {"addon version", "версия плагина", false},
	"support":           {"support", "поддержка", false},
	"namespace":         {"namespace", "пространство имён", false},
	"requires_kube":     {"needs Kubernetes", "нужен Kubernetes", false},
	"cluster_version":   {"cluster Kubernetes", "Kubernetes кластера", false},
	"requires_workers":  {"needs ready nodes", "нужно готовых узлов", false},
	"worker_nodes":      {"nodes in pools", "узлов в группах", false},
	"installed":         {"already installed", "уже установлен", false},
	"billable":          {"billable addon", "платный плагин", false},
	"resolve_mode":      {"on conflict", "при конфликте", false},
	"config_keys":       {"settings", "настройки", false},
}

// notCheckedLabels name what a k8s_addon preview could not check (its
// not_checked parameter).
var notCheckedLabels = map[string]struct{ en, ru string }{
	"kube_version":  {"the cluster's Kubernetes version (not in the API answer)", "версия Kubernetes кластера (нет в ответе API)"},
	"ready_workers": {"ready nodes (node pools unreadable or autoscaled)", "готовые узлы (группы узлов не прочитались или с автомасштабированием)"},
	"installed":     {"whether the addon is already installed", "установлен ли плагин уже"},
}

// paramLine is one parameter as "label: value"; "" for an empty value.
func paramLine(prm PreviewParam) string {
	if prm.Key == "not_checked" {
		return notCheckedLine(prm.Value)
	}
	v := paramValue(prm.Value)
	if v == "" {
		return ""
	}
	l, ok := paramLabels[prm.Key]
	if !ok {
		return previewLabel(strings.ReplaceAll(prm.Key, "_", "-"), strings.ReplaceAll(prm.Key, "_", "-")) + v
	}
	if l.gb {
		v += i18n.T(" GB", " ГБ")
	}
	return previewLabel(l.en, l.ru) + v
}

// notCheckedLine is the not_checked parameter in words.
func notCheckedLine(v any) string {
	var items []string
	switch x := v.(type) {
	case []string:
		items = x
	case []any:
		for _, e := range x {
			items = append(items, paramValue(e))
		}
	}
	if len(items) == 0 {
		return ""
	}
	words := make([]string, len(items))
	for i, it := range items {
		words[i] = it
		if l, ok := notCheckedLabels[it]; ok {
			words[i] = i18n.T(l.en, l.ru)
		}
	}
	return previewLabel("not checked", "не проверено") + strings.Join(words, "; ")
}

func paramValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return i18n.T("yes", "да")
		}
		return i18n.T("no", "нет")
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			parts = append(parts, paramValue(e))
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(v)
}
