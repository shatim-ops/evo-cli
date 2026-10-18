// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package confirm

import (
	"context"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func russian(t *testing.T) {
	t.Helper()
	i18n.SetLang("ru")
	t.Cleanup(func() { i18n.SetLang("en") })
}

// На терминале по-русски: вопрос понятен без знания английского, «да» и
// «д» (в любом регистре) — согласие, как и y/yes; остальное — отказ с кодом 3.
func TestAsk_Russian(t *testing.T) {
	russian(t)
	for _, in := range []string{"да\n", "Да\n", "ДА\n", "д\n", "y\n", "yes\n", "  да  \n"} {
		r := ask(t, context.Background(), Options{}, in, true)
		if !r.ok || r.err != nil {
			t.Errorf("answer %q: ok=%v err=%v, want consent", in, r.ok, r.err)
		}
		if !strings.Contains(r.stderr, "План: "+testPreview.Summary+"\n") ||
			!strings.Contains(r.stderr, "Продолжить? (y/д — да, Enter — нет) ") {
			t.Errorf("answer %q: prompt: %q", in, r.stderr)
		}
	}
	for _, in := range []string{"\n", "нет\n", "н\n", "n\n", "давай\n"} {
		r := ask(t, context.Background(), Options{}, in, true)
		if r.ok || clerrors.ExitCode(r.err) != clerrors.ExitConfirmationRequired {
			t.Errorf("answer %q: ok=%v err=%v, want refusal with exit 3", in, r.ok, r.err)
		}
	}
}

func TestAsk_RussianNoTTY(t *testing.T) {
	russian(t)
	r := ask(t, context.Background(), Options{}, "", false)
	if clerrors.ExitCode(r.err) != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit code = %d, want 3", clerrors.ExitCode(r.err))
	}
	var ce *clerrors.CLIError
	if !asCLI(r.err, &ce) || ce.Message != "deploy не выполняется: нужно подтверждение, а stdin — не терминал" ||
		!strings.Contains(ce.Hint, "Повторите с --yes") {
		t.Errorf("error = %#v", r.err)
	}
	if strings.Contains(r.stderr, "Продолжить?") {
		t.Errorf("no prompt without a terminal: %q", r.stderr)
	}
}

func TestAsk_RussianDryRunAndYes(t *testing.T) {
	russian(t)
	r := ask(t, context.Background(), Options{DryRun: true}, "", false)
	if !strings.HasPrefix(r.stdout, "Пробный запуск, ничего не меняется. План: "+testPreview.Summary+"\n") {
		t.Errorf("dry run: %q", r.stdout)
	}
	r = ask(t, context.Background(), Options{Yes: true}, "", false)
	if r.stderr != "Подтверждено: "+testPreview.Summary+"\n" {
		t.Errorf("--yes: %q", r.stderr)
	}
}

func asCLI(err error, target **clerrors.CLIError) bool {
	ce, ok := err.(*clerrors.CLIError)
	if ok {
		*target = ce
	}
	return ok
}
