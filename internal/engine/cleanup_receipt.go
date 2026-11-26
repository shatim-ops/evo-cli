// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
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
	ReceiptID               string    `json:"receipt_id"`
	Kind                    string    `json:"kind"`
	ProjectID               string    `json:"project_id"`
	DeploymentID            string    `json:"deployment_id"`
	VMID                    string    `json:"vm_id,omitempty"`
	VMName                  string    `json:"vm_name,omitempty"`
	Name                    string    `json:"name,omitempty"`
	Attempt                 string    `json:"attempt,omitempty"`
	AddressID               string    `json:"address_id,omitempty"`
	RequestedAt             time.Time `json:"requested_at"`
	Address                 string    `json:"address,omitempty"`
	NeedsEvidenceProject    bool      `json:"needs_evidence_project,omitempty"`
	ConflictsWithResolution bool      `json:"conflicts_with_resolution,omitempty"`
	index                   int       // position of the exact lost request in the locked snapshot
	contradictedEvidence    []string  // private references: never include them in a listing or preview
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

// Last resolution for exactly this legacy receipt, including an outcome later
// contradicted by a binding. Older decisions are retained in the audit trail.
func legacyIPResolution(project string, d *state.DeploymentRecord) *state.FloatingIPResolution {
	if len(d.FloatingIPResolutions) == 0 {
		return nil
	}
	id := makeIPReceipt(project, d, nil, -1).ReceiptID
	for i := len(d.FloatingIPResolutions) - 1; i >= 0; i-- {
		r := &d.FloatingIPResolutions[i]
		if r.Kind == "legacy_deployment" && r.ReceiptID == id {
			return r
		}
	}
	return nil
}

