// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/monitoring"
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

// renderMetricSeries shows the labels once per series and streams each
// sample with its time. The Monitoring API timestamps are Unix milliseconds.
func renderMetricSeries(p *output.Printer, w io.Writer, text string) error {
	var a struct {
		Project, Match, Window string
		Count                  int
		Series                 []monitoring.Series
	}
	if err := json.Unmarshal([]byte(text), &a); err != nil || a.Series == nil {
		// Preserve a diagnostic that is not a series answer.
		fmt.Fprintln(w, output.SanitizeCell(text))
		return nil
	}
	for _, series := range a.Series {
		if len(series.Values) != len(series.Timestamps) {
			return errors.New(i18n.T("The metric series has different numbers of timestamps and values.", "В ряду метрики разное число отметок времени и значений."))
		}
	}
	p.Table([]string{i18n.T("PROJECT", "ПРОЕКТ"), i18n.T("MATCH", "СЕЛЕКТОР"), i18n.T("WINDOW", "ПЕРИОД"), i18n.T("SERIES", "РЯДЫ")},
		[][]string{{a.Project, a.Match, a.Window, strconv.Itoa(a.Count)}})
	if len(a.Series) == 0 {
		fmt.Fprintln(w, i18n.T("No metric series.", "Рядов метрик нет."))
	}
	for _, series := range a.Series {
		fmt.Fprintf(w, "\n%s %s\n", i18n.T("Labels:", "Метки:"), metricLabelsJSON(series.Metric))
		if len(series.Values) == 0 {
			fmt.Fprintln(w, i18n.T("No samples.", "Измерений нет."))
			continue
		}
		fmt.Fprintln(w, i18n.T("TIME (UTC)                VALUE", "ВРЕМЯ (UTC)               ЗНАЧЕНИЕ"))
		for i, value := range series.Values {
			stamp := time.UnixMilli(series.Timestamps[i]).UTC().Format("2006-01-02T15:04:05.000Z")
			fmt.Fprintf(w, "%s  %s\n", stamp, strconv.FormatFloat(value, 'g', -1, 64))
		}
	}
	return nil
}

// json.Marshal sorts the label keys and escapes quotes, backslashes and ASCII
// controls. Escape the remaining non-printable runes as JSON Unicode escapes:
// replacing them with a printable character would merge distinct label sets.
func metricLabelsJSON(labels map[string]string) string {
	data, _ := json.Marshal(labels)
	var out strings.Builder
	out.Grow(len(data))
	for _, r := range string(data) {
		if unicode.IsPrint(r) {
			out.WriteRune(r)
			continue
		}
		var pair [2]uint16
		for _, unit := range utf16.AppendRune(pair[:0], r) {
			fmt.Fprintf(&out, `\u%04x`, unit)
		}
	}
	return out.String()
}
