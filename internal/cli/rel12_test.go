// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// REL-12: ошибки разбора флагов evo mcp (cobra разбирает их до runMCP) —
// по-английски и при EVO_LANG=ru; справка evo mcp и ошибки других команд —
// на языке CLI, как раньше.
func TestRel12_MCPFlagErrorsEnglish(t *testing.T) {
	setupCLI(t)
	for _, args := range [][]string{
		{"mcp", "--auth"},
		{"mcp", "--bogus"},
		{"mcp", "--toolsets"},
		{"--json", "mcp", "--auth"},
	} {
		i18n.SetLang(i18n.RU)
		code, _, stderr := runCLI(t, "", args...)
		if code != clerrors.ExitGeneral {
			t.Errorf("%v: exit %d, want %d\n%s", args, code, clerrors.ExitGeneral, stderr)
		}
		if hasCyrillic(stderr) {
			t.Errorf("%v: answered in Russian:\n%s", args, stderr)
		}
	}

	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	if code, stdout, stderr := runCLI(t, "", "mcp", "--help"); code != 0 || !hasCyrillic(stdout+stderr) {
		t.Errorf("mcp --help: exit %d, want the help in Russian:\n%s%s", code, stdout, stderr)
	}
	i18n.SetLang(i18n.RU)
	if _, _, stderr := runCLI(t, "", "vm", "list", "--bogus"); !hasCyrillic(stderr) || !strings.Contains(stderr, "--bogus") {
		t.Errorf("vm list --bogus: want a Russian error:\n%s", stderr)
	}
}
