// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// evo ca logs prints a log, not a table: each line whole, with its time,
// oldest first as the tool ordered them; the note goes under the lines; a
// control character from the container does not reach the terminal.
func TestRenderLogLines(t *testing.T) {
	if _, c, _ := findLeaf(t, "ca", "logs"); c.render == nil {
		t.Fatal("ca logs has no log view")
	}
	render := func(text string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := renderLogLines(output.NewWithWriter(&buf, output.ModePlain, false), &buf, text); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	long := strings.Repeat("x", 120)
	got := render(`{"logs":[{"timestamp":"2026-10-20T10:00:00Z","message":"started on :8080"},` +
		`{"timestamp":"2026-10-20T10:00:01Z","message":"` + long + `\u001b[31m"},{"reason":"Scaled"}],` +
		`"truncated":true,"note":"Read 500 lines"}`)
	want := "2026-10-20T10:00:00Z started on :8080\n" +
		"2026-10-20T10:00:01Z " + long + "?[31m\n" +
		`{"reason":"Scaled"}` + "\n" +
		"note: Read 500 lines\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := render(`{"logs":[]}`); got != "No log lines.\n" {
		t.Errorf("empty: %q", got)
	}
	if got := render(`{"logs":[],"note":"No app log lines."}`); got != "note: No app log lines.\n" {
		t.Errorf("empty with a note: %q", got)
	}
	if got := render(`{"other":1}`); got != `{"other":1}`+"\n" {
		t.Errorf("not a log: %q", got)
	}
}
