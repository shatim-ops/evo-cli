// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pager

import (
	"context"
	"encoding/json"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

type reconciliationKey struct{}

// ForReconciliation requests complete evidence before an interrupted create can settle.
func ForReconciliation(ctx context.Context) context.Context {
	return context.WithValue(ctx, reconciliationKey{}, true)
}

func IsReconciliation(ctx context.Context) bool {
	return ctx.Value(reconciliationKey{}) == true
}

// EvidencePages tracks the distinct rows and any total promised by a page.
type EvidencePages struct {
	ids   map[string]bool
	total int
}

// Check verifies the DNS and VPC token-page envelope and its
// resource identities. An omitted token is the normal end of pagination.
func (p *EvidencePages) Check(envelope map[string]json.RawMessage, rows []map[string]any, scope map[string]string, fields ...string) error {
	invalid := func() error {
		return clierrors.New(clierrors.CodeAPIError, i18n.T("The resource list is incomplete or contradicts the requested scope.", "Список ресурсов неполон или противоречит области запроса."), i18n.T("Read the target again in the original project.", "Повторите проверку цели в исходном проекте."))
	}
	if rows == nil {
		return invalid()
	}
	var token *string
	if raw, ok := envelope["nextPageToken"]; ok {
		if json.Unmarshal(raw, &token) != nil || token == nil {
			return invalid()
		}
	}
	if raw, ok := envelope["total"]; ok {
		var total *int
		if json.Unmarshal(raw, &total) != nil || total == nil || *total < 0 {
			return invalid()
		}
		p.total = max(*total, p.total)
	}
	if p.ids == nil {
		p.ids = make(map[string]bool)
	}
	for _, row := range rows {
		id, _ := row["id"].(string)
		if strings.TrimSpace(id) == "" || p.ids[id] {
			return invalid()
		}
		p.ids[id] = true
		for _, field := range fields {
			v, _ := row[field].(string)
			if strings.TrimSpace(v) == "" {
				return invalid()
			}
		}
		for key, expected := range scope {
			if value, ok := row[key]; ok && value != expected {
				return invalid()
			}
		}
	}
	if (token == nil || *token == "") && len(p.ids) < p.total {
		return invalid()
	}
	return nil
}
