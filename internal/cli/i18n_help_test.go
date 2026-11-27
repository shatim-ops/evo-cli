// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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

// toolsCallRE is a tool named the CLI way: 'evo tools call snat_create'
// for a tool without a command of its own.
var toolsCallRE = regexp.MustCompile(`evo tools (call|describe) [a-z0-9_]+`)

// russianHelpExceptions are commands whose help may stay English, with the
// reason. Empty: every command, hidden ones too, has Russian help. A new
// entry needs a reason a reviewer accepts (a command for machines only).
var russianHelpExceptions = map[string]string{}

// Ignore only a delimited quotation attributed to a service. The help on
// either side of the quotation is still evo's own prose.
var serviceHelpQuote = regexp.MustCompile(`(?i)(?:ответ (?:API|сервиса|платформы|[45][0-9]{2})|(?:API|сервис|платформа) отвечает)[: \t]*(?:«[^»\n]*»|'[^'\n]*'|"[^"\n]*")`)

// Most help sentences have function words. A short instruction such as
// "Select available profiles" also has a sentence-case verb followed by
// ordinary lower-case words. Command paths and product names can stay Latin.
var helpInstruction = regexp.MustCompile(`\b[A-Z][a-z]+[ \t]+[a-z]+[ \t]+[a-z]+\b`)

var englishStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "to": true, "of": true, "is": true, "are": true, "and": true,
	"for": true, "with": true, "by": true, "in": true, "on": true, "or": true, "it": true, "from": true,
	"this": true, "that": true, "be": true, "as": true, "its": true, "when": true, "only": true,
}

func englishProse(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = serviceHelpQuote.ReplaceAllString(line, " ")
		// Count words within a Latin passage. A Russian introduction or
		// explanation on the same line cannot hide the passage.
		for _, passage := range strings.FieldsFunc(line, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) {
			words, stop := 0, 0
			for _, w := range strings.Fields(passage) {
				w = strings.ToLower(strings.Trim(w, ".,:;()'\"`«»"))
				if w == "" || strings.IndexFunc(w, func(r rune) bool { return !unicode.IsLetter(r) }) >= 0 {
					continue
				}
				words++
				if englishStopWords[w] {
					stop++
				}
			}
			if (words >= 4 && stop >= 2) || helpInstruction.MatchString(passage) {
				return strings.Trim(passage, " \t.,:;!?«»")
			}
		}
	}
	return ""
}

func TestHelp_EnglishProseAndServiceQuotes(t *testing.T) {
	for _, tc := range []struct {
		text string
		bad  bool
	}{
		{"Описание. Выберите профиль для продолжения. Ответ API: «Choose a profile to continue».", false},
		{"Описание. Choose a profile to continue. Ответ API: «Choose a profile to continue».", true},
		{"API отвечает 'Choose a profile to continue'. Select available profiles.", true},
		{"Подсказка evo: «Choose a profile to continue».", true},
		{"Команда 'evo tools list' выводит список.", false},
	} {
		if got := englishProse(tc.text); (got != "") != tc.bad {
			t.Errorf("%q: English passage %q, want finding %t", tc.text, got, tc.bad)
		}
	}
}

// productNames reports whether title is a list of product names of
// Cloud.ru: each one an "Evolution …" product or one of the names the
// documentation gives without the prefix.
func productNames(title string) bool {
	for _, name := range strings.Split(strings.TrimSuffix(title, ":"), ", ") {
		if !strings.HasPrefix(name, "Evolution ") && name != "Secret Management" && name != "Certificate Manager" {
			return false
		}
	}
	return true
}

func hasCyrillic(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) >= 0
}

// cobraEnglish are cobra's and pflag's own English words in a help: the
// Russian usage template (help_ru.go) replaces them all.
var cobraEnglish = []string{"Usage:", "Aliases:", "Examples:", "Available Commands:", "Additional Commands:",
	"Flags:", "Global Flags:", "Additional help topics:", `Use "`, "help for ", "[flags]", "[command]", "(default "}

// The whole command tree is Russian in Russian: every command (hidden ones
// too) has a Russian summary, a Russian long help where the English one is
// not empty, examples and flags without English prose, and the usage around
// them has no cobra English. A new command or flag without its Russian text
// fails here (review r5 U3).
func TestHelp_RussianEverywhere(t *testing.T) {
	en := map[string]*cobra.Command{}
	i18n.SetLang(i18n.EN)
	enRoot := newRootCmd()
	enRoot.InitDefaultHelpCmd()
	enRoot.InitDefaultCompletionCmd()
	var walkEN func(c *cobra.Command)
	walkEN = func(c *cobra.Command) {
		en[c.CommandPath()] = c
		for _, sub := range c.Commands() {
			walkEN(sub)
		}
	}
	walkEN(enRoot)

	russian(t)
	root := newRootCmd()
	// A group may be titled by products of Cloud.ru under their official
	// names (#597, item 30), Latin by design; any other title is Russian.
	for _, g := range root.Groups() {
		if (!hasCyrillic(g.Title) && !productNames(g.Title)) || englishProse(g.Title) != "" {
			t.Errorf("group %s: English title %q", g.ID, g.Title)
		}
	}
	commands, flags := 0, 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		path := c.CommandPath()
		if reason, ok := russianHelpExceptions[path]; ok {
			t.Logf("%s: skipped (%s)", path, reason)
			return
		}
		commands++
		if !hasCyrillic(c.Short) {
			t.Errorf("%s: no Russian summary: %q", path, c.Short)
		}
		if e := en[path]; e != nil && e.Long != "" && !hasCyrillic(c.Long) {
			t.Errorf("%s: English long help without a Russian one", path)
		}
		for what, text := range map[string]string{"summary": c.Short, "long help": c.Long, "example": c.Example} {
			if line := englishProse(text); line != "" {
				t.Errorf("%s: English in the %s: %q", path, what, line)
			}
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			flags++
			if !hasCyrillic(f.Usage) {
				t.Errorf("%s --%s: no Russian help: %q", path, f.Name, f.Usage)
			} else if line := englishProse(f.Usage); line != "" {
				t.Errorf("%s --%s: English in the help: %q", path, f.Name, line)
			}
		})
		usage := c.UsageString()
		for _, w := range cobraEnglish {
			if strings.Contains(usage, w) {
				t.Errorf("%s: the usage has cobra's English %q:\n%s", path, w, usage)
			}
		}
	}
	walk(root)
	t.Logf("%d commands, %d flags checked", commands, flags)
	if commands < 100 {
		t.Errorf("only %d commands walked: the tree was not built", commands)
	}
	for path := range russianHelpExceptions {
		if en[path] == nil {
			t.Errorf("russianHelpExceptions: no command %s", path)
		}
	}
}
