// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

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
}

// russianTrees are the trees whose human tables speak Russian too: the
// column names, да/нет for booleans and the note under a list (the README
// promises the vm and disk trees in Russian). --json and MCP keep the
// tool's keys and English notes.
var russianTrees = map[string]bool{"vm": true, "disk": true}

// columnHeaderRU is the Russian title of a column of those tables, by the
// key of the tool's answer; a key without one is shown in capitals.
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
	"free_tier":       "Бесплатный уровень",
	"size_gb":         "Размер, ГБ",
	"disk_type":       "Тип",
	"attached_to":     "Подключён к",
	"enabled":         "Открыта",
}

// listNoteRU is the Russian text of a note a tool puts under its list, by
// its English text (the MCP answer keeps the English one).
var listNoteRU = map[string]string{
	"est_monthly_rub is a rate-card estimate incl. 10GB SSD, not a billed price": "Цена — оценка по тарифам, с диском SSD 10 ГБ; это не счёт.",
}

// noteLabelRU names the other notes under a list.
var noteLabelRU = map[string]string{"note": "Примечание", "warning": "Предупреждение", "hint": "Подсказка"}
