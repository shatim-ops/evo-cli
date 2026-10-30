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

// The root --help lists the commands by the products of Cloud.ru
// Evolution, so a newcomer sees which services evo reaches: the title of a
// group is the official name of its product, or the names of several; a
// group without one has a plain title. The cloud products come first,
// Evolution Compute first of them; the scenarios of evo (a compose project,
// a stack) follow (owner's decision 30.10, #597, item 30; before: review
// r5 U5, compose first). The sources of the names are in PR #688. The exit
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
	{"compute", "Evolution Compute, Evolution Disk:", "Evolution Compute, Evolution Disk:",
		[]string{"vm", "image", "marketplace", "disk", "list-zones"}},
	{"network", "Evolution VPC, Evolution DNS, Evolution Load Balancer, SNAT gateways:",
		"Evolution VPC, Evolution DNS, Evolution Load Balancer, SNAT-шлюзы:",
		[]string{"vpc", "dns", "lb", "tg", "snat"}},
	{"kubernetes", "Evolution Managed Kubernetes:", "Evolution Managed Kubernetes:",
		[]string{"k8s"}},
	{"containers", "Evolution Artifact Registry, Evolution Container Apps:", "Evolution Artifact Registry, Evolution Container Apps:",
		[]string{"ar", "ca"}},
	{"databases", "Evolution Managed PostgreSQL, Evolution Managed Redis, Evolution Managed Kafka:",
		"Evolution Managed PostgreSQL, Evolution Managed Redis, Evolution Managed Kafka:",
		[]string{"pg", "redis", "kafka"}},
	{"storage", "Evolution Object Storage:", "Evolution Object Storage:",
		[]string{"s3"}},
	{"security", "Secret Management, Certificate Manager:", "Secret Management, Certificate Manager:",
		[]string{"secret", "cert"}},
	{"account", "Monitoring, audit, billing, access:", "Мониторинг, аудит, биллинг, доступ:",
		[]string{"metrics", "audit", "billing", "org", "iam"}},
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
