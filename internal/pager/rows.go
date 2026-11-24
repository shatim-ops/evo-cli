// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pager

import (
	"bytes"
	"encoding/json"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// DecodePage reads an object envelope. A JSON null decoded into a Go
// struct would otherwise silently finish a paginated read.
func DecodePage(data []byte, dst any) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return Incomplete(i18n.T("expected a list page object", "ожидался объект страницы списка"))
	}
	if err := json.Unmarshal(data, dst); err != nil {
		// Keep diagnostics independent of potentially sensitive response values.
		return Incomplete(i18n.T("invalid list page", "повреждена страница списка"))
	}
	return nil
}

// Rows is an array of resource objects. An omitted optional field stays
// nil. An explicit [] stays non-nil, including when the caller requires
// positive evidence of a complete empty collection.
type Rows[T any] []T

func (rows *Rows[T]) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return Incomplete(i18n.T("expected a list array", "ожидался массив списка"))
	}
	values := make([]T, len(raw))
	for i, r := range raw {
		if len(r) == 0 || r[0] != '{' || json.Unmarshal(r, &values[i]) != nil {
			return Incomplete(i18n.T("invalid object in list", "повреждён объект списка"))
		}
	}
	*rows = values
	return nil
}

// ObjectPage reads the field named by the service, plus the rest of its
// envelope (for the service-specific continuation token).
func ObjectPage(data []byte, field string) (Rows[map[string]any], map[string]json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := DecodePage(data, &envelope); err != nil {
		return nil, nil, err
	}
	var rows Rows[map[string]any]
	if raw, ok := envelope[field]; ok {
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, nil, err
		}
	}
	return rows, envelope, nil
}

// CheckObjectRows validates collections in pages already decoded as generic
// JSON objects (Organization and IAM). A missing optional field is allowed.
func CheckObjectRows(page map[string]any, fields ...string) error {
	if page == nil {
		return Incomplete(i18n.T("expected a list page object", "ожидался объект страницы списка"))
	}
	for _, field := range fields {
		v, ok := page[field]
		if !ok {
			continue
		}
		rows, ok := v.([]any)
		if !ok {
			return Incomplete(i18n.T("expected a list array", "ожидался массив списка"))
		}
		for _, r := range rows {
			if _, ok := r.(map[string]any); !ok {
				return Incomplete(i18n.T("invalid object in list", "повреждён объект списка"))
			}
		}
	}
	return nil
}
