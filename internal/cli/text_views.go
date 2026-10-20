// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Two answers are text, not a table: 'evo ca logs' prints the lines of a
// ca_logs answer as a log — one line per entry, its time and message,
// oldest first, as the tool orders them, not a table that cuts every
// message to 40 characters; the note of the answer (a cut read, no lines
// yet) goes under the lines. 'evo metrics scrape' prints the Prometheus
// text of metrics_scrape as it is, for grep. --json prints the tools'
// answers as they are. A control character from the cloud does not reach
// the terminal.

type logLinesAnswer struct {
	Logs []map[string]any `json:"logs"`
	Note string           `json:"note"`
}

// logTimeKeys are the time of an entry: timestamp for the app's lines,
// lastTimestamp or firstTimestamp for the platform's events.
var logTimeKeys = []string{"timestamp", "lastTimestamp", "firstTimestamp"}

func renderLogLines(_ *output.Printer, w io.Writer, text string) error {
	var a logLinesAnswer
	if err := json.Unmarshal([]byte(text), &a); err != nil || (a.Logs == nil && a.Note == "") {
		fmt.Fprintln(w, text)
		return nil
	}
	if len(a.Logs) == 0 && a.Note == "" {
		fmt.Fprintln(w, i18n.T("No log lines.", "Строк журнала нет."))
	}
	for _, e := range a.Logs {
		fmt.Fprintln(w, output.SanitizeCell(logLine(e)))
	}
	if a.Note != "" {
		fmt.Fprintln(w, i18n.T("note: ", "Примечание: ")+output.SanitizeCell(a.Note))
	}
	return nil
}

// logLine is an entry as a line: "<time> <message>", or the entry as JSON
// when it has no message.
func logLine(e map[string]any) string {
	msg, ok := e["message"].(string)
	if !ok {
		b, _ := json.Marshal(e)
		return string(b)
	}
	for _, k := range logTimeKeys {
		if t, ok := e[k].(string); ok && t != "" {
			return t + " " + msg
		}
	}
	return msg
}

// renderExposition prints the "exposition" of a metrics_scrape answer: the
// Prometheus text, line by line.
func renderExposition(_ *output.Printer, w io.Writer, text string) error {
	var a struct {
		Exposition *string `json:"exposition"`
	}
	if err := json.Unmarshal([]byte(text), &a); err != nil || a.Exposition == nil {
		fmt.Fprintln(w, text)
		return nil
	}
	if strings.TrimSpace(*a.Exposition) == "" {
		fmt.Fprintln(w, i18n.T("No metrics.", "Метрик нет."))
		return nil
	}
	for _, line := range strings.Split(strings.TrimSuffix(*a.Exposition, "\n"), "\n") {
		fmt.Fprintln(w, output.SanitizeCell(line))
	}
	return nil
}
