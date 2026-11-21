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

	"github.com/shatim-ops/evo-cli/internal/config"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
	"github.com/shatim-ops/evo-cli/internal/state"
)

type upgradeReceipt struct {
	Project, Subject, Tool, Resource, Parent, From, Target, Operation string
}

type upgradeCard struct {
	version, status, parent, phase string
	available                      []string
	busy                           bool
}

func upgradeMismatch() error {
	return usageErr("The resource, its parent, status or version does not match this upgrade. Read the current cluster and node pool cards.", "Ресурс, родитель, состояние или версия не соответствуют обновлению. Прочитайте текущие карточки кластера и группы узлов.")
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
		cp, _ := parent["controlPlane"].(map[string]any)
		if stringArg(parent, "id") != card.parent || stringArg(parent, "projectId") != request.Project ||
			stringArg(parent, "status") != "OBJECT_STATUS_RUNNING" || stringArg(cp, "version") != request.Target || cp["upgradeInfo"] != nil {
			return card, upgradeMismatch()
		}
	} else if stringArg(data, "projectId") != request.Project {
		return card, upgradeMismatch()
	}
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
	if prior.Project != parent.Project || prior.Tool != parent.Tool || prior.Resource != parent.Resource || prior.Target != r.Target || !isUUID(prior.Operation) {
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

func (r upgradeReceipt) save(path string) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, data, 0600)
}

func upgradeResult(r upgradeReceipt, card upgradeCard, status string) map[string]any {
	out := map[string]any{"status": status, "resource_id": r.Resource, "target_version": r.Target, "current_version": card.version, "resource_status": card.status}
	if card.phase != "" {
		out["upgrade_phase"] = card.phase
	}
	if card.parent != "" {
		out["cluster_id"] = card.parent
	}
	if r.Operation != "" {
		out["operation_id"] = r.Operation
	}
	switch status {
	case "submitted", "upgrading", "version_unverified", "outcome_unknown":
		idArg := "cluster_id"
		if r.Tool == "k8s_nodepool_upgrade" {
			idArg = "nodepool_id"
		}
		nextStep(out, r.Tool, map[string]any{idArg: r.Resource, "version": r.Target})
		out["next"] = i18n.T("Read this upgrade again to check its operation and the actual resource version.", "Повторите чтение этого обновления для проверки операции и фактической версии ресурса.")
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
		if prior != nil {
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
					return card, nil, upgradeMismatch()
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
				out := upgradeResult(*prior, card, status)
				return card, answered(toJSON(out), status == "outcome_unknown" || status == "operation_failed"), nil
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
	r.From, r.Parent = card.version, card.parent
	preview := upgradeResult(r, card, "confirmation_required")
	preview["project_id"] = project
	preview["message"] = i18n.T("Upgrade only the control plane. Cluster API availability may be interrupted. The request cannot be withdrawn after submission. Node pools need separate approval.", "Обновить только управляющую часть кластера. Возможны перерывы в доступе к API. Отозвать отправленный запрос нельзя. Группы узлов требуют отдельного согласия.")
	if tool == "k8s_nodepool_upgrade" {
		preview["message"] = i18n.T("Upgrade this node pool. Workloads may be interrupted while nodes are updated. The request cannot be withdrawn after submission.", "Обновить эту группу узлов. Во время обновления узлов возможны перерывы в работе приложений. Отозвать отправленный запрос нельзя.")
	}
	return &Plan{Preview: preview, Execute: func(ctx context.Context) (string, bool) {
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
		if current.version != r.From || current.parent != r.Parent {
			return errResult(upgradeMismatch()), true
		}
		if err := r.save(path); err != nil {
			return errResult(err), true
		}
		var op *mk8s.Operation
		if tool == "k8s_nodepool_upgrade" {
			op, err = client.UpgradeNodePool(ctx, r.Resource, r.Target)
		} else {
			op, err = client.UpgradeCluster(ctx, r.Resource, r.Target)
		}
		if err != nil {
			return errAnswer(err, upgradeResult(r, current, "outcome_unknown")), true
		}
		if !isUUID(op.ID) || op.ResourceID != r.Resource {
			return errResult(upgradeMismatch()), true
		}
		r.Operation = op.ID
		if err := r.save(path); err != nil {
			return errResult(err), true
		}
		if len(op.Error) > 0 && string(op.Error) != "null" {
			return toJSON(upgradeResult(r, current, "operation_failed")), true
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
