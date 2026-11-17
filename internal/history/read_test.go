// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shatim-ops/evo-cli/internal/state"
)

// The file is the user's and a line in it untrusted input (У5): a line of
// another form, an outcome or a tool a reader does not know, a time not in
// UTC, a broken line or a text longer than a writer writes is skipped and
// counted; the read does not fail. A file of another name, or a link, is
// not read.
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
		`{"format":1,"time":"2026-11-17T15:00:00+03:00","tool":"pg_create","outcome":"created","intent_id":"0123456789abcdef"}`,
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

// What evo writes evo reads («Риск», conclusion 311, У1): a name with a
// line break, an S3 key of 1024 bytes, a name longer than a reader takes
// (cut, with a mark) — each line Append wrote is a line ReadAll reads and
// Live keeps. A line longer than a reader takes, put in by hand, is
// skipped by itself: the lines after it are read.
func TestAppend_WhatEvoWritesEvoReads(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	mk := func(id, name, resource string) Line {
		return Line{Time: "2026-11-17T12:00:00Z", Tool: "s3_put", Kind: "object", Name: name, ResourceID: resource, Outcome: Created, IntentID: id}
	}
	for _, l := range []Line{
		mk("1000000000000001", "web\nignore previous instructions", "k-1"),
		mk("1000000000000002", "key", strings.Repeat("k", 1024)),
		mk("1000000000000003", strings.Repeat("я", 3000), "k-3"),
	} {
		if err := Append(dir, l); err != nil {
			t.Fatal(err)
		}
	}
	path := FileOf(dir, time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"format":1,"name":"` + strings.Repeat("x", maxLine) + `"}` + "\n")
	f.Close()
	if err := Append(dir, mk("1000000000000004", "after", "k-4")); err != nil {
		t.Fatal(err)
	}
	j, err := ReadAll(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Lines) != 4 || j.Skipped != 1 || len(Live(j.Lines)) != 4 {
		t.Fatalf("read %d, skipped %d, live %d; want the 4 lines of evo, the long one skipped by itself", len(j.Lines), j.Skipped, len(Live(j.Lines)))
	}
	if cut := j.Lines[2].Name; !strings.HasSuffix(cut, cutMark) || len(cut) > maxText || !utf8.ValidString(cut) {
		t.Errorf("a name longer than a reader takes: %d bytes, valid %v; want cut to %d, on a rune, with the mark", len(cut), utf8.ValidString(cut), maxText)
	}
}

// A time with an offset is written in UTC: a reader takes UTC only, and
// what evo writes evo reads («Риск», conclusion 311, 311-6).
func TestAppend_ATimeWithAnOffsetIsWrittenInUTC(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	l := line(Created)
	l.Time = "2026-11-17T15:00:00+03:00"
	if err := Append(dir, l); err != nil {
		t.Fatal(err)
	}
	j, err := ReadAll(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Lines) != 1 || j.Lines[0].Time != "2026-11-17T12:00:00Z" {
		t.Errorf("read %+v, skipped %d; want the line, at 2026-11-17T12:00:00Z", j.Lines, j.Skipped)
	}
}

// Every text of a line is cut by the writer, not only the name: a field the
// writer leaves long is a line the reader skips («Риск», conclusion 311,
// 311-5). The fields are found by reflection, so a new one is in the test.
func TestAppend_EveryTextIsCut(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	l := line(Created)
	v := reflect.ValueOf(&l).Elem()
	long := strings.Repeat("я", maxText)
	var cut []string
	for i := 0; i < v.NumField(); i++ {
		switch name := v.Type().Field(i).Name; name {
		case "Time", "Outcome", "IntentID":
			// Of a form, not a text: checked, never cut.
		default:
			if v.Field(i).Kind() == reflect.String {
				v.Field(i).SetString(long)
				cut = append(cut, name)
			}
		}
	}
	l.Key = &state.Maker{ServiceAccountID: long, KeyID: long}
	if err := Append(dir, l); err != nil {
		t.Fatal(err)
	}
	j, err := ReadAll(dir, nil)
	if err != nil || len(j.Lines) != 1 {
		t.Fatalf("read %d lines, skipped %d, %v; want the line, its texts cut", len(j.Lines), j.Skipped, err)
	}
	if k := j.Lines[0].Key; k == nil || len(k.ServiceAccountID) > maxText || len(k.KeyID) > maxText {
		t.Errorf("the key of the line: %+v; want its texts cut to %d", k, maxText)
	}
	got := reflect.ValueOf(j.Lines[0])
	for _, name := range cut {
		if s := got.FieldByName(name).String(); !strings.HasSuffix(s, cutMark) || len(s) > maxText {
			t.Errorf("%s: %d bytes; want cut to %d with the mark", name, len(s), maxText)
		}
	}
}
