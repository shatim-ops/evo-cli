// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// Повторная приемка #90, N-09: кластер в DEPLOYING Cloud.ru не удаляет
// (400/500 FailedPrecondition) — превью предупреждает до подтверждения.
func TestDeletePreview_WarnsWhileBeingCreated(t *testing.T) {
	for status, warn := range map[string]bool{"DEPLOYING": true, "creating": true, "READY": false, "FAILED": false} {
		text, _ := New(nil).deletePreview("k8s_delete", "x_delete deletes %s.", "x_delete удаляет %s.", "id-1", func() (map[string]any, error) {
			return map[string]any{"name": "c1", "status": status}, nil
		})
		if got := strings.Contains(text, `"warning"`); got != warn {
			t.Errorf("status %s: warning=%v, want %v: %s", status, got, warn, text)
		}
	}
}
