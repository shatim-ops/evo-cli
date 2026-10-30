// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"maps"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The tree commands are made from the tools. A tool is described in English
// for the agent (MCP) and in Russian for the CLI, both in the registry
// (internal/tools, toolsRU): the Russian help of a tree command — summary,
// long help, every flag — is the tool's Russian description, written for the
// CLI: consent is the prompt or --yes, not confirmed=true, and other tools
// are named by their commands, or as 'evo tools call <tool>' while they have
// none (TestTrees_RussianFlags, TestTrees_RussianShorts). What stays here is
// the CLI's own presentation: the summary of each tree and the Russian
// tables.

// treeGroupShortRU is the Russian summary of each tree.
var treeGroupShortRU = map[string]string{
	"vm":          "Виртуальные машины",
	"image":       "Образы ВМ: операционные системы и продукты маркетплейса",
	"marketplace": "Витрина: образы ВМ и плагины Kubernetes с командой развёртывания каждого",
	"disk":        "Диски",
	"vpc":         "Сети: VPC, маршруты, подсети, группы безопасности, floating IP",
	"k8s":         "Кластеры Managed Kubernetes",
	"pg":          "Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии",
	"s3":          "Object Storage: бакеты и объекты",
	"secret":      "Secret Manager: секреты",
	"dns":         "Evolution DNS: зоны и записи",
	"cert":        "Certificate Manager: TLS-сертификаты",
	"lb":          "Балансировщики нагрузки (L4)",
	"tg":          "Группы целей балансировщиков",
	"snat":        "SNAT-шлюзы: исходящий интернет для приватных подсетей",
	"kafka":       "Кластеры Managed Kafka",
	"redis":       "Кластеры Managed Redis",
	"ar":          "Artifact Registry: реестры образов и пакетов",
	"ca":          "Container Apps: бессерверные контейнеры",
	"billing":     "Биллинг: баланс, потребление, отчёт FinOps",
	"audit":       "Журнал аудита: кто что сделал в проекте",
	"org":         "Организация: проекты и квоты",
	"iam":         "IAM: сервисные аккаунты",
	"metrics":     "Service Monitoring: метрики",
	"stack":       "Стеки: несколько ресурсов по одному манифесту",
}

// Every tree speaks Russian in its human tables when the CLI does: the
// column titles (treeHeadersRU), да/нет for booleans and the notes under a
// list (tableView). The README promised the vm and disk trees in Russian,
// every tree after them spoke Russian from the start (PAR-16), and the
// trees of v1.1 in between (image, vpc, k8s, pg, s3, secret) followed in
// 1.2 (owner's decision Q11). --json and MCP keep the tool's keys and
// English notes.

// legacyRussianTrees are the trees whose tables were Russian in v1.1: they
// take the titles from columnHeaderRU alone, so that a title added for
// another tree does not change them (a vm zones row has "default").
var legacyRussianTrees = map[string]bool{"vm": true, "disk": true}

// moreColumnHeaderRU are the Russian titles of the columns of the other
// trees beyond columnHeaderRU; treeColumnHeaderRU, by tree, goes before
// both where a key means something else in that tree.
var moreColumnHeaderRU = map[string]string{
	"domain":               "Домен",
	"visibility":           "Видимость",
	"delegated":            "Делегирована",
	"valid":                "Корректна",
	"confirm_state":        "Подтверждение",
	"type":                 "Тип",
	"values":               "Значения",
	"ttl":                  "TTL",
	"vpc_id":               "VPC",
	"nat_type":             "Тип NAT",
	"availability_zone":    "Зона",
	"availability_zone_id": "ID зоны",
	"version":              "Версия",
	"brokers":              "Брокеры",
	"health":               "Работоспособность",
	"shards":               "Шарды",
	"replicas":             "Реплики",
	"public":               "Публичный",
	"tariff":               "Тариф",
	"display_name":         "Имя",
	"cpu":                  "vCPU",
	"memory":               "RAM, ГБ",
	"deployment_mode":      "Режим",
	"min_storage_gb":       "Хранилище от, ГБ",
	"service":              "Сервис",
	"cost":                 "Стоимость",
	"resource":             "Ресурс",
	"limit":                "Лимит",
	"usage":                "Использовано",
	"email":                "Email",
	"vm_name":              "Имя ВМ",
	"vm_ip":                "IP ВМ",
	"flavor_name":          "Flavor",
	"created_at":           "Создано",
	// The trees of v1.1 (Q11).
	"default":       "По умолчанию",
	"subnet":        "Подсеть",
	"next_hop_type": "Тип следующего узла",
	"ip_address":    "IP-адрес",
	"available":     "Свободен",
	"nat_gateway":   "На NAT-шлюзе",
	"flavor":        "Flavor",
	"flavor_id":     "ID flavor",
	"ram":           "RAM",
	"desired":       "Узлов",
	"minCount":      "Узлов от",
	"maxCount":      "Узлов до",
	"memory_gb":     "RAM, ГБ",
	"flavor_type":   "Тип flavor",
	"max_hosts":     "Хостов до",
	"created":       "Создан",
	"key":           "Ключ",
	"size":          "Размер, байт",
	"last_modified": "Изменён",
	"path":          "Путь",
	"description":   "Описание",
}

// treeColumnHeaderRU are the titles a tree gives a column its own way.
var treeColumnHeaderRU = map[string]map[string]string{
	"iam": {"enabled": "Включён"}, // columnHeaderRU: a zone is "Открыта"
}

// treeHeadersRU are the Russian column titles of the tables of a tree:
// columnHeaderRU alone for a legacy Russian tree; columnHeaderRU,
// moreColumnHeaderRU and the tree's own treeColumnHeaderRU, each over the
// one before, for the others.
func treeHeadersRU(group string) map[string]string {
	if legacyRussianTrees[group] {
		return columnHeaderRU
	}
	out := make(map[string]string, len(columnHeaderRU)+len(moreColumnHeaderRU))
	for _, m := range []map[string]string{columnHeaderRU, moreColumnHeaderRU, treeColumnHeaderRU[group]} {
		maps.Copy(out, m)
	}
	return out
}

// columnHeaderRU is the Russian title of a column by the key of the tool's
// answer, the one every tree has (treeHeadersRU); a key without a title is
// shown in capitals.
var columnHeaderRU = map[string]string{
	"id":              "ID",
	"name":            "Имя",
	"state":           "Состояние",
	"status":          "Состояние",
	"zone":            "Зона",
	"created_time":    "Создана",
	"vcpus":           "vCPU",
	"ram_gb":          "RAM, ГБ",
	"est_monthly_rub": "≈ руб./мес",
	"cpu_share":       "Доля vCPU",
	"spot":            "Spot",
	"free_tier":       "Бесплатный уровень",
	"size_gb":         "Размер, ГБ",
	"disk_type":       "Тип",
	"attached_to":     "Подключён к",
	"enabled":         "Открыта",
}

// listNoteRU is the Russian text of a note a tool puts under its list, by
// its English text (the MCP answer keeps the English one).
var listNoteRU = map[string]string{
	tools.FlavorsNote: "Цена — оценка по тарифам, с диском SSD 10 ГБ; это не счёт. Доля vCPU — гарантированная доля ядра: 1:1 — 100 %, 1:3 — 30 %, 1:10 — 10 % (спецификация Cloud.ru). У flavor с долей не 1:1 оценка долю не учитывает — точные ставки в тарифах.",
}

// noteLabelRU names the other notes under a list.
var noteLabelRU = map[string]string{"note": "Примечание", "warning": "Предупреждение", "hint": "Подсказка", "tariff_url": "Тарифы"}
