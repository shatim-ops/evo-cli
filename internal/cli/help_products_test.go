// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"slices"
	"strings"
	"testing"
)

// Группы корневой справки — продукты Cloud.ru Evolution под их
// официальными именами: человек видит, какие сервисы ему доступны.
// Evolution Compute — первой, сценарии evo (compose-проект, стек) — после
// продуктов (решение владельца 30.10, #597, п. 30).
func TestRootHelp_GroupsAreTheProducts(t *testing.T) {
	want := []struct {
		ru   string
		cmds []string
	}{
		{"Evolution Compute, Evolution Disk:", []string{"vm", "image", "marketplace", "disk", "list-zones"}},
		{"Evolution VPC, Evolution DNS, Evolution Load Balancer, SNAT-шлюзы:", []string{"vpc", "dns", "lb", "tg", "snat"}},
		{"Evolution Managed Kubernetes:", []string{"k8s"}},
		{"Evolution Artifact Registry, Evolution Container Apps:", []string{"ar", "ca"}},
		{"Evolution Managed PostgreSQL, Evolution Managed Redis, Evolution Managed Kafka:", []string{"pg", "redis", "kafka"}},
		{"Evolution Object Storage:", []string{"s3"}},
		{"Secret Management, Certificate Manager:", []string{"secret", "cert"}},
		{"Мониторинг, аудит, биллинг, доступ:", []string{"metrics", "audit", "billing", "org", "iam"}},
		{"Сценарии: compose-проект, стек:", []string{"init", "deploy", "status", "logs", "verify", "destroy", "stack"}},
	}
	if len(commandGroups) < len(want) {
		t.Fatalf("групп %d, нужно не меньше %d", len(commandGroups), len(want))
	}
	for i, w := range want {
		g := commandGroups[i]
		if g.ru != w.ru || !slices.Equal(g.cmds, w.cmds) {
			t.Errorf("группа %d: %q %v, нужно %q %v", i, g.ru, g.cmds, w.ru, w.cmds)
		}
	}
	if long := newRootCmd().Long; !strings.Contains(long, "Cloud.ru Evolution") {
		t.Errorf("шапка справки не называет Cloud.ru Evolution: %q", long)
	}
}
