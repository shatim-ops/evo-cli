// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"os"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// TestMain runs the tests in English, as 'evo mcp' runs the server: tool
// results, the engine's log in them and errors are English for agents.
func TestMain(m *testing.M) {
	i18n.SetLang("en")
	os.Exit(m.Run())
}
