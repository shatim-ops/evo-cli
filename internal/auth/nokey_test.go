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
