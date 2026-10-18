// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// TestMain runs the tests in English: they assert the English texts.
// Tests of the Russian output set the language themselves.
func TestMain(m *testing.M) {
	i18n.SetLang("en")
	os.Exit(m.Run())
}
