// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// docs/auth.md: evo mcp отвечает по-английски при любой локали.
// Ошибки до i18n.SetLang(EN) в runMCP идут на языке CLI.
func TestBreakR4_R4_12_MCPStartupIsEnglish(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })

	for _, args := range [][]string{
		{"mcp", "--auth", "bearer"},
		{"mcp", "--http", "not-an-address"},
	} {
		_, errOut, code := execCLI(t, args...)
		if code == 0 {
			t.Fatalf("%v: exit 0\n%s", args, errOut)
		}
		if hasCyrillic(errOut) {
			t.Errorf("%v: MCP answered in Russian:\n%s", args, errOut)
		}
	}
}
