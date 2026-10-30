// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/pricing"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// costNote goes next to a rate-card price shown before an operation
// (internal/pricing): the price-calculation API of Cloud.ru was not asked
// or gave none.
const costNote = "evo rate-card estimate, not a bill"

// apiCostNote goes next to a price of the price-calculation API.
const apiCostNote = "Cloud.ru price-calculation API, not a bill; image license not included"

func costNoteText() string {
	return i18n.T(costNote, "оценка по тарифной карте evo, а не счёт")
}

// costNoteFor is the note of a price, by its source.
func costNoteFor(cost pricing.Estimate) string {
	if cost.Source == pricing.SourceAPI {
		return i18n.T(apiCostNote, "по API расчёта цены Cloud.ru, а не счёт; без лицензии образа")
	}
	return costNoteText()
}

// previewLabel is the name of a preview line, padded so the values line up;
// a longer name ("пространство имён") keeps one space before its value.
func previewLabel(en, ru string) string {
	label, width := ru+":", 12
	if i18n.Lang() == i18n.EN {
		label, width = en+":", 11
	}
	if utf8.RuneCountInString(label) >= width {
		return label + " "
	}
	return fmt.Sprintf("%-*s", width, label)
}

// errDryRun stops provisionVM before the VM is created; Deploy turns it into
// success.
var errDryRun = errors.New("dry run")

// confirmOptions wires confirm.Ask to the engine's printer: results to its
// writer, previews and prompts to its error writer.
func (e *Engine) confirmOptions(yes, dryRun, noPrompt bool) confirm.Options {
	return confirm.Options{
		Yes:      yes,
		DryRun:   dryRun,
		NoPrompt: noPrompt,
		JSON:     e.Printer.IsJSON(),
		Out:      e.Printer.Writer(),
		Err:      e.Printer.ErrWriter(),
	}
}

// deployPreview describes the VM about to be created, priced as the MCP
// deploy_plan prices it (deployplan.PriceVM: the price-calculation API, else
// the rate card). Lookups of names are best effort: the preview falls back
// to the IDs rather than failing the deploy.
func (e *Engine) deployPreview(ctx context.Context, req provision.CreateVMRequest, img *provision.ImageDetail, diskTypeID string, network vmNetwork, ports, udpPorts []int) confirm.Preview {
	vm := deployplan.VMSpec{
		Name:   req.Name,
		Flavor: deployplan.FlavorSpec{Name: req.FlavorName},
		Image:  deployplan.RefSpec{Name: req.ImageName},
		Zone:   deployplan.RefSpec{ID: req.AvailabilityZoneID, Name: req.AvailabilityZoneID},
	}
	if img != nil {
		vm.Image.ID = img.ID
	}
	if flavors, err := e.Cloud.ListFlavors(ctx); err == nil {
		for _, f := range flavors {
			if f.Name == req.FlavorName || f.ID == req.FlavorName {
				vm.Flavor = deployplan.SpecOf(f)
				break
			}
		}
	}
	if zones, err := e.Cloud.ListAvailabilityZones(ctx); err == nil {
		for _, z := range zones {
			if z.ID == req.AvailabilityZoneID {
				vm.Zone.Name = z.Name
				break
			}
		}
	}
	if img != nil && img.Name != "" {
		vm.Image.Name = img.Name
	}
	if len(req.Disks) > 0 {
		vm.Disk = deployplan.DiskSpec{SizeGB: req.Disks[0].Size, Type: req.Disks[0].DiskTypeName, TypeID: diskTypeID}
	}
	cost := deployplan.PriceVM(ctx, e.Cloud, deployplan.PriceInput{ProjectID: req.ProjectID, VM: vm, PublicIP: true,
		Marketplace: img != nil && img.Type == provision.ImageTypeMarketplace})

	lines := []string{
		previewLabel("project", "проект") + projectText(req.ProjectID, e.projectFrom(nil)),
		previewLabel("flavor", "flavor") + flavorText(vm.Flavor),
		previewLabel("zone", "зона") + refText(vm.Zone),
		previewLabel("image", "образ") + refText(vm.Image),
		bootDiskLine(vm.Disk.SizeGB),
		previewLabel("network", "сеть") + i18n.Tf("subnet %s, security group %s", "подсеть %s, группа безопасности %s",
			network.SubnetName, network.SecurityGroup),
		publicIPLine(true),
	}
	if len(ports)+len(udpPorts) > 0 {
		ps := make([]string, 0, len(ports)+len(udpPorts))
		for _, p := range ports {
			ps = append(ps, fmt.Sprint(p))
		}
		for _, p := range udpPorts {
			ps = append(ps, fmt.Sprintf("%d/udp", p))
		}
		lines = append(lines, previewLabel("ports", "порты")+i18n.Tf("%s opened to %s in security group %s (shared by every VM in it); "+
			"ports already open there are left as they are, destroy deletes only the rules this deploy creates",
			"%s открываются для %s в группе безопасности %s (она общая для всех ВМ в ней); "+
				"уже открытые остаются как есть, destroy удалит только правила, которые создаст этот deploy",
			strings.Join(ps, ","), sgrules.Anywhere, network.SecurityGroup))
	}
	lines = append(lines, vmCostLines(cost, vm.Flavor.VCPUs > 0)...)
	quota := e.quotaCheck(ctx, req.ProjectID)
	if l := quota.line(); l != "" {
		lines = append(lines, l)
	}
	details := map[string]any{
		"project_id":     req.ProjectID,
		"project_from":   e.projectFrom(nil),
		"vm":             vm,
		"floating_ip":    true,
		"subnet_id":      network.SubnetID,
		"security_group": map[string]string{"id": network.SecurityGroupID, "name": network.SecurityGroup},
		"open_ports":     ports,
		"open_udp_ports": udpPorts,
		"cost":           cost,
		"cost_note":      costNoteFor(cost),
	}
	return confirm.Preview{
		Action:  "deploy",
		Summary: i18n.Tf("create VM %s (%s) in %s", "создать ВМ %s (%s) в зоне %s", vm.Name, vm.Flavor.Name, vm.Zone.Name),
		Lines:   lines,
		Details: details,
	}
}

