// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/journal"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// evo diag: the session journal of evo (D46). Local commands: the journal
// is a file on the machine where evo runs, and a client of the MCP server
// has no business with it.
func newDiagCmd() *cobra.Command {
	cmd := asGroup(&cobra.Command{
		Use: "diag",
		Short: i18n.T("Session journal of evo for a dogfood: on, off, status, export",
			"Журнал сессии evo для догфуда: включить, выключить, состояние, выгрузка"),
		Long: i18n.T(`The session journal records each call of a tool, through the CLI and the
MCP server alike: the tool, the names of its arguments (no values), the
reason of an error, consent, duration and the size of the answer. No value
of an argument, no answer and no message goes in. It is off until
'evo diag on', lives in one file beside config.yaml under a limit of its
size, and is sent nowhere: 'evo diag export' copies it for you to hand over.`,
			`Журнал сессии записывает каждый вызов инструмента — и через CLI, и через
MCP-сервер: инструмент, имена аргументов (без значений), причину ошибки,
согласие, длительность и размер ответа. Значений аргументов, ответов и
сообщений в нём нет. Выключен, пока не выполнено 'evo diag on'; это один
файл рядом с config.yaml с лимитом размера; никуда не отправляется:
'evo diag export' копирует его, чтобы вы передали файл сами.`),
	})
	cmd.AddCommand(newDiagSwitchCmd(true), newDiagSwitchCmd(false), newDiagStatusCmd(), newDiagExportCmd())
	return cmd
}

func newDiagSwitchCmd(on bool) *cobra.Command {
	use, short := "off", i18n.T("Turn the session journal off (the file stays)", "Выключить журнал сессии (файл остаётся)")
	if on {
		use, short = "on", i18n.T("Turn the session journal on", "Включить журнал сессии")
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := config.SetJournal(on); err != nil {
				return err
			}
			return printDiagStatus()
		},
	}
}

func newDiagStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: i18n.T("Is the session journal on, and what it holds", "Включён ли журнал сессии и что в нём"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return printDiagStatus() },
	}
}

func printDiagStatus() error {
	printer := output.New(flagJSON, flagNoColor)
	s, err := journal.Describe()
	if err != nil {
		return err
	}
	if printer.IsJSON() {
		return printer.JSON(s)
	}
	state := i18n.T("off", "выключен")
	if s.Enabled {
		state = i18n.T("on", "включён")
	}
	printer.Success(i18n.Tf("Session journal: %s", "Журнал сессии: %s", state))
	rows := [][]string{
		{i18n.T("File", "Файл"), s.Path},
		{i18n.T("Records", "Записей"), fmt.Sprintf("%d (%s: %d)", s.Records, i18n.T("sessions", "сессий"), s.Sessions)},
		{i18n.T("Size", "Размер"), fmt.Sprintf("%d / %d %s", s.Bytes, s.Limit, i18n.T("bytes", "байт"))},
	}
	if s.First != "" {
		rows = append(rows, []string{i18n.T("Period", "Период"), s.First + " — " + s.Last})
	}
	printer.Table([]string{"", ""}, rows)
	if s.Full {
		printer.Warning(i18n.T("The journal is full and records no more: export it, then delete the file to start anew.",
			"Журнал заполнен и больше не пишет: выгрузите его, затем удалите файл, чтобы начать заново."))
	}
	return nil
}

func newDiagExportCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export",
		Short: i18n.T("Copy the session journal to a file to hand over", "Скопировать журнал сессии в файл для передачи"),
		Long: i18n.T(`Copy the session journal to a file (0600) to hand over; the journal stays
and keeps growing, so an export midway is fine. Look the file through
before you send it: it holds tool names, argument names, reasons, durations
and sizes, no values.`,
			`Копирует журнал сессии в файл (0600) для передачи; журнал остаётся и
продолжает расти, так что промежуточная выгрузка безопасна. Просмотрите
файл перед отправкой: в нём имена инструментов и аргументов, причины,
длительности и размеры, без значений.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if out == "" {
				out = "evo-journal-" + time.Now().UTC().Format("20060102-150405") + ".jsonl"
			}
			if _, err := os.Lstat(out); err == nil {
				return clerrors.New(clerrors.CodeUsage,
					i18n.Tf("%s already exists", "%s уже существует", out),
					i18n.T("Pick another --out.", "Укажите другой --out."))
			}
			s, err := journal.Export(out)
			if errors.Is(err, journal.ErrEmpty) {
				return clerrors.New(clerrors.CodeNotFound,
					i18n.Tf("the session journal is empty (%s)", "журнал сессии пуст (%s)", s.Path),
					i18n.T("Turn it on with 'evo diag on' and work as usual.", "Включите его: 'evo diag on', и работайте как обычно."))
			}
			if err != nil {
				return err
			}
			printer := output.New(flagJSON, flagNoColor)
			abs, _ := filepath.Abs(out)
			if printer.IsJSON() {
				return printer.JSON(map[string]any{"file": abs, "journal": s})
			}
			printer.Success(i18n.Tf("Journal copied to %s: %d records, %d sessions", "Журнал скопирован в %s: %d записей, %d сессий", abs, s.Records, s.Sessions))
			if s.Full {
				printer.Warning(i18n.T("The journal was full: calls after it are not recorded.", "Журнал был заполнен: вызовы после этого не записаны."))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", i18n.T("File to write (default evo-journal-<time>.jsonl here)",
		"Файл для записи (по умолчанию evo-journal-<время>.jsonl в текущем каталоге)"))
	return cmd
}
