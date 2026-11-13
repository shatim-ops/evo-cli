// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// TestMain runs the tests in English: they assert the English texts.
// Tests of the Russian output set the language themselves.
func TestMain(m *testing.M) {
	i18n.SetLang("en")
	// The check of port 22 of deploy_apply gets no answer without a
	// connect: the fake clouds' documentation addresses are not dialled,
	// whatever network the tests run in (#1707).
	deployplan.DialSSH = func(context.Context, string, time.Duration) error { return sshNoAnswer{} }
	os.Exit(m.Run())
}
