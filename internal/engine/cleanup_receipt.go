// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/shatim-ops/evo-cli/internal/clock"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/floatingip"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// FloatingIPReceipt identifies one attempt in the current directory. Its digest
// binds the preview to the deployment, original request and available evidence.
type FloatingIPReceipt struct {
	ReceiptID            string    `json:"receipt_id"`
	Kind                 string    `json:"kind"`
	ProjectID            string    `json:"project_id"`
	DeploymentID         string    `json:"deployment_id"`
	VMID                 string    `json:"vm_id,omitempty"`
	VMName               string    `json:"vm_name,omitempty"`
	Name                 string    `json:"name,omitempty"`
	Attempt              string    `json:"attempt,omitempty"`
	AddressID            string    `json:"address_id,omitempty"`
	RequestedAt          time.Time `json:"requested_at"`
	Address              string    `json:"address,omitempty"`
	NeedsEvidenceProject bool      `json:"needs_evidence_project,omitempty"`
	index                int       // position of the exact lost request in the locked snapshot
}

type FloatingIPReconciliation struct {
	DeploymentID, ReceiptID, Outcome, AddressID, Evidence, EvidenceProjectID string
}

func makeIPReceipt(project string, d *state.DeploymentRecord, m *state.LostFloatingIP, index int) FloatingIPReceipt {
	r := FloatingIPReceipt{Kind: "legacy_deployment", ProjectID: project, DeploymentID: d.ID, VMID: d.VMID, VMName: d.VMName, RequestedAt: d.CreatedAt, Address: d.VMIP, index: index}
	if m != nil {
		r.Kind, r.Name, r.Attempt, r.AddressID, r.RequestedAt, r.Address = "lost_request", m.Name, m.Attempt, m.ID, m.RequestedAt, ""
	}
	// Do not hash status timestamps, the order in the file, or the unrelated
	// "reserve another" answer. Do hash the original VM and maker on both
	// records, including when a before-VM request recorded only a VM name.
	var markOwner *state.Maker
	var markVM, markName string
	if m != nil {
		markOwner, markVM, markName = m.CreatedBy, m.VMID, m.VMName
	}
	b, _ := json.Marshal([]any{r, d.CreatedAt, d.CreatedBy, d.ProjectID, markOwner, markVM, markName})
	sum := sha256.Sum256(b)
	r.ReceiptID = hex.EncodeToString(sum[:])
	return r
}

func legacyReceiptResolved(project string, d *state.DeploymentRecord) bool {
	if len(d.FloatingIPResolutions) == 0 {
		return false
	}
	id := makeIPReceipt(project, d, nil, -1).ReceiptID
	return slices.ContainsFunc(d.FloatingIPResolutions, func(v state.FloatingIPResolution) bool { return v.Kind == "legacy_deployment" && v.ReceiptID == id })
}

func noIPReceipt() error {
	return clerrors.New(clerrors.CodeNotFound, i18n.T("No matching pending floating IP receipt.", "Подходящей незакрытой квитанции floating IP нет."), i18n.T("List the current receipts with 'evo cleanup receipts'.", "Посмотрите текущие квитанции командой 'evo cleanup receipts'."))
}

func ambiguousIPReceipt() error {
	return clerrors.New(clerrors.CodeUsage, i18n.T("The floating IP receipt cannot be assigned to exactly one request and deployment.", "Нельзя однозначно связать квитанцию floating IP с одной попыткой и одним развёртыванием."), "")
}

// A receipt tied to the exact VM can supply the project of an older
// deployment. A profile switch must not convert conflicting evidence into
// permission to retire the receipt.
func recordedReceiptProject(sf *state.File, d *state.DeploymentRecord) (string, bool) {
	if d.ProjectID != "" {
		return d.ProjectID, true
	}
	project := ""
	for _, r := range d.FloatingIPResolutions {
		if r.ProjectID == "" {
			continue
		}
		if project != "" && project != r.ProjectID {
			return "", false
		}
		project = r.ProjectID
	}
	for _, m := range sf.LostFloatingIPs {
		if m.ProjectID == "" {
			continue
		}
		if m.VMID != "" {
			if m.VMID != d.VMID {
				continue
			}
		} else {
			if m.VMName == "" || m.VMName != d.VMName {
				continue
			}
			for i := range sf.Deployments {
				other := &sf.Deployments[i]
				if other != d && other.VMName == m.VMName && (other.ProjectID == "" || other.ProjectID == m.ProjectID) {
					return "", false
				}
			}
		}
		if project != "" && project != m.ProjectID {
			return "", false
		}
		project = m.ProjectID
	}
	return project, true
}

