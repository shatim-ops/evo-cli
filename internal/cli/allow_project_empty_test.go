// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// Grok audit R5-21 (06.11, fixtures; release plan 1.8, #1095): a value of
// --allow-project that is no project («garbage», «,») closes the server —
// «project … is not allowed» — and an empty one (--allow-project=, a
// template that put an empty variable into the command of an HTTP
// deployment) opened it to every project the key sees, without a word.
// An empty --allow-project is refused at the start, as the others are
// refused at the call: the limit the person meant to set is not dropped.
func TestMCP_EmptyAllowProjectRefused(t *testing.T) {
	for _, args := range [][]string{
		{"mcp", "--allow-project="},
		{"mcp", "--allow-project", ""},
	} {
		code, stdout, stderr := runCLI(t, "", args...)
		if code == 0 || !strings.Contains(stdout+stderr, "--allow-project") {
			t.Errorf("%q: exit %d — an empty --allow-project must refuse the start, naming the flag\n%s\n%s", args, code, stdout, stderr)
		}
	}
}
