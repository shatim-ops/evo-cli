// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"slices"
	"testing"
)

// Группы корневой справки — разделы каталога Cloud.ru (cloud.ru/docs,
// «Документация по сервисам»): ресурсы облака идут первыми, «Инфраструктура»
// (виртуальные машины, образы) — первой из них; сценарии evo (compose-проект,
// стек) — после ресурсов (решение владельца 30.10, #597, п. 30).
func TestRootHelp_GroupsFollowTheCatalog(t *testing.T) {
	want := map[string][]string{
		"Инфраструктура:":    {"vm", "image", "marketplace", "list-zones"},
		"Сеть:":              {"vpc", "dns", "lb", "tg", "snat"},
		"Хранение данных:":   {"disk", "s3"},
		"Контейнеры:":        {"k8s", "ca", "ar"},
		"Базы данных:":       {"pg", "redis"},
		"Брокеры сообщений:": {"kafka"},
		"Безопасность:":      {"secret", "cert"},
	}
	if commandGroups[0].ru != "Инфраструктура:" {
		t.Errorf("первая группа — %q, нужна «Инфраструктура:»", commandGroups[0].ru)
	}
	scenarios := -1
	for i, g := range commandGroups {
		if cmds, ok := want[g.ru]; ok {
			if scenarios >= 0 {
				t.Errorf("группа %q после сценариев", g.ru)
			}
			if !slices.Equal(g.cmds, cmds) {
				t.Errorf("группа %q: %v, нужно %v", g.ru, g.cmds, cmds)
			}
			delete(want, g.ru)
		}
		if slices.Contains(g.cmds, "deploy") {
			scenarios = i
			if !slices.Contains(g.cmds, "stack") {
				t.Errorf("stack не в группе сценариев: %v", g.cmds)
			}
		}
	}
	for g := range want {
		t.Errorf("нет группы %q", g)
	}
}
