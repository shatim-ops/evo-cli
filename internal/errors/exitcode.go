// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"context"
	stderrors "errors"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Process exit codes of the CLI. Scripts rely on them, so the values are
// part of the public contract: never renumber, only add.
const (
	ExitOK                   = 0   // success
	ExitGeneral              = 1   // any other error, including usage errors
	ExitAuth                 = 2   // authentication or configuration problem
	ExitConfirmationRequired = 3   // confirmation not given (or no TTY to ask)
	ExitNotFound             = 4   // requested object does not exist
	ExitAPI                  = 5   // Cloud.ru API returned an error (4xx/5xx other than auth)
	ExitInterrupted          = 130 // interrupted by the user (SIGINT/SIGTERM)
)

// ExitCodesHelp documents the exit codes for the root command's --help,
// in the language of the CLI.
func ExitCodesHelp() string {
	return i18n.T(`Exit codes:
  0    success
  1    general error (including unknown command or flag)
  2    authentication or configuration error
  3    confirmation required (not given, or stdin is not a terminal; use --yes)
  4    object not found
  5    Cloud.ru API error (HTTP 4xx/5xx other than authentication)
  130  interrupted by the user`, `Коды выхода:
  0    успех
  1    прочая ошибка (в том числе неизвестная команда или флаг)
  2    ошибка аутентификации или конфигурации
  3    нужно подтверждение (не дано или stdin не терминал; используйте --yes)
  4    объект не найден
  5    ошибка API Cloud.ru (HTTP 4xx/5xx, кроме аутентификации)
  130  прервано пользователем`)
}

// Reason is the stable symbolic code of err for the "reason" field of a JSON
// error: the Code of the CLIError in the wrap chain ("AUTH_FAILED"),
// "INTERRUPTED" for a cancelled command, "ERROR" for anything else. Unlike
// the message it does not depend on the language.
func Reason(err error) string {
	if err == nil {
		return ""
	}
	if ExitCode(err) == ExitInterrupted {
		return ReasonInterrupted
	}
	var cliErr *CLIError
	if stderrors.As(err, &cliErr) && cliErr.Code != "" {
		return cliErr.Code
	}
	return ReasonGeneral
}

// ExitCode maps an error to the process exit code. It looks through
// wrapping (fmt.Errorf("...: %w")) to find a *CLIError or a context
// cancellation.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if stderrors.Is(err, context.Canceled) {
		return ExitInterrupted
	}
	var cliErr *CLIError
	if !stderrors.As(err, &cliErr) {
		return ExitGeneral
	}
	switch cliErr.Code {
	case CodeAuthRequired, CodeAuthExpired, CodeAuthFailed, CodeConfigInvalid:
		return ExitAuth
	case CodeConfirmationRequired:
		return ExitConfirmationRequired
	case CodeVMNotFound, CodeNotFound:
		return ExitNotFound
	case CodeAPIError, CodeAPIRateLimited:
		return ExitAPI
	}
	return ExitGeneral
}
