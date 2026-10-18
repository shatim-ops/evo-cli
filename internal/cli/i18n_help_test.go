// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// helpTexts collects Long and Example of every command, by command path,
// in the given language.
func helpTexts(t *testing.T, lang string) map[string][2]string {
	t.Helper()
	i18n.SetLang(lang)
	t.Cleanup(func() { i18n.SetLang("en") })
	texts := map[string][2]string{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		texts[c.CommandPath()] = [2]string{c.Long, c.Example}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	root := newRootCmd()
	// cobra adds help and completion only at Execute in English; the
	// Russian build adds them up front to translate them.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	walk(root)
	return texts
}

// English help must not lag behind Russian: a command with a Russian Long
// or Example has an English one too. i18n.T("", ru) gave English readers
// (EVO_LANG=en) a deploy --help without a description or examples.
func TestHelp_EnglishNotEmptyWhereRussianIs(t *testing.T) {
	ru := helpTexts(t, "ru")
	en := helpTexts(t, "en")
	for path, r := range ru {
		e, ok := en[path]
		if !ok {
			t.Errorf("%s: no such command in English", path)
			continue
		}
		if r[0] != "" && e[0] == "" {
			t.Errorf("%s: Russian Long, empty English Long", path)
		}
		if r[1] != "" && e[1] == "" {
			t.Errorf("%s: Russian Example, empty English Example", path)
		}
	}
}
