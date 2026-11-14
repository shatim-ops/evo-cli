// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// englishSentence is an English sentence evo wrote: three Latin words in a
// row, the names of commands and arguments aside.
var englishSentence = regexp.MustCompile(`\b[A-Za-z]{3,} [a-z]{2,} [a-z]{2,}\b`)

// russianText tells a text of the Russian CLI: no English sentence of
// evo's once the names it may keep (D6, #1834) are taken out.
func russianText(s string, keep ...string) bool {
	for _, k := range keep {
		s = strings.ReplaceAll(s, k, "")
	}
	return !englishSentence.MatchString(s)
}

// The texts of the answers of the VM and deploy tools speak the language
// of the CLI: Russian by default, English over MCP (D6; Астра #1835; release
// plan 1.9, «Язык CLI: весь перевод — в 1.9»).
func TestLangA_VMAndDeployAnswersSpeakTheCLILanguage(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	if s := passwordOnce(); !russianText(s) {
		t.Errorf("passwordOnce: %q", s)
	}
	out := provisionCreating(map[string]any{"name": "web"}, faultID(0x41), "web", "creating", "pw", 10, "")
	if s, _ := out["next"].(string); !russianText(s, "evo vm create", "evo vm get") {
		t.Errorf("provision, creating: %q", s)
	}
}
