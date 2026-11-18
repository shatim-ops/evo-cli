// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/state"
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
	// What is read, not in what order the files are: LiveName is read last.
	sort.Strings(out)
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
	p, err := Prune(dir, pruneNow, nil)
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
	p, err := Prune(dir, pruneNow, nil)
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
	if p, _ := Prune(dir, pruneNow.Add(24*time.Hour), nil); !p.OverCeiling {
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
	if _, err := Prune(dir, pruneNow, nil); err != nil {
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
	if _, err := Prune(dir, pruneNow, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, LiveName))
	if strings.Count(string(data), "d000000000000001") != 1 {
		t.Errorf("%s: %q; want the line once", LiveName, data)
	}
	// Deleted later: the next pruning drops it from LiveName.
	writeDay(t, dir, "2026-11-18", lineOf("d000000000000002", Deleted, "disk", "d-1", "2026-11-18T10:00:00Z"))
	if _, err := Prune(dir, pruneNow.Add(24*time.Hour), nil); err != nil {
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
	if _, err := Prune(dir, pruneNow, nil); err != nil {
		t.Fatal(err)
	}
	writeDay(t, dir, "2026-07-01", lineOf("e000000000000002", Deleted, "disk", "d-9", "2026-07-01T10:00:00Z"))
	if p, _ := Prune(dir, pruneNow.Add(time.Hour), nil); len(p.Retired) != 0 {
		t.Errorf("a second pruning the same day retired %v", p.Retired)
	}
	if p, _ := Prune(dir, pruneNow.Add(24*time.Hour), nil); len(p.Retired) != 1 {
		t.Errorf("the next day's pruning retired %v, want the old file", p.Retired)
	}
}

// Describe gives doctor the size, the oldest day and the live lines.
func TestDescribe_SizeOldestLive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	if s, err := Describe(dir, nil); err != nil || !reflect.DeepEqual(s, Summary{}) {
		t.Errorf("no journal: %+v, %v; want an empty one", s, err)
	}
	writeDay(t, dir, "2026-11-10",
		lineOf("9000000000000001", Created, "disk", "d-1", "2026-11-10T10:00:00Z"),
		lineOf("9000000000000002", Created, "disk", "d-2", "2026-11-10T10:01:00Z"),
		lineOf("9000000000000003", Deleted, "disk", "d-2", "2026-11-10T10:02:00Z"))
	writeDay(t, dir, "2026-11-12", lineOf("9000000000000004", Unknown, "", "", "2026-11-12T10:00:00Z"))
	s, err := Describe(dir, nil)
	if err != nil || s.Oldest != "2026-11-10" || s.Live != 2 || s.Bytes == 0 || s.OverCeiling {
		t.Errorf("%+v, %v; want oldest 2026-11-10, 2 live, some bytes", s, err)
	}
}

// Today's file never goes over the ceiling, though it is the only one and
// all its lines are closed: a call writes its line to it now («Риск»,
// conclusion 324, 324-1).
func TestPrune_TodaysFileNeverGoes(t *testing.T) {
	was := ceiling
	t.Cleanup(func() { ceiling = was })
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-11-17",
		lineOf("f100000000000001", Created, "disk", "d-1", "2026-11-17T10:00:00Z"),
		lineOf("f100000000000002", Deleted, "disk", "d-1", "2026-11-17T10:01:00Z"))
	ceiling = 10
	p, err := Prune(dir, pruneNow, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Retired) != 0 {
		t.Errorf("retired %v; want today's file kept", p.Retired)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-11-17.jsonl")); err != nil {
		t.Errorf("today's file is gone: %v", err)
	}
}

// The pruning finds what is live by the rule history reads it by: the
// lines in the order of their times, not of the file. A clock turned back
// writes a delete at 10:00 after a create at 12:00; history takes the
// delete as earlier, and so does the pruning — the create stays live, and
// its line is kept (Астра, #2296).
func TestPrune_LiveByTheTimeOfTheLines(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-07-01",
		lineOf("a000000000000001", Created, "secret", "app/db", "2026-07-01T12:00:00Z"),
		lineOf("a000000000000002", Deleted, "secret", "app/db", "2026-07-01T10:00:00Z"))
	j, _ := ReadAll(dir, nil)
	if len(Live(j.Lines)) != 1 {
		t.Fatalf("history's rule: live %+v; want the create", Live(j.Lines))
	}
	if _, err := Prune(dir, pruneNow, nil); err != nil {
		t.Fatal(err)
	}
	if got := intents(t, dir); len(got) != 1 || got[0] != "a000000000000001:created" {
		t.Errorf("after pruning: %v; want the live create kept", got)
	}
}

