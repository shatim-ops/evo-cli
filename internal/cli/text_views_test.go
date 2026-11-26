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

// evo metrics scrape prints the Prometheus text as it is, line by line, for
// grep: not one FIELD/VALUE cell with the newlines gone.
func TestRenderExposition(t *testing.T) {
	if _, c, _ := findLeaf(t, "metrics", "scrape"); c.render == nil {
		t.Fatal("metrics scrape has no text view")
	}
	render := func(text string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := renderExposition(output.NewWithWriter(&buf, output.ModePlain, false), &buf, text); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	got := render(`{"project":"p","match":"{}","format":"prometheus-text","exposition":"# TYPE cpu gauge\ncpu{vm=\"a\"} 0.5\ncpu{vm=\"b\u001b\"} 1\n"}`)
	if want := "# TYPE cpu gauge\ncpu{vm=\"a\"} 0.5\ncpu{vm=\"b?\"} 1\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := render(`{"exposition":""}`); got != "No metrics.\n" {
		t.Errorf("empty: %q", got)
	}
	if got := render(`{"error":"x"}`); got != `{"error":"x"}`+"\n" {
		t.Errorf("not an exposition: %q", got)
	}
}

// On the normal command the instance identifies the series, and each value
// belongs to one timestamp. Seeing "up" or a count of 1 proves neither.
func TestMetricsHistory_HumanPairsSamplesWithTheirLabelsAndTime(t *testing.T) {
	withJSONWorld(t)
	const url = "GET monitoring.api.cloud.ru/v1/{id}/export_metrics"
	before := parityAnswers[url]
	t.Cleanup(func() { parityAnswers[url] = before })
	parityAnswers[url] = `[{"metric":{"__name__":"up","instance":"node-one\n\u001b[31m"},"timestamps":[1000,2123],"values":[97.125,-0.75]},{"metric":{"__name__":"up","instance":"node-two"},"timestamps":[3000],"values":[23.5]}]`
	newProcess()
	code, human, stderr := runCLI(t, "", "metrics", "history", "up")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	// Fold spacing but keep each timestamp joined to its own value; no label
	// or control character from the cloud may start its own terminal line.
	at := strings.Index(human, "node-one")
	later := strings.Index(human, "node-two")
	if at < 0 || later <= at || strings.Contains(human, "\x1b") || strings.Contains(human, "node-one\n") {
		t.Fatalf("series labels missing, unordered, or not escaped: %q", human)
	}
	first := strings.Join(strings.Fields(human[at:later]), " ")
	second := strings.Join(strings.Fields(human[later:]), " ")
	for _, pair := range []string{"1970-01-01T00:00:01.000Z 97.125", "1970-01-01T00:00:02.123Z -0.75"} {
		if !strings.Contains(first, pair) || strings.Contains(second, pair) {
			t.Errorf("first series: missing or misplaced %q: %s", pair, human)
		}
	}
	if !strings.Contains(second, "1970-01-01T00:00:03.000Z 23.5") {
		t.Errorf("second series: %s", human)
	}
}

func TestRenderMetricSeries_EmptyAndMalformed(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		wantErr           bool
	}{
		{"no series", `{"series":[],"count":0}`, "No metric series.", false},
		{"empty series", `{"series":[{"metric":{"instance":"worker"},"timestamps":[],"values":[]}],"count":1}`, "No samples.", false},
		{"error object", `{"error":"no monitoring tenant"}`, "no monitoring tenant", false},
		{"invalid response", "upstream read failed", "upstream read failed", false},
		{"value without time", `{"series":[{"metric":{},"values":[1],"timestamps":[]}]}`, "different numbers of timestamps and values", true},
		{"time without value", `{"series":[{"metric":{},"timestamps":[1],"values":[]}]}`, "different numbers of timestamps and values", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			err := renderMetricSeries(output.NewWithWriter(&b, output.ModePlain, false), &b, tc.input)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), tc.want) || b.Len() != 0 {
					t.Fatalf("output %q, error %v", b.String(), err)
				}
			} else if err != nil || !strings.Contains(b.String(), tc.want) {
				t.Fatalf("output %q, error %v", b.String(), err)
			}
		})
	}
}
