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

// The root --help lists the commands by scenario, not by alphabet: a
// newcomer sees "deploy a compose project" first and the resource trees
// next, as gh shows CORE before ADDITIONAL COMMANDS and kubectl Basic before
// Cluster Management (review r5 U5). The exit codes go after the list.

func init() {
	cobra.AddTemplateFunc("exitCodesHelp", clerrors.ExitCodesHelp)
}

// commandGroups: group ID, English and Russian title, the commands in it.
// A command missing here fails TestRootHelp_EveryCommandHasGroup.
var commandGroups = []struct {
	id, en, ru string
	cmds       []string
}{
	{"compose", "Deploy a compose project:", "Развёртывание compose-проекта:",
		[]string{"init", "deploy", "status", "logs", "verify", "destroy"}},
	{"resources", "Project resources:", "Ресурсы проекта:",
		[]string{"vm", "image", "marketplace", "disk", "vpc", "k8s", "pg", "s3", "secret", "list-zones"}},
	{"setup", "Setup:", "Настройка:",
		[]string{"configure", "project", "whoami", "doctor", "version", "completion", "help"}},
	{"agents", "For AI agents:", "Для ИИ-агентов:",
		[]string{"mcp", "tools"}},
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
