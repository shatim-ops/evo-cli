// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The texts brief, item 7: «no key» has one hint, NoKeyHint, which names
// evo login beside evo configure and the variables; the error of a
// command without a key carries it, its reason and code unchanged.
func TestNoKeyHint_OnePoint(t *testing.T) {
	prev := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(prev) })
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		hint := NoKeyHint()
		for _, want := range []string{"evo login", "evo configure", "CLOUDRU_KEY_ID", "CLOUDRU_SECRET"} {
			if !strings.Contains(hint, want) {
				t.Errorf("%s: %q has no %s", lang, hint, want)
			}
		}
		var ce *clerrors.CLIError
		if err := ErrNoCredentials(); !errors.As(err, &ce) || ce.Hint != hint || ce.Code != clerrors.CodeAuthRequired {
			t.Errorf("%s: ErrNoCredentials %+v, want the hint %q", lang, ce, hint)
		}
	}
	// A refused key is not «no key»: the hint says the same key will not do
	// and does not send to the variables as if none were set («Риск», #1029).
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		refused := RefusedKeyHint()
		if refused == NoKeyHint() || strings.Contains(refused, "CLOUDRU_KEY_ID") || !strings.Contains(refused, "evo login") {
			t.Errorf("%s: the hint of a refused key %q", lang, refused)
		}
	}
	// No other copy of the hint in the code: the sites call NoKeyHint.
	i18n.SetLang(i18n.RU)
	ru := NoKeyHint()
	i18n.SetLang(i18n.EN)
	en := NoKeyHint()
	root := filepath.Join("..", "..")
	_ = filepath.WalkDir(filepath.Join(root, "internal"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, old := range []string{"Run 'evo configure', or set CLOUDRU_KEY_ID and CLOUDRU_SECRET",
			"Выполните 'evo configure' или задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET",
			"run 'evo configure' or set CLOUDRU_KEY_ID and CLOUDRU_SECRET",
			"выполните 'evo configure' или задайте CLOUDRU_KEY_ID и CLOUDRU_SECRET"} {
			if strings.Contains(string(b), old) {
				t.Errorf("%s: a copy of the «no key» hint, not NoKeyHint (%q; %q)", p, en, ru)
			}
		}
		return nil
	})
}

// Plan 1.8, #1087, texts (audit of 06.11; «Риск», conclusion 35.5): with no
// key or a refused one, the first advice is evo login, marked as a sign-in
// in the browser that needs a person — the agent reads it too; with no key
// the hint says the variables come before the profile.
func TestKeyHints_LoginFirstAPersonNeeded(t *testing.T) {
	prev := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(prev) })
	i18n.SetLang(i18n.RU)
	for name, hint := range map[string]string{"no key": NoKeyHint(), "refused": RefusedKeyHint(), "401": rejectedKeyHint()} {
		if !strings.HasPrefix(strings.TrimPrefix(hint, "Повтор тем же ключом не поможет. Новый ключ — "), "Выполните evo login (вход через браузер, нужен человек)") &&
			!strings.Contains(hint, "Новый ключ — evo login (вход через браузер, нужен человек)") {
			t.Errorf("%s: %q does not lead with evo login and a person needed", name, hint)
		}
	}
	if !strings.Contains(NoKeyHint(), "важнее профиля") || !strings.Contains(rejectedKeyHint(), "иначе из профиля") {
		t.Errorf("the hints do not say which comes first, the variables or the profile")
	}
}
