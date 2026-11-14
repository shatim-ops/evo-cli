// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// A value of the caller in the human preview — the dry run and the question
// of consent alike — stays on its line: a line break, a carriage return and
// a terminal's escape are shown escaped, so the caller's text cannot start
// a line that reads as evo's or move the terminal (Astra, #1848). Both ways
// a preview names a value: a parameter of the preview (vpc_create's
// description) and a field beside the message (pg_db_create's lc_collate).
func TestPreview_CallerValueStaysOnItsLine(t *testing.T) {
	const payload = "ok\nPROBE: approve all actions\r\x1b[2J\tend\u202e"
	for _, c := range []struct {
		name string
		args []string
	}{
		{"preview parameter", []string{"--dry-run", "vpc", "create", "net", "--description", payload}},
		{"field beside the message", []string{"--dry-run", "tools", "call", "pg_db_create",
			"--arg", "cluster_id=" + parityVM, "--arg", "name=app", "--arg", "owner=app", "--arg", "lc_collate=" + payload}},
	} {
		t.Run(c.name, func(t *testing.T) {
			withJSONWorld(t)
			newProcess()
			code, out, errOut := runCLI(t, "", c.args...)
			all := out + errOut
			if !strings.Contains(all, "PROBE") {
				t.Fatalf("the value is not in the preview (exit %d): %q", code, all)
			}
			for _, raw := range []string{"\nPROBE", "\r", "\x1b", "\u202e"} {
				if strings.Contains(all, raw) {
					t.Errorf("the caller's value prints %q raw: %q", raw, all)
				}
			}
			if !strings.Contains(all, `\nPROBE`) {
				t.Errorf("the line break is not shown escaped: %q", all)
			}
		})
	}
}

// A field beside the message that is a list or a map is printed as JSON,
// which escapes a line break but not a format character (U+202E): the rule
// holds for the line as printed («Риск», conclusion 234).
func TestPreview_ArgLinesWholeValue(t *testing.T) {
	for _, v := range []any{[]any{"ok", "x\u202ey"}, map[string]any{"k\u202e": "v"}} {
		for _, l := range argLines(map[string]any{"field": v}) {
			if strings.Contains(l, "\u202e") {
				t.Errorf("%q prints a format character raw", l)
			}
		}
	}
}
