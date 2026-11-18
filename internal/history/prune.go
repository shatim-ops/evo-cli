// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The bounds of the journal (the owner's decision of 17.11: 90 days,
// 64 MB; the ТЗ of the journal, §0, 9).
const (
	// KeepDays is how long a file of a day stays.
	KeepDays = 90
	// Ceiling is the size the journal is brought under, closed lines
	// first.
	Ceiling = 64 << 20
)

// ceiling is Ceiling; tests lower it.
var ceiling int64 = Ceiling

const (
	// LiveName is the file the live lines of the files retired go to:
	// read always, whatever their age.
	LiveName = "live.jsonl"
	// prunedName marks the day of the last pruning: once a day.
	prunedName = "pruned"
	// retiredName names the newest day whose file was retired: the
	// lines of that day and before are kept only if live.
	retiredName = "retired"
	// pruneLimit bounds a pruning: what does not fit goes on next time.
	pruneLimit = 2 * time.Second
)

// Pruned is what a pruning did and what the journal is after it.
type Pruned struct {
	// Retired are the files of days removed, their live lines kept.
	Retired []string
	// Live is how many live lines the journal holds.
	Live int
	// OverCeiling: the live lines alone are more than the ceiling — none
	// is removed; doctor and history say so.
	OverCeiling bool
}

// rawLine is a line of a file as it is, and what a reader makes of it.
type rawLine struct {
	text string
	line Line
	ok   bool
	// stray: a line read whole that does not stand in its file (ofFile).
	// It is not taken and not live: it goes with its file, never to
	// LiveName, where its day is no more known.
	stray bool
}

// Prune keeps the journal of dir in its bounds: a file of a day older than
// KeepDays goes, then the oldest ones while the journal is over the
// ceiling — today's never. What is live in a file that goes is kept in
// LiveName first: a resource made and not removed, an outcome not known,
// and any line this reader cannot read or does not know (a newer evo wrote
// it: not this one's to drop) — checked by check as the reader checks it
// (ReadAll), so a line history skips closes nothing here either (Астра,
// #2297). A line of another day in the file of a day (ofFile) is not live:
// it goes with its file. The live lines are written whole to a temp
// file and renamed, then the files go: a kill between the two leaves a
// line twice, which a reader reads once (ReadAll), never none.
//
// It runs at most once a day (a mark in the directory), under the lock of
// the journal and a limit of time: what does not fit goes on next time. A
// directory that is not there is nothing to prune.
func Prune(dir string, now time.Time, check Check) (Pruned, error) {
	var p Pruned
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return p, nil
	}
	unlock, err := state.FileLock(filepath.Join(dir, DirName+".lock"), lockWait)
	if err != nil {
		return p, err
	}
	defer unlock()
	today := now.UTC().Format(time.DateOnly)
	if mark, err := config.ReadFileNoFollow(filepath.Join(dir, prunedName)); err == nil && strings.TrimSpace(string(mark)) == today {
		return p, nil
	}
	deadline := time.Now().Add(pruneLimit) // wall-clock: the limit of one pruning, not a value of an answer

	days, sizes, err := dayFiles(dir)
	if err != nil {
		return p, err
	}
	var all []rawLine
	from := map[string][2]int{} // file → its lines in all
	for _, name := range readOrder(days) {
		data, err := config.ReadFileNoFollow(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return p, err
		}
		start := len(all)
		all = append(all, rawLines(name, data, check)...)
		from[name] = [2]int{start, len(all)}
		if time.Now().After(deadline) { // wall-clock: the limit of one pruning
			return p, nil
		}
	}
	live := liveOfRaw(all)

	// The files that go: too old, then the oldest while over the ceiling.
	cutoff := now.UTC().AddDate(0, 0, -KeepDays).Format(time.DateOnly)
	var total int64
	for _, n := range days {
		total += sizes[n]
	}
	if fi, err := os.Lstat(filepath.Join(dir, LiveName)); err == nil {
		total += fi.Size()
	}
	// A file that goes frees the bytes of its closed lines: its live
	// lines move to LiveName. Over the ceiling only a file with closed
	// lines goes — one of live lines alone frees nothing.
	closed := func(name string) (n int64) {
		r := from[name]
		for i := r[0]; i < r[1]; i++ {
			if !live[i] {
				n += int64(len(all[i].text) + 1)
			}
		}
		return n
	}
	retire := map[string]bool{}
	for _, n := range days {
		day := strings.TrimSuffix(n, ".jsonl")
		if free := closed(n); day < cutoff || (total > ceiling && day != today && free > 0) {
			retire[n] = true
			total -= free
		}
	}

	// The live lines of LiveName and of the files that go, once each.
	var kept bytes.Buffer
	seen := map[string]bool{}
	for _, name := range readOrder(days) {
		r, ok := from[name]
		if !ok || (name != LiveName && !retire[name]) {
			continue
		}
		for i := r[0]; i < r[1]; i++ {
			if live[i] && !seen[all[i].text] {
				seen[all[i].text] = true
				kept.WriteString(all[i].text)
				kept.WriteByte('\n')
			}
		}
	}
	var liveBytes int64
	counted := map[string]bool{}
	for i := range all {
		if live[i] && !counted[all[i].text] {
			counted[all[i].text] = true
			p.Live++
			liveBytes += int64(len(all[i].text) + 1)
		}
	}
	p.OverCeiling = liveBytes > ceiling

	if len(retire) > 0 || from[LiveName] != [2]int{} {
		if err := config.WriteFileAtomic(filepath.Join(dir, LiveName), kept.Bytes(), 0o600); err != nil {
			return p, err
		}
	}
	for _, n := range days {
		if !retire[n] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !os.IsNotExist(err) {
			return p, err
		}
		p.Retired = append(p.Retired, n)
	}
	if len(p.Retired) > 0 {
		through := strings.TrimSuffix(p.Retired[len(p.Retired)-1], ".jsonl")
		if was := RetiredThrough(dir); was > through {
			through = was
		}
		if err := config.WriteFileAtomic(filepath.Join(dir, retiredName), []byte(through+"\n"), 0o600); err != nil {
			return p, err
		}
	}
	return p, config.WriteFileAtomic(filepath.Join(dir, prunedName), []byte(today+"\n"), 0o600)
}