// flavorText is the flavor of a preview: its name and, when known, its
// size, the guaranteed share of a vCPU, the GPUs and spot — each only as
// the catalogue says it (С5: no field, no word).
func flavorText(f deployplan.FlavorSpec) string {
	s := f.Name
	if f.VCPUs > 0 {
		s += i18n.Tf(" (%d vCPU", " (%d vCPU", f.VCPUs)
		if f.Share != "" {
			s += i18n.Tf(", vCPU share %s", ", доля vCPU %s", f.Share)
		}
		s += i18n.Tf(", %d GB RAM", ", %d ГБ RAM", f.RAMGB)
		if f.GPU > 0 {
			s += i18n.Tf(", %d GPU", ", %d GPU", f.GPU)
		}
		s += ")"
	}
	if f.Spot {
		s += i18n.T(", spot: the VM may be stopped by the platform", ", spot: платформа может остановить ВМ")
	}
	return s
}

// bootDiskLine is the boot disk of a VM preview.
func bootDiskLine(sizeGB int) string {
	return previewLabel("disk", "диск") + i18n.Tf("%d GB boot disk (priced at the SSD rate)",
		"загрузочный, %d ГБ (цена по тарифу SSD)", sizeGB)
}

// publicIPLine says whether a VM preview gets a public address.
func publicIPLine(public bool) string {
	if !public {
		return previewLabel("public IP", "внешний IP") + i18n.T("none (private VM, no floating IP)", "нет (ВМ без floating IP)")
	}
	return previewLabel("public IP", "внешний IP") + i18n.T("a floating IP will be created and attached", "будет создан и привязан floating IP")
}

