// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package i18n picks the language of the human-facing CLI text.
//
// The CLI speaks Russian by default; EVO_LANG=en (or en_US and the like)
// switches it to English. Only EVO_LANG counts: LANG and LC_* describe the
// terminal, not the reader of the messages. The MCP server (evo mcp) sets
// English for the whole process: tool descriptions and results are read by
// agents and stay English.
//
// There is no catalog: every call site keeps both texts, English first, so
// EVO_LANG=en gives exactly the English output.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// Language codes returned by Lang.
const (
	RU = "ru"
	EN = "en"
)

// english is the current language: true for English, false for Russian.
var english atomic.Bool

func init() {
	SetLang(os.Getenv("EVO_LANG"))
}

// isEnglish reports whether lang names English: "en", "en_US",
// "en-GB", "en_US.UTF-8" (any case).
func isEnglish(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	if l == EN {
		return true
	}
	for _, sep := range []string{"_", "-", "."} {
		if strings.HasPrefix(l, EN+sep) {
			return true
		}
	}
	return false
}

// Unsupported reports a non-empty language that is neither English nor
// Russian ("ru", "ru_RU", …): the CLI falls back to Russian and says so.
func Unsupported(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	if l == "" || isEnglish(l) || l == RU {
		return false
	}
	for _, sep := range []string{"_", "-", "."} {
		if strings.HasPrefix(l, RU+sep) {
			return false
		}
	}
	return true
}

// SetLang sets the language for the whole process: English for "en" and its
// regional forms, Russian for anything else (including "").
func SetLang(lang string) {
	english.Store(isEnglish(lang))
}

// Lang returns the current language, RU or EN.
func Lang() string {
	if english.Load() {
		return EN
	}
	return RU
}

// T returns en or ru, depending on the current language.
func T(en, ru string) string {
	if english.Load() {
		return en
	}
	return ru
}

// Tf formats the text of the current language with fmt.Sprintf.
func Tf(en, ru string, args ...any) string {
	return fmt.Sprintf(T(en, ru), args...)
}

// Plural picks the Russian word form for n: one (1, 21: «проект»), few
// (2–4, 22–24: «проекта») or many (0, 5–20, 25: «проектов»).
func Plural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	switch {
	case n%10 == 1 && n%100 != 11:
		return one
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return few
	}
	return many
}