// CheckFloatingIPReceiptProject prevents an ordinary destroy retry from
// losing old receipts when the active profile changes. Explicit projects on
// deployment records keep the established destroy semantics.
func (e *Engine) CheckFloatingIPReceiptProject(sf *state.File, d *state.DeploymentRecord) error {
	project, consistent := recordedReceiptProject(sf, d)
	if !consistent {
		return ambiguousIPReceipt()
	}
	if d.ProjectID == "" && project != "" && project != e.Config.ProjectID {
		return clerrors.New(clerrors.CodeConfigInvalid, i18n.T("Select the project recorded in the floating IP receipt.", "Выберите проект, записанный в квитанции floating IP."), "")
	}
	for _, m := range floatingip.Of(sf, e.projectOf(d), d.VMName, d.VMID) {
		if m.CreatedBy.Other(e.Maker()) {
			return clerrors.New(clerrors.CodeConfigInvalid, i18n.T("A pending floating IP request belongs to another profile. Select the original profile.", "Незакрытая заявка на floating IP принадлежит другому профилю. Выберите исходный профиль."), "")
		}
	}
	return nil
}

// FloatingIPReceiptHint points a human to the read-only preview in the same directory.
func FloatingIPReceiptHint() string {
	return i18n.T("Run 'evo cleanup receipts' here. Ask the person to verify the original operation and resolve that receipt with 'evo cleanup resolve'. Ask whether to finish cleanup with 'evo destroy'.",
		"Выполните здесь 'evo cleanup receipts'. Попросите пользователя сверить исходную операцию и закрыть эту квитанцию через 'evo cleanup resolve'. Спросите человека, завершить ли уборку через 'evo destroy'.")
}

func (e *Engine) cleanupDeployment(sf *state.File, id string) (*state.DeploymentRecord, error) {
	var rec *state.DeploymentRecord
	for i := range sf.Deployments {
		d := &sf.Deployments[i]
		if id != "" && d.ID != id || d.Status != "destroying" {
			continue
		}
		if rec != nil || d.ID == "" {
			return nil, ambiguousIPReceipt()
		}
		rec = d
	}
	if rec == nil {
		return nil, noIPReceipt()
	}
	if err := e.CheckFloatingIPReceiptProject(sf, rec); err != nil {
		return nil, err
	}
	project, _ := recordedReceiptProject(sf, rec)
	if e.Config.ProjectID == "" || project != "" && project != e.Config.ProjectID || rec.CreatedBy.Other(e.Maker()) {
		return nil, clerrors.New(clerrors.CodeConfigInvalid, i18n.T("Select the project and profile that own this receipt.", "Выберите проект и профиль этой квитанции."), "")
	}
	return rec, nil
}

// HasUnresolvedLegacyFloatingIP reports a remaining old address receipt,
// including after the previous destroy found only an ambiguous namesake.
func (e *Engine) HasUnresolvedLegacyFloatingIP(d *state.DeploymentRecord) bool {
	return d.FloatingIPID == "" && (d.LegacyFloatingIPPending || recordedExternalIP(d.VMIP)) && !legacyReceiptResolved(e.projectOf(d), d)
}

