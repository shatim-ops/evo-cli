// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/tools"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newIntentCmd() *cobra.Command {
	group := &cobra.Command{Use: "intent", Short: i18n.T("Review local receipts for interrupted operations", "Проверять локальные квитанции оборванных операций")}
	var noninteractive bool
	closeCmd := &cobra.Command{Use: "close <id>", Short: i18n.T("Close the local review of one receipt in a terminal", "Закрыть разбор одной локальной квитанции в терминале"), Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		refused := func() error {
			return clerrors.New(clerrors.CodeConfirmationRequired, i18n.T("Closing a receipt requires a new confirmation in a local terminal.", "Для закрытия квитанции необходимо новое подтверждение в локальном терминале."), "")
		}
		if flagYes || noninteractive || flagJSON || flagDryRun || !manualTerminal() {
			return refused()
		}
		for _, k := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "JENKINS_URL", "TF_BUILD", "BUILDKITE", "CIRCLECI", "TEAMCITY_VERSION"} {
			if os.Getenv(k) != "" {
				return refused()
			}
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
		fmt.Fprintf(os.Stderr, i18n.T("Receipt %s in %q\nTool %q, name %q, target %q, project %q, owner %q (key %q), started %s, write sent.\n", "Квитанция %s в %q\nИнструмент %q, имя %q, цель %q, проект %q, владелец %q (ключ %q), начало %s, запись отправлена.\n"), in.ID, in.Dir, in.Tool, in.Name, in.Target, in.ProjectID, in.Key.ServiceAccountID, in.Key.KeyID, in.StartedAt.Format(time.RFC3339))
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
