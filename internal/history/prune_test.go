// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var pruneNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// writeDay writes the lines of the day day (2026-08-01) to dir as they are.
func writeDay(t *testing.T, dir, day string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, day+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lineOf(intent, outcome, kind, resource, at string) string {
	b, _ := json.Marshal(Line{Format: Format, Time: at, Tool: "disk_create", IntentID: intent, Outcome: outcome, Kind: kind, ResourceID: resource})
	return string(b)
}

func intents(t *testing.T, dir string) []string {
	t.Helper()
	j, err := ReadAll(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range j.Lines {
		out = append(out, l.IntentID+":"+l.Outcome)
	}
	return out
}

// A file older than KeepDays goes; what is live in it — a resource made
// and not removed, an outcome not known — stays in LiveName and is read as
// before; what is closed goes with the file. The files of the window stay.
func TestPrune_AnOldFileGoesItsLiveLinesStay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-07-01",
		lineOf("a000000000000001", Created, "disk", "d-1", "2026-07-01T10:00:00Z"), // live: never deleted
		lineOf("a000000000000002", Created, "disk", "d-2", "2026-07-01T10:01:00Z"), // closed below
		lineOf("a000000000000003", Deleted, "disk", "d-2", "2026-07-01T10:02:00Z"),
		lineOf("a000000000000004", Unknown, "", "", "2026-07-01T10:03:00Z"), // live: outcome unknown
	)
	writeDay(t, dir, "2026-11-16", lineOf("a000000000000005", Created, "disk", "d-5", "2026-11-16T10:00:00Z"))
	p, err := Prune(dir, pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Retired) != 1 || p.Retired[0] != "2026-07-01.jsonl" || RetiredThrough(dir) != "2026-07-01" {
		t.Errorf("retired %v through %q, want the file of July only", p.Retired, RetiredThrough(dir))
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-07-01.jsonl")); !os.IsNotExist(err) {
		t.Error("the old file is still there")
	}
	got := strings.Join(intents(t, dir), " ")
	want := "a000000000000001:created a000000000000004:unknown a000000000000005:created"
	if got != want {
		t.Errorf("after pruning: %s; want %s", got, want)
	}
}

// Over the ceiling, the oldest files go first, their live lines kept;
// today's file never goes. Live lines alone over the ceiling are kept and
// said, not cut.
func TestPrune_OverTheCeilingTheOldestGoFirst(t *testing.T) {
	was := ceiling
	t.Cleanup(func() { ceiling = was })
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-11-10",
		lineOf("b000000000000001", Created, "disk", "d-1", "2026-11-10T10:00:00Z"),
		lineOf("b000000000000002", Deleted, "disk", "d-1", "2026-11-10T10:01:00Z"))
	writeDay(t, dir, "2026-11-11", lineOf("b000000000000003", Created, "disk", "d-3", "2026-11-11T10:00:00Z"))
	writeDay(t, dir, "2026-11-17", lineOf("b000000000000004", Created, "disk", "d-4", "2026-11-17T10:00:00Z"))
	ceiling = 400 // about two lines: the oldest closed lines go
	p, err := Prune(dir, pruneNow)
	if err != nil {
		t.Fatal(err)
	}
	// 11-10 frees its two closed lines and the journal fits; 11-11 holds a
	// live line only — it frees nothing, and stays.
	if strings.Join(p.Retired, " ") != "2026-11-10.jsonl" {
		t.Errorf("retired %v; want the oldest file with closed lines, not today's", p.Retired)
	}
	if got := strings.Join(intents(t, dir), " "); got != "b000000000000003:created b000000000000004:created" {
		t.Errorf("after pruning: %s", got)
	}
	// Live lines over the ceiling: kept, and said.
	ceiling = 10
	_ = os.Remove(filepath.Join(dir, prunedName))
	if p, _ := Prune(dir, pruneNow.Add(24*time.Hour)); !p.OverCeiling {
		t.Error("the live lines are over the ceiling and the pruning does not say so")
	}
	if got := len(intents(t, dir)); got != 2 {
		t.Errorf("a live line was cut to fit the ceiling: %d left", got)
	}
}

