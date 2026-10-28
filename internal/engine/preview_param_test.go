// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"
)

// Release plan 1.5, С4: a parameter whose value is an object (the health
// check lb_create sets) is shown to a person field by field, in a stable
// order — not as a Go map.
func TestParamLine_ObjectFieldByField(t *testing.T) {
	line := paramLine(PreviewParam{Key: "health_check", Value: map[string]any{
		"interval": "10s", "timeout": "5s", "healthy_threshold": 3, "unhealthy_threshold": 3,
	}})
	const want = "healthy-threshold 3, interval 10s, timeout 5s, unhealthy-threshold 3"
	if !strings.Contains(line, want) || strings.Contains(line, "map[") {
		t.Errorf("paramLine = %q, want it to hold %q", line, want)
	}
}
