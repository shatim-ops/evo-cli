// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/state"
)

type upgradeReceipt struct {
	Project, Subject, Tool, Resource, Parent, From, Target, Operation, Topology string
	PreviewID                                                                   string `json:"preview_id,omitempty"`
	// RejectionID in a receipt proves the latest submission was refused. In an
	// offer it binds approval to the latest refusal observed during planning.
	RejectionID string `json:"rejection_id,omitempty"`
}

type upgradeCard struct {
	version, status, parent, parentName, phase, topology string
	available                                            []string
	busy                                                 bool
}

func upgradeMismatch() error {
	return usageErr("The resource, its parent, status or version does not match this upgrade. Read the current cluster and node pool cards.", "Ресурс, родитель, состояние или версия не соответствуют обновлению. Прочитайте текущие карточки кластера и группы узлов.")
}

func upgradeEvidenceError() error {
	return clierrors.New(clierrors.CodeAPIError, i18n.T("The operation does not identify the expected upgrade resource.", "Операция не подтверждает обновление ожидаемого ресурса."), i18n.T("Read the original operation and resource in the same project before taking any further action.", "Проверьте исходную операцию и ресурс в том же проекте перед дальнейшими действиями."))
}

func upgradeOutcome(r upgradeReceipt, card upgradeCard, status string) (string, bool) {
	out := upgradeResult(r, card, status)
	switch status {
	case "outcome_unknown":
		return errAnswer(clierrors.New(clierrors.CodeAPIError, i18n.T("The original upgrade has no verified operation ID.", "Для исходного обновления нет подтверждённого ID операции."), i18n.T("Read the resource in the same project. Its current version alone cannot identify who made the change.", "Прочитайте карточку ресурса в том же проекте. Одна текущая версия не подтверждает, кто выполнил изменение.")), out), true
	case "operation_failed":
		return errAnswer(clierrors.New(clierrors.CodeAPIError, i18n.T("The upgrade operation reported an error, cancellation or rollback.", "Операция обновления сообщила об ошибке, отмене или откате."), i18n.T("Inspect the operation and the resource before deciding what to do next.", "Проверьте операцию и ресурс перед выбором дальнейшего действия.")), out), true
	}
	return toJSON(out), false
}

func readUpgradeCard(ctx context.Context, cli *mk8s.Client, request upgradeReceipt) (upgradeCard, error) {
	var card upgradeCard
	var data map[string]any
	var err error
	pool := request.Tool == "k8s_nodepool_upgrade"
	if pool {
		data, err = cli.GetNodePool(ctx, request.Resource)
	} else {
		data, err = cli.GetCluster(ctx, request.Resource)
	}
	if err != nil {
		return card, err
	}
	if stringArg(data, "id") != request.Resource {
		return card, upgradeMismatch()
	}
	source, _ := data["controlPlane"].(map[string]any)
	if pool {
		source = data
		card.parent = stringArg(data, "clusterId")
		if !isUUID(card.parent) {
			return card, upgradeMismatch()
		}
		if project, ok := data["projectId"]; ok && project != request.Project {
			return card, upgradeMismatch()
		}
		parent, err := cli.GetCluster(ctx, card.parent)
		if err != nil {
			return card, err
		}
		card.parentName = stringArg(parent, "name")
		cp, _ := parent["controlPlane"].(map[string]any)
		if stringArg(parent, "id") != card.parent || stringArg(parent, "projectId") != request.Project ||
			stringArg(parent, "status") != "OBJECT_STATUS_RUNNING" || stringArg(cp, "version") != request.Target || cp["upgradeInfo"] != nil {
			return card, upgradeMismatch()
		}
	} else if stringArg(data, "projectId") != request.Project {
		return card, upgradeMismatch()
	}
	// Bind the displayed resource to its configuration without volatile timestamps.
	shape := []any{source["count"], source["zones"], source["machineConfiguration"], data["scalePolicy"]}
	encoded, err := json.Marshal(shape)
	if err != nil {
		return card, err
	}
	card.topology = fmt.Sprintf("%x", sha256.Sum256(encoded))
	card.version = stringArg(source, "version")
	card.status = stringArg(data, "status")
	card.busy = source["upgradeInfo"] != nil
	if info, ok := source["upgradeInfo"].(map[string]any); ok {
		card.phase = stringArg(info, "phase")
	}
	if card.version == "" || card.status == "" {
		return card, upgradeMismatch()
	}
	upgrade, _ := data["versionUpgrade"].(map[string]any)
	if allowed, _ := upgrade["upgradeAvailable"].(bool); allowed {
		card.available = stringSliceArg(upgrade, "availableVersions")
	}
	return card, nil
}

