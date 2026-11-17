// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// historyEntry is a line of the answer of history.
type historyEntry struct {
	Time       string `json:"time"`
	Tool       string `json:"tool"`
	Name       string `json:"name"`
	Target     string `json:"target"`
	ResourceID string `json:"resource_id"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason"`
	Check      string `json:"check"`
}

// renderHistory shows the journal of changes (D43): the full path of the
// journal — the person's own machine, which the answer names by the base
// name of the folder only (MCP gives no local path, and the JSON of the
// CLI is the tool's answer) — then the lines of the window and what is
// live. The cells are sanitized by the table: a name is the cloud's or a
// file's text.
func renderHistory(p *output.Printer, w io.Writer, reg *tools.Registry, text string) error {
	var a struct {
		Since     string         `json:"since"`
		Lines     []historyEntry `json:"lines"`
		Live      []historyEntry `json:"live"`
		Skipped   int            `json:"skipped"`
		Hidden    int            `json:"hidden_without_project"`
		Truncated bool           `json:"truncated"`
		Retired   string         `json:"retired_through"`
		Note      string         `json:"note"`
	}
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		return err
	}
	if dir, err := stateDir(); err == nil {
		fmt.Fprintln(w, i18n.Tf("Journal: %s", "Журнал: %s", output.SanitizeCell(history.DirOf(intent.Dir(dir)))))
	}
	fmt.Fprintln(w, i18n.Tf("Since: %s", "С: %s", a.Since))
	what := func(e historyEntry) string {
		if e.Name != "" {
			return e.Name
		}
		return e.Target
	}
	if len(a.Lines) == 0 {
		fmt.Fprintln(w, i18n.T("No changes in this period.", "Изменений за этот период нет."))
	} else {
		rows := make([][]string, len(a.Lines))
		for i, e := range a.Lines {
			rows[i] = []string{e.Time, e.Tool, what(e), e.ResourceID, e.Outcome, e.Reason}
		}
		p.Table([]string{i18n.T("Time", "Время"), i18n.T("Tool", "Инструмент"), i18n.T("Resource", "Ресурс"), "ID",
			i18n.T("Outcome", "Итог"), i18n.T("Reason", "Код")}, rows)
	}
	fmt.Fprintln(w)
	if len(a.Live) == 0 {
		fmt.Fprintln(w, i18n.T("Live: nothing — every change has an outcome, and what was made was removed.",
			"Живое: ничего — у каждого изменения известен итог, созданное удалено."))
	} else {
		fmt.Fprintln(w, i18n.T("Live — may still be in the cloud:", "Живое — может оставаться в облаке:"))
		// The read that tells an outcome is named as the person runs it:
		// the command, not the tool (D6).
		rows := make([][]string, len(a.Live))
		for i, e := range a.Live {
			check := e.Check
			if t, ok := reg.Lookup(check); ok && toolCommandPath(t) != "" {
				check = toolCommandPath(t)
			}
			rows[i] = []string{e.Time, e.Tool, what(e), e.ResourceID, e.Outcome, check}
		}
		p.Table([]string{i18n.T("Time", "Время"), i18n.T("Tool", "Инструмент"), i18n.T("Resource", "Ресурс"), "ID",
			i18n.T("Outcome", "Итог"), i18n.T("Check with", "Проверить")}, rows)
	}
	if a.Truncated {
		fmt.Fprintln(w, i18n.T("Only the last lines are shown: give a later --since.", "Показаны только последние строки: укажите более позднее --since."))
	}
	if a.Retired != "" {
		fmt.Fprintln(w, i18n.Tf("Lines of %s and before were pruned: only what was live of them is kept.",
			"Строки за %s и раньше вычищены: из них оставлено только живое.", a.Retired))
	}
	if a.Skipped > 0 {
		fmt.Fprintln(w, i18n.Tf("%d lines of the journal are not evo's form and were skipped.", "Строк журнала не в форме evo, пропущено: %d.", a.Skipped))
	}
	if a.Hidden > 0 {
		fmt.Fprintln(w, i18n.Tf("%d calls with no project are not shown on this server.", "Вызовов без проекта, не показанных на этом сервере: %d.", a.Hidden))
	}
	if a.Note != "" {
		fmt.Fprintln(w, a.Note)
	}
	return nil
}