// What the pruning cannot read or does not know — a line of a newer evo,
// a broken line, a line longer than a reader takes — it does not drop: it
// is kept as live (У8); a live line after the long one is kept too, the
// file read on past it («Риск», conclusion 311, У1).
func TestPrune_WhatItDoesNotKnowItKeeps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	newer := `{"format":2,"time":"2026-07-01T10:00:00Z","tool":"disk_create","intent_id":"c000000000000001","outcome":"moved"}`
	broken := `{"format":1,"time"`
	long := `{"format":1,"name":"` + strings.Repeat("x", maxLine) + `"}`
	after := lineOf("c000000000000002", Created, "disk", "d-after", "2026-07-01T10:05:00Z")
	writeDay(t, dir, "2026-07-01", newer, broken, long, after)
	if _, err := Prune(dir, pruneNow); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, LiveName))
	for _, want := range []string{newer, broken, long, after} {
		if !strings.Contains(string(data), want) {
			t.Errorf("%s does not keep %.80q; want the lines it does not know as they are, and the live line after the long one", LiveName, want)
		}
	}
}

// A pruning killed after it kept a live line and before it removed the
// file leaves the line twice: a reader reads it once, and the next pruning
// keeps it once. A live line closed later — its resource deleted — goes
// at the next pruning.
func TestPrune_AKilledPruningLosesNothingAndDoublesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	live := lineOf("d000000000000001", Created, "disk", "d-1", "2026-07-01T10:00:00Z")
	writeDay(t, dir, "2026-07-01", live)
	// The state a kill leaves: the line kept, the file not removed.
	if err := os.WriteFile(filepath.Join(dir, LiveName), []byte(live+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := intents(t, dir); len(got) != 1 {
		t.Errorf("read %v; want the line once", got)
	}
	if _, err := Prune(dir, pruneNow); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, LiveName))
	if strings.Count(string(data), "d000000000000001") != 1 {
		t.Errorf("%s: %q; want the line once", LiveName, data)
	}
	// Deleted later: the next pruning drops it from LiveName.
	writeDay(t, dir, "2026-11-18", lineOf("d000000000000002", Deleted, "disk", "d-1", "2026-11-18T10:00:00Z"))
	if _, err := Prune(dir, pruneNow.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, LiveName)); strings.Contains(string(data), "d000000000000001") {
		t.Errorf("%s keeps a line whose resource was deleted: %q", LiveName, data)
	}
}

// Once a day: a second pruning the same day does nothing.
func TestPrune_OnceADay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-11-16", lineOf("e000000000000001", Created, "disk", "d-1", "2026-11-16T10:00:00Z"))
	if _, err := Prune(dir, pruneNow); err != nil {
		t.Fatal(err)
	}
	writeDay(t, dir, "2026-07-01", lineOf("e000000000000002", Deleted, "disk", "d-9", "2026-07-01T10:00:00Z"))
	if p, _ := Prune(dir, pruneNow.Add(time.Hour)); len(p.Retired) != 0 {
		t.Errorf("a second pruning the same day retired %v", p.Retired)
	}
	if p, _ := Prune(dir, pruneNow.Add(24*time.Hour)); len(p.Retired) != 1 {
		t.Errorf("the next day's pruning retired %v, want the old file", p.Retired)
	}
}

// Describe gives doctor the size, the oldest day and the live lines.
func TestDescribe_SizeOldestLive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	if s, err := Describe(dir); err != nil || s != (Summary{}) {
		t.Errorf("no journal: %+v, %v; want an empty one", s, err)
	}
	writeDay(t, dir, "2026-11-10",
		lineOf("9000000000000001", Created, "disk", "d-1", "2026-11-10T10:00:00Z"),
		lineOf("9000000000000002", Created, "disk", "d-2", "2026-11-10T10:01:00Z"),
		lineOf("9000000000000003", Deleted, "disk", "d-2", "2026-11-10T10:02:00Z"))
	writeDay(t, dir, "2026-11-12", lineOf("9000000000000004", Unknown, "", "", "2026-11-12T10:00:00Z"))
	s, err := Describe(dir)
	if err != nil || s.Oldest != "2026-11-10" || s.Live != 2 || s.Bytes == 0 || s.OverCeiling {
		t.Errorf("%+v, %v; want oldest 2026-11-10, 2 live, some bytes", s, err)
	}
}
