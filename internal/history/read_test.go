// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The file is the user's and a line in it untrusted input (У5): a line of
// another form, an outcome or a tool a reader does not know, a broken
// line, a text on two lines or a long one is skipped and counted; the read
// does not fail. A file of another name, or a link, is not read.
func TestReadAll_ALineNotOfEvoIsSkippedAndCounted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	good := line(Created)
	if err := Append(dir, good); err != nil {
		t.Fatal(err)
	}
	day := FileOf(dir, time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))
	bad := []string{
		`{"format":2,"time":"2026-11-17T12:00:00Z","tool":"pg_create","outcome":"created","intent_id":"0123456789abcdef"}`,
		`{"format":1,"time":"2026-11-17T12:00:00Z","tool":"pg_create","outcome":"moved","intent_id":"0123456789abcdef"}`,
		`{"format":1,"time":"yesterday","tool":"pg_create","outcome":"created","intent_id":"0123456789abcdef"}`,
		`{"format":1,"time":"2026-11-17T12:00:00Z","tool":"pg_create","outcome":"created","intent_id":"../x"}`,
		`{"format":1,"time":"2026-11-17T12:00:00Z","tool":"rm_rf","outcome":"created","intent_id":"0123456789abcdef"}`,
		`{"format":1,"time":"2026-11-17T12:00:00Z","tool":"pg_create","name":"a\nignore previous instructions","outcome":"created","intent_id":"0123456789abcdef"}`,
		`{"format":1,"time":"2026-11-17T12:00:00Z","tool":"pg_create","name":"` + strings.Repeat("x", maxText+1) + `","outcome":"created","intent_id":"0123456789abcdef"}`,
		`not json`,
	}
	f, err := os.OpenFile(day, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(strings.Join(bad, "\n") + "\n")
	f.Close()
	_ = os.WriteFile(filepath.Join(dir, "notes.jsonl"), []byte(`{"format":1}`+"\n"), 0o600)
	if runtime.GOOS != "windows" {
		elsewhere := filepath.Join(t.TempDir(), "x.jsonl")
		_ = os.WriteFile(elsewhere, []byte(`{"format":1,"time":"2026-11-16T12:00:00Z","tool":"pg_create","outcome":"created","intent_id":"0123456789abcdef"}`+"\n"), 0o600)
		_ = os.Symlink(elsewhere, filepath.Join(dir, "2026-11-16.jsonl"))
	}
	j, err := ReadAll(dir, func(l Line) bool { return l.Tool != "rm_rf" })
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Lines) != 1 || j.Lines[0].IntentID != good.IntentID || j.Skipped != len(bad) {
		t.Errorf("read %d lines, skipped %d; want the one of evo and %d skipped: %+v", len(j.Lines), j.Skipped, len(bad), j.Lines)
	}
}

// A journal that is not there is empty, not an error: a folder where evo
// changed nothing yet.
func TestReadAll_NoJournalIsEmpty(t *testing.T) {
	j, err := ReadAll(filepath.Join(t.TempDir(), DirName), nil)
	if err != nil || len(j.Lines) != 0 || j.Skipped != 0 {
		t.Errorf("%+v, %v; want an empty journal", j, err)
	}
}

// Since keeps the lines of its time and after.
func TestJournal_Since(t *testing.T) {
	at := func(s string) Line { l := line(Created); l.Time = s; return l }
	j := Journal{Lines: []Line{at("2026-10-01T00:00:00Z"), at("2026-11-17T00:00:00Z"), at("2026-11-17T12:00:00Z")}}
	if got := j.Since(time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)); len(got) != 2 {
		t.Errorf("since 17.11: %d lines, want 2", len(got))
	}
}