// vmCostLines is the price of a VM preview with its source: the
// price-calculation API, or the rate card — then marked as an estimate,
// with why the API gave no price when it was asked. Without the flavor size
// the compute part of an estimate may be missing from the total. A
// marketplace image gets a line that its license is not in the price.
func vmCostLines(cost pricing.Estimate, sizeKnown bool) []string {
	var lines []string
	if cost.Source == pricing.SourceAPI {
		lines = append(lines, apiCostLine(cost))
	} else {
		line := previewLabel("cost", "цена") + i18n.Tf("~%.2f %s/month (compute %.2f + disk %.2f), %s",
			"~%.2f %s в месяц (вычисления %.2f + диск %.2f), %s",
			cost.TotalMonthlyRub, cost.Currency, cost.ComputeMonthlyRub, cost.DiskMonthlyRub, costNoteText())
		if !sizeKnown {
			line += i18n.T("; flavor size unknown, compute may be missing from the total",
				"; размер flavor неизвестен, вычисления могут не войти в итог")
		}
		lines = append(lines, line)
		if cost.PriceAPIError != "" {
			lines = append(lines, previewLabel("API price", "цена API")+i18n.Tf("none (%s), the estimate is shown",
				"не получена (%s), показана оценка", cost.PriceAPIError))
		}
	}
	if cost.LicenseNote != "" {
		lines = append(lines, previewLabel("license", "лицензия")+cost.LicenseNote)
	}
	return lines
}

// apiCostLine is a price of the price-calculation API: the totals and, when
// the components add up to one of them, what makes the month.
func apiCostLine(cost pricing.Estimate) string {
	line := previewLabel("cost", "цена") + i18n.Tf("~%.2f %s/month, %.2f/hour", "~%.2f %s в месяц, %.2f в час",
		cost.TotalMonthlyRub, cost.Currency, cost.TotalHourlyRub)
	if b := cost.Breakdown; b != nil && b.Period != "unknown" {
		parts := []string{i18n.Tf("VM %.2f", "ВМ %.2f", cost.ComputeMonthlyRub), i18n.Tf("disk %.2f", "диск %.2f", cost.DiskMonthlyRub)}
		if cost.FloatingIPMonthlyRub > 0 {
			parts = append(parts, i18n.Tf("floating IP %.2f", "floating IP %.2f", cost.FloatingIPMonthlyRub))
		}
		if rest := cost.TotalMonthlyRub - cost.ComputeMonthlyRub - cost.DiskMonthlyRub - cost.FloatingIPMonthlyRub; rest > 0.02 { // beyond rounding
			parts = append(parts, i18n.Tf("other %.2f", "прочее %.2f", rest))
		}
		line += " (" + strings.Join(parts, " + ") + ")"
	}
	return line + ", " + costNoteFor(cost)
}

// redeployCreates are the billable resources an update of rec makes.
func redeployCreates(rec *state.DeploymentRecord) []string {
	if needsFloatingIP(rec) {
		return []string{"floating_ip"}
	}
	return []string{}
}

// redeployPreview describes an update of an already running VM;
// composeChanged: the compose file is not the deployed one — else the
// update is --force, or (force false) the retry of a deploy that did not
// finish.
func redeployPreview(rec *state.DeploymentRecord, project, from string, composeChanged, force bool) confirm.Preview {
	lines := []string{
		previewLabel("project", "проект") + projectText(project, from),
		i18n.T("upload the compose file to the existing VM and run 'docker compose up -d'",
			"загрузить compose-файл на существующую ВМ и выполнить 'docker compose up -d'"),
	}
	switch {
	case composeChanged:
	case force:
		lines = append(lines, i18n.T("the compose file has not changed: redeployed because of --force",
			"compose-файл не изменился: развёртывание заново из-за --force"))
	default:
		lines = append(lines, i18n.Tf("the compose file has not changed: the last deploy did not finish (%s), this one retries it",
			"compose-файл не изменился: прошлый deploy не завершился (%s), этот повторяет его", rec.Status))
	}
	creates := redeployCreates(rec)
	if len(creates) > 0 {
		lines = append(lines, i18n.T("the VM has no public IP: a floating IP is created and attached to it (billed); destroy releases it",
			"у ВМ нет внешнего IP: будет создан и привязан floating IP (тарифицируется); destroy его освободит"))
	} else {
		lines = append(lines, i18n.T("no new resources are created", "новые ресурсы не создаются"))
	}
	if rec.SecurityGroupID != "" {
		lines = append(lines, i18n.Tf("ports of the compose file not yet open are opened in security group %s"+
			"; ports already open there are left as they are, destroy deletes only the rules deploy creates",
			"ещё не открытые порты compose-файла открываются в группе безопасности %s"+
				"; уже открытые остаются как есть, destroy удалит только правила, которые создаст deploy", groupText(rec)))
	}
	summary := i18n.Tf("update VM %s", "обновить ВМ %s", vmText(rec))
	if rec.VMIP != "" {
		summary += ", " + rec.VMIP
	}
	return confirm.Preview{
		Action:  "deploy",
		Summary: summary,
		Lines:   lines,
		Details: map[string]any{"project_id": project, "vm_id": rec.VMID, "vm_ip": rec.VMIP, "creates": creates,
			"update": true, "compose_changed": composeChanged},
	}
}

