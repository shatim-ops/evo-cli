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
	Result     string `json:"check_result"`
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
		LiveCut   bool           `json:"live_truncated"`
		Retired   string         `json:"retired_through"`
		Note      string         `json:"note"`
		Closed    []historyEntry `json:"closed"`
		Checked   *struct {
			Read       int `json:"read"`
			Closed     int `json:"closed"`
			NotChecked int `json:"not_checked"`
		} `json:"checked"`
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
			if a.Checked != nil {
				rows[i] = append(rows[i], e.Result)
			}
		}
		head := []string{i18n.T("Time", "Время"), i18n.T("Tool", "Инструмент"), i18n.T("Resource", "Ресурс"), "ID",
			i18n.T("Outcome", "Итог"), i18n.T("Check with", "Проверить")}
		if a.Checked != nil {
			head = append(head, i18n.T("Read by id", "Чтение по id"))
		}
		p.Table(head, rows)
	}
	if a.Checked != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, i18n.Tf("Checked: %d read by id, %d gone and closed, %d not read.",
			"Проверено: прочитано по id %d, исчезло и закрыто %d, не прочитано %d.", a.Checked.Read, a.Checked.Closed, a.Checked.NotChecked))
		if len(a.Closed) > 0 {
			rows := make([][]string, len(a.Closed))
			for i, e := range a.Closed {
				rows[i] = []string{e.Time, e.Tool, what(e), e.ResourceID}
			}
			fmt.Fprintln(w, i18n.T("Gone — closed with a line not_found:", "Исчезло — закрыто строкой not_found:"))
			p.Table([]string{i18n.T("Time", "Время"), i18n.T("Tool", "Инструмент"), i18n.T("Resource", "Ресурс"), "ID"}, rows)
		}
	}
	if a.LiveCut {
		fmt.Fprintln(w, i18n.T("Only the last live lines are shown, there are more: narrow it to a project with --project-id.",
			"Показаны только последние живые строки, есть ещё — сузьте по проекту через --project-id."))
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