func (e *Engine) receipts(sf *state.File, d *state.DeploymentRecord) ([]FloatingIPReceipt, error) {
	project := e.projectOf(d)
	out := []FloatingIPReceipt{}
	if e.HasUnresolvedLegacyFloatingIP(d) {
		r := makeIPReceipt(project, d, nil, -1)
		recorded, _ := recordedReceiptProject(sf, d)
		r.NeedsEvidenceProject = recorded == ""
		out = append(out, r)
	}
	for i := range sf.LostFloatingIPs {
		m := &sf.LostFloatingIPs[i]
		if m.ProjectID != project {
			continue
		}
		if (m.VMID != "" && m.VMID == d.VMID) || (m.VMID == "" && m.VMName != "" && m.VMName == d.VMName) {

			out = append(out, makeIPReceipt(project, d, m, i))
		}
	}
	return out, nil
}

// PendingFloatingIPReceipts is local. A CLI or MCP preview cannot clear a
// receipt, or substitute a subsequent receipt with the same cloud name.
func (e *Engine) PendingFloatingIPReceipts(deploymentID string) ([]FloatingIPReceipt, error) {
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		return nil, err
	}
	rec, err := e.cleanupDeployment(sf, deploymentID)
	if err != nil {
		if deploymentID == "" && clerrors.Reason(err) == clerrors.CodeNotFound {
			return []FloatingIPReceipt{}, nil
		}
		return nil, err
	}
	return e.receipts(sf, rec)
}

func invalidIPProof() error {
	return clerrors.New(clerrors.CodeUsage, i18n.T("Use released with the address ID, or not_created without an address ID, and reference the independent record for this request.", "Выберите released с ID адреса или not_created без ID адреса и укажите независимую запись о результате этой попытки."), "")
}

// The human verifies the referenced record. Reject the target's own
// identifiers and empty/list output offered as independent evidence.
func independentIPReference(ref string, r *FloatingIPReceipt, ipID string) bool {
	if r.Kind == "legacy_deployment" && provision.IsEvoFIPOf(ref, r.VMID, r.VMName) {
		return false
	}
	for _, same := range []string{r.Name, r.VMID, r.VMName, r.ProjectID, r.ReceiptID, r.Attempt, r.Address, r.AddressID, r.DeploymentID, ipID, "null", "none", "empty", "not_created", "released"} {
		if strings.EqualFold(ref, same) {
			return false
		}
	}
	return strings.IndexFunc(ref, func(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) }) >= 0 && !strings.HasPrefix(ref, "[") && !strings.HasPrefix(ref, "{")
}

func contradictoryIPProof() error {
	return clerrors.New(clerrors.CodeAPIError, i18n.T("The address evidence conflicts with the reported outcome. The receipt is still pending.", "Данные об адресе противоречат указанному исходу. Квитанция остаётся незакрытой."), "")
}

func (e *Engine) checkIPProof(ctx context.Context, d *state.DeploymentRecord, m *state.LostFloatingIP, req FloatingIPReconciliation) error {
	if m != nil && m.ID != "" && (req.Outcome != "released" || req.AddressID != m.ID) {
		return contradictoryIPProof()
	}
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	e.Cloud.SetToken(tok.Value)
	// Read the project even before accepting an exact-ID 404: a key without
	// access to the project cannot distinguish a hidden object from a deletion.
	ips, err := e.Cloud.ListFloatingIPs(ctx, e.projectOf(d))
	if err != nil {
		return err
	}
	for _, f := range ips {
		if f == nil {
			return contradictoryIPProof()
		}
		// A unique marker in the cloud is positive evidence even if the
		// address was renamed, attached elsewhere, or duplicated in the list.
		if m != nil && m.Attempt == "" && f.Name == m.Name && d.VMID != "" {
			var bound struct{ ID string }
			if json.Unmarshal(f.VM, &bound) == nil && bound.ID == d.VMID {
				return contradictoryIPProof()
			}
		}
		if req.AddressID != "" && f.ID == req.AddressID || m != nil && m.Attempt != "" && f.Description == m.Attempt {
			return contradictoryIPProof()
		}
	}
	if m == nil {
		// For old records also check the VM's own address, as destroy does.
		var vm *provision.VM
		if d.VMID != "" {
			vm, err = e.Cloud.GetVM(ctx, d.VMID)
			if err != nil && !floatingip.ServiceNotFound(err) {
				return err
			}
			if err == nil && (vm == nil || vm.ID != d.VMID) {
				return contradictoryIPProof()
			}
		}
		proved, _, ambiguous := legacyBinding(d, vm, ips, nil)
		if proved != nil || ambiguous {
			return contradictoryIPProof()
		}
	}
	if req.Outcome == "released" {
		_, err := e.Cloud.GetFloatingIP(ctx, req.AddressID)
		if err == nil {
			return contradictoryIPProof()
		}
		if !floatingip.ServiceNotFound(err) {
			return err
		}
	}
	return ctx.Err()
}

