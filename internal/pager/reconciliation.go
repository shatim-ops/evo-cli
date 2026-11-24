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
	total *int64
}

// Check verifies the DNS and VPC token-page envelope and its
// resource identities. An omitted token is the normal end of pagination.
func (p *EvidencePages) Check(envelope map[string]json.RawMessage, rows []map[string]any, scope map[string]string, fields ...string) error {
	var token *string
	if raw, ok := envelope["nextPageToken"]; ok {
		if json.Unmarshal(raw, &token) != nil || token == nil {
			return invalidEvidence()
		}
	}
	return p.CheckRows(envelope["total"], rows, token == nil || *token == "", scope, fields...)
}

func invalidEvidence() error {
	return clierrors.New(clierrors.CodeAPIError, i18n.T("The resource list is incomplete or contradicts the requested scope.", "Список ресурсов неполон или противоречит области запроса."), i18n.T("Read the target again in the original project.", "Повторите проверку цели в исходном проекте."))
}

// CheckTotalCount records the optional uint32 totalCount of a VPC route page.
// Any other count supplied on this or a later page must agree.
func (p *EvidencePages) CheckTotalCount(raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	var n *uint32
	if json.Unmarshal(raw, &n) != nil || n == nil {
		return invalidEvidence()
	}
	return p.recordTotal(int64(*n))
}

func (p *EvidencePages) recordTotal(n int64) error {
	if n < 0 || (p.total != nil && *p.total != n) {
		return invalidEvidence()
	}
	p.total = &n
	return nil
}

// CheckRows validates distinct identities and the declared count across token or offset pages.
func (p *EvidencePages) CheckRows(raw json.RawMessage, rows []map[string]any, lastPage bool, scope map[string]string, fields ...string) error {
	if rows == nil {
		return invalidEvidence()
	}
	if raw != nil {
		var total *int64
		if json.Unmarshal(raw, &total) != nil || total == nil {
			return invalidEvidence()
		}
		if err := p.recordTotal(*total); err != nil {
			return err
		}
	}
	if p.ids == nil {
		p.ids = make(map[string]bool)
	}
	for _, row := range rows {
		id, _ := row["id"].(string)
		if strings.TrimSpace(id) == "" || p.ids[id] {
			return invalidEvidence()
		}
		p.ids[id] = true
		for _, field := range fields {
			v, _ := row[field].(string)
			if strings.TrimSpace(v) == "" {
				return invalidEvidence()
			}
		}
		for key, expected := range scope {
			if value, ok := row[key]; ok && value != expected {
				return invalidEvidence()
			}
		}
	}
	if p.total != nil {
		seen := int64(len(p.ids))
		if seen > *p.total || (lastPage && seen != *p.total) {
			return invalidEvidence()
		}
	}
	return nil
}