// dayFiles are the files of days of dir, oldest first, with their sizes:
// regular files of evo's name only.
func dayFiles(dir string) ([]string, map[string]int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	sizes := map[string]int64{}
	var names []string
	for _, e := range entries {
		if !e.Type().IsRegular() || !fileForm.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		names = append(names, e.Name())
		sizes[e.Name()] = info.Size()
	}
	sort.Strings(names)
	return names, sizes, nil
}

// rawLines are the lines of the file name, data, each with what a reader
// makes of it; an empty line is none. A line too long for a reader is one
// line it does not read — kept — and the lines after it are read as
// ReadAll reads them (lines).
func rawLines(name string, data []byte, check Check) []rawLine {
	var out []rawLine
	for _, raw := range lines(data) {
		r := rawLine{text: string(raw)}
		// One reading of a line for the reader and the pruning (parse,
		// fileCheck): what history skips, the pruning keeps as it is — but
		// a line of another day, which no evo writes there.
		r.line, r.ok = parse(raw)
		r.stray = r.ok && !ofFile(name, r.line)
		r.ok = r.ok && fileCheck(name, check)(r.line)
		out = append(out, r)
	}
	return out
}

// liveOfRaw is the live mask of raw lines: Live of the lines a reader
// reads, and every line it does not — kept, never dropped — but a stray one.
func liveOfRaw(all []rawLine) []bool {
	// Each line once, where the reader first meets it (readOrder): a copy
	// of a line is the line, and is as live as it is.
	var lines []Line
	first := map[string]int{} // text → index in lines
	for _, r := range all {
		if _, dup := first[r.text]; r.ok && !dup {
			first[r.text] = len(lines)
			lines = append(lines, r.line)
		}
	}
	mask := liveMask(lines)
	live := make([]bool, len(all))
	for i, r := range all {
		live[i] = (!r.ok && !r.stray) || (r.ok && mask[first[r.text]])
	}
	return live
}

// RetiredThrough is the newest day whose file a pruning retired
// (2026-08-18): the lines of that day and before are kept only if live.
// "" when none was.
func RetiredThrough(dir string) string {
	data, err := config.ReadFileNoFollow(filepath.Join(dir, retiredName))
	if err != nil {
		return ""
	}
	day := strings.TrimSpace(string(data))
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return ""
	}
	return day
}

// Summary is what doctor says of a journal: its size, its oldest day, how
// many lines are live, and whether the live ones alone are over the
// ceiling.
type Summary struct {
	Bytes       int64
	Oldest      string
	Live        int
	OverCeiling bool
}

// Describe reads the journal of dir for doctor, its lines checked by check
// as Prune checks them; a journal that is not there is an empty one.
func Describe(dir string, check Check) (Summary, error) {
	var s Summary
	days, sizes, err := dayFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	for _, n := range days {
		s.Bytes += sizes[n]
	}
	if len(days) > 0 {
		s.Oldest = strings.TrimSuffix(days[0], ".jsonl")
	}
	var all []rawLine
	for _, name := range readOrder(days) {
		data, err := config.ReadFileNoFollow(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if name == LiveName {
			s.Bytes += int64(len(data))
		}
		all = append(all, rawLines(name, data, check)...)
	}
	var liveBytes int64
	seen := map[string]bool{}
	for i, live := range liveOfRaw(all) {
		if live && !seen[all[i].text] {
			seen[all[i].text] = true
			s.Live++
			liveBytes += int64(len(all[i].text) + 1)
		}
	}
	s.OverCeiling = liveBytes > ceiling
	return s, nil
}
