// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/tools"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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
	ProjectID  string `json:"project_id"`
	Zone       string `json:"zone"`
	Surface    string `json:"surface"`
	IntentID   string `json:"intent_id"`
	Result     string `json:"check_result"`
}

// where is the project and the zone of e, as one cell.
func (e historyEntry) where() string { return strings.TrimSpace(e.ProjectID + " " + e.Zone) }

// call is the surface and the id of the record of the call of e, as one
// cell: the lines of one call share the id.
func (e historyEntry) call() string { return strings.TrimSpace(e.Surface + " " + e.IntentID) }

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
		Would     []historyEntry `json:"would_close"`
		CheckNote string         `json:"check_note"`
		Checked   *struct {
			Read       int            `json:"read"`
			WouldClose int            `json:"would_close"`
			NotChecked map[string]int `json:"not_checked"`
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
			rows[i] = []string{e.Time, e.Tool, what(e), e.ResourceID, e.Outcome, e.Reason, e.where(), e.call()}
		}
		p.Table([]string{i18n.T("Time", "Время"), i18n.T("Tool", "Инструмент"), i18n.T("Resource", "Ресурс"), "ID",
			i18n.T("Outcome", "Итог"), i18n.T("Reason", "Код"), i18n.T("Project · zone", "Проект · зона"), i18n.T("Call", "Вызов")}, rows)
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
			rows[i] = []string{e.Time, e.Tool, what(e), e.ResourceID, e.Outcome, check, e.where(), e.call()}
			if a.Checked != nil {
				rows[i] = append(rows[i], e.Result)
			}
		}
		head := []string{i18n.T("Time", "Время"), i18n.T("Tool", "Инструмент"), i18n.T("Resource", "Ресурс"), "ID",
			i18n.T("Outcome", "Итог"), i18n.T("Check with", "Проверить"), i18n.T("Project · zone", "Проект · зона"), i18n.T("Call", "Вызов")}
		if a.Checked != nil {
			head = append(head, i18n.T("Read by id", "Чтение по id"))
		}
		p.Table(head, rows)
	}
	if a.Checked != nil {
		fmt.Fprintln(w)
		notRead := 0
		for _, n := range a.Checked.NotChecked {
			notRead += n
		}
		fmt.Fprintln(w, i18n.Tf("Checked: %d read by id, %d not seen by this key, %d not read.",
			"Проверено: прочитано по id %d, этот ключ не видит %d, не прочитано %d.", a.Checked.Read, a.Checked.WouldClose, notRead))
		if len(a.Would) > 0 {
			rows := make([][]string, len(a.Would))
			for i, e := range a.Would {
				rows[i] = []string{e.Time, e.Tool, what(e), e.ResourceID}
			}
			fmt.Fprintln(w, i18n.T("Not seen by this key now — removed, or no longer seen by it:", "Этот ключ сейчас не видит — удалено или больше не видно ему:"))
			p.Table([]string{i18n.T("Time", "Время"), i18n.T("Tool", "Инструмент"), i18n.T("Resource", "Ресурс"), "ID"}, rows)
		}
		if a.CheckNote != "" {
			fmt.Fprintln(w, a.CheckNote)
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

func newIntentCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "intent",
		Short: i18n.T("Review local receipts for interrupted operations", "Проверять локальные квитанции оборванных операций"),
		Args:  noArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	var noninteractive bool
	closeCmd := &cobra.Command{Use: "close <id>", Short: i18n.T("Close the local review of one receipt in a terminal", "Закрыть разбор одной локальной квитанции в терминале"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		refused := func() error {
			return clerrors.New(clerrors.CodeConfirmationRequired, i18n.T("Closing a receipt requires a new confirmation in a local terminal.", "Для закрытия квитанции необходимо новое подтверждение в локальном терминале."), "")
		}
		if flagYes || noninteractive || flagJSON || flagDryRun || !manualTerminal() {
			return refused()
		}
		if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" ||
			os.Getenv("GITLAB_CI") != "" || os.Getenv("JENKINS_URL") != "" ||
			os.Getenv("TF_BUILD") != "" || os.Getenv("BUILDKITE") != "" ||
			os.Getenv("CIRCLECI") != "" || os.Getenv("TEAMCITY_VERSION") != "" {
			return refused()
		}
		load := func() (*intent.ManualSnapshot, error) {
			root, err := stateDir()
			if err != nil {
				return nil, err
			}
			cfg, err := configOf(scopeOverrides())
			if err != nil {
				return nil, err
			}
			s, err := intent.PreviewManual(root, args[0], cfg.Profile)
			if err != nil {
				return nil, err
			}
			eng := &engine.Engine{Config: cfg}
			if err = tools.CheckManualIntentScope(s.Record, cfg); err == nil {
				err = s.Eligible(eng.Maker(), cfg.ProjectID)
			}
			if err != nil {
				s.Close()
				return nil, err
			}
			return s, nil
		}
		s, err := load()
		if err != nil {
			return err
		}
		defer s.Close()
		marked, err := s.Marked()
		if err != nil {
			return err
		}
		if marked {
			fmt.Fprintln(os.Stderr, i18n.T("This version was already closed locally. The cloud outcome is unknown.", "Разбор этой версии уже закрыт локально. Облачный исход неизвестен."))
			return nil
		}
		in := s.Record
		fmt.Fprintf(os.Stderr, i18n.T("Receipt %q in %q\nTool %q, name %q, target %q, project %q, owner %q (key %q), started %s, write sent.\n", "Квитанция %q в %q\nИнструмент %q, имя %q, цель %q, проект %q, владелец %q (ключ %q), начало %s, запись отправлена.\n"), in.ID, in.Dir, in.Tool, in.Name, in.Target, in.ProjectID, in.Key.ServiceAccountID, config.MaskKeyID(in.Key.KeyID), in.StartedAt.Format(time.RFC3339))
		fmt.Fprintln(os.Stderr, i18n.T("This closes local review only. The cloud outcome is unknown. The resource may exist and incur charges. This does not authorize a retry. Enter the full receipt ID:", "Закрывается только локальный разбор. Облачный исход неизвестен. Ресурс может существовать и тарифицироваться. Повтор не разрешается. Введите полный ID квитанции:"))
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != in.ID {
			return refused()
		}
		if err := s.Apply(load); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, i18n.T("Local review closed. The cloud outcome is unknown.", "Локальный разбор закрыт. Облачный исход неизвестен."))
		return nil
	}}
	closeCmd.Flags().BoolVar(&noninteractive, "non-interactive", false, i18n.T("Disable interactive confirmation", "Отключить интерактивное подтверждение"))
	group.AddCommand(closeCmd)
	return group
}

func manualTerminal() bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
		return false
	}
	// A Windows process attaches to a single console; GetConsoleMode above
	// distinguishes its handles from redirected files or pipes.
	if runtime.GOOS == "windows" {
		return true
	}
	input, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		st, err := f.Stat()
		if err != nil || !os.SameFile(input, st) {
			return false
		}
	}
	return true
}
