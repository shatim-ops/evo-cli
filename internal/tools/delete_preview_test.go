// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// Повторная приемка #90, N-09: кластер в DEPLOYING Cloud.ru не удаляет
// (400/500 FailedPrecondition) — до 1.6 превью предупреждало до
// подтверждения, с 1.6 удаление отказывает до вопроса (D61; подробно —
// TestDeletePlan_PassingStateRefusedBeforeTheQuestion). Готовый кластер
// — превью без предупреждения. FAILED и ERROR —
// TestDeletePreview_WarnsInFailedState (G13-19).
func TestDeletePreview_WarnsWhileBeingCreated(t *testing.T) {
	for status, refused := range map[string]bool{"DEPLOYING": true, "creating": true, "READY": false} {
		p := New(nil).deletePlan("k8s_delete", "x_delete deletes %s.", "x_delete удаляет %s.", "id-1", func() (map[string]any, error) {
			return map[string]any{"name": "c1", "status": status}, nil
		}, nil)
		if got := p.Preview == nil && p.answerErr && strings.Contains(p.answer, `"USAGE"`); got != refused {
			t.Errorf("status %s: refused before the question=%v, want %v: %s %s", status, got, refused, toJSON(p.Preview), p.answer)
		}
		if !refused && strings.Contains(toJSON(p.Preview), `"warning"`) {
			t.Errorf("status %s: a ready cluster gets a warning: %s", status, toJSON(p.Preview))
		}
	}
}