// groupText is the security group of a deployment: its name and ID.
func groupText(rec *state.DeploymentRecord) string {
	return named(rec.SecurityGroupName, rec.SecurityGroupID)
}

// destroyResource is one entry of the destroy preview.
type destroyResource struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Address string `json:"address,omitempty"`
	Action  string `json:"action"`
	Note    string `json:"note,omitempty"`
}

// rulePreview lists the port rules destroy deletes or keeps for the given
// deployments, with a warning about the group being shared.
func rulePreview(sf *state.File, ids []string) ([]destroyResource, []string) {
	var res []destroyResource
	var lines []string
	deleting := false
	for _, p := range sgrules.Plan(sf, ids...) {
		label := sgrules.Label(p.Rule.Port)
		if p.KeptFor != "" {
			note := i18n.T("still needed by deployment ", "нужно развёртыванию ") + p.KeptFor
			res = append(res, destroyResource{Type: "security_group_rule", ID: p.Rule.ID, Address: p.Rule.Port, Action: "keep", Note: note})
			lines = append(lines, i18n.Tf("keep the rule for port %s in security group %s (%s)",
				"оставить правило для порта %s в группе безопасности %s (%s)", label, p.Rule.SecurityGroupID, note))
			continue
		}
		deleting = true
		r := destroyResource{Type: "security_group_rule", ID: p.Rule.ID, Address: p.Rule.Port, Action: "delete"}
		line := i18n.Tf("delete the rule %s opening port %s in security group %s",
			"удалить правило %s, которое открывает порт %s в группе безопасности %s", p.Rule.ID, label, p.Rule.SecurityGroupID)
		if rec := sf.FindDeployment(p.RecordID); rec != nil && rec.Status == "destroyed" {
			r.Note = i18n.T("left by an earlier destroy", "осталось от прошлого destroy")
			line += " (" + r.Note + ")"
		}
		res = append(res, r)
		lines = append(lines, line)
	}
	if deleting {
		// Other projects cannot be seen from this state: a VM deployed from
		// another directory may rely on a rule this deploy created.
		lines = append(lines, i18n.T("the security group is shared: other VMs in it that rely on these ports lose them too "+
			"(re-run 'evo deploy --force' in their directory to reopen)",
			"группа безопасности общая: другие ВМ в ней, которым нужны эти порты, тоже их потеряют "+
				"(чтобы открыть снова, выполните 'evo deploy --force' в их каталоге)"))
	}
	return res, lines
}

// pendingRulesPreview is the preview of a destroy that only finishes the
// rules an earlier destroy could not delete.
func pendingRulesPreview(sf *state.File, pending []string) confirm.Preview {
	res, lines := rulePreview(sf, pending)
	return confirm.Preview{
		Action: "destroy",
		Summary: i18n.T("delete security group rules left by an earlier destroy",
			"удалить правила группы безопасности, оставшиеся от прошлого destroy"),
		Lines:   lines,
		Details: map[string]any{"resources": res},
	}
}