// The pruning checks a line as the reader does (Check): a line the reader
// skips — a tool it does not know, a reason not of evo — closes nothing and
// is kept as it is, with the create it would have closed (Астра, #2297).
func TestPrune_ALineTheReaderSkipsClosesNothing(t *testing.T) {
	check := func(l Line) bool { return l.Tool == "disk_create" || l.Tool == "disk_delete" }
	dir := filepath.Join(t.TempDir(), DirName)
	odd := strings.Replace(lineOf("b000000000000002", Deleted, "disk", "d-1", "2026-07-01T11:00:00Z"), `"disk_create"`, `"future_delete"`, 1)
	writeDay(t, dir, "2026-07-01", lineOf("b000000000000001", Created, "disk", "d-1", "2026-07-01T10:00:00Z"), odd)
	if _, err := Prune(dir, pruneNow, check); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, LiveName))
	if !strings.Contains(string(data), "b000000000000001") || !strings.Contains(string(data), "future_delete") {
		t.Errorf("%s: %q; want the create and the line the reader skips, kept", LiveName, data)
	}
	j, _ := ReadAll(dir, check)
	if len(Live(j.Lines)) != 1 {
		t.Errorf("after pruning live %+v; want the create", Live(j.Lines))
	}
}

// liveTexts is what history finds live in dir, as the texts of the lines:
// ReadAll with check, every line by its time (handleHistory), then Live.
func liveTexts(t *testing.T, dir string, check Check) []string {
	t.Helper()
	j, err := ReadAll(dir, check)
	if err != nil {
		t.Fatal(err)
	}
	sort.SliceStable(j.Lines, func(a, b int) bool { return j.Lines[a].Time < j.Lines[b].Time })
	var out []string
	for _, l := range Live(j.Lines) {
		b, _ := json.Marshal(l)
		out = append(out, string(b))
	}
	sort.Strings(out)
	return out
}

