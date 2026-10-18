// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestStep_TTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeText, true)

	p.Step(1, 5, "Authenticating...")
	got := buf.String()

	if !strings.Contains(got, "==> [1/5] Authenticating...") {
		t.Errorf("expected step message, got: %s", got)
	}
	if !strings.Contains(got, colorBold) {
		t.Errorf("expected bold color code in TTY mode, got: %s", got)
	}
	if !strings.Contains(got, colorBlue) {
		t.Errorf("expected blue color code in TTY mode, got: %s", got)
	}
	if !strings.Contains(got, colorReset) {
		t.Errorf("expected reset color code in TTY mode, got: %s", got)
	}
}

func TestStep_Plain(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModePlain, false)

	p.Step(1, 5, "Authenticating...")
	got := buf.String()

	if !strings.Contains(got, "==> [1/5] Authenticating...") {
		t.Errorf("expected step message, got: %s", got)
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("expected no ANSI codes in plain mode, got: %s", got)
	}
}

func TestStep_JSON(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeJSON, false)

	p.Step(1, 5, "Authenticating...")

	if buf.Len() != 0 {
		t.Errorf("expected no output in JSON mode, got: %s", buf.String())
	}
}

func TestSuccess(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeText, true)

	p.Success("VM created successfully")
	got := buf.String()

	if !strings.Contains(got, "VM created successfully") {
		t.Errorf("expected success message, got: %s", got)
	}
	if !strings.Contains(got, colorGreen) {
		t.Errorf("expected green color code, got: %s", got)
	}
}

func TestSuccess_Plain(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModePlain, false)

	p.Success("VM created successfully")
	got := buf.String()

	if !strings.Contains(got, "VM created successfully") {
		t.Errorf("expected success message, got: %s", got)
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("expected no ANSI codes in plain mode, got: %s", got)
	}
}

func TestSuccess_JSON(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeJSON, false)

	p.Success("VM created successfully")

	if buf.Len() != 0 {
		t.Errorf("expected no output in JSON mode for Success, got: %s", buf.String())
	}
}

func TestTable(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModePlain, false)

	headers := []string{"NAME", "STATUS", "IP"}
	rows := [][]string{
		{"web-1", "RUNNING", "10.0.0.1"},
		{"db-1", "STOPPED", "10.0.0.2"},
	}

	p.Table(headers, rows)
	got := buf.String()

	if !strings.Contains(got, "NAME") {
		t.Errorf("expected header NAME, got: %s", got)
	}
	if !strings.Contains(got, "web-1") {
		t.Errorf("expected row data web-1, got: %s", got)
	}
	if !strings.Contains(got, "db-1") {
		t.Errorf("expected row data db-1, got: %s", got)
	}

	// Check alignment: columns should be separated by spaces (tabwriter)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (header + 2 rows), got %d", len(lines))
	}
}

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeJSON, false)

	data := map[string]string{"name": "web-1", "status": "RUNNING"}
	err := p.JSON(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\nGot: %s", err, buf.String())
	}
	if result["name"] != "web-1" {
		t.Errorf("expected name=web-1, got %s", result["name"])
	}

	// Check indentation (pretty print)
	if !strings.Contains(buf.String(), "  ") {
		t.Error("expected indented JSON output")
	}
}

func TestNDJSON(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeJSON, false)

	data := map[string]string{"name": "web-1"}
	err := p.NDJSON(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()

	// NDJSON should be a single line ending with newline
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected single line NDJSON, got %d lines: %s", len(lines), got)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &result); err != nil {
		t.Fatalf("NDJSON line is not valid JSON: %v", err)
	}
}

func TestColorEnabled_NoColor(t *testing.T) {
	// Set NO_COLOR env var
	t.Setenv("NO_COLOR", "1")

	if ColorEnabled(false) {
		t.Error("expected color disabled when NO_COLOR is set")
	}
}

func TestColorEnabled_ExplicitNoColor(t *testing.T) {
	if ColorEnabled(true) {
		t.Error("expected color disabled when noColor flag is true")
	}
}

func TestColorEnabled_DumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	// Also unset NO_COLOR to isolate the TERM=dumb check
	os.Unsetenv("NO_COLOR")

	if ColorEnabled(false) {
		t.Error("expected color disabled when TERM=dumb")
	}
}

func TestStepDone(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeText, true)

	p.StepDone("completed in 2.3s")
	got := buf.String()

	if !strings.Contains(got, "completed in 2.3s") {
		t.Errorf("expected detail message, got: %s", got)
	}
	if !strings.Contains(got, "    ") {
		t.Errorf("expected indentation, got: %s", got)
	}
}

func TestWarning(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeText, true)

	p.Warning("disk space low")
	got := buf.String()

	if !strings.Contains(got, "Warning: disk space low") {
		t.Errorf("expected warning message, got: %s", got)
	}
	if !strings.Contains(got, colorYellow) {
		t.Errorf("expected yellow color code, got: %s", got)
	}
}

func TestErrorf(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModePlain, false)

	p.Errorf("failed to connect to %s", "server-1")
	got := buf.String()

	if !strings.Contains(got, "Error: failed to connect to server-1") {
		t.Errorf("expected formatted error, got: %s", got)
	}
}

func TestHint(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeText, true)

	p.Hint("Try running with --verbose")
	got := buf.String()

	if !strings.Contains(got, "Try running with --verbose") {
		t.Errorf("expected hint message, got: %s", got)
	}
	if !strings.Contains(got, colorDim) {
		t.Errorf("expected dim color code, got: %s", got)
	}
}

func TestIsJSON(t *testing.T) {
	p := NewWithWriter(&bytes.Buffer{}, ModeJSON, false)
	if !p.IsJSON() {
		t.Error("expected IsJSON() to return true in JSON mode")
	}

	p2 := NewWithWriter(&bytes.Buffer{}, ModeText, false)
	if p2.IsJSON() {
		t.Error("expected IsJSON() to return false in text mode")
	}
}

// TestTable_ColorAligned: with color on, the header columns start where the
// row columns do (seen misaligned in 'evo doctor' and 'evo vm list' on a
// color terminal: tabwriter counted the bold escapes as width).
func TestTable_ColorAligned(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf, ModeText, true)
	p.Table([]string{"ID", "Имя", "Состояние", "Зона"}, [][]string{
		{"a1b2c3d4-aaaa", "evo-web", "running", "ru.AZ-2"},
		{"1", "x", "stopped", "ru.AZ-1"},
	})
	if !strings.Contains(buf.String(), colorBold) {
		t.Fatalf("no bold header: %q", buf.String())
	}
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	var want []int
	for i, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		got := columnStarts(ansi.ReplaceAllString(line, ""))
		if i == 0 {
			want = got
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("line %d starts columns at %v, the header at %v:\n%s", i, got, want, ansi.ReplaceAllString(buf.String(), ""))
		}
	}
}

// columnStarts is the rune offset of each space-separated cell of a line.
func columnStarts(line string) []int {
	var starts []int
	prevSpace := true
	for i, r := range []rune(line) {
		if r != ' ' && prevSpace {
			starts = append(starts, i)
		}
		prevSpace = r == ' '
	}
	return starts
}