func (r upgradeReceipt) path(dir string) string {
	key := sha256.Sum256([]byte(r.Project + "\x00" + r.Tool + "\x00" + r.Resource))
	return filepath.Join(dir, fmt.Sprintf("%x.json", key))
}

func readUpgradeReceipt(path string) (*upgradeReceipt, error) {
	data, err := config.ReadFileNoFollow(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r upgradeReceipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// An existing local submission for this control plane must also have finished.
// A RUNNING card alone cannot settle a lost or failed operation.
func verifyUpgradeParent(ctx context.Context, client *mk8s.Client, dir string, r upgradeReceipt, card upgradeCard) error {
	if card.parent == "" {
		return nil
	}
	parent := upgradeReceipt{Project: r.Project, Tool: "k8s_upgrade", Resource: card.parent}
	prior, err := readUpgradeReceipt(parent.path(dir))
	if err != nil || prior == nil {
		return err
	}
	if prior.Project != parent.Project || prior.Tool != parent.Tool || prior.Resource != parent.Resource {
		return upgradeMismatch()
	}
	if prior.rejected() {
		return nil // The parent card was checked against the target above.
	}
	if prior.Target != r.Target || !isUUID(prior.Operation) {
		return upgradeMismatch()
	}
	op, err := client.GetOperation(ctx, prior.Operation)
	if err != nil {
		return err
	}
	if !op.Done || op.ID != prior.Operation || op.ResourceID != parent.Resource || (len(op.Error) > 0 && string(op.Error) != "null") {
		return upgradeMismatch()
	}
	current, err := readUpgradeCard(ctx, client, parent)
	if err != nil {
		return err
	}
	if current.version != r.Target || current.busy || current.status != "OBJECT_STATUS_RUNNING" {
		return upgradeMismatch()
	}
	return nil
}

func (r upgradeReceipt) rejected() bool {
	return r.Operation == "" && isUUID(r.RejectionID)
}

func (r upgradeReceipt) save(path string) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return config.WriteDurableReceipt(path, data)
}

// The client can return this capability only after receiving the preview.
func (r upgradeReceipt) offer(path string, mcpCall bool, preview map[string]any) error {
	if mcpCall {
		id, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		r.PreviewID = id.String()
		preview["preview_id"] = r.PreviewID
		preview["approval"] = i18n.T("Show this preview to the person. Only after their explicit approval, return its preview_id with confirmed=true in this session.", "Покажите это превью человеку. Только после его явного согласия передайте preview_id этого превью вместе с confirmed=true в текущей сессии.")
	}
	return r.save(path)
}

func upgradeResult(r upgradeReceipt, card upgradeCard, status string) map[string]any {
	out := map[string]any{"status": status, "resource_id": r.Resource, "target_version": r.Target, "current_version": card.version, "resource_status": card.status}
	if card.phase != "" {
		out["upgrade_phase"] = card.phase
	}
	if card.parent != "" {
		out["cluster_id"] = card.parent
		if card.parentName != "" {
			out["cluster_name"] = card.parentName
		}
	}
	if r.Operation != "" {
		out["operation_id"] = r.Operation
	}
	switch status {
	case "submitted", "upgrading", "version_unverified", "outcome_unknown":
		idArg, read := "cluster_id", "k8s_get"
		if r.Tool == "k8s_nodepool_upgrade" {
			idArg, read = "nodepool_id", "k8s_nodepool_get"
		}
		if r.Operation == "" {
			nextStep(out, read, map[string]any{idArg: r.Resource})
			out["next"] = i18n.T("Read the current resource card in the same project.", "Прочитайте текущую карточку ресурса в том же проекте.")
		} else {
			nextStep(out, "k8s_operation", map[string]any{"operation_id": r.Operation})
			out["next"] = i18n.T("Read this upgrade again to check its operation and the actual resource version.", "Повторите чтение этого обновления для проверки операции и фактической версии ресурса.")
		}
	}
	return out
}

func (s *Registry) planUpgrade(ctx context.Context, args map[string]any, tool, idArg string) (*Plan, error) {
	client, project, subject, err := s.k8sClient(ctx)
	if err != nil {
		return nil, err
	}
	if project == "" {
		return nil, clierrors.ProjectRequired()
	}
	eng, err := s.localEngine(ctx, nil)
	if err != nil {
		return nil, err
	}
	if subject == "" {
		subject = eng.Config.ServiceAccountID
	}
	if subject == "" && eng.Config.KeyID != "" {
		subject = fmt.Sprintf("key-%x", sha256.Sum256([]byte(eng.Config.KeyID)))
	}
	if subject == "" {
		return nil, usageErr("Cannot identify the caller of the upgrade.", "Не удалось определить субъекта обновления.")
	}
	r := upgradeReceipt{Project: project, Subject: subject, Tool: tool, Resource: stringArg(args, idArg), Target: stringArg(args, "version")}
	if !isUUID(r.Resource) || r.Target == "" {
		return nil, upgradeMismatch()
	}
	root := config.Dir()
	if root == "" {
		return nil, errNoStateDir()
	}
	dir := filepath.Join(root, "k8s-upgrades")
	path := r.path(dir)
	// Serialize the parent check and submission against other local upgrades in this project.
	lock := filepath.Join(dir, fmt.Sprintf("%x.lock", sha256.Sum256([]byte(project))))
	check := func(ctx context.Context) (upgradeCard, *Plan, error) {
		card, err := readUpgradeCard(ctx, client, r)
		if err != nil {
			return card, nil, err
		}
		if err := verifyUpgradeParent(ctx, client, dir, r, card); err != nil {
			return card, nil, err
		}
		prior, err := readUpgradeReceipt(path)
		if err != nil {
			return card, nil, err
		}
		if prior != nil && prior.rejected() {
			if prior.Project != r.Project || prior.Tool != tool || prior.Resource != r.Resource {
				return card, nil, upgradeMismatch()
			}
			r.RejectionID = prior.RejectionID
		} else if prior != nil {
			if prior.Project != r.Project || prior.Resource != r.Resource || prior.Tool != tool || prior.Subject != subject || prior.Parent != card.parent || prior.Target == "" {
				return card, nil, upgradeMismatch()
			}
			status := "outcome_unknown"
			if prior.Operation != "" {
				if !isUUID(prior.Operation) {
					return card, nil, upgradeMismatch()
				}
				op, err := client.GetOperation(ctx, prior.Operation)
				if err != nil {
					return card, nil, err
				}
				if op.ID != prior.Operation || op.ResourceID != prior.Resource {
					return card, nil, upgradeEvidenceError()
				}
				if op.Done {
					card, err = readUpgradeCard(ctx, client, r)
					if err != nil {
						return card, nil, err
					}
					if prior.Parent != card.parent {
						return card, nil, upgradeMismatch()
					}
				}
				status = "upgrading"
				switch {
				case strings.HasSuffix(card.phase, "_CANCELLED") || strings.HasSuffix(card.phase, "_ROLLBACK"):
					status = "operation_failed"
				case len(op.Error) > 0 && string(op.Error) != "null":
					status = "operation_failed"
				case op.Done && !card.busy && card.status == "OBJECT_STATUS_RUNNING" && card.version == prior.Target:
					status = "upgraded"
				case op.Done:
					status = "version_unverified"
				}
			}
			if prior.Target == r.Target {
				text, isErr := upgradeOutcome(*prior, card, status)
				return card, answered(text, isErr), nil
			}
			if status != "upgraded" {
				return card, nil, upgradeMismatch()
			}
		}
		if card.busy || card.status != "OBJECT_STATUS_RUNNING" {
			return card, nil, upgradeMismatch()
		}
		if card.version == r.Target {
			return card, answered(toJSON(upgradeResult(r, card, "already_upgraded")), false), nil
		}
		newer, known := mk8s.CheckKubeVersion(">"+card.version, r.Target)
		if !known || !newer || !slices.Contains(card.available, r.Target) {
			return card, nil, upgradeMismatch()
		}
		return card, nil, nil
	}
	card, result, err := check(ctx)
	if err != nil || result != nil {
		return result, err
	}
	r.From, r.Parent, r.Topology = card.version, card.parent, card.topology
	preview := upgradeResult(r, card, "confirmation_required")
	preview["project_id"] = project
	preview["price_known"] = false
	preview["message"] = i18n.T("Upgrade only the control plane. Cluster API availability may be interrupted. The request cannot be withdrawn after submission. Node pools need separate approval.", "Обновить только управляющую часть кластера. Возможны перерывы в доступе к API. Отозвать отправленный запрос нельзя. Группы узлов требуют отдельного согласия.")
	if tool == "k8s_nodepool_upgrade" {
		preview["message"] = i18n.T("Upgrade this node pool. Workloads may be interrupted while nodes are updated. The request cannot be withdrawn after submission.", "Обновить эту группу узлов. Во время обновления узлов возможны перерывы в работе приложений. Отозвать отправленный запрос нельзя.")
	}
	offerDir := filepath.Join(dir, "previews")
	scope := "stdio-" + s.upgradeClient
	session, httpCall := ctx.Value(networkSessionKey{}).(string)
	caller := callerOf(ctx)
	if httpCall && session == "" {
		return nil, clierrors.New(clierrors.CodeConfirmationRequired,
			i18n.T("An initialized MCP session is required to approve this upgrade.", "Для согласования обновления нужна открытая сессия MCP."),
			i18n.T("Call initialize, include the returned Mcp-Session-Id in the HTTP header, then request the preview without confirmation in that session.", "Вызовите initialize, передайте полученный Mcp-Session-Id в заголовке HTTP и запросите превью без подтверждения в этой сессии."))
	}
	if httpCall {
		scope = "http-" + session
	} else if caller != nil {
		scope = "cli"
	}
	offerPath := filepath.Join(offerDir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(scope))))
	return &Plan{PreviewOf: func(ctx context.Context) (map[string]any, error) {
		if caller != nil {
			offer := r
			caller.upgradePreview = &offer
		}
		unlock, err := state.FileLock(offerPath+".lock", 5*time.Second)
		if err != nil {
			return nil, err
		}
		defer unlock()
		if caller != nil && caller.AutomaticPreview {
			shown, err := readUpgradeReceipt(offerPath)
			if err != nil {
				return nil, err
			}
			if shown == nil || *shown == r {
				return preview, nil
			}
			caller.RequireNewApproval = true
		}
		if err := r.offer(offerPath, caller == nil, preview); err != nil {
			return nil, err
		}
		return preview, nil
	}, Execute: func(ctx context.Context) (string, bool) {
		unlock, err := state.FileLock(lock, 5*time.Second)
		if err != nil {
			return errResult(err), true
		}
		defer unlock()
		current, prior, err := check(ctx)
		if err != nil {
			return errResult(err), true
		}
		if prior != nil {
			return prior.answer, prior.answerErr
		}
		cardChanged := current.version != r.From || current.parent != r.Parent || current.topology != r.Topology
		if cardChanged {
			r.From, r.Parent, r.Topology = current.version, current.parent, current.topology
			preview["current_version"] = current.version
			if current.parent != "" {
				preview["cluster_id"] = current.parent
				preview["cluster_name"] = current.parentName
			}
		}
		contextChanged := cardChanged || caller != nil && caller.upgradePreview != nil && *caller.upgradePreview != r
		allowed, err := reserveUpgrade(ctx, offerPath, path, caller == nil, contextChanged, r, stringArg(args, "preview_id"), preview)
		if err != nil {
			return errResult(err), true
		}
		if !allowed {
			return toJSON(preview), true
		}
		r.RejectionID = "" // This attempt is now reserved; no outcome is known.
		var op *mk8s.Operation
		if tool == "k8s_nodepool_upgrade" {
			op, err = client.UpgradeNodePool(ctx, r.Resource, r.Target)
		} else {
			op, err = client.UpgradeCluster(ctx, r.Resource, r.Target)
		}
		if err != nil {
			return r.submissionError(path, current, err)
		}
		if !isUUID(op.ID) || op.ResourceID != r.Resource {
			return errAnswer(upgradeEvidenceError(), upgradeResult(r, current, "outcome_unknown")), true
		}
		r.Operation = op.ID
		if err := r.save(path); err != nil {
			return errResult(err), true
		}
		if len(op.Error) > 0 && string(op.Error) != "null" {
			return upgradeOutcome(r, current, "operation_failed")
		}
		return toJSON(upgradeResult(r, current, "submitted")), false
	}}, nil
}

func (s *Registry) planK8sUpgrade(ctx context.Context, args map[string]any) (*Plan, error) {
	return s.planUpgrade(ctx, args, "k8s_upgrade", "cluster_id")
}
func (s *Registry) planK8sNodePoolUpgrade(ctx context.Context, args map[string]any) (*Plan, error) {
	return s.planUpgrade(ctx, args, "k8s_nodepool_upgrade", "nodepool_id")
}
