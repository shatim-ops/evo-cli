// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Mode determines the output format.
type Mode int

const (
	ModeText  Mode = iota // default TTY
	ModePlain             // no TTY / piped
	ModeJSON              // --json flag
)

// Printer handles all output formatting.
type Printer struct {
	out    io.Writer
	errOut io.Writer
	mode   Mode
	color  bool // ANSI colors on out
	// errColor is decided by errOut: "2>err.log" on a terminal must not
	// write escape codes into the file.
	errColor bool
	// progressLen is the width of the progress line on the terminal
	// (Progress), 0 when there is none.
	progressLen int
}

// New creates a Printer. Detects mode from flags and TTY state.
func New(jsonMode bool, noColor bool) *Printer {
	mode := ModeText
	if jsonMode {
		mode = ModeJSON
	} else if !IsTTY(int(os.Stdout.Fd())) {
		mode = ModePlain
	}
	return &Printer{
		out:      os.Stdout,
		errOut:   os.Stderr,
		mode:     mode,
		color:    ColorEnabled(noColor) && mode == ModeText,
		errColor: colorAllowed(noColor) && mode != ModeJSON && IsTTY(int(os.Stderr.Fd())),
	}
}

// NewWithWriter creates a Printer writing to a specific writer (for testing).
func NewWithWriter(w io.Writer, mode Mode, color bool) *Printer {
	return &Printer{
		out:      w,
		errOut:   w,
		mode:     mode,
		color:    color,
		errColor: color,
	}
}

// NewWithWriters creates a Printer with separate result and diagnostic
// writers (for testing what goes to stdout vs stderr).
func NewWithWriters(out, errOut io.Writer, mode Mode, color bool) *Printer {
	return &Printer{
		out:      out,
		errOut:   errOut,
		mode:     mode,
		color:    color,
		errColor: color,
	}
}

// Step prints a step indicator: "==> [1/5] Message..."
func (p *Printer) Step(n, total int, msg string) {
	p.ProgressDone()
	if p.mode == ModeJSON {
		return
	}
	prefix := fmt.Sprintf("==> [%d/%d] %s", n, total, msg)
	if p.color {
		fmt.Fprintf(p.out, "%s%s%s%s\n", colorBold, colorBlue, prefix, colorReset)
	} else {
		fmt.Fprintln(p.out, prefix)
	}
}

// StepDone prints step completion: "    detail"
func (p *Printer) StepDone(detail string) {
	p.ProgressDone()
	if p.mode == ModeJSON {
		return
	}
	if p.color {
		fmt.Fprintf(p.out, "    %s%s%s\n", colorDim, detail, colorReset)
	} else {
		fmt.Fprintf(p.out, "    %s\n", detail)
	}
}

// Success prints a success message in green.
func (p *Printer) Success(msg string) {
	p.ProgressDone()
	if p.mode == ModeJSON {
		return
	}
	if p.color {
		fmt.Fprintf(p.out, "%s%s%s\n", colorGreen, msg, colorReset)
	} else {
		fmt.Fprintln(p.out, msg)
	}
}

// Warning prints a warning message in yellow to the error writer; in JSON
// mode as a {"warning": "..."} line, so that it is neither lost nor breaks
// a parser reading stderr.
func (p *Printer) Warning(msg string) {
	p.ProgressDone()
	if p.mode == ModeJSON {
		_ = json.NewEncoder(p.errOut).Encode(map[string]string{"warning": msg})
		return
	}
	if p.errColor {
		fmt.Fprintf(p.errOut, "%s%s%s%s%s\n", colorBold, colorYellow, warningPrefix(), msg, colorReset)
	} else {
		fmt.Fprintf(p.errOut, "%s%s\n", warningPrefix(), msg)
	}
}

// Errorf prints an error message in red.
func (p *Printer) Errorf(msg string, args ...any) {
	p.ProgressDone()
	formatted := fmt.Sprintf(msg, args...)
	if p.errColor {
		fmt.Fprintf(p.errOut, "%s%s%s%s%s\n", colorBold, colorRed, errorPrefix(), formatted, colorReset)
	} else {
		fmt.Fprintf(p.errOut, "%s%s\n", errorPrefix(), formatted)
	}
}

// Hint prints a hint/suggestion in dim text.
func (p *Printer) Hint(msg string) {
	p.ProgressDone()
	if p.mode == ModeJSON {
		return
	}
	if p.errColor {
		fmt.Fprintf(p.errOut, "%s%s%s\n", colorDim, msg, colorReset)
	} else {
		fmt.Fprintln(p.errOut, msg)
	}
}

// Table prints data as an aligned table. The columns are laid out on the
// plain text and the bold goes on the finished header line: tabwriter
// counts escape bytes as width, so a bold header cell pushed its column
// off the rows on a color terminal.
func (p *Printer) Table(headers []string, rows [][]string) {
	p.ProgressDone()
	if p.mode == ModeJSON {
		return
	}
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(sanitizeRow(headers), "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(sanitizeRow(row), "\t"))
	}
	tw.Flush()
	out := buf.String()
	if p.color {
		head, rest, _ := strings.Cut(out, "\n")
		out = colorBold + head + colorReset + "\n" + rest
	}
	fmt.Fprint(p.out, out)
}

func sanitizeRow(row []string) []string {
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = sanitizeCell(c)
	}
	return out
}

// SanitizeCell is sanitizeCell for text printed outside a table (a card
// line with a name from the cloud).
func SanitizeCell(s string) string { return sanitizeCell(s) }

// sanitizeCell makes a table cell safe for a terminal. Cells carry names
// and descriptions from the cloud: an escape sequence could repaint or
// clear the screen, a tab or a line break would break the columns.
// Invalid UTF-8 becomes U+FFFD.
func sanitizeCell(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r):
			return '?'
		}
		return r
	}, s)
}

// JSON outputs v as JSON to stdout.
func (p *Printer) JSON(v any) error {
	enc := json.NewEncoder(p.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// NDJSON outputs v as a single NDJSON line.
func (p *Printer) NDJSON(v any) error {
	enc := json.NewEncoder(p.out)
	return enc.Encode(v)
}

// IsJSON returns true if in JSON output mode.
func (p *Printer) IsJSON() bool {
	return p.mode == ModeJSON
}

// Writer returns the underlying output writer.
func (p *Printer) Writer() io.Writer {
	return p.out
}

// ErrWriter returns the writer for diagnostics and prompts (stderr), so that
// stdout carries only the command's result.
func (p *Printer) ErrWriter() io.Writer {
	return p.errOut
}

// ErrorBody is the payload of a JSON-mode error.
type ErrorBody struct {
	// Code is the process exit code.
	Code int `json:"code"`
	// Reason is the symbolic code ("AUTH_FAILED"), the same in every
	// language of the CLI: scripts match on it, not on Message.
	Reason  string `json:"reason"`
	Message string `json:"message"`
	// Hint is left out when the error has none, not printed as "".
	Hint string `json:"hint,omitempty"`
}

// JSONError writes
// {"error": {"code": N, "reason": "...", "message": "...", "hint": "..."}}
// to the error writer. code is the process exit code, reason the symbolic
// code of the error; without a hint the "hint" key is left out.
func (p *Printer) JSONError(code int, reason, message, hint string) error {
	enc := json.NewEncoder(p.errOut)
	return enc.Encode(struct {
		Error ErrorBody `json:"error"`
	}{ErrorBody{Code: code, Reason: reason, Message: message, Hint: hint}})
}

func warningPrefix() string { return i18n.T("Warning: ", "Предупреждение: ") }

func errorPrefix() string { return i18n.T("Error: ", "Ошибка: ") }