func legacyReceiptResolved(project string, d *state.DeploymentRecord) bool {
	r := legacyIPResolution(project, d)
	return r != nil && !r.Contradicted
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
	project := d.CleanupProjectID
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
			// An addressed destroy also checks a copy of the selected record.
			// Count that record once, by its stored identity, not its address.
			found := false
			for i := range sf.Deployments {
				other := &sf.Deployments[i]
				if other.VMName != m.VMName || other.ProjectID != "" && other.ProjectID != m.ProjectID {
					continue
				}
				if found || d.ID == "" || other.ID != d.ID || other.VMID != d.VMID {
					return "", false
				}
				found = true
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

func (e *Engine) chooseCleanupDeployment(sf *state.File) error {
	// A duplicate ID cannot select a single record, even when only one of
	// those records belongs to the current project.
	counts := make(map[string]int)
	for _, d := range sf.Deployments {
		if d.Status == "destroying" {
			counts[d.ID]++
		}
	}
	var choices []string
	for i := range sf.Deployments {
		d := &sf.Deployments[i]
		if d.Status != "destroying" || d.ID == "" || counts[d.ID] != 1 {
			continue
		}
		if e.checkCleanupOwner(sf, d) == nil {
			choices = append(choices, i18n.Tf("%q (VM %q)", "%q (ВМ %q)", d.ID, d.VMID))
		}
	}
	if len(choices) == 0 {
		return clerrors.New(clerrors.CodeUsage, i18n.T("No pending deployment can be selected by ID with this profile.", "В этом профиле нельзя выбрать незавершённое развёртывание по ID."),
			i18n.T("Select the project and profile that own the receipt, then run 'evo cleanup receipts'.", "Выберите проект и профиль нужной квитанции и выполните 'evo cleanup receipts'."))
	}
	return clerrors.New(clerrors.CodeUsage,
		i18n.T("Select a local deployment to list its receipts.", "Выберите локальное развёртывание для просмотра его квитанций."),
		i18n.Tf("Use 'evo cleanup receipts --deployment-id <ID>' for the matching deployment. Available deployment IDs and VMs: %s.",
			"Выполните 'evo cleanup receipts --deployment-id <ID>' для нужного развёртывания. Доступные ID развёртываний и ВМ: %s.", strings.Join(choices, ", ")))
}

func (e *Engine) cleanupDeployment(sf *state.File, id string) (*state.DeploymentRecord, error) {
	var rec *state.DeploymentRecord
	for i := range sf.Deployments {
		d := &sf.Deployments[i]
		if id != "" && d.ID != id || d.Status != "destroying" {
			continue
		}
		if id == "" && (rec != nil || d.ID == "") {
			return nil, e.chooseCleanupDeployment(sf)
		}
		if rec != nil || d.ID == "" {
			return nil, ambiguousIPReceipt()
		}
		rec = d
	}
	if rec == nil {
		return nil, noIPReceipt()
	}
	if err := e.checkCleanupOwner(sf, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func (e *Engine) checkCleanupOwner(sf *state.File, rec *state.DeploymentRecord) error {
	if err := e.CheckFloatingIPReceiptProject(sf, rec); err != nil {
		return err
	}
	project, _ := recordedReceiptProject(sf, rec)
	if e.Config.ProjectID == "" || project != "" && project != e.Config.ProjectID || rec.CreatedBy.Other(e.Maker()) {
		return clerrors.New(clerrors.CodeConfigInvalid, i18n.T("Select the project and profile that own this receipt.", "Выберите проект и профиль этой квитанции."), "")
	}
	return nil
}

// HasLegacyFloatingIPResolution reports whether a previous decision about this
// exact deployment must be checked before its local receipt can be retired.
func (e *Engine) HasLegacyFloatingIPResolution(d *state.DeploymentRecord) bool {
	return legacyIPResolution(e.projectOf(d), d) != nil
}

// HasUnresolvedLegacyFloatingIP reports a remaining old address receipt,
// including after the previous destroy found only an ambiguous namesake.
func (e *Engine) HasUnresolvedLegacyFloatingIP(d *state.DeploymentRecord) bool {
	return d.FloatingIPID == "" && (d.LegacyFloatingIPPending || recordedExternalIP(d.VMIP) && !legacyReceiptResolved(e.projectOf(d), d))
}

func (e *Engine) receipts(sf *state.File, d *state.DeploymentRecord) ([]FloatingIPReceipt, error) {
	project := e.projectOf(d)
	out := []FloatingIPReceipt{}
	if e.HasUnresolvedLegacyFloatingIP(d) {
		r := makeIPReceipt(project, d, nil, -1)
		recorded, _ := recordedReceiptProject(sf, d)
		r.NeedsEvidenceProject = recorded == ""
		if resolution := legacyIPResolution(project, d); resolution != nil && (resolution.Contradicted || d.LegacyFloatingIPPending) {
			r.ConflictsWithResolution = true
			r.AddressID = resolution.ConflictingAddressID
			for i := range d.FloatingIPResolutions {
				past := &d.FloatingIPResolutions[i]
				if past.Kind == r.Kind && past.ReceiptID == r.ReceiptID && (past.Contradicted || past == resolution) {
					r.contradictedEvidence = append(r.contradictedEvidence, strings.TrimSpace(past.Evidence))
				}
			}
		}
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

// A labelled case number (for example, "provider case 123" or "OPS-123")
// and a UUID retain their identity when the letters change case.
var numberedEvidence = regexp.MustCompile(`^[\p{L}]+([ _-][\p{L}]+)*([_-]| #?)[0-9]+$`)
var escapedOctet = regexp.MustCompile(`%[[:xdigit:]]{2}`)

func sameEvidenceReference(ref, previous string) bool {
	if ref == previous {
		return true
	}
	if !strings.EqualFold(ref, previous) {
		return false
	}
	if isUUID(ref) || numberedEvidence.MatchString(ref) {
		return true
	}
	return evidenceLinkKey(ref) == evidenceLinkKey(previous)
}

func evidenceLinkKey(ref string) string {
	u, err := url.Parse(ref)
	if err != nil || u.Scheme == "" && !strings.ContainsAny(ref, "/?#") {
		return ref
	}
	// Parse already folds the scheme. Only the hostname and the hex digits
	// of escapes fold, not an IPv6 zone, user, path, query or fragment.
	host, zone, scoped := strings.Cut(u.Host, "%")
	u.Host = strings.ToLower(host)
	if scoped {
		u.Host += "%" + zone
	}
	return escapedOctet.ReplaceAllStringFunc(u.String(), strings.ToUpper)
}

// CheckEvidence rejects a receipt's own identifiers and list output before
// consent and again against the receipt selected under the state lock. The
// human verifies the independent record itself.
func (req FloatingIPReconciliation) CheckEvidence(r *FloatingIPReceipt) error {
	ref := strings.TrimSpace(req.Evidence)
	if slices.ContainsFunc(r.contradictedEvidence, func(past string) bool { return sameEvidenceReference(ref, past) }) {
		return clerrors.New(clerrors.CodeUsage,
			i18n.T("This reference has already been contradicted for this receipt.", "Это свидетельство для данной квитанции уже опровергнуто."),
			i18n.T("Check the original operation again and supply a new independent reference.", "Снова проверьте результат операции и укажите новое независимое свидетельство."))
	}
	if r.Kind == "legacy_deployment" && provision.IsEvoFIPOf(ref, r.VMID, r.VMName) {
		return invalidIPProof()
	}
	for _, same := range []string{r.Name, r.VMID, r.VMName, r.ProjectID, r.ReceiptID, r.Attempt, r.Address, r.AddressID, r.DeploymentID, req.AddressID, "receipt_id", "null", "none", "empty", "not_created", "released"} {
		if strings.EqualFold(ref, same) {
			return invalidIPProof()
		}
	}
	if strings.IndexFunc(ref, func(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) }) < 0 || strings.HasPrefix(ref, "[") || strings.HasPrefix(ref, "{") {
		return invalidIPProof()
	}
	return nil
}

func contradictoryIPProof() error {
	return clerrors.New(clerrors.CodeAPIError, i18n.T("The address evidence conflicts with the reported outcome. The receipt is still pending.", "Данные об адресе противоречат указанному исходу. Квитанция остаётся незакрытой."), "")
}

func listsReleasedAddress(ips []*provision.FloatingIP, resolution *state.FloatingIPResolution) bool {
	return resolution != nil && resolution.Outcome == "released" && resolution.AddressID != "" && slices.ContainsFunc(ips, func(ip *provision.FloatingIP) bool {
		return ip != nil && ip.ID == resolution.AddressID
	})
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
	if req.Outcome == "released" {
		card, err := e.Cloud.GetFloatingIP(ctx, req.AddressID)
		if err == nil {
			if card == nil || card.ID != req.AddressID {
				return clerrors.New(clerrors.CodeAPIError,
					i18n.T("The service did not identify the requested address. Its release is unconfirmed.", "Сервис не вернул карточку с указанным ID адреса. Освобождение не подтверждено."),
					i18n.T("The receipt is still pending. Check the original operation.", "Квитанция остаётся незакрытой. Сверьте исходную операцию."))
			}
			return clerrors.New(clerrors.CodeAPIError,
				i18n.T("The service still returns the address by its ID. Its release cannot be recorded.", "Сервис всё ещё возвращает адрес по его ID. Записать освобождение адреса нельзя."),
				i18n.T("The receipt is still pending. Check the original operation.", "Квитанция остаётся незакрытой. Сверьте исходную операцию."))
		}
		if !floatingip.ServiceNotFound(err) {
			return err
		}
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
		resolution := legacyIPResolution(e.projectOf(d), d)
		proved, _, ambiguous := legacyBinding(d, vm, ips, nil, resolution)
		if proved != nil || ambiguous || listsReleasedAddress(ips, resolution) {
			return contradictoryIPProof()
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

type ipReceiptSelection struct {
	deployment *state.DeploymentRecord
	receipt    *FloatingIPReceipt
	mark       *state.LostFloatingIP
}

func (e *Engine) selectIPReceipt(sf *state.File, req FloatingIPReconciliation) (*ipReceiptSelection, error) {
	d, err := e.cleanupDeployment(sf, req.DeploymentID)
	if err != nil {
		return nil, err
	}
	var selected *FloatingIPReceipt
	receipts, err := e.receipts(sf, d)
	if err != nil {
		return nil, err
	}
	for _, r := range receipts {
		if r.ReceiptID != req.ReceiptID {
			continue
		}
		if selected != nil {
			return nil, ambiguousIPReceipt()
		}
		selected = &r
	}
	if selected == nil {
		return nil, noIPReceipt()
	}
	if err := req.CheckEvidence(selected); err != nil {
		return nil, err
	}
	if (selected.NeedsEvidenceProject || req.EvidenceProjectID != "") && req.EvidenceProjectID != selected.ProjectID {
		return nil, clerrors.New(clerrors.CodeConfigInvalid, i18n.T("Confirm the exact project from the independent record and select that project.", "Подтвердите точный проект из независимой записи и выберите этот проект."), "")
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
				return nil, ambiguousIPReceipt()
			}
		}
	}
	return &ipReceiptSelection{deployment: d, receipt: selected, mark: mark}, nil
}

// PreviewFloatingIPReconciliation checks local evidence first. A released
// address must also have a confirmed service-level absence before consent.
func (e *Engine) PreviewFloatingIPReconciliation(ctx context.Context, req FloatingIPReconciliation) (*FloatingIPReceipt, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		return nil, err
	}
	sel, err := e.selectIPReceipt(sf, req)
	if err != nil {
		return nil, err
	}
	if sel.receipt.AddressID != "" && sel.receipt.AddressID != req.AddressID {
		return nil, clerrors.New(clerrors.CodeUsage, i18n.T("The supplied outcome or address ID conflicts with the receipt.",
			"Указанный исход или ID адреса расходится с квитанцией."), i18n.T("Check the receipt and the independent evidence.", "Сверьте квитанцию и независимое свидетельство."))
	}
	if req.Outcome == "released" {
		if err := e.checkIPProof(ctx, sel.deployment, sel.mark, req); err != nil {
			return nil, err
		}
	}
	return sel.receipt, nil
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
	sel, err := e.selectIPReceipt(sf, req)
	if err != nil {
		return err
	}
	d, selected, mark := sel.deployment, sel.receipt, sel.mark
	if selected.ConflictsWithResolution && selected.AddressID != "" && (req.Outcome != "released" || req.AddressID != selected.AddressID) {
		return contradictoryIPProof()
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