// destroyPreview lists what destroy deletes and what it leaves behind.
func destroyPreview(rec *state.DeploymentRecord, sf *state.File, pending []string, project, from string) confirm.Preview {
	res := []destroyResource{
		{Type: "vm", ID: rec.VMID, Name: rec.VMName, Address: rec.VMIP, Action: "delete",
			Note: i18n.T("all data on its disks is lost", "все данные на её дисках будут потеряны")},
	}
	lines := []string{
		previewLabel("project", "проект") + projectText(project, from),
		i18n.Tf("delete VM %s and all data on its disks", "удалить ВМ %s и все данные на её дисках", vmText(rec)),
	}
	if rec.VMIP != "" || rec.FloatingIPID != "" {
		// The public address of a deployed VM is a floating IP; destroy
		// releases it after the VM (by floating_ip_id, else by address).
		res = append(res, destroyResource{Type: "floating_ip", ID: rec.FloatingIPID, Address: rec.VMIP, Action: "release"})
		lines = append(lines, i18n.Tf("release floating IP %s", "освободить floating IP %s", fipLabel(rec.VMIP, rec.FloatingIPID)))
	}
	ruleRes, ruleLines := rulePreview(sf, append([]string{rec.ID}, pending...))
	res = append(res, ruleRes...)
	lines = append(lines, ruleLines...)
	for _, key := range rec.PortsAlreadyOpen {
		note := i18n.T("not opened by this deploy", "открывался не этим deploy")
		res = append(res, destroyResource{Type: "security_group_rule", Address: key, Action: "keep", Note: note})
		lines = append(lines, i18n.Tf("keep port %s open in security group %s (%s)", "оставить порт %s открытым в группе безопасности %s (%s)",
			sgrules.Label(key), groupText(rec), note))
	}
	res = append(res, destroyResource{Type: "state_record", ID: rec.ID, Action: "remove"})
	lines = append(lines, i18n.T("remove the deployment from local state", "удалить запись о развёртывании из локального state"))

	summary := i18n.T("destroy VM ", "удалить ВМ ") + vmText(rec)
	if z := zoneText(rec.ZoneName, rec.Region); z != "" {
		summary += i18n.T(" in ", " в зоне ") + z
	}
	return confirm.Preview{
		Action:  "destroy",
		Summary: summary,
		Lines:   lines,
		Details: map[string]any{"project_id": project, "project_from": from, "resources": res},
	}
}

// projectText names the project in a preview and where it comes from: with
// a key that sees several projects, the person approving must see which one
// is changed and why that one (QA P6; like 'aws configure list').
func projectText(id, from string) string {
	if id == "" {
		return i18n.T("(not set)", "(не задан)")
	}
	if label := originLabel(from); label != "" {
		return id + " (" + label + ")"
	}
	return id
}

// Origins of the project in a preview, besides the config ones: the state
// record of a deployment, and the saved plan deploy_apply executes.
const (
	originRecord = "record"
	originPlan   = "plan"
)

func originLabel(from string) string {
	switch from {
	case config.OriginProfile:
		return i18n.T("from the profile", "из профиля")
	case config.OriginProject:
		return i18n.T("from ", "из ") + config.ProjectFileName
	case config.OriginEnv:
		return i18n.T("from CLOUDRU_PROJECT_ID", "из CLOUDRU_PROJECT_ID")
	case config.OriginFlag:
		return i18n.T("from --project-id", "из --project-id")
	case originRecord:
		return i18n.T("where the deployment was created", "где создано развёртывание")
	case originPlan:
		return i18n.T("from the plan", "из плана")
	}
	return ""
}

// projectFrom is the origin of the project a deployment works in: its
// state record, else the configuration.
func (e *Engine) projectFrom(rec *state.DeploymentRecord) string {
	if rec != nil && rec.ProjectID != "" {
		return originRecord
	}
	return e.Config.ProjectOrigin
}

func refText(r deployplan.RefSpec) string {
	if r.ID == "" {
		return r.Name
	}
	if r.Name == "" || r.Name == r.ID {
		return r.ID
	}
	return fmt.Sprintf("%s (%s)", r.Name, r.ID)
}
