// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"

	"github.com/spf13/cobra"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The root --help lists the commands by the sections of the Cloud.ru
// service catalog (cloud.ru/docs, «Документация по сервисам»), cloud
// resources first and Infrastructure (VMs, images) first of them; the
// scenarios of evo (a compose project, a stack) follow the resources
// (owner's decision 30.10, #597, item 30; before: review r5 U5, compose
// first). A section without a catalog name has a plain one. The catalog is
// published in Russian: the English titles are its translation. The exit
// codes go after the list.

func init() {
	cobra.AddTemplateFunc("exitCodesHelp", clerrors.ExitCodesHelp)
}

// commandGroups: group ID, English and Russian title, the commands in it.
// A command missing here fails TestRootHelp_EveryCommandHasGroup.
var commandGroups = []struct {
	id, en, ru string
	cmds       []string
}{
	{"infra", "Infrastructure:", "Инфраструктура:",
		[]string{"vm", "image", "marketplace", "list-zones"}},
	{"network", "Network:", "Сеть:",
		[]string{"vpc", "dns", "lb", "tg", "snat"}},
	{"storage", "Data storage:", "Хранение данных:",
		[]string{"disk", "s3"}},
	{"containers", "Containers:", "Контейнеры:",
		[]string{"k8s", "ca", "ar"}},
	{"databases", "Databases:", "Базы данных:",
		[]string{"pg", "redis"}},
	{"brokers", "Message brokers:", "Брокеры сообщений:",
		[]string{"kafka"}},
	{"security", "Security:", "Безопасность:",
		[]string{"secret", "cert"}},
	{"monitoring", "Monitoring and management:", "Мониторинг и управление:",
		[]string{"metrics", "audit"}},
	{"account", "Administration:", "Администрирование:",
		[]string{"billing", "org", "iam"}},
	{"scenarios", "Scenarios: a compose project, a stack:", "Сценарии: compose-проект, стек:",
		[]string{"init", "deploy", "status", "logs", "verify", "destroy", "stack"}},
	{"setup", "Setup:", "Настройка:",
		[]string{"configure", "project", "whoami", "doctor", "diag", "version", "completion", "help"}},
	{"agents", "For AI agents:", "Для ИИ-агентов:",
		[]string{"mcp", "tools", "agent"}},
}

// exitCodesTail is appended to the usage template: the exit codes after
// the command list, in the root help only.
const exitCodesTail = `{{if not .HasParent}}

{{exitCodesHelp}}{{end}}
`

// groupCommands puts the root's commands into commandGroups. Call it after
// every command is added and before localizeHelp.
func groupCommands(root *cobra.Command) {
	byName := map[string]string{}
	for _, g := range commandGroups {
		root.AddGroup(&cobra.Group{ID: g.id, Title: i18n.T(g.en, g.ru)})
		for _, name := range g.cmds {
			byName[name] = g.id
		}
	}
	root.SetHelpCommandGroupID(byName["help"])
	root.SetCompletionCommandGroupID(byName["completion"])
	for _, c := range root.Commands() {
		if id, ok := byName[c.Name()]; ok {
			c.GroupID = id
		}
	}
	root.SetUsageTemplate(strings.TrimSuffix(root.UsageTemplate(), "\n") + exitCodesTail)
}
