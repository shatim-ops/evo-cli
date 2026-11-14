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

// A Russian text of the CLI names the command where the English one names
// the tool (D6, #1834): one point, cliCommand.
func TestCLICommand_NamesTheCommandOfTheTool(t *testing.T) {
	for tool, want := range map[string]string{
		"disk_attach": "evo disk attach",
		"deploy":      "evo deploy",
		"lb_get":      "evo lb get",
		"no_such":     "no_such",
	} {
		if got := cliCommand(tool); got != want {
			t.Errorf("cliCommand(%s) = %q, want %q", tool, got, want)
		}
	}
}

// The next step of a disk at the time limit speaks the language of the
// CLI and names the command; over MCP (English) it names the tool, and the
// state of the cloud is quoted in both (Астра #1835).
func TestLangA_DiskAtLimitSpeaksTheCLILanguage(t *testing.T) {
	was := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(was) })
	latin := regexp.MustCompile(`\b[A-Za-z]{3,} [a-z]{2,} [a-z]{2,}\b`)
	i18n.SetLang(i18n.RU)
	ru, _ := diskAtLimitAnswer(&diskAtLimit{state: "creating\nX"}, faultDisk, i18n.T("it is attached once available.", "диск подключится, когда станет available."), "disk_attach", nil)["next"].(string)
	if strings.Contains(ru, "\n") || !strings.Contains(ru, "evo disk attach") || latin.MatchString(strings.ReplaceAll(ru, "evo disk attach", "")) {
		t.Errorf("Russian: %q", ru)
	}
	i18n.SetLang(i18n.EN)
	en, _ := diskAtLimitAnswer(&diskAtLimit{state: "creating"}, faultDisk, "it is attached once available.", "disk_attach", nil)["next"].(string)
	if !strings.Contains(en, "Call disk_attach") {
		t.Errorf("English: %q", en)
	}
}
