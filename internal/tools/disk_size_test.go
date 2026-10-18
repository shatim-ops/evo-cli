// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// Диск — не больше 16 ТБ (документация «Диски», maximum 16384 в
// спецификации compute): больший размер — USAGE до обращения к облаку.
func TestDiskSize_OverMaximumIsUsage(t *testing.T) {
	r := New(func(map[string]any) (*engine.Engine, error) {
		t.Fatal("the engine is built for a size over the maximum")
		return nil, nil
	})
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"disk_create", map[string]any{"name": "data", "size_gb": 16385}},
		{"disk_resize", map[string]any{"disk_id": "d", "size_gb": 20000}},
	} {
		m, text, isErr := callNamed(t, r, c.tool, c.args)
		if !isErr || m["reason"] != "USAGE" {
			t.Errorf("%s: want USAGE, got %s", c.tool, text)
		}
	}
}
