// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

func (s *Registry) handleVMHistory(ctx context.Context, args map[string]any) (string, bool) {
	vm, _ := args["vm_id"].(string)
	if !isUUID(vm) {
		return usageT("vm_id must be a VM UUID", "vm_id должен быть UUID ВМ"), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	api, ok := eng.Cloud.(provision.VMTaskReader)
	if !ok {
		return errResult(clierrors.New(clierrors.CodeConfigInvalid, i18n.T("This Compute client cannot read VM tasks.", "Этот клиент Compute не поддерживает чтение задач ВМ."), "")), true
	}
	const pageSize = 100
	const maxPages = 32
	rows := make([]map[string]any, 0)
	seen := make(map[string]bool)
	total := -1
	result := func() map[string]any {
		return map[string]any{"source": "Compute Tasks", "tasks": rows, "count": len(rows), "partial": false}
	}
	failed := func(err error) (string, bool) {
		if len(rows) == 0 {
			return errResult(err), true
		}
		out := result()
		out["partial"] = true
		out["status"] = "partial"
		return errAnswer(err, out), true
	}
	for range maxPages {
		offset := len(rows)
		p, err := api.VMTasks(ctx, eng.Config.ProjectID, vm, offset, pageSize)
		if err != nil {
			return failed(err)
		}
		if !validTaskPage(p, offset, pageSize, total) {
			return failed(clierrors.New(clierrors.CodeAPIError, i18n.T("Inconsistent Compute Tasks pagination.", "Нарушена последовательность страниц задач Compute."), ""))
		}
		total = *p.Total
		for _, t := range p.Items {
			key := strings.ToLower(t.ID)
			if !strings.EqualFold(t.EntityID, vm) || t.EntityType != "vm" || !isUUID(key) || seen[key] || t.Created.IsZero() || strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Status) == "" {
				return failed(clierrors.New(clierrors.CodeAPIError, i18n.T("Compute Tasks returned an incomplete, unrelated or repeated task.", "Compute вернул неполную, постороннюю или повторную запись задачи."), ""))
			}
			seen[key] = true
			row := map[string]any{"id": t.ID, "action": t.Name, "state": t.Status, "created_time": t.Created}
			if t.Modified != nil {
				row["modified_time"] = t.Modified
			}
			rows = append(rows, row)
		}
		if len(rows) == total {
			return toJSON(result()), false
		}
	}
	return failed(clierrors.New(clierrors.CodeAPIError, i18n.T("Compute Tasks page limit reached.", "Достигнут предел числа страниц задач Compute."), ""))
}

func validTaskPage(p *provision.VMTaskPage, offset, requested, previousTotal int) bool {
	if p == nil || p.Items == nil || p.Total == nil {
		return false
	}
	total := *p.Total
	if total < offset || (previousTotal >= 0 && total != previousTotal) {
		return false
	}
	if p.Offset != nil && *p.Offset != offset {
		return false
	}
	limit := requested
	if p.Limit != nil {
		limit = *p.Limit
		if limit < 1 || limit > requested {
			return false
		}
	}
	n := len(p.Items)
	return n <= limit && n <= total-offset && (n > 0 || offset == total)
}
