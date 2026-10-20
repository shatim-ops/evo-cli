// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "testing"

// Пробельная строка в одном из пары аргументов после TrimSpace исчезает,
// и остаётся второй. Оба поля при этом были переданы.
func TestBreakR4_R4_11_WhitespaceAliasSilentlyTaken(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	runUsageCases(t, []usageCase{
		{"vm_get", map[string]any{"vm_id": " ", "name": "web"}, "not both"},
		{"vm_get", map[string]any{"vm_id": id, "name": " \t"}, "not both"},
		{"image_get", map[string]any{"image": " ", "image_id": id}, "not both"},
		{"image_get", map[string]any{"image": "ubuntu", "image_id": " "}, "not both"},
		{"pg_get", map[string]any{"id": " ", "cluster_id": id}, "cluster_id"},
		{"pg_delete", map[string]any{"id": " ", "cluster_id": id, "confirmed": true}, "cluster_id"},
	})
}
