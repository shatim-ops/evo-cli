// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Имена и описания ресурсов приходят из облака: ESC-последовательность в
// имени ВМ перекрашивала или стирала терминал, табуляция ломала колонки,
// невалидный UTF-8 уходил в терминал как есть.
func TestTable_SanitizesCells(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModePlain, false)
	p.Table([]string{"ID", "NAME"}, [][]string{
		{"vm-1", "evil\x1b[2J\x1b[31mname\tx\ry\nz\x07\x9b"},
		{"vm-2", "ok \xff\xfe имя"},
	})
	got := buf.String()
	for _, bad := range []string{"\x1b", "\r", "\x07", "\t", "\xff", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("table output contains %q:\n%q", bad, got)
		}
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("a cell broke the table into %d lines:\n%s", len(lines), got)
	}
	if !strings.Contains(got, "имя") || !strings.Contains(got, "name") {
		t.Errorf("printable text lost:\n%s", got)
	}
	// Цвет заголовков — наш, его санитайзер не трогает.
	buf.Reset()
	NewWithWriter(&buf, ModeText, true).Table([]string{"ID"}, [][]string{{"x"}})
	if !strings.Contains(buf.String(), colorBold) {
		t.Errorf("header color lost: %q", buf.String())
	}
}

// С --json предупреждения не пропадают и не ломают stderr: строка JSON
// {"warning": "..."}. Раньше Warning в режиме JSON молчал — «Could not open
// ports …» и «VM … left running» после deploy --json терялись.
func TestWarning_JSON(t *testing.T) {
	var out, errOut bytes.Buffer
	p := NewWithWriters(&out, &errOut, ModeJSON, false)
	p.Warning("ports 80 not opened")
	if out.Len() != 0 {
		t.Errorf("stdout: %q", out.String())
	}
	var w struct {
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal(errOut.Bytes(), &w); err != nil || w.Warning != "ports 80 not opened" {
		t.Errorf("stderr %q: %v", errOut.String(), err)
	}
}

// NO_COLOR по no-color.org: отключает цвет, когда задан и не пуст.
func TestColorAllowed_EmptyNoColorKeepsColor(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	if !colorAllowed(false) {
		t.Error("NO_COLOR= (empty) disabled color")
	}
	t.Setenv("NO_COLOR", "0")
	if colorAllowed(false) {
		t.Error("NO_COLOR=0 must disable color")
	}
}

// Цвет сообщений об ошибках решается по stderr, а не по stdout:
// «evo … 2>err.log» в терминале писал ANSI-коды в файл.
func TestNew_ErrorColorFollowsStderr(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm")
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stderr
	os.Stderr = f
	p := New(false, false)
	os.Stderr = old
	if p.errColor {
		t.Error("stderr is a file: no color for errors")
	}
	p.Errorf("boom")
	data, _ := os.ReadFile(f.Name())
	if strings.Contains(string(data), "\x1b") {
		t.Errorf("ANSI in a redirected stderr: %q", data)
	}
}