// Validate checks the syntax that CLI previews and the locked action share.
func (req FloatingIPReconciliation) Validate() error {
	req.Evidence = strings.TrimSpace(req.Evidence)
	if req.Outcome == "released" && (!isUUID(req.AddressID) || req.AddressID == "00000000-0000-0000-0000-000000000000") {
		return invalidIPProof()
	}
	if req.DeploymentID == "" || req.ReceiptID == "" || req.Evidence == "" || len(req.Evidence) > 1024 || strings.IndexFunc(req.Evidence, unicode.IsControl) >= 0 ||
		(req.Outcome != "released" && req.Outcome != "not_created") || (req.Outcome == "released") != (strings.TrimSpace(req.AddressID) != "") || strings.TrimSpace(req.AddressID) != req.AddressID {
		return invalidIPProof()
	}
	return nil
}

// ReconcileFloatingIP runs after the CLI consent gate. It rechecks and updates
// one receipt under the same state lock as destroy. The next destroy handles
// retirement, the VM and any remaining resources.
func (e *Engine) ReconcileFloatingIP(ctx context.Context, req FloatingIPReconciliation) error {
	req.Evidence = strings.TrimSpace(req.Evidence)
	if err := req.Validate(); err != nil {
		return err
	}
	path := state.DefaultPath(e.StateDir)
	unlock, err := state.Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	sf, err := state.Read(path)
	if err != nil {
		return err
	}
	d, err := e.cleanupDeployment(sf, req.DeploymentID)
	if err != nil {
		return err
	}
	var selected *FloatingIPReceipt
	receipts, err := e.receipts(sf, d)
	if err != nil {
		return err
	}
	for _, r := range receipts {
		if r.ReceiptID != req.ReceiptID {
			continue
		}
		if selected != nil {
			return ambiguousIPReceipt()
		}
		selected = &r
	}
	if selected == nil {
		return noIPReceipt()
	}
	if !independentIPReference(req.Evidence, selected, req.AddressID) {
		return invalidIPProof()
	}
	if (selected.NeedsEvidenceProject || req.EvidenceProjectID != "") && req.EvidenceProjectID != selected.ProjectID {
		return clerrors.New(clerrors.CodeConfigInvalid, i18n.T("Confirm the exact project from the independent record and select that project.", "Подтвердите точный проект из независимой записи и выберите этот проект."), "")
	}
	var mark *state.LostFloatingIP
	if selected.index >= 0 {
		mark = &sf.LostFloatingIPs[selected.index]
		// A pre-VM mark can match multiple historical deployments by name.
		// Neither a caller's choice of deployment nor a 404 resolves that tie.
		for i := range sf.Deployments {
			other := &sf.Deployments[i]
			if other == d || e.projectOf(other) != selected.ProjectID {
				continue
			}
			if (mark.VMID != "" && other.VMID == mark.VMID) || (mark.VMID == "" && other.VMName == mark.VMName) {
				return ambiguousIPReceipt()
			}
		}
	}
	if err := e.checkIPProof(ctx, d, mark, req); err != nil {
		return err
	}
	if mark != nil {
		sf.LostFloatingIPs = slices.Delete(sf.LostFloatingIPs, selected.index, selected.index+1)
	} else {
		d.LegacyFloatingIPPending = false
	}
	d.FloatingIPResolutions = append(d.FloatingIPResolutions, state.FloatingIPResolution{ReceiptID: selected.ReceiptID, ProjectID: selected.ProjectID, Kind: selected.Kind, Name: selected.Name, Outcome: req.Outcome, AddressID: req.AddressID, Evidence: req.Evidence, At: clock.Now()})
	return state.Write(path, sf)
}
