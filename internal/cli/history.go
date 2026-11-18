// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// newHistoryCmd is evo history: the tool history (D43) at the root, as
// evo doctor is — the journal of the folder of state this command runs
// in. --json prints the tool's answer as is; a person also reads the full
// path of the journal, which the answer names by the folder's base name
// only (renderHistory).
func newHistoryCmd() *cobra.Command {
	var since, project string
	cmd := &cobra.Command{
		Use:   "history",
		Short: i18n.T("What evo changed in the cloud from this folder (the journal of changes)", "Что evo изменил в облаке из этой папки (журнал изменений)"),
		Long: i18n.T(`Read the journal of changes of this folder of state (.evo/history/): a
line for each end of a call that changed the cloud — what it made or
removed, and what is not known of it — and what may still be in the cloud:
made and not removed, or with an outcome not known, with the command that
reads it. Local, no API call.

A line names a resource as the call did: it does not prove the resource
is there or is evo's — read it before you act on it.`, `Прочитать журнал изменений этой папки состояния (.evo/history/): строку на
каждое окончание вызова, который изменил облако, — что он создал или удалил
и что о нём неизвестно, — и что может оставаться в облаке: создано и не
удалено или с неизвестным итогом, с командой, которая это прочитает.
Локально, без обращения к API.

Строка называет ресурс так, как его назвал вызов: что ресурс есть и что он
evo, она не доказывает — прочитайте его до действий с ним.`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			targs := map[string]any{}
			if since != "" {
				targs["since"] = since
			}
			if project != "" {
				targs["project_id"] = project
			}
			text, err := callEngineTool(cmd, "history", targs, &tools.Caller{})
			if err != nil {
				return err
			}
			p := cmdPrinter(cmd)
			if p.IsJSON() {
				fmt.Fprintln(cmd.OutOrStdout(), text)
				return nil
			}
			return renderHistory(p, cmd.OutOrStdout(), registryOf(cmd), text)
		},
	}
	cmd.Flags().StringVar(&since, "since", "", i18n.T("lines from this date (2026-11-01) or time; default: the last 30 days",
		"строки с этой даты (2026-11-01) или времени; по умолчанию — последние 30 дней"))
	cmd.Flags().StringVar(&project, "project-id", "", i18n.T("only the lines of this project", "только строки этого проекта"))
	noEmptyScope(cmd.Flags())
	return engineCmd(cmd, "history")
}