// The two journals of «Риск», conclusion 324 revised (the property its
// random check broke): a line written twice, and the state a pruning killed
// after it kept a line leaves. The reader and the pruning read the same
// lines in the same order: the files of the days, then LiveName, each line
// once.
func TestPrune_AReaderAndThePruningReadTheSameLines(t *testing.T) {
	at := "2026-07-01T10:00:00Z"
	mk := func(outcome, resource string) string {
		return lineOf("c000000000000001", outcome, "disk", resource, at)
	}
	// (1) error, found without an id, the same error again: one line of
	// error, read once — the last line of the call is found, live.
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-07-01", mk(Error, ""), mk(Found, ""), mk(Error, ""))
	before := liveTexts(t, dir, nil)
	if _, err := Prune(dir, pruneNow, nil); err != nil {
		t.Fatal(err)
	}
	if after := liveTexts(t, dir, nil); len(before) != 1 || strings.Join(after, "") != strings.Join(before, "") {
		t.Errorf("a line twice: live before %v, after %v; want the found, kept", before, after)
	}
	// (2) a pruning killed after it kept found: LiveName holds it, the
	// file of the day error, found. history reads the day first: found is
	// last, live — before the next pruning and after it.
	dir = filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-07-01", mk(Error, ""), mk(Found, ""))
	if err := os.WriteFile(filepath.Join(dir, LiveName), []byte(mk(Found, "")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := liveTexts(t, dir, nil); len(got) != 1 {
		t.Errorf("after a killed pruning history finds live %v; want the found", got)
	}
	if _, err := Prune(dir, pruneNow, nil); err != nil {
		t.Fatal(err)
	}
	if got := liveTexts(t, dir, nil); len(got) != 1 {
		t.Errorf("after the next pruning live %v; want the found", got)
	}
}

// The invariant of the pruning on random journals («Риск», conclusion 324
// revised, У2): what is live as history finds it (liveTexts) is the same
// before and after Prune, and every line the reader does not take is kept
// byte for byte. The journals mix days within and past KeepDays, times out
// of the order of the file (a clock turned back) and equal seconds, several
// lines of one call, lines written twice, the state a killed pruning
// leaves (a copy of a line in LiveName), a ceiling the journal is over,
// lines of a tool the reader does not know, broken lines and lines of
// another format.
func TestPrune_KeepsWhatHistoryFindsLive(t *testing.T) {
	check := func(l Line) bool { return l.Tool != "future_delete" }
	was, wasKilled := ceiling, killed
	t.Cleanup(func() { ceiling, killed = was, wasKilled })
	days := []string{"2026-06-01", "2026-07-01", "2026-08-01", "2026-11-10", "2026-11-17"}
	outcomes := []string{Created, Created, Deleted, AlreadyDeleted, Found, Unknown, NotFound, Error}
	for seed := int64(1); seed <= 1000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		ceiling = Ceiling
		if rng.Intn(3) == 0 {
			ceiling = int64(200 + rng.Intn(2000))
		}
		dir := filepath.Join(t.TempDir(), DirName)
		var odd, all []string
		for _, day := range days {
			if rng.Intn(4) == 0 {
				continue
			}
			var lines []string
			for k := rng.Intn(8); k >= 0; k-- {
				intent := fmt.Sprintf("%016x", rng.Intn(6)+1)         // calls share ids: several lines of one
				at := fmt.Sprintf("%sT%02d:00:00Z", day, rng.Intn(3)) // few hours: equal seconds
				res := ""
				if rng.Intn(4) != 0 {
					res = fmt.Sprintf("r-%d", rng.Intn(3))
				}
				l := lineOf(intent, outcomes[rng.Intn(len(outcomes))], "disk", res, at)
				switch rng.Intn(12) {
				case 0:
					l = strings.Replace(l, `"disk_create"`, `"future_delete"`, 1)
					odd = append(odd, l)
				case 1:
					l = fmt.Sprintf(`{"format":1,"time"%d`, rng.Intn(1000))
					odd = append(odd, l)
				case 2:
					l = strings.Replace(l, `"format":1`, `"format":2`, 1)
					odd = append(odd, l)
				case 3:
					if len(lines) > 0 { // a line written twice
						l = lines[rng.Intn(len(lines))]
					}
				case 4: // a line of another day: not evo's (Астра, #2346, #2347)
					if other := days[rng.Intn(len(days))]; other != day {
						l = strings.Replace(l, `"time":"`+day, `"time":"`+other, 1)
						odd = append(odd, l)
					}
				}
				lines = append(lines, l)
			}
			all = append(all, lines...)
			writeDay(t, dir, day, lines...)
		}
		plain := len(odd) == 0 // lines evo writes, as it writes them
		// The state a killed pruning leaves: some lines copied to LiveName.
		if rng.Intn(3) == 0 && len(all) > 0 {
			plain = false
			var copied []string
			for k := rng.Intn(3); k >= 0; k-- {
				copied = append(copied, all[rng.Intn(len(all))])
			}
			if err := os.WriteFile(filepath.Join(dir, LiveName), []byte(strings.Join(copied, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// The mark a pruning writes before LiveName: the lines of the
			// days through it may stand there, the others may not.
			if err := os.WriteFile(filepath.Join(dir, retiredName), []byte(days[rng.Intn(len(days))]+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		before := liveTexts(t, dir, check)
		// A pruning, then one a hundred days later that retires every file
		// of a day, each killed after a write of its own once in two seeds
		// and run again: what is live and what the reader does not take
		// stay, between the two and after.
		for _, now := range []time.Time{pruneNow, pruneNow.AddDate(0, 0, 100)} {
			if rng.Intn(2) == 0 {
				writes := rng.Intn(4) + 1
				killed = func() bool { writes--; return writes == 0 }
				if _, err := Prune(dir, now, check); err != nil {
					t.Fatalf("seed %d: %v", seed, err)
				}
				killed = func() bool { return false }
				if after := liveTexts(t, dir, check); strings.Join(after, "\n") != strings.Join(before, "\n") {
					t.Fatalf("seed %d, killed pruning %s: live before %v\nafter %v", seed, now.Format(time.DateOnly), before, after)
				}
			}
			if _, err := Prune(dir, now, check); err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
			if after := liveTexts(t, dir, check); strings.Join(after, "\n") != strings.Join(before, "\n") {
				t.Fatalf("seed %d, pruned %s: live before %v\nafter %v", seed, now.Format(time.DateOnly), before, after)
			}
			var kept strings.Builder
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
				kept.Write(data)
			}
			for _, l := range odd {
				if !strings.Contains(kept.String(), l+"\n") {
					t.Fatalf("seed %d, pruned %s: the line the reader does not take is gone: %s", seed, now.Format(time.DateOnly), l)
				}
			}
		}
		// A journal of lines evo writes is pruned, not left as it is: a
		// hundred days later no file of a day stays.
		if days, _, _ := dayFiles(dir); plain && len(days) > 0 {
			t.Fatalf("seed %d: files of days %v stay", seed, days)
		}
	}
}

// A line in the file of a day not its own is not evo's — Append writes a
// line to the file of the day of its time — and is not taken: the reader
// skips and counts it, it closes nothing, and the pruning does not move it
// to LiveName — where its day is no more known — but keeps it in StrayName,
// which no reader takes lines of («Риск», conclusion 324, 324-3: a line of
// another day reordered what is live; #2297: what evo does not understand
// is kept).
func TestPrune_ALineInTheFileOfAnotherDayIsNotTaken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	made := lineOf("d100000000000001", Created, "disk", "d-1", "2026-07-01T10:00:00Z")
	stray := lineOf("d100000000000002", Deleted, "disk", "d-1", "2026-11-17T10:00:00Z") // a time of another day
	writeDay(t, dir, "2026-07-01", made, stray)
	read := func(when string) {
		t.Helper()
		j, err := ReadAll(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if j.Skipped != 1 || len(Live(j.Lines)) != 1 {
			t.Fatalf("%s: skipped %d, live %+v; want the stray line skipped, the create live", when, j.Skipped, Live(j.Lines))
		}
	}
	read("before pruning")
	if _, err := Prune(dir, pruneNow, nil); err != nil {
		t.Fatal(err)
	}
	live, _ := os.ReadFile(filepath.Join(dir, LiveName))
	strays, _ := os.ReadFile(filepath.Join(dir, StrayName))
	if !strings.Contains(string(live), made) || strings.Contains(string(live), stray) || string(strays) != stray+"\n" {
		t.Errorf("%s: %q, %s: %q; want the create live, the stray line kept apart", LiveName, live, StrayName, strays)
	}
	read("after pruning")
	// A pruning killed after it kept the stray line and before the file of
	// its day went: the line is in both, and read once.
	writeDay(t, dir, "2026-07-01", made, stray)
	read("after a killed pruning")
	// The next pruning keeps it once.
	if err := os.Remove(filepath.Join(dir, prunedName)); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(dir, pruneNow, nil); err != nil {
		t.Fatal(err)
	}
	if strays, _ := os.ReadFile(filepath.Join(dir, StrayName)); string(strays) != stray+"\n" {
		t.Errorf("after the next pruning %s: %q; want the stray line once", StrayName, strays)
	}
	read("after the next pruning")
}

// Астра, #2346: a call's create in its file and its unknown, of the same
// second, in the file of another day. The unknown is not evo's there and
// is not taken; the pruning keeps it, and what is live stays, after a
// pruning and after the next one retires the file it stood in.
func TestPrune_AnUnknownInTheFileOfAnotherDayChangesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	at := "2026-07-01T10:00:00Z"
	made := lineOf("a000000000000001", Created, "disk", "d-1", at)
	unknown := lineOf("a000000000000001", Unknown, "disk", "d-1", at)
	writeDay(t, dir, "2026-07-01", made)
	writeDay(t, dir, "2026-11-16", unknown)
	before := liveTexts(t, dir, nil)
	for _, now := range []time.Time{pruneNow, pruneNow.AddDate(0, 0, 100)} {
		if _, err := Prune(dir, now, nil); err != nil {
			t.Fatal(err)
		}
		if after := liveTexts(t, dir, nil); strings.Join(after, "\n") != strings.Join(before, "\n") {
			t.Errorf("pruned %s: live %v; want %v", now.Format(time.DateOnly), after, before)
		}
	}
	if strays, _ := os.ReadFile(filepath.Join(dir, StrayName)); !strings.Contains(string(strays), unknown+"\n") {
		t.Errorf("%s: %q; want the unknown kept", StrayName, strays)
	}
}

// Астра, #2347: a delete in the file of a day before its own and the
// create it closes in the file of a later day — both not evo's there. No
// line is taken before the pruning or after it: a closed create does not
// come back live.
func TestPrune_ACloseInTheFileOfAnotherDayBringsNothingBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	gone := lineOf("a000000000000002", Deleted, "disk", "d-1", "2026-07-02T10:00:00Z")
	writeDay(t, dir, "2026-07-01", gone)
	writeDay(t, dir, "2026-11-16", lineOf("a000000000000001", Created, "disk", "d-1", "2026-07-01T10:00:00Z"))
	before := liveTexts(t, dir, nil)
	if _, err := Prune(dir, pruneNow, nil); err != nil {
		t.Fatal(err)
	}
	if after := liveTexts(t, dir, nil); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("live before %v, after %v", before, after)
	}
	if strays, _ := os.ReadFile(filepath.Join(dir, StrayName)); !strings.Contains(string(strays), gone+"\n") {
		t.Errorf("%s: %q; want the delete kept", StrayName, strays)
	}
}

// LiveName holds the lines of the days retired: a line of a day after the
// mark is not taken there — it would close a line of a file kept. Taken in
// no file of a day, it may be evo's own (a mark gone): it is not moved, and
// the pruning is put off with LiveName named («Риск», conclusion 356, У1).
func TestPrune_ALineInLiveOfADayNotRetiredIsNotTaken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	at := "2026-11-17T02:00:00Z"
	unknown := lineOf("a000000000000004", Unknown, "disk", "", at)
	writeDay(t, dir, "2026-07-01", lineOf("a000000000000001", Deleted, "disk", "d-0", "2026-07-01T10:00:00Z"))
	writeDay(t, dir, "2026-11-17", unknown)
	closer := lineOf("a000000000000004", Deleted, "disk", "r-2", at)
	if err := os.WriteFile(filepath.Join(dir, LiveName), []byte(closer+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, retiredName), []byte("2026-06-01\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := liveTexts(t, dir, nil)
	if len(before) != 1 {
		t.Fatalf("live %v; want the unknown, the delete of another day not taken", before)
	}
	p, err := Prune(dir, pruneNow, nil)
	if want := []DeferredFile{{Name: LiveName, NotEvos: 1}}; err != nil || len(p.Retired) != 0 || !reflect.DeepEqual(p.Deferred, want) {
		t.Fatalf("pruned %+v, %v; want it put off with %+v", p, err, want)
	}
	if after := liveTexts(t, dir, nil); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("live before %v, after %v", before, after)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, LiveName)); string(data) != closer+"\n" {
		t.Errorf("%s: %q; want the delete left where it is", LiveName, data)
	}
}

// A pruning that would change what the reader finds live in a state a
// kill leaves — a delete in LiveName, which evo never writes there, closes
// a create of a file that goes after LiveName is written anew — is left
// undone: nothing is removed, the file in the way is named (Deferred,
// Describe) and the pruning tries again the next day; one that goes on
// clears the name («Риск» to 324-3: a pruning put off is said).
func TestPrune_LeftUndoneNamesTheFileInTheWay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	made := lineOf("a000000000000001", Created, "disk", "d-1", "2026-07-01T10:00:00Z")
	writeDay(t, dir, "2026-07-01", made)
	closer := lineOf("a000000000000002", Deleted, "disk", "d-1", "2026-07-15T10:00:00Z")
	unknown := lineOf("a000000000000003", Unknown, "disk", "", "2026-07-20T10:00:00Z") // live: a file not to remove
	if err := os.WriteFile(filepath.Join(dir, LiveName), []byte(closer+"\n"+unknown+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, retiredName), []byte("2026-08-01\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := liveTexts(t, dir, nil)
	p, err := Prune(dir, pruneNow, nil)
	if want := []DeferredFile{{Name: LiveName, NotEvos: 1, Live: 1}}; err != nil || len(p.Retired) != 0 || !reflect.DeepEqual(p.Deferred, want) {
		t.Fatalf("pruned %+v, %v; want nothing retired and %+v named", p, err, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-07-01.jsonl")); err != nil {
		t.Errorf("the file of the day is gone: %v", err)
	}
	if after := liveTexts(t, dir, nil); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("live before %v, after %v", before, after)
	}
	if s, err := Describe(dir, nil); err != nil || !reflect.DeepEqual(s.Deferred, p.Deferred) {
		t.Errorf("Describe: %+v, %v; want %+v named", s, err, p.Deferred)
	}
	// The next day, the file in the way gone: the pruning goes on, and the
	// name is cleared.
	if err := os.Remove(filepath.Join(dir, LiveName)); err != nil {
		t.Fatal(err)
	}
	p, err = Prune(dir, pruneNow.AddDate(0, 0, 1), nil)
	if err != nil || len(p.Retired) != 1 || len(p.Deferred) != 0 || len(DeferredFiles(dir)) != 0 {
		t.Errorf("the next day: %+v, %v, named %v; want the file retired and no name", p, err, DeferredFiles(dir))
	}
}

// A pruning reads the journal and checks its states without the lock: a
// line written meanwhile does not wait for it. Its writes go only if the
// journal is as it read it: a line written meanwhile puts them off to the
// next call, which prunes («Риск» to 324-3: a line of a create is not lost
// to the lock of a pruning).
func TestPrune_ALineWrittenMeanwhileWaitsForNoPruning(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-07-01", lineOf("a000000000000001", Deleted, "disk", "d-1", "2026-07-01T10:00:00Z"))
	writeDay(t, dir, "2026-11-17", lineOf("a000000000000002", Created, "disk", "d-2", "2026-11-17T09:00:00Z"))
	t.Cleanup(func() { planned = func() {} })
	made := Line{Time: "2026-11-17T10:00:00Z", Tool: "disk_create", Kind: "disk", ResourceID: "d-3", Outcome: Created, IntentID: "a000000000000003"}
	planned = func() {
		unlock, err := state.FileLock(filepath.Join(dir, DirName+".lock"), time.Millisecond)
		if err != nil {
			t.Fatalf("the lock is held while the pruning reads: %v", err)
		}
		unlock()
		if err := Append(dir, made); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := Prune(dir, pruneNow, nil); err != nil || len(p.Retired) != 0 {
		t.Fatalf("pruned %+v, %v; want nothing written: a line came meanwhile", p, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-07-01.jsonl")); err != nil {
		t.Fatalf("the file of the day is gone: %v", err)
	}
	planned = func() {}
	if p, err := Prune(dir, pruneNow, nil); err != nil || len(p.Retired) != 1 {
		t.Errorf("the next call: %+v, %v; want the old file retired", p, err)
	}
	if live := liveTexts(t, dir, nil); len(live) != 2 {
		t.Errorf("live %v; want the two creates", live)
	}
}

// A line written while the pruning holds the lock — after any of its
// writes — waits for the lock (lockWait: the lock is held for the writes
// only), and is kept: written, read live, not removed by the pruning («Риск»
// to 324-3: a line written at every step of a pruning).
func TestPrune_ALineWrittenUnderTheLockIsKept(t *testing.T) {
	for step := 1; step <= 4; step++ {
		dir := filepath.Join(t.TempDir(), DirName)
		writeDay(t, dir, "2026-06-01", lineOf("a000000000000001", Created, "disk", "d-1", "2026-06-01T10:00:00Z"))
		writeDay(t, dir, "2026-07-01", lineOf("a000000000000002", Unknown, "disk", "", "2026-07-01T10:00:00Z"))
		writeDay(t, dir, "2026-11-17", lineOf("a000000000000003", Created, "disk", "d-3", "2026-11-17T09:00:00Z"))
		made := Line{Time: "2026-11-17T11:00:00Z", Tool: "disk_create", Kind: "disk", ResourceID: "d-4", Outcome: Created, IntentID: "a000000000000004"}
		done := make(chan error, 1)
		writes := step
		killed = func() bool {
			if writes--; writes == 0 {
				go func() { done <- Append(dir, made) }()
			}
			return false
		}
		_, err := Prune(dir, pruneNow, nil)
		killed = func() bool { return false }
		if err != nil {
			t.Fatal(err)
		}
		if writes > 0 { // fewer writes than step: the line is written after
			go func() { done <- Append(dir, made) }()
		}
		if err := <-done; err != nil {
			t.Fatalf("after write %d: the line was not written: %v", step, err)
		}
		live := liveTexts(t, dir, nil)
		if len(live) != 4 || !strings.Contains(strings.Join(live, "\n"), `"resource_id":"d-4"`) {
			t.Errorf("after write %d: live %v; want the three and the line written", step, live)
		}
	}
}

// «Риск», conclusion 356, У1: a mark of the retired days gone — a folder
// copied in part, the small files of evo cleaned away — leaves the live
// lines of LiveName out of their place. They may be evo's own: the next
// pruning moves none of them, is put off, and names LiveName; the mark back,
// history finds them again.
func TestPrune_AMarkGoneMovesNoLiveLine(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-06-01", lineOf("a000000000000001", Created, "disk", "d-1", "2026-06-01T10:00:00Z"))
	writeDay(t, dir, "2026-11-17", lineOf("a000000000000002", Created, "disk", "d-2", "2026-11-17T09:00:00Z"))
	if p, err := Prune(dir, pruneNow, nil); err != nil || len(p.Retired) != 1 {
		t.Fatalf("pruned %+v, %v; want the old file retired", p, err)
	}
	if live := liveTexts(t, dir, nil); len(live) != 2 {
		t.Fatalf("live %v; want two", live)
	}
	mark, err := os.ReadFile(filepath.Join(dir, retiredName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, retiredName)); err != nil {
		t.Fatal(err)
	}
	p, err := Prune(dir, pruneNow.AddDate(0, 0, 1), nil)
	if want := []DeferredFile{{Name: LiveName, NotEvos: 1}}; err != nil || !reflect.DeepEqual(p.Deferred, want) {
		t.Errorf("the mark gone: %+v, %v; want it put off with %+v", p, err, want)
	}
	if strays, _ := os.ReadFile(filepath.Join(dir, StrayName)); len(strays) != 0 {
		t.Errorf("%s: %q; want no line moved", StrayName, strays)
	}
	if err := os.WriteFile(filepath.Join(dir, retiredName), mark, 0o600); err != nil {
		t.Fatal(err)
	}
	if live := liveTexts(t, dir, nil); len(live) != 2 {
		t.Errorf("the mark back: live %v; want two", live)
	}
}

// «Риск», conclusion 356, У2: a pruning that does not fit its limit of
// time is marked — it tries again the next day, not at each call — and
// doctor says so (Describe); one that goes on clears the mark.
func TestPrune_OneThatDoesNotFitTriesTheNextDay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), DirName)
	writeDay(t, dir, "2026-06-01", lineOf("a000000000000001", Deleted, "disk", "d-1", "2026-06-01T10:00:00Z"))
	writeDay(t, dir, "2026-11-17", lineOf("a000000000000002", Created, "disk", "d-2", "2026-11-17T09:00:00Z"))
	was := pruneLimit
	t.Cleanup(func() { pruneLimit = was })
	pruneLimit = 0
	if p, err := Prune(dir, pruneNow, nil); err != nil || !p.Unfinished || len(p.Retired) != 0 {
		t.Fatalf("pruned %+v, %v; want it unfinished", p, err)
	}
	if s, err := Describe(dir, nil); err != nil || !s.Unfinished {
		t.Errorf("Describe: %+v, %v; want it unfinished", s, err)
	}
	pruneLimit = was
	if p, err := Prune(dir, pruneNow, nil); err != nil || p.Unfinished || len(p.Retired) != 0 {
		t.Errorf("the same day: %+v, %v; want no second try", p, err)
	}
	if p, err := Prune(dir, pruneNow.AddDate(0, 0, 1), nil); err != nil || p.Unfinished || len(p.Retired) != 1 {
		t.Errorf("the next day: %+v, %v; want the old file retired", p, err)
	}
	if s, _ := Describe(dir, nil); s.Unfinished {
		t.Errorf("Describe after a pruning that went on: %+v; want the mark cleared", s)
	}
}
